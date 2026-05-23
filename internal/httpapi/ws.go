package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
)

const (
	humanSide   = 0
	aiSide      = 1
	turnTimeout = 90 * time.Second // safety net: auto-resolve an idle turn
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true }, // demo: same-origin SPA
}

type wsClientMsg struct {
	Type  string `json:"type"`  // "action"
	Kind  string `json:"kind"`  // "move" | "switch"
	Index int    `json:"index"` // move slot, or team index for a switch
}

// handleWS is the dispatcher for /api/battles/{id}/play. The URL shape is
// the same for single-player live mode and live_pvp; the presence of a slot
// query param is what distinguishes them. Keeping one route and routing
// here (rather than two routes) keeps the route table flat and lets future
// trainer-shaped clients use the same endpoint.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("slot") != "" {
		s.handlePvPWS(w, r)
		return
	}
	s.handleLiveWS(w, r)
}

// handleLiveWS coordinates a single-player live battle. The gateway owns
// the loop: it runs the (microsecond) engine inline and offloads only the
// AI's decision — the expensive, variable-latency part — to the ai-service
// over the queue.
func (s *Server) handleLiveWS(w http.ResponseWriter, r *http.Request) {
	battleID := chi.URLParam(r, "id")
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	st, err := s.cache.LoadState(ctx, battleID)
	if err != nil {
		writeWS(conn, errMsg("battle not found or expired"))
		return
	}
	difficulty := s.cfg.AIDifficulty
	if b, err := s.store.GetBattle(ctx, battleID); err == nil && b.AIDifficulty != "" {
		difficulty = b.AIDifficulty
	}

	subID, events, err := s.hub.Subscribe(battleID)
	if err != nil {
		writeWS(conn, errMsg("could not subscribe to battle events"))
		return
	}
	defer s.hub.Unsubscribe(battleID, subID)

	// A reader goroutine turns blocking WebSocket reads into channel sends.
	clientMsgs := make(chan wsClientMsg, 8)
	go func() {
		defer cancel()
		conn.SetReadLimit(4096)
		for {
			var m wsClientMsg
			if err := conn.ReadJSON(&m); err != nil {
				close(clientMsgs)
				return
			}
			select {
			case clientMsgs <- m:
			case <-ctx.Done():
				return
			}
		}
	}()

	writeWS(conn, map[string]any{"type": "state", "state": st})

	for ctx.Err() == nil && !st.Ended() {
		// Loop invariant: st.Phase is PhaseChoosing here.
		actions, ok := s.collectTurnActions(ctx, conn, st, battleID, difficulty, clientMsgs, events)
		if !ok {
			return
		}
		turnLog := engine.ResolveTurn(s.dex, st, actions)
		writeWS(conn, turnMsg(turnLog, st))

		if st.Phase == engine.PhaseReplace {
			sw, ok := s.collectReplaceActions(ctx, conn, st, clientMsgs)
			if !ok {
				return
			}
			replaceLog := engine.ResolveReplace(st, sw)
			turnLog = append(turnLog, replaceLog...)
			writeWS(conn, turnMsg(replaceLog, st))
		}
		s.persistLiveTurn(st, turnLog)
	}

	if st.Ended() {
		s.finishLiveBattle(st)
		writeWS(conn, map[string]any{"type": "end", "winner": st.Winner, "state": st})
	}
}

// collectTurnActions gathers both sides' actions for a choosing turn: the
// human's over the WebSocket, the AI's via the ai-service. The turn timer is
// a safety net — if either side stalls, an action is filled in locally.
func (s *Server) collectTurnActions(ctx context.Context, conn *websocket.Conn, st *engine.BattleState,
	battleID, difficulty string, clientMsgs <-chan wsClientMsg, events <-chan Event) ([2]engine.Action, bool) {

	var actions [2]engine.Action
	jobID := s.publishAIJob(ctx, battleID, st.Turn, aiSide, difficulty)
	gotHuman, gotAI := false, false

	timer := time.NewTimer(turnTimeout)
	defer timer.Stop()

	for !gotHuman || !gotAI {
		select {
		case <-ctx.Done():
			return actions, false
		case m, ok := <-clientMsgs:
			if !ok {
				return actions, false
			}
			if gotHuman {
				continue
			}
			act, err := parseClientAction(m, st, humanSide)
			if err != nil {
				writeWS(conn, errMsg(err.Error()))
				continue
			}
			actions[humanSide], gotHuman = act, true
		case ev := <-events:
			if ev.Type != messages.EventAIDecided {
				continue
			}
			var d messages.AIDecided
			if json.Unmarshal(ev.Body, &d) != nil || d.JobID != jobID {
				continue
			}
			actions[aiSide], gotAI = d.Action, true
			if d.Reasoning != "" {
				writeWS(conn, map[string]any{"type": "ai", "reasoning": d.Reasoning})
			}
		case <-timer.C:
			if !gotHuman {
				actions[humanSide], gotHuman = engine.LegalActions(st, humanSide)[0], true
				writeWS(conn, map[string]any{"type": "info", "message": "Turn timer expired — a move was auto-selected."})
			}
			if !gotAI {
				actions[aiSide], gotAI = s.localAIDecision(st, aiSide), true
			}
		}
	}
	return actions, true
}

