package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"pokearena/internal/cache"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
	"pokearena/internal/protocol"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true }, // demo: same-origin SPA
}

// handleWS is the dispatcher for /api/battles/{id}/play. The URL shape is the
// same for single-player live mode and live_pvp; the presence of a slot query
// param is what distinguishes them.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("slot") != "" {
		s.handlePvPWS(w, r)
		return
	}
	s.handleLiveWS(w, r)
}

// handleLiveWS bridges a human WS to a live-mode battle. The coordinator lives
// in the battle-session service; this gateway only terminates the socket and
// relays. Live mode hardcodes the human to slot p1 (the AI takes p2 in the
// session). No token check: a live battle is a single-player game, so the
// battle ID is the entire auth model (same as handleSSE).
func (s *Server) handleLiveWS(w http.ResponseWriter, r *http.Request) {
	battleID := chi.URLParam(r, "id")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	b, err := s.store.GetBattle(ctx, battleID)
	if err != nil || b.Mode != "live" || !joinableStatus(b.Status) {
		writeErr(w, http.StatusBadRequest, "battle is not joinable")
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	s.bridgeSlot(ctx, conn, battleID, cache.SlotP1)
}

// joinableStatus reports whether a battle in the given lifecycle status still
// accepts a WS client. Only "open" (picker room) and "running" (active, possibly
// mid-failover) are joinable; "completed"/"abandoned" — and any other terminal
// status — are not. The earlier guard compared against "complete", a value the
// writer side never produces (CompleteBattle writes "completed", cleanup writes
// "abandoned"), so it was dead and dead battles were upgraded to a socket that
// then hung until timeout.
func joinableStatus(status string) bool {
	return status == "open" || status == "running"
}

func writeWS(conn *websocket.Conn, v any) { _ = conn.WriteJSON(v) }

func errMsg(msg string) map[string]any {
	return map[string]any{"type": "error", "message": msg}
}

// --- live_pvp ---

// handlePvPWS serves a live_pvp WebSocket client. It claims the slot, upgrades
// the connection, and bridges it to the battle-session coordinator. The
// coordinator owns the authoritative state, action validation, turn resolution,
// and broadcast — wherever it happens to be leased.
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
	// "Unknown battle", "wrong mode", and "completed/expired" collapse to one
	// message so an attacker can't probe which one applies.
	b, err := s.store.GetBattle(ctx, battleID)
	if err != nil || b.Mode != "live_pvp" || !joinableStatus(b.Status) {
		writeErr(w, http.StatusBadRequest, "battle is not joinable as a pvp slot")
		return
	}

	// All four ClaimSlot failures collapse to one client-facing message. The
	// operator gets the precise reason via log; the client doesn't get to
	// enumerate which one occurred. See cache.ClaimSlot for why.
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

	s.bridgeSlot(ctx, conn, battleID, slot)
}

