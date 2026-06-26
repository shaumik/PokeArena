//go:build integration

package session_test

// Concurrency: one battle-session owner coordinates more than one live battle at
// the same time. The owner acks each session job and spawns a coordinator rather
// than blocking the consumer for a battle's lifetime, so two live_pvp battles
// published back to back must both run and complete under the single owner. This
// is the multiplexing property the other tests (one battle each) never exercise.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/httpapi"
	"pokearena/internal/messages"
	"pokearena/internal/mq"
	"pokearena/internal/session"
	"pokearena/internal/store"
)

// startPvP wires one live_pvp battle through the given gateways and drives both
// sides to completion in the background, returning a channel closed once both
// slots have seen the terminal frame.
func startPvP(t *testing.T, ctx context.Context, brokerA, brokerB *mq.Broker, hubA, hubB *httpapi.Hub, st *store.Store, battleID string, t1, t2 []engine.TeamPick, seed uint64) <-chan struct{} {
	t.Helper()
	tr1, _ := st.UpsertTrainer(ctx, "Red")
	tr2, _ := st.UpsertTrainer(ctx, "Blue")
	if err := st.CreateBattle(ctx, store.Battle{
		ID: battleID, Mode: "live_pvp", Seed: int64(seed),
		P1Trainer: tr1, P2Trainer: tr2, P1Name: "Red", P2Name: "Blue",
		Winner: -1, Status: "open",
	}); err != nil {
		t.Fatalf("create battle %s: %v", battleID, err)
	}
	if err := brokerA.PublishLiveSession(ctx, messages.LiveSessionStart{
		BattleID: battleID, Mode: "live_pvp", Seed: seed,
		P1Name: "Red", P2Name: "Blue", Kinds: [2]string{"ws", "ws"},
	}); err != nil {
		t.Fatalf("publish session %s: %v", battleID, err)
	}

	_, framesA, err := hubA.SubscribeFrames(battleID, "p1")
	if err != nil {
		t.Fatalf("subscribe p1 %s: %v", battleID, err)
	}
	_, framesB, err := hubB.SubscribeFrames(battleID, "p2")
	if err != nil {
		t.Fatalf("subscribe p2 %s: %v", battleID, err)
	}

	publishAction(t, ctx, brokerA, battleID, "p1", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, brokerB, battleID, "p2", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, brokerA, battleID, "p1", messages.LivePhaseSubmit, t1, engine.Action{})
	publishAction(t, ctx, brokerB, battleID, "p2", messages.LivePhaseSubmit, t2, engine.Action{})

	doneA := make(chan struct{})
	doneB := make(chan struct{})
	go driveSide(ctx, brokerA, battleID, "p1", framesA, doneA)
	go driveSide(ctx, brokerB, battleID, "p2", framesB, doneB)

	both := make(chan struct{})
	go func() {
		<-doneA
		<-doneB
		close(both)
	}()
	return both
}

func TestConcurrent_OneOwnerRunsTwoBattles(t *testing.T) {
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

	// A single owner for both battles.
	brokerS := newBroker()
	defer brokerS.Close()
	svc := session.New(session.Config{
		InstanceID: "sess-concurrent", Dex: dex, Store: st, Cache: rc, Broker: brokerS,
	})
	go func() { _ = svc.Run(ctx) }()

	// Two gateways serve every slot of both battles (SubscribeFrames is keyed by
	// battle+slot, so one hub multiplexes many battles).
	brokerA, brokerB := newBroker(), newBroker()
	defer brokerA.Close()
	defer brokerB.Close()
	hubA := mustHub(t, brokerA)
	hubB := mustHub(t, brokerB)
	go func() { _ = hubA.Run(ctx) }()
	go func() { _ = hubB.Run(ctx) }()

	battle1, battle2 := uuid.NewString(), uuid.NewString()
	t1a, _ := pool.Pick(randSource(31))
	t1b, _ := pool.Pick(randSource(32))
	t2a, _ := pool.Pick(randSource(41))
	t2b, _ := pool.Pick(randSource(42))

	done1 := startPvP(t, ctx, brokerA, brokerB, hubA, hubB, st, battle1, t1a, t1b, 101)
	done2 := startPvP(t, ctx, brokerA, brokerB, hubA, hubB, st, battle2, t2a, t2b, 202)

	deadline := time.After(90 * time.Second)
	for _, d := range []<-chan struct{}{done1, done2} {
		select {
		case <-d:
		case <-deadline:
			t.Fatal("both battles did not complete within 90s under one owner")
		}
	}

	for _, id := range []string{battle1, battle2} {
		b, err := st.GetBattle(ctx, id)
		if err != nil {
			t.Fatalf("get battle %s: %v", id, err)
		}
		if b.Status != "completed" {
			t.Fatalf("battle %s status = %q, want completed", id, b.Status)
		}
		if b.Winner != 0 && b.Winner != 1 {
			t.Fatalf("battle %s winner = %d, want 0 or 1", id, b.Winner)
		}
	}
}
