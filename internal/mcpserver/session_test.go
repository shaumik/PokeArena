package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"

	"github.com/gorilla/websocket"
)

// fakeGateway spins up an httptest server with one WS endpoint. Each
// connection runs the provided handler — the test scripts the server
// side of the conversation by writing/reading frames directly.
func fakeGateway(t *testing.T, handler func(t *testing.T, conn *websocket.Conn)) (baseURL string, cleanup func()) {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()
		handler(t, c)
	}))
	// httptest gives http://; the WS dialer needs ws://.
	return "ws" + strings.TrimPrefix(srv.URL, "http"), srv.Close
}

func must(t *testing.T, label string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

// fakeView returns a minimal BattleView good enough for a session to
// hold and return. The session never inspects move legality (the
// gateway does); only the trainer name and turn number matter here.
func fakeView(trainer string, turn int) *ai.View {
	return &ai.View{
		Me:   0,
		Turn: turn,
		Self: engine.Side{Trainer: trainer, Active: 0, Team: []engine.Pokemon{{Name: "Pikachu", HP: 100, MaxHP: 100}}},
	}
}

func newTestSession(baseURL string) *session {
	return newSession(Config{GatewayURL: baseURL})
}

func TestJoinReturnsFirstView(t *testing.T) {
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		must(t, "state", conn.WriteJSON(protocol.MatchUpdate{
			Type: protocol.FrameState, View: fakeView("Red", 0), Turn: 0,
		}))
		blockUntilPeerClose(conn)
	})
	defer cleanup()

	sess := newTestSession(base)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := sess.Join(ctx, "battle-x", "p1", "tok")
	must(t, "Join", err)
	if out.YourTrainer != "Red" {
		t.Errorf("YourTrainer=%q, want Red", out.YourTrainer)
	}
	if out.BattleID != "battle-x" || out.Slot != "p1" {
		t.Errorf("identity wrong: %+v", out)
	}
}

func TestJoinTwiceFails(t *testing.T) {
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		must(t, "state", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, View: fakeView("Red", 0)}))
		blockUntilPeerClose(conn)
	})
	defer cleanup()

	sess := newTestSession(base)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := sess.Join(ctx, "b", "p1", "t")
	must(t, "Join", err)

	_, err = sess.Join(ctx, "b2", "p2", "t2")
	if !errors.Is(err, errAlreadyJoined) {
		t.Errorf("second Join: got %v, want errAlreadyJoined", err)
	}
}

func TestViewWaitActBeforeJoin(t *testing.T) {
	sess := newTestSession("ws://unused")
	if _, err := sess.View(); !errors.Is(err, errNotJoined) {
		t.Errorf("View before Join: got %v", err)
	}
	if _, err := sess.Wait(context.Background(), 1); !errors.Is(err, errNotJoined) {
		t.Errorf("Wait before Join: got %v", err)
	}
	if _, err := sess.Act(protocol.ActionKindMove, 0); !errors.Is(err, errNotJoined) {
		t.Errorf("Act before Join: got %v", err)
	}
	// Leave is a no-op when not joined.
	if err := sess.Leave(); err != nil {
		t.Errorf("Leave before Join: got %v", err)
	}
}

func TestWaitReturnsImmediatelyAfterJoin(t *testing.T) {
	// Join blocks for the first state frame; after Join returns,
	// needsAction is already true, so Wait should return without
	// blocking on the tick channel.
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		must(t, "state", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, View: fakeView("Red", 0)}))
		blockUntilPeerClose(conn)
	})
	defer cleanup()

	sess := newTestSession(base)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := sess.Join(ctx, "b", "p1", "t")
	must(t, "Join", err)

	start := time.Now()
	w, err := sess.Wait(ctx, 60)
	must(t, "Wait", err)
	if !w.Ready || w.Terminal || w.View == nil {
		t.Errorf("Wait after Join: %+v", w)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("Wait took %v; should be near-instant", d)
	}
}