// collectReplaceActions gathers forced-switch choices after faints. Picking a
// replacement is a cheap decision, so the gateway computes the AI's inline —
// the ai-service exists to offload the expensive per-turn search, not this.
// Only the human's choice has to be awaited.
func (s *Server) collectReplaceActions(ctx context.Context, conn *websocket.Conn,
	st *engine.BattleState, clientMsgs <-chan wsClientMsg) ([2]*engine.Action, bool) {

	var sw [2]*engine.Action
	if st.Replace[aiSide] {
		a := s.localAIDecision(st, aiSide)
		sw[aiSide] = &a
	}
	if !st.Replace[humanSide] {
		return sw, true
	}

	writeWS(conn, map[string]any{"type": "info", "message": "Your Pokémon fainted — choose a replacement."})
	timer := time.NewTimer(turnTimeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return sw, false
		case m, ok := <-clientMsgs:
			if !ok {
				return sw, false
			}
			act, err := parseClientAction(m, st, humanSide)
			if err != nil || act.Kind != engine.ActionSwitch {
				writeWS(conn, errMsg("choose a Pokémon to send in"))
				continue
			}
			a := act
			sw[humanSide] = &a
			return sw, true
		case <-timer.C:
			a := engine.LegalActions(st, humanSide)[0]
			sw[humanSide] = &a
			return sw, true
		}
	}
}

// publishAIJob asks the ai-service for one decision and returns the job id to
// correlate with the AIDecided reply.
func (s *Server) publishAIJob(ctx context.Context, battleID string, turn, side int, difficulty string) string {
	jobID := uuid.NewString()
	_ = s.broker.PublishJob(ctx, messages.QueueAI, messages.AIJob{
		JobID: jobID, BattleID: battleID, Turn: turn, Side: side, Difficulty: difficulty,
	})
	return jobID
}

// localAIDecision is the gateway's own fallback when the ai-service is silent.
func (s *Server) localAIDecision(st *engine.BattleState, side int) engine.Action {
	act, _ := s.fallbackAI.Decide(context.Background(), ai.MakeView(st, side))
	return act
}

// persistLiveTurn writes the post-turn state to Redis and the turn to
// Postgres. It uses its own context: persistence must not be cancelled just
// because the client disconnected.
func (s *Server) persistLiveTurn(st *engine.BattleState, log []engine.LogLine) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.cache.SaveState(ctx, st); err != nil {
		return
	}
	logJSON, _ := json.Marshal(log)
	stateJSON, _ := json.Marshal(st)
	_ = s.store.AppendTurn(ctx, st.ID, st.Turn, logJSON, stateJSON)
}

// finishLiveBattle records the result and announces it for the leaderboard.
// It runs on an independent context so a client that disconnects the instant
// the battle ends cannot prevent the result being recorded.
func (s *Server) finishLiveBattle(st *engine.BattleState) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.store.CompleteBattle(ctx, st.ID, st.Winner, st.Turn)
	_ = s.broker.PublishEvent(ctx, messages.EventBattleCompleted, st.ID, messages.BattleCompleted{
		BattleID: st.ID, Winner: st.Winner, TurnCount: st.Turn,
	})
	_ = s.cache.DeleteState(ctx, st.ID)
}

// parseClientAction validates a client message against the legal actions.
func parseClientAction(m wsClientMsg, st *engine.BattleState, side int) (engine.Action, error) {
	kind := engine.ActionMove
	if m.Kind == "switch" {
		kind = engine.ActionSwitch
	}
	act := engine.Action{Kind: kind, Index: m.Index}
	for _, legal := range engine.LegalActions(st, side) {
		if legal == act {
			return act, nil
		}
	}
	return engine.Action{}, errors.New("that action is not legal right now")
}

func writeWS(conn *websocket.Conn, v any) { _ = conn.WriteJSON(v) }

func errMsg(msg string) map[string]any {
	return map[string]any{"type": "error", "message": msg}
}

func turnMsg(log []engine.LogLine, st *engine.BattleState) map[string]any {
	return map[string]any{"type": "turn", "log": log, "state": st}
}

