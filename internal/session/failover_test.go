package session_test

// Chaos test for failover: a live battle whose owner has died (lease expired,
// state persisted) is reclaimed by a surviving instance's scan and driven to
// completion. Needs real infra; skips fast when absent.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
	"pokearena/internal/session"
	"pokearena/internal/store"
)

func TestFailover_SurvivorReclaimsOrphanedBattle(t *testing.T) {
	st, rc, newBroker := dialInfra(t)
	defer st.Close()
	defer rc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dex, err := domain.LoadDex("../../data", "test")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	pool, err := ai.LoadTeamPool(dex, "../../data/ai-teams.json")
	if err != nil {
		t.Fatalf("team pool: %v", err)
	}
	t1, _ := pool.Pick(randSource(11))
	t2, _ := pool.Pick(randSource(22))

	battleID := uuid.NewString()
	seed := uint64(424242)

	// Stand up a battle mid-flight WITHOUT a live owner: build state, advance a
	// couple of turns, persist it, and write the "running" row — exactly what a
	// now-dead owner would have left behind. We do NOT claim a lease (or we let a
	// short one expire), so the battle is orphaned.
	state, err := engine.NewBattleFromPicks(dex, battleID, "Red", t1, "Blue", t2, seed)
	if err != nil {
		t.Fatalf("build battle: %v", err)
	}
	for i := 0; i < 2 && !state.Ended(); i++ {
		engine.ResolveTurn(dex, state, [2]engine.Action{
			engine.LegalActions(state, 0)[0], engine.LegalActions(state, 1)[0],
		})
	}
	if err := rc.SaveState(ctx, state); err != nil {
		t.Fatalf("save state: %v", err)
	}
	tr1, _ := st.UpsertTrainer(ctx, "Red")
	tr2, _ := st.UpsertTrainer(ctx, "Blue")
	if err := st.CreateBattle(ctx, store.Battle{
		ID: battleID, Mode: "live_pvp", Seed: int64(seed),
		P1Trainer: tr1, P2Trainer: tr2, P1Name: "Red", P2Name: "Blue",
		Winner: -1, Status: "running",
	}); err != nil {
		t.Fatalf("create battle: %v", err)
	}
	// A dead owner's lease that has already lapsed.
	if _, err := rc.ClaimBattleOwner(ctx, battleID, "dead-owner", 1*time.Second); err != nil {
		t.Fatalf("seed dead lease: %v", err)
	}
	time.Sleep(1500 * time.Millisecond) // let it expire

	// The survivor: short timings so the test reclaims quickly.
	brokerS := newBroker()
	defer brokerS.Close()
	svc := session.New(session.Config{
		InstanceID: "survivor", Dex: dex, Store: st, Cache: rc, Broker: brokerS,
		LeaseTTL: 6 * time.Second, LeaseRenew: 2 * time.Second, ScanInterval: 1 * time.Second,
	})
	go func() { _ = svc.Run(ctx) }()

	// Two gateway bridges (one broker + hub each) keep relaying — they don't know
	// the owner changed.
	brokerA, brokerB := newBroker(), newBroker()
	defer brokerA.Close()
	defer brokerB.Close()
	hubA := mustHub(t, brokerA)
	hubB := mustHub(t, brokerB)
	go func() { _ = hubA.Run(ctx) }()
	go func() { _ = hubB.Run(ctx) }()

	_, framesA, err := hubA.SubscribeFrames(battleID, "p1")
	if err != nil {
		t.Fatalf("subscribe p1: %v", err)
	}
	_, framesB, err := hubB.SubscribeFrames(battleID, "p2")
	if err != nil {
		t.Fatalf("subscribe p2: %v", err)
	}

	// Nudge the new owner: the bridges re-announce attach so the pump wires the
	// slots, then drive normally.
	publishAction(t, ctx, brokerA, battleID, "p1", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, brokerB, battleID, "p2", messages.LivePhaseAttach, nil, engine.Action{})

	doneA := make(chan struct{})
	doneB := make(chan struct{})
	go driveSide(ctx, brokerA, battleID, "p1", framesA, doneA)
	go driveSide(ctx, brokerB, battleID, "p2", framesB, doneB)

	deadline := time.After(60 * time.Second)
	for got := 0; got < 2; {
		select {
		case <-doneA:
			doneA = nil
			got++
		case <-doneB:
			doneB = nil
			got++
		case <-deadline:
			t.Fatal("orphaned battle was not reclaimed and finished within 60s")
		}
	}

	owner, _ := rc.GetBattleOwner(ctx, battleID)
	if owner == "dead-owner" {
		t.Fatal("battle is still owned by the dead instance — no takeover happened")
	}
	b, err := st.GetBattle(ctx, battleID)
	if err != nil {
		t.Fatalf("get battle: %v", err)
	}
	if b.Status != "completed" {
		t.Fatalf("battle status = %q, want completed", b.Status)
	}
}
