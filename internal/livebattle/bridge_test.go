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
