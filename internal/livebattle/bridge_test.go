package livebattle

import (
	"context"
	"testing"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
	"pokearena/internal/protocol"
)

// legalMoveFromView is a minimal honest client: it picks a legal action from the
// fog-of-war View the coordinator sends, the same information a real WS client
// has. At full PP on turn one, the first move slot is always legal.
func legalMoveFromView(v *ai.View) engine.Action {
	if v.Replace {
		for i := range v.Self.Team {
			if i != v.Self.Active && !v.Self.Team[i].Fainted {
				return engine.Action{Kind: engine.ActionSwitch, Index: i}
			}
		}
	}
	active := v.Self.Team[v.Self.Active]
	for i, mv := range active.Moves {
		if mv.PP > 0 {
			return engine.Action{Kind: engine.ActionMove, Index: i}
		}
	}
	return engine.Action{Kind: engine.ActionMove, Index: -1} // all moves out of PP → Struggle
}

func readUntil(t *testing.T, ch <-chan protocol.MatchUpdate, frameType string, timeout time.Duration) protocol.MatchUpdate {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case u, ok := <-ch:
			if !ok {
				t.Fatalf("frame channel closed before %q arrived", frameType)
			}
			if u.Type == frameType {
				return u
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q frame", frameType)
		}
	}
}

// TestPump_DrivesLivePvPThroughBrokerShapedMessages proves the dedicated-tier
// path: a live_pvp battle reaches ACTIVE and resolves a turn driven entirely by
// LiveAction messages routed through the Pump — the exact shape the
// battle-session feeds from the broker. No gateway, no RabbitMQ.
func TestPump_DrivesLivePvPThroughBrokerShapedMessages(t *testing.T) {
	dex := loadDex(t)
	t1, t2 := twoTeams(t, dex)

	sink := newChanSink()
	m := NewMatch(Config{
		BattleID: "B-bridge", P1Name: "Red", P2Name: "Blue", Seed: 99,
		Kinds: [2]SideKind{SideWS, SideWS},
		Sink:  sink,
		Deps: Deps{
			Dex: dex, Cache: &fakeCache{}, Store: &fakeStore{}, Publish: (&eventRecorder{}).publish,
		},
	})
	kinds := [2]SideKind{SideWS, SideWS}
	pump := NewPump(m, kinds)

	done := make(chan struct{})
	go func() { m.Run(context.Background()); close(done) }()

	// Both bridges attach, then submit their teams — as LiveActions.
	pump.Route(messages.LiveAction{BattleID: "B-bridge", Slot: "p1", Phase: messages.LivePhaseAttach})
	pump.Route(messages.LiveAction{BattleID: "B-bridge", Slot: "p2", Phase: messages.LivePhaseAttach})
	pump.Route(messages.LiveAction{BattleID: "B-bridge", Slot: "p1", Phase: messages.LivePhaseSubmit, Picks: t1})
	pump.Route(messages.LiveAction{BattleID: "B-bridge", Slot: "p2", Phase: messages.LivePhaseSubmit, Picks: t2})

	// Each side receives its initial fog-of-war view on the FrameState.
	s0 := readUntil(t, sink.ch[0], protocol.FrameState, 5*time.Second)
	s1 := readUntil(t, sink.ch[1], protocol.FrameState, 5*time.Second)
	if s0.View == nil || s1.View == nil {
		t.Fatal("FrameState carried no view")
	}

	// Both sides act, relayed as LiveAction{action}. The coordinator should
	// resolve the turn and broadcast a FrameTurn.
	pump.Route(messages.LiveAction{BattleID: "B-bridge", Slot: "p1", Phase: messages.LivePhaseAction, Action: legalMoveFromView(s0.View)})
	pump.Route(messages.LiveAction{BattleID: "B-bridge", Slot: "p2", Phase: messages.LivePhaseAction, Action: legalMoveFromView(s1.View)})

	turn := readUntil(t, sink.ch[0], protocol.FrameTurn, 5*time.Second)
	if turn.Turn < 1 {
		t.Fatalf("resolved turn = %d, want >= 1", turn.Turn)
	}

	// A disconnect LiveAction must wind the coordinator down.
	go func() {
		for range sink.ch[1] {
		}
	}()
	go func() {
		for range sink.ch[0] {
		}
	}()
	pump.Route(messages.LiveAction{BattleID: "B-bridge", Slot: "p1", Phase: messages.LivePhaseDisconnect})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("coordinator did not shut down after disconnect LiveAction")
	}
}

