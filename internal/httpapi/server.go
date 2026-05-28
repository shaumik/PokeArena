package httpapi

import (
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

	// Per-battle pvp coordinators. Created lazily by the first WS handler
	// to claim a slot, removed when the coordinator's run loop exits.
	// Only live_pvp battles populate this map; legacy live battles don't.
	matchesMu sync.Mutex
	matches   map[string]*pvpMatch
}

// NewServer wires the gateway dependencies.
func NewServer(cfg config.Config, dex *domain.Dex, st *store.Store, c *cache.Cache, b *mq.Broker, hub *Hub, webDir string) *Server {
	return &Server{
		cfg: cfg, dex: dex, store: st, cache: c, broker: b, hub: hub,
		webDir:     webDir,
		fallbackAI: ai.NewHeuristicAgent(dex),
		matches:    map[string]*pvpMatch{},
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
	DexNo int           `json:"dex_no"`
	Name  string        `json:"name"`
	Type1 string        `json:"type1"`
	Type2 string        `json:"type2"`
	Base  domain.Stats  `json:"base"`
	Moves []domain.Move `json:"moves"`
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
			Base:  sp.Base, Moves: moves,
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
	Mode         string `json:"mode"`
	P1Name       string `json:"p1_name"`
	P2Name       string `json:"p2_name"`
	P1Team       []int  `json:"p1_team"`
	P2Team       []int  `json:"p2_team"`
	P1Difficulty string `json:"p1_difficulty"`
	P2Difficulty string `json:"p2_difficulty"`
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
	// Teams are validated at create-time only for modes where the engine
	// state is built here (quicksim, live). For live_pvp the picker room
	// owns team submission via submit_team; team fields in the create
	// body are ignored for that mode.
	if req.Mode != "live_pvp" {
		if err := s.validateTeam(req.P1Team); err != nil {
			writeErr(w, http.StatusBadRequest, "p1 team: "+err.Error())
			return
		}
		if err := s.validateTeam(req.P2Team); err != nil {
			writeErr(w, http.StatusBadRequest, "p2 team: "+err.Error())
			return
		}
	}

	// live_pvp has no AI on either side, so difficulty fields are nonsensical
	// — reject them explicitly rather than silently ignoring so the operator
	// learns the contract immediately. (We could default them, but defaulting
	// fields that have no effect is a footgun.)
	if req.Mode == "live_pvp" && (req.P1Difficulty != "" || req.P2Difficulty != "") {
		writeErr(w, http.StatusBadRequest, "live_pvp battles do not accept difficulty fields (both sides are human/external)")
		return
	}

	p1Name := orDefault(req.P1Name, "Trainer Red")
	p2Name := orDefault(req.P2Name, ternary(req.Mode == "live", "AI", "Trainer Blue"))

	// Difficulty is only meaningful when at least one side is the internal AI
	// — that's quicksim and live, never live_pvp.
	var p1Diff, p2Diff string
	if req.Mode != "live_pvp" {
		p1Diff = orDefault(req.P1Difficulty, "hard")
		p2Diff = orDefault(req.P2Difficulty, ternary(req.Mode == "live", s.cfg.AIDifficulty, "easy"))
		if err := s.validateRequestDifficulty(p1Diff, req.Mode); err != nil {
			writeErr(w, http.StatusBadRequest, "p1_difficulty: "+err.Error())
			return
		}
		if err := s.validateRequestDifficulty(p2Diff, req.Mode); err != nil {
			writeErr(w, http.StatusBadRequest, "p2_difficulty: "+err.Error())
			return
		}
	}

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
		b.AIDifficulty = p1Diff + "/" + p2Diff
		if err := s.store.CreateBattle(ctx, b); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to create battle")
			return
		}
		job := messages.QuickSimJob{
			BattleID: battleID, Seed: seed,
			P1Name: p1Name, P2Name: p2Name,
			P1Team: req.P1Team, P2Team: req.P2Team,
			P1Difficulty: p1Diff, P2Difficulty: p2Diff,
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
		b.Status = "open"      // picker phase; flipped to "running" by the Room on transition
		b.P1Team = nil         // teams arrive via submit_team, not the create body
		b.P2Team = nil
		b.AIDifficulty = ""    // pvp has no internal AI on either side
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

	// live: build the engine state up front; the AI on slot 2 has no
	// submission step.
	st, err := engine.NewBattle(s.dex, battleID, p1Name, req.P1Team, p2Name, req.P2Team, seed)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cache.SaveState(ctx, st); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to initialize battle")
		return
	}
	b.Status = "running"
	b.AIDifficulty = p2Diff
	if err := s.store.CreateBattle(ctx, b); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create battle")
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

// validateRequestDifficulty rejects unknown difficulty values at intake so
// the user gets a 400 with an actionable message instead of a battle row
// that creates and immediately marks itself failed.
func (s *Server) validateRequestDifficulty(d, _ string) error {
	return ai.ValidateDifficulty(d)
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
