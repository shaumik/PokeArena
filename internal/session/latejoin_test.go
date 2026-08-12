//go:build integration

package session_test

// Spectator late join. The spectator test subscribes before the battle starts;
// this one subscribes after it is already mid-flight, the way a real viewer who
// opens the page partway through does. The hub binds the battle's event routing
// key on first subscribe, so a late watcher must still pick up the remaining
// turns and the terminal battle-completed — proving the fan-out binds on demand
// rather than only at battle creation.

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

func TestSpectator_LateJoinStillSeesCompletion(t *testing.T) {
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
	t1, _ := pool.Pick(randSource(3))
	t2, _ := pool.Pick(randSource(4))

	brokerS := newBroker()
	defer brokerS.Close()
	svc := session.New(session.Config{
		InstanceID: "sess-latejoin", Dex: dex, Store: st, Cache: rc, Broker: brokerS,
	})
	defer startSession(ctx, svc)()

	brokerA, brokerB := newBroker(), newBroker()
	defer brokerA.Close()
	defer brokerB.Close()
	hubA := mustHub(t, brokerA)
	hubB := mustHub(t, brokerB)
	go func() { _ = hubA.Run(ctx) }()
	go func() { _ = hubB.Run(ctx) }()

	brokerSpec := newBroker()
	defer brokerSpec.Close()
	specHub := mustHub(t, brokerSpec)
	go func() { _ = specHub.Run(ctx) }()

	battleID := uuid.NewString()
	seed := uint64(20260626)
	tr1, _ := st.UpsertTrainer(ctx, "Red")
	tr2, _ := st.UpsertTrainer(ctx, "Blue")
	if err := st.CreateBattle(ctx, store.Battle{
		ID: battleID, Mode: "live_pvp", Seed: int64(seed),
		P1Trainer: tr1, P2Trainer: tr2, P1Name: "Red", P2Name: "Blue",
		Winner: -1, Status: "open",
	}); err != nil {
		t.Fatalf("create battle: %v", err)
	}

	if err := brokerA.PublishLiveSession(ctx, messages.LiveSessionStart{
		BattleID: battleID, Mode: "live_pvp", Seed: seed,
		P1Name: "Red", P2Name: "Blue", Kinds: [2]string{"ws", "ws"},
	}); err != nil {
		t.Fatalf("publish session: %v", err)
	}

	_, framesA, err := hubA.SubscribeFrames(battleID, "p1")
	if err != nil {
		t.Fatalf("subscribe p1 frames: %v", err)
	}
	_, framesB, err := hubB.SubscribeFrames(battleID, "p2")
	if err != nil {
		t.Fatalf("subscribe p2 frames: %v", err)
	}

	publishAction(t, ctx, brokerA, battleID, "p1", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, brokerB, battleID, "p2", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, brokerA, battleID, "p1", messages.LivePhaseSubmit, t1, engine.Action{})
	publishAction(t, ctx, brokerB, battleID, "p2", messages.LivePhaseSubmit, t2, engine.Action{})

	doneA := make(chan struct{})
	doneB := make(chan struct{})
	go driveSide(ctx, brokerA, battleID, "p1", framesA, doneA)
	go driveSide(ctx, brokerB, battleID, "p2", framesB, doneB)

	// Join only once the battle is genuinely under way (a turn already resolved),
	// then start watching — the bind must catch the turns still to come.
	if !waitFor(ctx, 15*time.Second, func() bool {
		s, err := rc.LoadState(ctx, battleID)
		return err == nil && s.Turn >= 1
	}) {
		t.Fatal("battle never reached turn 1")
	}
	_, events, err := specHub.Subscribe(battleID)
	if err != nil {
		t.Fatalf("late spectator subscribe: %v", err)
	}
	w := &specWatch{seen: map[string]int{}}
	go w.collect(ctx, events)

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
			t.Fatal("battle did not complete within 60s")
		}
	}

	if !waitFor(ctx, 10*time.Second, func() bool {
		_, ok := w.completedWinner()
		return ok
	}) {
		t.Fatal("late spectator never received battle-completed")
	}
	if n := w.count(messages.EventTurnResolved); n == 0 {
		t.Fatal("late spectator saw no turn-resolved events after joining")
	}
	// 2 is a draw — reachable via a simultaneous double-KO.
	if win, _ := w.completedWinner(); win < 0 || win > 2 {
		t.Fatalf("late spectator saw winner = %d, want a resolved result (0, 1, or 2 for a draw)", win)
	}
}