// --- live_pvp ---

// handlePvPWS serves a live_pvp WebSocket client. It claims the slot,
// upgrades the connection, and attaches to the per-battle pvpMatch
// coordinator — then becomes a dumb shuttle: WS frames in → coordinator
// actions; coordinator updates → WS frames out. The coordinator owns the
// authoritative state, action validation, turn resolution, and broadcast.
//
// Two goroutines live for the duration of the connection:
//   - this function (the reader): blocks on conn.ReadJSON; pushes raw
//     actions into the slot's actions channel; closing that channel on
//     exit is how the coordinator learns the slot disconnected.
//   - the writer: drains the slot's updates channel; writes each frame
//     to the WS until the channel closes (coordinator shut down) or the
//     write fails (client gone).
func (s *Server) handlePvPWS(w http.ResponseWriter, r *http.Request) {
	battleID := chi.URLParam(r, "id")
	slot := cache.PvPSlot(r.URL.Query().Get("slot"))
	token := r.URL.Query().Get("token")

	if !slot.Valid() {
		writeErr(w, http.StatusBadRequest, "slot must be 'p1' or 'p2'")
		return
	}
	if token == "" {
		writeErr(w, http.StatusUnauthorized, "join token required")
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Confirm the battle is joinable as pvp before we touch the slot hash.
	// "Unknown battle" and "wrong mode" collapse to one message so an
	// attacker can't probe which one applies.
	b, err := s.store.GetBattle(ctx, battleID)
	if err != nil || b.Mode != "live_pvp" {
		writeErr(w, http.StatusBadRequest, "battle is not joinable as a pvp slot")
		return
	}
	st, err := s.cache.LoadState(ctx, battleID)
	if err != nil || st.Ended() {
		writeErr(w, http.StatusGone, "battle is not in progress")
		return
	}

	// All four ClaimSlot failures collapse to one client-facing message.
	// The operator gets the precise reason via log; the client doesn't
	// get to enumerate which one occurred. See cache.ClaimSlot for why.
	if err := s.cache.ClaimSlot(ctx, battleID, slot, token); err != nil {
		log.Printf("pvp claim refused battle=%s slot=%s: %v", battleID, slot, err)
		writeErr(w, http.StatusForbidden, "slot is not available")
		return
	}
	defer s.releaseSlotBest(battleID, slot)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	attach, ok := s.attachPvPSlot(battleID, slot, st)
	if !ok {
		// Two WS handlers raced through ClaimSlot for the same slot —
		// shouldn't be possible given ClaimSlot's atomicity, but if it
		// somehow happens we refuse the late arrival rather than letting
		// it corrupt the coordinator.
		writeWS(conn, errMsg("slot is already attached to its match"))
		return
	}
	// Closing actions signals "this slot disconnected" to the coordinator.
	// It must happen exactly once, after the reader loop exits.
	defer close(attach.actions)

	// Writer goroutine: drain coordinator updates onto the WS until the
	// updates channel closes (coordinator shutdown) or a write fails. On
	// exit, force the reader's ReadJSON to unblock by setting a past
	// deadline — otherwise a half-open connection (writes failing, reads
	// still blocking) would leave the reader stuck, attach.actions never
	// closed, and the coordinator eventually deadlocked on a full updates
	// buffer. SetReadDeadline is safe to call concurrently with Read.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer conn.SetReadDeadline(time.Now())
		for u := range attach.updates {
			if err := conn.WriteJSON(u); err != nil {
				return
			}
		}
	}()

	// Reader loop: WS → raw action → coordinator's actions channel. We
	// don't validate here; the coordinator does, against its authoritative
	// state. The handler just translates wire format to engine.Action.
	conn.SetReadLimit(4096)
	for {
		var m wsClientMsg
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		act := engine.Action{Kind: kindFromWire(m.Kind), Index: m.Index}
		select {
		case attach.actions <- act:
		case <-writerDone:
			return
		}
	}
}

// kindFromWire maps the wire string to an engine.ActionKind. "switch" is
// the only non-default; anything else is treated as a move (parseClientAction
// follows the same convention for handleLiveWS).
func kindFromWire(s string) engine.ActionKind {
	if s == "switch" {
		return engine.ActionSwitch
	}
	return engine.ActionMove
}

// releaseSlotBest releases on an independent context so it can't be
// cancelled by the client disconnect that triggered it. Errors are
// swallowed — at worst the slot stays claimed for the rest of the battle's
// TTL, which is the v0 worst case we accept.
func (s *Server) releaseSlotBest(battleID string, slot cache.PvPSlot) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.cache.ReleaseSlot(ctx, battleID, slot)
}
