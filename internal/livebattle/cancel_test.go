package livebattle

import (
	"context"
	"testing"
	"time"

	"github.com/shaumik/PokeArena/internal/protocol"
)

// TestMatch_RunStopsOnContextCancel pins the contract that the coordinator's
// turn loop is bound to a caller-supplied context: when the host cancels it
// (graceful shutdown, or a lost ownership lease being yielded to another
// instance), Run must return promptly instead of blocking forever on its inbound
// channels.
//
// Before the fix Run built its own context.Background() internally, so a host
// that "yielded" could stop the action pump but never the engine loop — the
// Match goroutine leaked. This test would hang (and fail on the timeout) against
// that version.
func TestMatch_RunStopsOnContextCancel(t *testing.T) {
	dex := loadDex(t)
	t1, t2 := twoTeams(t, dex)

	sink := newChanSink()
	m := NewMatch(Config{
		BattleID: "B-cancel", P1Name: "Red", P2Name: "Blue", Seed: 7,
		Kinds: [2]SideKind{SideWS, SideWS},
		Sink:  sink,
		Deps: Deps{
			Dex: dex, Cache: &fakeCache{}, Store: &fakeStore{}, Publish: (&eventRecorder{}).publish,
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan Reason, 1)
	go func() { done <- m.Run(ctx) }()

	// Drive the room to ACTIVE: attach both slots and submit teams. Drain slot 1;
	// watch slot 0 for the FrameState that means the turn loop has begun.
	p0, ok := m.Attach(0)
	if !ok {
		t.Fatal("attach slot 0 failed")
	}
	p1, ok := m.Attach(1)
	if !ok {
		t.Fatal("attach slot 1 failed")
	}
	go func() {
		for range sink.ch[1] {
		}
	}()
	p0.Submits <- t1
	p1.Submits <- t2
	if !waitForFrame(t, sink.ch[0], protocol.FrameState, 5*time.Second) {
		t.Fatal("never observed FrameState — battle did not reach ACTIVE")
	}

	// Neither side will ever act now. The only thing that can stop the loop is the
	// context. Canceling it must unblock Run.
	cancel()

	select {
	case r := <-done:
		if r != ReasonYielded {
			t.Fatalf("Run returned reason %v, want ReasonYielded on context cancel", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after context cancel — the turn loop leaked")
	}
}