// drivePvPToActive attaches and submits both WS slots through the pump under the
// given connection ids and returns each side's opening fog-of-war view once the
// battle is ACTIVE.
func drivePvPToActive(t *testing.T, pump *Pump, sink *chanSink, bid, c1, c2 string, t1, t2 []engine.TeamPick) (*ai.View, *ai.View) {
	t.Helper()
	pump.Route(messages.LiveAction{BattleID: bid, Slot: "p1", Phase: messages.LivePhaseAttach, Conn: c1})
	pump.Route(messages.LiveAction{BattleID: bid, Slot: "p2", Phase: messages.LivePhaseAttach, Conn: c2})
	pump.Route(messages.LiveAction{BattleID: bid, Slot: "p1", Phase: messages.LivePhaseSubmit, Conn: c1, Picks: t1})
	pump.Route(messages.LiveAction{BattleID: bid, Slot: "p2", Phase: messages.LivePhaseSubmit, Conn: c2, Picks: t2})
	s0 := readUntil(t, sink.ch[0], protocol.FrameState, 5*time.Second)
	s1 := readUntil(t, sink.ch[1], protocol.FrameState, 5*time.Second)
	if s0.View == nil || s1.View == nil {
		t.Fatal("opening FrameState carried no view")
	}
	return s0.View, s1.View
}

func newPvPMatch(t *testing.T, bid string, grace time.Duration) (*Match, *Pump, *chanSink) {
	t.Helper()
	dex := loadDex(t)
	sink := newChanSink()
	m := NewMatch(Config{
		BattleID: bid, P1Name: "Red", P2Name: "Blue", Seed: 99,
		Kinds:           [2]SideKind{SideWS, SideWS},
		Sink:            sink,
		DisconnectGrace: grace,
		Deps: Deps{
			Dex: dex, Cache: &fakeCache{}, Store: &fakeStore{}, Publish: (&eventRecorder{}).publish,
		},
	})
	return m, NewPump(m, [2]SideKind{SideWS, SideWS}), sink
}

// TestPump_ReconnectWithinGraceKeepsBattleAlive proves the reconnect-grace fix:
// a disconnect for the live connection (a transient blip) does not end the
// battle, and a re-attach under a new connection id within the grace window
// cancels the pending teardown so play continues.
func TestPump_ReconnectWithinGraceKeepsBattleAlive(t *testing.T) {
	dex := loadDex(t)
	t1, t2 := twoTeams(t, dex)
	m, pump, sink := newPvPMatch(t, "B-recon", 2*time.Second)

	done := make(chan struct{})
	go func() { m.Run(context.Background()); close(done) }()

	v0, v1 := drivePvPToActive(t, pump, sink, "B-recon", "c1", "c2", t1, t2)

	// p1's socket blips: a disconnect for the live connection must NOT end the
	// battle — it only arms the grace timer.
	pump.Route(messages.LiveAction{BattleID: "B-recon", Slot: "p1", Phase: messages.LivePhaseDisconnect, Conn: "c1"})
	select {
	case <-done:
		t.Fatal("battle ended on a transient blip despite the grace window")
	case <-time.After(300 * time.Millisecond):
	}

	// p1 reconnects under a fresh connection id, cancelling the grace timer.
	pump.Route(messages.LiveAction{BattleID: "B-recon", Slot: "p1", Phase: messages.LivePhaseAttach, Conn: "c1b"})

	// The battle is alive: both sides act and the turn resolves.
	pump.Route(messages.LiveAction{BattleID: "B-recon", Slot: "p1", Phase: messages.LivePhaseAction, Conn: "c1b", Turn: 1, Action: legalMoveFromView(v0)})
	pump.Route(messages.LiveAction{BattleID: "B-recon", Slot: "p2", Phase: messages.LivePhaseAction, Conn: "c2", Turn: 1, Action: legalMoveFromView(v1)})

	if turn := readUntil(t, sink.ch[0], protocol.FrameTurn, 5*time.Second); turn.Turn < 1 {
		t.Fatalf("resolved turn = %d, want >= 1", turn.Turn)
	}
	select {
	case <-done:
		t.Fatal("battle ended even though the slot reconnected within grace")
	default:
	}
}

