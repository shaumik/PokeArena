package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sort"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/config"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
	"pokearena/internal/mq"
	"pokearena/internal/protocol"
	"pokearena/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Server is the gateway HTTP/WebSocket service. It owns no live battle state and
// runs no game logic: live battles are coordinated by the battle-session tier,
// and the gateway is a pure WebSocket↔broker bridge for them.
type Server struct {
	cfg     config.Config
	dex     *domain.Dex
	store   *store.Store
	cache   *cache.Cache
	broker  *mq.Broker
	hub     *Hub
	webDir  string
	aiTeams *ai.TeamPool // curated AI rosters for mode=live, sent in the session job
}

// NewServer wires the gateway dependencies.
func NewServer(cfg config.Config, dex *domain.Dex, st *store.Store, c *cache.Cache, b *mq.Broker, hub *Hub, aiTeams *ai.TeamPool, webDir string) *Server {
	return &Server{
		cfg: cfg, dex: dex, store: st, cache: c, broker: b, hub: hub,
		webDir:  webDir,
		aiTeams: aiTeams,
	}
}

// Routes builds the HTTP handler.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/healthz", s.handleHealth)
		r.Get("/pokemon", s.handlePokemon)
		r.Get("/items", s.handleItems)
		r.Get("/natures", s.handleNatures)
		r.Get("/rules", s.handleRules)
		r.Get("/leaderboard", s.handleLeaderboard)
		r.Post("/battles", s.handleCreateBattle)
		r.Get("/battles", s.handleListBattles)
		r.Get("/battles/{id}", s.handleGetBattle)
		r.Get("/battles/{id}/stream", s.handleSSE)
		r.Get("/battles/{id}/play", s.handleWS)
	})

	r.Handle("/*", noCache(http.FileServer(http.Dir(s.webDir))))
	return r
}

// noCache forces browsers to revalidate static assets on every request. Without
// this, http.FileServer only sends Last-Modified, and the browser's heuristic
// cache may skip even the If-Modified-Since check — meaning a rebuilt UI
// silently serves the old assets until the user hard-refreshes. Since the SPA
// is small and the gateway is the only origin, the bandwidth cost of
// "always send 304-or-200" is negligible; the cost of stale assets confusing
// the user (and the developer who just shipped a fix) is not.
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		h.ServeHTTP(w, r)
	})
}

// --- REST handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status, code := "ok", http.StatusOK
	if err := s.store.Ping(ctx); err != nil {
		status, code = "degraded", http.StatusServiceUnavailable
	}
	if err := s.cache.Ping(ctx); err != nil {
		status, code = "degraded", http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]string{"status": status, "data_version": s.cfg.DataVersion})
}

// pokedexEntry is the API DTO for one Pokédex row: a species with its full
// moves expanded inline. Built fresh from the in-memory dex on each request
// — the dataset is reference data, not transactional, so JSON is the system
// of record and Postgres is not consulted for this endpoint.
type pokedexEntry struct {
	DexNo     int           `json:"dex_no"`
	Name      string        `json:"name"`
	Type1     string        `json:"type1"`
	Type2     string        `json:"type2"`
	Base      domain.Stats  `json:"base"`
	Abilities []string      `json:"abilities,omitempty"`
	Moves     []domain.Move `json:"moves"`
}

