package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

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
}

// NewServer wires the gateway dependencies.
func NewServer(cfg config.Config, dex *domain.Dex, st *store.Store, c *cache.Cache, b *mq.Broker, hub *Hub, webDir string) *Server {
	return &Server{
		cfg: cfg, dex: dex, store: st, cache: c, broker: b, hub: hub,
		webDir:     webDir,
		fallbackAI: ai.NewHeuristicAgent(dex),
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

// handlePokemon serves the Pokédex with a Redis read-through cache keyed by
// data version.
func (s *Server) handlePokemon(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := "pokedex:" + s.cfg.DataVersion

	var list []store.PokedexEntry
	if err := s.cache.GetJSON(ctx, key, &list); err == nil {
		writeJSON(w, http.StatusOK, list)
		return
	}
	list, err := s.store.ListPokedex(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load Pokédex")
		return
	}
	_ = s.cache.SetJSON(ctx, key, list, time.Hour)
	writeJSON(w, http.StatusOK, list)
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
	if err := s.validateTeam(req.P1Team); err != nil {
		writeErr(w, http.StatusBadRequest, "p1 team: "+err.Error())
		return
	}
	if err := s.validateTeam(req.P2Team); err != nil {
		writeErr(w, http.StatusBadRequest, "p2 team: "+err.Error())
		return
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

	// live and live_pvp both initialize the engine state in Redis the same
	// way — the difference is who drives each slot at play time, which is a
	// WS-handler concern, not a creation concern.
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

	if req.Mode == "live_pvp" {
		// Mint one join token per slot, persist alongside the state with the
		// same TTL, return the two claim URLs. Tokens are the only capability
		// that gates slot ownership — battle_id alone gets you nothing.
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
		b.AIDifficulty = "" // pvp has no internal AI on either side
		if err := s.store.CreateBattle(ctx, b); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to create battle")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"battle_id": battleID, "mode": "live_pvp",
			"p1_url": playURL(battleID, cache.SlotP1, p1Token),
			"p2_url": playURL(battleID, cache.SlotP2, p2Token),
		})
		return
	}

	// live: server-driven AI on slot p2.
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

// playURL builds the WebSocket join URL for one slot. Centralized so the
// shape stays consistent between gateway-issued URLs and any client that
// constructs them (e.g. the MCP server building its connect URL from a
// battle_id + token pair).
func playURL(battleID string, slot cache.PvPSlot, token string) string {
	return "/api/battles/" + battleID + "/play?slot=" + string(slot) + "&token=" + token
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

// validateRequestDifficulty enforces both the agent-level validity rule
// (delegated to ai.ValidateDifficulty — unknown values, missing LLM key) and
// the deployment-topology policy that quicksim cannot serve "nightmare"
// because batch workers do not hold the API key by design. Rejecting at
// intake is the right layer: the user gets a 400 with an actionable message
// instead of a battle row that creates and immediately marks itself failed.
func (s *Server) validateRequestDifficulty(d, mode string) error {
	if d == "nightmare" && mode == "quicksim" {
		return errors.New("nightmare is not available in quicksim (batch workers do not run the LLM agent)")
	}
	if err := ai.ValidateDifficulty(d, s.cfg.AnthropicKey); err != nil {
		if errors.Is(err, ai.ErrLLMKeyMissing) {
			return errors.New("nightmare is not enabled on this deployment (ANTHROPIC_API_KEY not set)")
		}
		return err // covers ErrUnknownDifficulty with its %q-quoted value
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
