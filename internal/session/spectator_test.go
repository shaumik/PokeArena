//go:build integration

package session_test

// Spectator fan-out: a third gateway that holds no player socket subscribes to a
// battle's domain events and must watch it play out — turn-resolved frames and a
// terminal battle-completed carrying the winner. This is the read path behind the
// SSE/WS spectator endpoint, and it rides the same broker event topic the players
// never touch, so it proves the result reaches a watcher independently of either
// player's frame channel.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/httpapi"
	"pokearena/internal/messages"
	"pokearena/internal/session"
	"pokearena/internal/store"
)

// specWatch records the domain events a spectator receives. A slow watcher must
// never stall the hub (it drops on a full channel), so we assert on what arrived,
// not on an exact sequence.
type specWatch struct {
	mu     sync.Mutex
	seen   map[string]int
	winner int
	gotWin bool
}

func (w *specWatch) collect(ctx context.Context, events <-chan httpapi.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			w.mu.Lock()
			w.seen[ev.Type]++
			if ev.Type == messages.EventBattleCompleted {
				var bc messages.BattleCompleted
				if json.Unmarshal(ev.Body, &bc) == nil {
					w.winner, w.gotWin = bc.Winner, true
				}
			}
			w.mu.Unlock()
		}
	}
}

func (w *specWatch) count(eventType string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seen[eventType]
}

func (w *specWatch) completedWinner() (int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.winner, w.gotWin
}

func TestSpectator_WatchesLiveBattleToCompletion(t *testing.T) {
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

	// One battle-session owner.
	brokerS := newBroker()
	defer brokerS.Close()
	svc := session.New(session.Config{
		InstanceID: "sess-spec", Dex: dex, Store: st, Cache: rc, Broker: brokerS,
	})
	go func() { _ = svc.Run(ctx) }()

	// Two gateways bridge the players (as in the distribution test).
	brokerA, brokerB := newBroker(), newBroker()
	defer brokerA.Close()
	defer brokerB.Close()
	hubA := mustHub(t, brokerA)
	hubB := mustHub(t, brokerB)
	go func() { _ = hubA.Run(ctx) }()
	go func() { _ = hubB.Run(ctx) }()

	// A third gateway is the spectator: it watches domain events and bridges no
	// slot. Its own broker keeps it independent of the players' frame traffic.
	brokerSpec := newBroker()
	defer brokerSpec.Close()
	specHub := mustHub(t, brokerSpec)
	go func() { _ = specHub.Run(ctx) }()

	battleID := uuid.NewString()
	seed := uint64(20260624)
	tr1, _ := st.UpsertTrainer(ctx, "Red")
	tr2, _ := st.UpsertTrainer(ctx, "Blue")
	if err := st.CreateBattle(ctx, store.Battle{
		ID: battleID, Mode: "live_pvp", Seed: int64(seed),
		P1Trainer: tr1, P2Trainer: tr2, P1Name: "Red", P2Name: "Blue",
		Winner: -1, Status: "open",
	}); err != nil {
		t.Fatalf("create battle: %v", err)
	}

	// Subscribe the spectator BEFORE the battle starts so the routing-key bind is
	// in place ahead of the first published event.
	_, events, err := specHub.Subscribe(battleID)
	if err != nil {
		t.Fatalf("spectator subscribe: %v", err)
	}
	w := &specWatch{seen: map[string]int{}}
	go w.collect(ctx, events)

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

	// The spectator must observe completion over its OWN event stream — the players'
	// frame channels are a separate path. Give the final event a moment to arrive.
	if !waitFor(ctx, 10*time.Second, func() bool {
		_, ok := w.completedWinner()
		return ok
	}) {
		t.Fatal("spectator never received a battle-completed event")
	}
	if n := w.count(messages.EventTurnResolved); n == 0 {
		t.Fatal("spectator received no turn-resolved events")
	}
	if win, _ := w.completedWinner(); win != 0 && win != 1 {
		t.Fatalf("spectator saw winner = %d, want 0 or 1", win)
	}
}