func (s *Server) handlePokemon(w http.ResponseWriter, _ *http.Request) {
	species := s.dex.AllSpecies()
	out := make([]pokedexEntry, 0, len(species))
	for _, sp := range species {
		moves := make([]domain.Move, 0, len(sp.Moves))
		for _, mid := range sp.Moves {
			if m, ok := s.dex.Moves[mid]; ok {
				moves = append(moves, m)
			}
		}
		out = append(out, pokedexEntry{
			DexNo: sp.DexNo, Name: sp.Name,
			Type1: string(sp.Type1), Type2: string(sp.Type2),
			Base:      sp.Base,
			Abilities: sp.Abilities,
			Moves:     moves,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleItems serves the curated held-item catalog: every legal value for a
// TeamPick's optional `item` field, with the display name and a one-line
// description of what the engine does with it. Same "reference data, built
// fresh from memory" shape as handlePokemon — the descriptions come from the
// engine's item registry, so the endpoint can't advertise an item the engine
// doesn't honor.
func (s *Server) handleItems(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, engine.ItemCatalog(s.dex))
}

// handleNatures serves the 25-nature table: every legal value for a
// TeamPick's optional `nature` field. A faithful projection of the dataset —
// plus/minus are Stats keys, and the five neutral natures carry neither, so a
// client renders "no effect" from absence rather than from a name list.
// Sorted by id for a stable UI order.
func (s *Server) handleNatures(w http.ResponseWriter, _ *http.Request) {
	out := make([]domain.Nature, 0, len(s.dex.Natures))
	for _, n := range s.dex.Natures {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, out)
}

// formatRules is the numeric side of the ruleset: the constants a client needs
// to build a legal team and preview what it will do. Served rather than
// duplicated client-side because these are the engine's constants — a builder
// that hardcodes 510 keeps rendering a valid budget after the engine's changes.
type formatRules struct {
	Level        int `json:"level"`
	TeamSize     int `json:"team_size"`
	MovesMin     int `json:"moves_min"`
	MovesMax     int `json:"moves_max"`
	EVMaxPerStat int `json:"ev_max_per_stat"`
	EVMaxTotal   int `json:"ev_max_total"`
	IVMax        int `json:"iv_max"`
}

func (s *Server) handleRules(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, formatRules{
		Level:        engine.Level,
		TeamSize:     engine.TeamSize,
		MovesMin:     engine.MovesMin,
		MovesMax:     engine.MovesMax,
		EVMaxPerStat: engine.MaxEVPerStat,
		EVMaxTotal:   engine.MaxEVTotal,
		IVMax:        engine.MaxIV,
	})
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if top, err := s.cache.TopRatings(ctx, 25); err == nil && len(top) > 0 {
		writeJSON(w, http.StatusOK, top)
		return
	}
	// Cold cache: fall back to the system of record.
	rows, err := s.store.Leaderboard(ctx, 25)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "leaderboard unavailable")
		return
	}
	out := make([]cache.RankEntry, 0, len(rows))
	for _, e := range rows {
		out = append(out, cache.RankEntry{Name: e.Name, Rating: e.Rating})
	}
	writeJSON(w, http.StatusOK, out)
}

type createBattleReq struct {
	Mode   string `json:"mode"`
	P1Name string `json:"p1_name"`
	P2Name string `json:"p2_name"`
	P1Team []int  `json:"p1_team"`
	P2Team []int  `json:"p2_team"`
	// Optional full picks for quicksim: per-Pokémon movesets and abilities.
	// When present they override the bare dex lineups; the dex arrays still
	// form the persisted battle record. Ignored for live / live_pvp (those
	// draft in the picker room).
	P1Picks []engine.TeamPick `json:"p1_picks"`
	P2Picks []engine.TeamPick `json:"p2_picks"`
}