// bridgeSlot is the gateway's whole live-battle job: shuttle bytes between one
// WebSocket and the broker. Inbound client messages become LiveActions on the
// durable action queue; outbound per-slot frames arrive over the events topic
// (via the Hub's frame binding) and are written straight to the socket. The
// gateway holds no battle state and runs no game logic — the session owner does.
//
// Two goroutines live for the connection: the writer (frames → socket) and this
// function's reader loop (socket → actions). On reader exit the bridge tells the
// session the slot disconnected; the writer's deferred read-deadline unblocks a
// half-open reader so the slot is never leaked.
func (s *Server) bridgeSlot(ctx context.Context, conn *websocket.Conn, battleID string, slot cache.PvPSlot) {
	slotName := string(slot)

	subID, frames, err := s.hub.SubscribeFrames(battleID, slotName)
	if err != nil {
		writeWS(conn, errMsg("unable to join battle stream"))
		return
	}
	defer s.hub.UnsubscribeFrames(battleID, slotName, subID)

	// connID identifies this specific WS connection for the slot. It is stamped on
	// every action this bridge publishes so the session owner can tell a stale or
	// redelivered disconnect (from a connection that is no longer current) from a
	// live one, and can cancel a disconnect's reconnect-grace timer when the same
	// slot re-attaches under a new id. A fresh id per bridgeSlot is exactly the
	// per-connection identity that earlier broke when disconnect detection moved
	// from an in-process channel close to a broker message.
	connID := uuid.NewString()

	// Announce attachment so the session shows this slot connected; announce
	// disconnect on exit so it can wind the match down (after its grace window).
	s.sendLiveAction(messages.LiveAction{BattleID: battleID, Slot: slotName, Conn: connID, Phase: messages.LivePhaseAttach})
	defer s.sendLiveAction(messages.LiveAction{BattleID: battleID, Slot: slotName, Conn: connID, Phase: messages.LivePhaseDisconnect})

	bridgeCtx, cancelBridge := context.WithCancel(ctx)
	defer cancelBridge()

	// lastTurn tracks the turn number of the most recent outbound frame, so an
	// inbound action is stamped with the turn it responds to — making the
	// session's redelivery dedup possible.
	var lastTurn int64

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = conn.SetReadDeadline(time.Now()) }() // unblock the reader on exit
		for {
			select {
			case <-bridgeCtx.Done():
				return
			case body, ok := <-frames:
				if !ok {
					return
				}
				var probe struct {
					Turn int `json:"turn"`
				}
				_ = json.Unmarshal(body, &probe)
				// Only advance lastTurn from a frame that actually carries a turn.
				// State/turn/end frames stamp the real turn; FrameError/FrameInfo/
				// FrameRoom omit it (turn==0). Stamping those would reset lastTurn to
				// 0, and the next client action would be published Turn=0 — which the
				// session's dedup never drops (its a.Turn>0 guard) — so on failover
				// the new owner replays that action and the move executes twice.
				if probe.Turn > 0 {
					atomic.StoreInt64(&lastTurn, int64(probe.Turn))
				}
				if err := conn.WriteMessage(websocket.TextMessage, body); err != nil {
					return
				}
			}
		}
	}()

	// Reader loop: WS → action queue. We don't validate here; the coordinator
	// does, against its authoritative state. The handler just splits the wire
	// format by Type into the right LiveAction phase.
	conn.SetReadLimit(8192)
	for {
		var m protocol.WsClientMsg
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		switch m.Type {
		case protocol.MsgAction:
			act := engine.Action{Kind: kindFromWire(m.Kind), Index: m.Index, SwitchTarget: m.SwitchTarget}
			s.sendLiveAction(messages.LiveAction{
				BattleID: battleID, Slot: slotName, Conn: connID,
				Turn:  int(atomic.LoadInt64(&lastTurn)),
				Phase: messages.LivePhaseAction, Action: act,
			})
		case protocol.MsgSubmitTeam:
			s.sendLiveAction(messages.LiveAction{
				BattleID: battleID, Slot: slotName, Conn: connID,
				Phase: messages.LivePhaseSubmit, Picks: m.Picks,
			})
		case protocol.MsgLeaveRoom:
			// Equivalent to closing the connection; the deferred disconnect
			// LiveAction fires and the session sees the slot leave.
			return
		default:
			// Unknown type — silently ignore.
		}
	}
}

// sendLiveAction publishes one inbound action on an independent context so a
// client disconnect that triggers it (the disconnect phase) can't cancel the
// publish. Best-effort: a dropped action is a transient broker failure the
// per-turn timeout in the session backstops.
func (s *Server) sendLiveAction(a messages.LiveAction) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.broker.PublishLiveAction(ctx, a); err != nil {
		log.Printf("publish live action battle=%s slot=%s phase=%s: %v", a.BattleID, a.Slot, a.Phase, err)
	}
}

// kindFromWire maps the wire string to an engine.ActionKind. "switch" is the
// only non-default; anything else is treated as a move.
func kindFromWire(s string) engine.ActionKind {
	if s == protocol.ActionKindSwitch {
		return engine.ActionSwitch
	}
	return engine.ActionMove
}

// releaseSlotBest releases on an independent context so it can't be canceled by
// the client disconnect that triggered it. Errors are swallowed — at worst the
// slot stays claimed for the rest of the battle's TTL.
func (s *Server) releaseSlotBest(battleID string, slot cache.PvPSlot) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.cache.ReleaseSlot(ctx, battleID, slot)
}
