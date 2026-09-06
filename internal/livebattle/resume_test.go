package livebattle

import (
	"context"
	"testing"
	"time"

	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/messages"
)

// TestMatch_ResumeFromMidBattle proves the failover-takeover path: a battle is
// advanced several turns with the raw engine (simulating progress under a
// now-dead owner), then a fresh coordinator adopts that persisted state and
// drives it to completion — without re-running the picker phase or re-announcing
// the battle.
func TestMatch_ResumeFromMidBattle(t *testing.T) {
	dex := loadDex(t)
	t1, t2 := twoTeams(t, dex)

	st, err := engine.NewBattleFromPicks(dex, "B-resume", "Red", t1, "Blue", t2, 7)
	if err != nil {
		t.Fatalf("build battle: %v", err)
	}
	// Warm up a few turns the way the dead owner would have.
	for i := 0; i < 3 && !st.Ended(); i++ {
		engine.ResolveTurn(dex, st, [2]engine.Action{
			engine.LegalActions(st, 0)[0], engine.LegalActions(st, 1)[0],
		})
		if st.Phase == engine.PhaseReplace {
			var sw [2]*engine.Action
			for s := 0; s < 2; s++ {
				if st.Replace[s] {
					a := engine.LegalActions(st, s)[0]
					sw[s] = &a
				}
			}
			engine.ResolveReplace(st, sw)
		}
	}
	if st.Ended() {
		t.Skip("battle ended during warm-up; nothing to resume")
	}
	resumeTurn := st.Turn

	store := &fakeStore{}
	cache := &fakeCache{}
	events := &eventRecorder{}
	sink := &recordSink{}

	m := NewResumedMatch(Config{
		BattleID: "B-resume", P1Name: "Red", P2Name: "Blue", Seed: 7,
		Kinds: [2]SideKind{SideAI, SideAI},
		Sink:  sink,
		Deps: Deps{
			Dex: dex, Cache: cache, Store: store, Publish: events.publish, AI: legalAI{},
		},
	}, st)

	if m.CurrentTurn() != resumeTurn {
		t.Fatalf("resumed CurrentTurn = %d, want %d", m.CurrentTurn(), resumeTurn)
	}

	done := make(chan struct{})
	go func() { m.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("resumed battle did not finish within 30s")
	}

	if !store.completed {
		t.Fatal("resumed battle never completed")
	}
	if store.turns < resumeTurn {
		t.Fatalf("final turn %d < resume turn %d — battle went backwards", store.turns, resumeTurn)
	}
	// A takeover must NOT re-announce the battle, but must still complete it.
	if events.saw(messages.EventBattleStarted) {
		t.Fatal("resume wrongly re-published battle-started")
	}
	if !events.saw(messages.EventBattleCompleted) {
		t.Fatal("resumed battle never published battle-completed")
	}
}