func TestActThenWaitForOpponent(t *testing.T) {
	// Full round-trip: state arrives → agent acts → gateway acks with
	// a turn frame for turn 1 → next Wait reports turn 1 as ready.
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		// Initial state.
		must(t, "state", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, View: fakeView("Red", 0), Turn: 0}))
		// Wait for the agent's action.
		var msg protocol.WsClientMsg
		must(t, "read action", conn.ReadJSON(&msg))
		if msg.Kind != protocol.ActionKindMove || msg.Index != 1 {
			t.Errorf("server got %+v; want move/1", msg)
		}
		// Resolve: send back a turn frame for turn 1.
		must(t, "turn", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameTurn, View: fakeView("Red", 1), Turn: 1}))
		blockUntilPeerClose(conn)
	})
	defer cleanup()

	sess := newTestSession(base)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := sess.Join(ctx, "b", "p1", "t")
	must(t, "Join", err)

	// First Wait → ready for turn 0.
	w0, err := sess.Wait(ctx, 5)
	must(t, "Wait 0", err)
	if !w0.Ready || w0.View.Turn != 0 {
		t.Errorf("Wait 0: %+v", w0)
	}

	// Act submits, returns immediately with the turn we acted on.
	act, err := sess.Act(protocol.ActionKindMove, 1)
	must(t, "Act", err)
	if !act.Accepted || act.Turn != 0 {
		t.Errorf("Act: %+v", act)
	}

	// Second Act before a new frame must fail — clears needsAction
	// until a turn arrives.
	if _, err := sess.Act(protocol.ActionKindMove, 0); !errors.Is(err, errNotYourTurn) {
		t.Errorf("Act twice: got %v, want errNotYourTurn", err)
	}

	// Wait again — should pick up turn 1 from the gateway.
	w1, err := sess.Wait(ctx, 5)
	must(t, "Wait 1", err)
	if !w1.Ready || w1.View.Turn != 1 {
		t.Errorf("Wait 1: %+v", w1)
	}
}

func TestWaitTimesOut(t *testing.T) {
	// Server sends initial state, then nothing. Agent acts and then
	// waits; with no turn-frame coming, the second Wait should return
	// ready=false after the timeout (clamped to >=1s).
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		must(t, "state", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, View: fakeView("Red", 0)}))
		var msg protocol.WsClientMsg
		_ = conn.ReadJSON(&msg) // swallow the action; never resolve
		blockUntilPeerClose(conn)
	})
	defer cleanup()

	sess := newTestSession(base)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := sess.Join(ctx, "b", "p1", "t")
	must(t, "Join", err)
	_, _ = sess.Wait(ctx, 1)
	_, _ = sess.Act(protocol.ActionKindMove, 0)

	start := time.Now()
	w, err := sess.Wait(ctx, 1) // 1s — minimum
	must(t, "Wait", err)
	if w.Ready {
		t.Errorf("Wait should have timed out, got %+v", w)
	}
	if d := time.Since(start); d < 900*time.Millisecond || d > 1500*time.Millisecond {
		t.Errorf("Wait duration %v not near 1s", d)
	}
}

func TestEndFrameTerminatesSession(t *testing.T) {
	// Models the real agent loop: state → act → end. The post-act Wait
	// is what catches the end frame; we don't try to race the
	// dispatcher by spinning on Wait, because that's not how an agent
	// uses the protocol.
	winner := 0
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		must(t, "state", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, View: fakeView("Red", 0)}))
		var msg protocol.WsClientMsg
		must(t, "read action", conn.ReadJSON(&msg))
		must(t, "end", conn.WriteJSON(protocol.MatchUpdate{
			Type: protocol.FrameEnd, View: fakeView("Red", 5), Winner: &winner, Turn: 5,
		}))
		blockUntilPeerClose(conn)
	})
	defer cleanup()

	sess := newTestSession(base)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := sess.Join(ctx, "b", "p1", "t")
	must(t, "Join", err)
	_, _ = sess.Wait(ctx, 1) // returns immediately with state from Join
	_, err = sess.Act(protocol.ActionKindMove, 0)
	must(t, "Act", err)

	// Now needsAction is false; Wait blocks until the end frame ticks us.
	w, err := sess.Wait(ctx, 2)
	must(t, "Wait", err)
	if !w.Terminal {
		t.Fatalf("Wait after act should be terminal, got %+v", w)
	}
	if w.View == nil || w.View.Turn != 5 {
		t.Errorf("terminal Wait view: %+v", w.View)
	}

	if _, err := sess.Act(protocol.ActionKindMove, 0); !errors.Is(err, errBattleEnded) {
		t.Errorf("Act after end: got %v, want errBattleEnded", err)
	}
}

func TestLeaveAllowsRejoin(t *testing.T) {
	mkServer := func(trainer string) (string, func()) {
		return fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
			must(t, "state", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, View: fakeView(trainer, 0)}))
			blockUntilPeerClose(conn)
		})
	}

	base1, cleanup1 := mkServer("Red")
	defer cleanup1()
	sess := newTestSession(base1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := sess.Join(ctx, "b1", "p1", "t1")
	must(t, "Join 1", err)
	must(t, "Leave", sess.Leave())

	base2, cleanup2 := mkServer("Blue")
	defer cleanup2()
	sess.cfg.GatewayURL = base2

	out, err := sess.Join(ctx, "b2", "p2", "t2")
	must(t, "Join 2", err)
	if out.YourTrainer != "Blue" || out.BattleID != "b2" {
		t.Errorf("rejoin lost identity: %+v", out)
	}
}

// blockUntilPeerClose is the standard server-side body for "stay alive
// until the client hangs up". Reused by every test that needs the
// server to outlive its scripted write phase.
func blockUntilPeerClose(conn *websocket.Conn) {
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
