package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
	"pokearena/internal/protocol"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true }, // demo: same-origin SPA
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

// handleLiveWS attaches a human WS to a live-mode match's p1 slot. The
// match itself (picker phase, turn loop, AI driver) lives in pvpMatch
// and runs the same coordinator code as live_pvp — the only difference
// is that slot p2 is an in-process AI rather than a remote WS. This
// handler is just the shuttle: WS frames in → coordinator channels;
// coordinator updates → WS frames out.
//
// No token check: a live battle is a single-player local game, so
// possessing the battle ID is the entire "auth" model. (The same
// applies to handleSSE, which also takes only the battle ID.)
func (s *Server) handleLiveWS(w http.ResponseWriter, r *http.Request) {
	battleID := chi.URLParam(r, "id")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	b, err := s.store.GetBattle(ctx, battleID)
	if err != nil || b.Mode != "live" || b.Status == "complete" {
		writeErr(w, http.StatusBadRequest, "battle is not joinable")
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Live mode hardcodes the human to slot p1 — the AI takes p2 in
	// startLiveRoom. attachPvPSlot is the shared attach path; its
	// once-guard prevents a second WS from hijacking the same slot.
	attach, ok, err := s.attachPvPSlot(battleID, cache.SlotP1)
	if err != nil {
		writeWS(conn, errMsg("room expired or not found"))
		return
	}
	if !ok {
		writeWS(conn, errMsg("a player is already attached to this battle"))
		return
	}
	defer close(attach.actions)
	defer close(attach.submits)

	// Writer goroutine: drain coordinator updates onto the WS. On
	// exit, force the reader's ReadJSON to unblock by setting a past
	// deadline — otherwise a half-open connection would strand the
	// reader and leak the slot.
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

	// Reader loop: WS → coordinator. Validation belongs to the
	// coordinator, which owns the authoritative state.
	conn.SetReadLimit(8192)
	for {
		var m protocol.WsClientMsg
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		switch m.Type {
		case protocol.MsgAction:
			act := engine.Action{Kind: kindFromWire(m.Kind), Index: m.Index}
			select {
			case attach.actions <- act:
			case <-writerDone:
				return
			}
		case protocol.MsgSubmitTeam:
			select {
			case attach.submits <- m.Picks:
			case <-writerDone:
				return
			}
		case protocol.MsgLeaveRoom:
			return
		default:
			// Unknown type — silently ignore.
		}
	}
}

// localAIDecision is the local heuristic fallback the AI driver in
// pvpMatch.driveAITurn uses when the ai-service is silent past the
// per-turn budget. It's also the inline decider for forced-switch
// (Replace) turns, where a queue round-trip would be pure overhead.
func (s *Server) localAIDecision(st *engine.BattleState, side int) engine.Action {
	act, _ := s.fallbackAI.Decide(context.Background(), ai.MakeView(st, side))
	return act
}

// persistLiveTurn fans out the post-turn state. Critical path is short and
// in this order:
//
//  1. SaveState (Redis) — must precede the publish so a late SSE attacher
//     sees a turn whose state Redis already knows about.
//  2. publishLiveEvent — local Hub.Inject (sub-millisecond) + transient
//     Rabbit publish for cross-process consumers.
//  3. AppendTurn (Postgres) — replay history for late joiners. Moved off
//     the publish critical path: the spectator already has the live event;
//     the DB write only matters to clients that attach after this turn.
//
// Persistence runs on its own context so a client disconnect can't cancel
// the writes.
func (s *Server) persistLiveTurn(st *engine.BattleState, log []engine.LogLine) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.cache.SaveState(ctx, st); err != nil {
		return
	}
	logJSON, _ := json.Marshal(log)
	stateJSON, _ := json.Marshal(st)
	s.publishLiveEvent(ctx, messages.EventTurnResolved, st.ID, messages.TurnResolved{
		BattleID: st.ID, Turn: st.Turn, Log: log, State: st,
	})
	_ = s.store.AppendTurn(ctx, st.ID, st.Turn, logJSON, stateJSON)
}

// finishLiveBattle records the result and announces it for the leaderboard.
// It runs on an independent context so a client that disconnects the instant
// the battle ends cannot prevent the result being recorded.
//
// CompleteBattle (Postgres) must run before publishing BattleCompleted —
// the leaderboard worker, which consumes that event cross-process, reads
// the battle row to score it.
func (s *Server) finishLiveBattle(st *engine.BattleState) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.store.CompleteBattle(ctx, st.ID, st.Winner, st.Turn)
	s.publishLiveEvent(ctx, messages.EventBattleCompleted, st.ID, messages.BattleCompleted{
		BattleID: st.ID, Winner: st.Winner, TurnCount: st.Turn,
	})
	_ = s.cache.DeleteState(ctx, st.ID)
}

func writeWS(conn *websocket.Conn, v any) { _ = conn.WriteJSON(v) }

func errMsg(msg string) map[string]any {
	return map[string]any{"type": "error", "message": msg}
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
	// "Unknown battle", "wrong mode", and "completed/expired" collapse to
	// one message so an attacker can't probe which one applies.
	b, err := s.store.GetBattle(ctx, battleID)
	if err != nil || b.Mode != "live_pvp" || b.Status == "complete" {
		writeErr(w, http.StatusBadRequest, "battle is not joinable as a pvp slot")
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

	attach, ok, err := s.attachPvPSlot(battleID, slot)
	if err != nil {
		// Room has timed out or was never created — the URL is dead.
		writeWS(conn, errMsg("room expired or not found"))
		return
	}
	if !ok {
		// Two WS handlers raced through ClaimSlot for the same slot —
		// shouldn't be possible given ClaimSlot's atomicity, but if it
		// somehow happens we refuse the late arrival rather than letting
		// it corrupt the coordinator.
		writeWS(conn, errMsg("slot is already attached to its match"))
		return
	}
	// Closing actions+submits signals "this slot disconnected" to the
	// coordinator. Must happen exactly once, after the reader loop exits.
	defer close(attach.actions)
	defer close(attach.submits)

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

	// Reader loop: WS → coordinator. We don't validate here; the
	// coordinator does, against its authoritative state. The handler
	// just splits the wire format by Type onto the right channel.
	conn.SetReadLimit(8192)
	for {
		var m protocol.WsClientMsg
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		switch m.Type {
		case protocol.MsgAction:
			act := engine.Action{Kind: kindFromWire(m.Kind), Index: m.Index}
			select {
			case attach.actions <- act:
			case <-writerDone:
				return
			}
		case protocol.MsgSubmitTeam:
			select {
			case attach.submits <- m.Picks:
			case <-writerDone:
				return
			}
		case protocol.MsgLeaveRoom:
			// Equivalent to closing the connection; the deferred
			// closes will fire and the coordinator will see disconnect.
			return
		default:
			// Unknown type — silently ignore. A stricter posture
			// would FrameError here; for v1 we keep it lenient.
		}
	}
}

// kindFromWire maps the wire string to an engine.ActionKind. "switch" is
// the only non-default; anything else is treated as a move (parseClientAction
// follows the same convention for handleLiveWS).
func kindFromWire(s string) engine.ActionKind {
	if s == protocol.ActionKindSwitch {
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
