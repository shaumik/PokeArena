package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/config"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
	"pokearena/internal/mq"
	"pokearena/internal/protocol"
	"pokearena/internal/store"
)

// Server is the gateway HTTP/WebSocket service.
type Server struct {
	cfg        config.Config
	dex        *domain.Dex
	store      *store.Store
	cache      *cache.Cache
	broker     *mq.Broker
	hub        *Hub
	webDir     string
	fallbackAI *ai.HeuristicAgent // local AI used if the ai-service is unreachable
	aiTeams    *ai.TeamPool       // curated AI rosters for mode=live picker auto-submit

	// Per-battle live coordinators. Rooms are created eagerly at POST so the
	// picker deadline starts at create-time; the entry is deleted when the
	// coordinator's run loop exits (via livebattle.Deps.OnDone).
	matchesMu sync.Mutex
	matches   map[string]*gwMatch
}

// NewServer wires the gateway dependencies.
func NewServer(cfg config.Config, dex *domain.Dex, st *store.Store, c *cache.Cache, b *mq.Broker, hub *Hub, aiTeams *ai.TeamPool, webDir string) *Server {
	return &Server{
		cfg: cfg, dex: dex, store: st, cache: c, broker: b, hub: hub,
		webDir:     webDir,
		fallbackAI: ai.NewHeuristicAgent(dex),
		aiTeams:    aiTeams,
		matches:    map[string]*gwMatch{},
	}
}

// Routes builds the HTTP handler.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/healthz", s.handleHealth)
		r.Get("/pokemon", s.handlePokemon)
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

	// live_pvp defers engine-state construction to the picker room: both
	// sides submit_team over the WS, the Room validates, and only then is
	// NewBattleFromPicks called. live still builds the state here because
	// its AI side has no team-submission surface yet.
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
		b.Status = "open" // picker phase; flipped to "running" by the Room on transition
		b.P1Team = nil    // teams arrive via submit_team, not the create body
		b.P2Team = nil
		if err := s.store.CreateBattle(ctx, b); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to create battle")
			return
		}
		// Eager Room creation so the 300s picker deadline starts at POST,
		// not at first WS attach. An abandoned URL therefore dies in
		// bounded time without needing a separate reaper.
		s.startPvPRoom(battleID, p1Name, p2Name, seed)
		writeJSON(w, http.StatusCreated, map[string]any{
			"battle_id": battleID, "mode": "live_pvp",
			"p1_url": protocol.PlayPath(battleID, string(cache.SlotP1), p1Token),
			"p2_url": protocol.PlayPath(battleID, string(cache.SlotP2), p2Token),
		})
		return
	}

	// live: one WS slot (human) + one AI slot, sharing the same Room
	// machinery as live_pvp. The AI's team is pre-picked here from the
	// curated pool — seeded by the battle's seed so the same battle ID
	// always faces the same opponent. The human submits their team over
	// the WS during the picker phase.
	_ = req.P1Team // ignored: live mode now uses picker, not in-band teams
	_ = req.P2Team
	b.P1Team = nil
	b.P2Team = nil
	b.Status = "open"
	if err := s.store.CreateBattle(ctx, b); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create battle")
		return
	}
	if err := s.startLiveRoom(battleID, p1Name, p2Name, seed); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to start live room: "+err.Error())
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

// publishLiveEvent fans a gateway-generated event out two ways: in-process
// via the Hub (microseconds, no broker round-trip) and via Rabbit for any
// cross-process consumer. The EventQueue's AppId filter drops the Rabbit
// loopback so local Hub subscribers don't see the event twice.
//
// All gateway-side event emission goes through here. Events produced in
// other services (battle-worker, ai-service) reach this gateway's Hub via
// the Rabbit consumer in hub.go::Run, unchanged.
func (s *Server) publishLiveEvent(ctx context.Context, eventType, battleID string, msg any) {
	body, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.hub.Inject(eventType, battleID, body)
	_ = s.broker.PublishEvent(ctx, eventType, battleID, msg)
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