func (s *Server) handleCreateBattle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req createBattleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Mode != "quicksim" && req.Mode != "live" && req.Mode != "live_pvp" {
		writeErr(w, http.StatusBadRequest, "mode must be 'quicksim', 'live', or 'live_pvp'")
		return
	}
	// Teams are validated at create-time only for quicksim — the only
	// mode where the engine state is still built here. live and live_pvp
	// both defer team submission to the picker room (submit_team over
	// the WS), so team fields in the create body are ignored.
	if req.Mode == "quicksim" {
		if err := s.validateTeam(req.P1Team); err != nil {
			writeErr(w, http.StatusBadRequest, "p1 team: "+err.Error())
			return
		}
		if err := s.validateTeam(req.P2Team); err != nil {
			writeErr(w, http.StatusBadRequest, "p2 team: "+err.Error())
			return
		}
		// When custom picks are supplied, hold them to the same rules the
		// picker room enforces (legal moves, ≤4, valid abilities) so the
		// worker never receives an unbuildable team.
		if len(req.P1Picks) > 0 {
			if err := engine.ValidateTeam(req.P1Picks, s.dex); err != nil {
				writeErr(w, http.StatusBadRequest, "p1 picks: "+err.Error())
				return
			}
		}
		if len(req.P2Picks) > 0 {
			if err := engine.ValidateTeam(req.P2Picks, s.dex); err != nil {
				writeErr(w, http.StatusBadRequest, "p2 picks: "+err.Error())
				return
			}
		}
	}

	p1Name := orDefault(req.P1Name, "Trainer Red")
	p2Name := orDefault(req.P2Name, ternary(req.Mode == "live", "AI", "Trainer Blue"))

	seed := rand.Uint64()
	battleID := uuid.NewString()

	t1, err := s.store.UpsertTrainer(ctx, p1Name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to register trainer")
		return
	}
	t2, err := s.store.UpsertTrainer(ctx, p2Name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to register trainer")
		return
	}

	b := store.Battle{
		ID: battleID, Mode: req.Mode, Seed: int64(seed),
		P1Trainer: t1, P2Trainer: t2, P1Name: p1Name, P2Name: p2Name,
		P1Team: req.P1Team, P2Team: req.P2Team, Winner: -1,
	}

	if req.Mode == "quicksim" {
		b.Status = "pending"
		if err := s.store.CreateBattle(ctx, b); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to create battle")
			return
		}
		job := messages.QuickSimJob{
			BattleID: battleID, Seed: seed,
			P1Name: p1Name, P2Name: p2Name,
			P1Team: req.P1Team, P2Team: req.P2Team,
			P1Picks: req.P1Picks, P2Picks: req.P2Picks,
		}
		if err := s.broker.PublishJob(ctx, messages.QueueQuickSim, job); err != nil {
			writeErr(w, http.StatusServiceUnavailable, "battle queue unavailable")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"battle_id": battleID, "mode": "quicksim"})
		return
	}

	// live_pvp defers engine-state construction to the picker room: both sides
	// submit_team over the WS, the battle-session validates, and only then is
	// NewBattleFromPicks called. The gateway publishes a session-start job; one
	// battle-session instance is elected owner and runs the coordinator.
	if req.Mode == "live_pvp" {
		p1Token, err := cache.GenerateToken()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to mint join token")
			return
		}
		p2Token, err := cache.GenerateToken()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to mint join token")
			return
		}
		if err := s.cache.SavePvPTokens(ctx, battleID, p1Token, p2Token); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to store join tokens")
			return
		}
		b.Status = "open" // picker phase; flipped to "running" by the session on transition
		b.P1Team = nil    // teams arrive via submit_team, not the create body
		b.P2Team = nil
		if err := s.store.CreateBattle(ctx, b); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to create battle")
			return
		}
		// Publish the session-start job eagerly at POST so the picker deadline
		// (owned by the session) starts near create-time, not at first attach.
		if err := s.broker.PublishLiveSession(ctx, messages.LiveSessionStart{
			BattleID: battleID, Mode: "live_pvp", Seed: seed,
			P1Name: p1Name, P2Name: p2Name,
			Kinds: [2]string{"ws", "ws"},
		}); err != nil {
			writeErr(w, http.StatusServiceUnavailable, "battle session queue unavailable")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"battle_id": battleID, "mode": "live_pvp",
			// No name in the issued URLs: the slot's name is the joiner's to
			// declare at connect time, not the creator's to assign in advance.
			"p1_url": protocol.PlayPath(battleID, string(cache.SlotP1), p1Token, ""),
			"p2_url": protocol.PlayPath(battleID, string(cache.SlotP2), p2Token, ""),
		})
		return
	}

	// live: one WS slot (human) + one AI slot. The AI's team is pre-picked here
	// from the curated pool — seeded by the battle's seed so the same battle ID
	// always faces the same opponent — and carried in the session job. The human
	// submits their team over the WS during the picker phase.
	_ = req.P1Team // ignored: live mode now uses picker, not in-band teams
	_ = req.P2Team
	b.P1Team = nil
	b.P2Team = nil
	b.Status = "open"
	aiTeam, err := s.pickAITeam(seed)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "no AI team available: "+err.Error())
		return
	}
	if err := s.store.CreateBattle(ctx, b); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create battle")
		return
	}
	if err := s.broker.PublishLiveSession(ctx, messages.LiveSessionStart{
		BattleID: battleID, Mode: "live", Seed: seed,
		P1Name: p1Name, P2Name: p2Name,
		Kinds: [2]string{"ws", "ai"}, AITeam: aiTeam,
	}); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "battle session queue unavailable")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"battle_id": battleID, "mode": "live",
		"ws_url": "/api/battles/" + battleID + "/play",
	})
}

func (s *Server) handleListBattles(w http.ResponseWriter, r *http.Request) {
	battles, err := s.store.ListBattles(r.Context(), 30)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to list battles")
		return
	}
	writeJSON(w, http.StatusOK, battles)
}

func (s *Server) handleGetBattle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	battle, err := s.store.GetBattle(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "battle not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load battle")
		return
	}
	turns, err := s.store.GetTurns(ctx, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load turns")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"battle": battle, "turns": turns})
}

// --- helpers ---

func (s *Server) validateTeam(team []int) error {
	if len(team) < 1 || len(team) > 6 {
		return errors.New("must have 1 to 6 Pokémon")
	}
	for _, dex := range team {
		if _, ok := s.dex.Species[dex]; !ok {
			return fmt.Errorf("unknown Pokédex number %d", dex)
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