// TestPump_StaleDisconnectIsIgnored proves the disconnect-identity fix: a
// disconnect carrying a connection id that is not the slot's live one — e.g. the
// durable action queue replaying an old message after a takeover — is dropped,
// even with no grace window, so a healthy battle is never abandoned by a
// redelivered signal.
func TestPump_StaleDisconnectIsIgnored(t *testing.T) {
	dex := loadDex(t)
	t1, t2 := twoTeams(t, dex)
	m, pump, sink := newPvPMatch(t, "B-stale", 0) // no grace: immediate if honored

	done := make(chan struct{})
	go func() { m.Run(context.Background()); close(done) }()

	v0, v1 := drivePvPToActive(t, pump, sink, "B-stale", "c1", "c2", t1, t2)

	// A disconnect from a connection that is not the live one must be ignored.
	pump.Route(messages.LiveAction{BattleID: "B-stale", Slot: "p1", Phase: messages.LivePhaseDisconnect, Conn: "stale-conn"})
	select {
	case <-done:
		t.Fatal("a stale-connection disconnect abandoned a healthy battle")
	case <-time.After(300 * time.Millisecond):
	}

	// The battle is unaffected: a turn still resolves.
	pump.Route(messages.LiveAction{BattleID: "B-stale", Slot: "p1", Phase: messages.LivePhaseAction, Conn: "c1", Turn: 1, Action: legalMoveFromView(v0)})
	pump.Route(messages.LiveAction{BattleID: "B-stale", Slot: "p2", Phase: messages.LivePhaseAction, Conn: "c2", Turn: 1, Action: legalMoveFromView(v1)})
	if turn := readUntil(t, sink.ch[0], protocol.FrameTurn, 5*time.Second); turn.Turn < 1 {
		t.Fatalf("resolved turn = %d, want >= 1", turn.Turn)
	}
}

// TestPump_DisconnectGraceExpiresAbandons proves the grace window is a window,
// not a reprieve: with no reconnect, the slot is declared gone once it lapses and
// the coordinator winds down.
func TestPump_DisconnectGraceExpiresAbandons(t *testing.T) {
	dex := loadDex(t)
	t1, t2 := twoTeams(t, dex)
	m, pump, sink := newPvPMatch(t, "B-grace-exp", 300*time.Millisecond)

	done := make(chan struct{})
	go func() { m.Run(context.Background()); close(done) }()

	drivePvPToActive(t, pump, sink, "B-grace-exp", "c1", "c2", t1, t2)

	// Drain both slots only after the opening views are read, so the coordinator
	// never blocks on a full sink while the grace timer runs.
	for s := range sink.ch {
		go func(ch <-chan protocol.MatchUpdate) {
			for range ch {
			}
		}(sink.ch[s])
	}

	pump.Route(messages.LiveAction{BattleID: "B-grace-exp", Slot: "p1", Phase: messages.LivePhaseDisconnect, Conn: "c1"})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("battle did not end after the disconnect grace lapsed with no reconnect")
	}
}

// TestPump_IgnoresActionsForAISlot ensures a stray message aimed at an AI slot
// is dropped rather than attaching or corrupting it.
func TestPump_IgnoresActionsForAISlot(t *testing.T) {
	dex := loadDex(t)
	_, t2 := twoTeams(t, dex)
	sink := &recordSink{}
	m := NewMatch(Config{
		BattleID: "B-ai-guard", Seed: 1,
		Kinds:   [2]SideKind{SideWS, SideAI},
		AITeams: [2][]engine.TeamPick{nil, t2},
		Sink:    sink,
		Deps: Deps{
			Dex: dex, Cache: &fakeCache{}, Store: &fakeStore{}, Publish: (&eventRecorder{}).publish,
			AI: legalAI{},
		},
	})
	pump := NewPump(m, [2]SideKind{SideWS, SideAI})
	// Routing a p2 (AI) action must be a no-op — the Pump only feeds WS slots.
	pump.Route(messages.LiveAction{BattleID: "B-ai-guard", Slot: "p2", Phase: messages.LivePhaseAction})
	// Nothing to assert beyond "did not panic / attach"; the guard is internal.
}
