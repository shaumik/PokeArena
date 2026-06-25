//go:build integration

package session_test

// This is the headline test for the distribution work: a single live_pvp battle
// whose two players are bridged by *different* gateway instances plays to
// completion. It proves the property the old in-process coordinator could not
// hold — that ownership is a lease in a dedicated tier, not co-tenancy with
// whichever gateway a socket happened to land on.
//
// It needs real infra (Postgres, Redis, RabbitMQ) and so lives behind the
// `integration` build tag: a plain `go test ./...` never compiles it, while
// `make test-integration` brings the backends up and runs it. Under the tag a
// missing backend is a hard failure, not a skip. Each component gets its OWN
// broker connection so the per-process AppId self-publish filter behaves exactly
// as it does across real processes.

import (
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"testing"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/httpapi"
	"pokearena/internal/messages"
	"pokearena/internal/mq"
	"pokearena/internal/protocol"
	"pokearena/internal/session"
	"pokearena/internal/store"

	"github.com/google/uuid"
)

func randSource(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// dialInfra connects to the three backends. Returns a fresh broker each call so
// callers can give each simulated process its own sourceID. These tests only
// build under `-tags=integration`, where the backends are expected to be up
// (see `make test-integration`); a failed dial is therefore fatal, not a skip,
// so a misconfigured CI run fails loudly instead of going silently green.
func dialInfra(t *testing.T) (*store.Store, *cache.Cache, func() *mq.Broker) {
	t.Helper()
	// Short dial budget so a missing backend fails fast (the connect helpers
	// otherwise retry for ~30s). The returned broker factory uses the same
	// budget per call.
	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	st, err := store.New(dialCtx, env("DATABASE_URL", "postgres://pokearena:pokearena@localhost:5432/pokearena?sslmode=disable"))
	if err != nil {
		t.Fatalf("no Postgres: %v", err)
	}
	if err := st.Migrate(dialCtx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rc, err := cache.New(dialCtx, env("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		t.Fatalf("no Redis: %v", err)
	}
	newBroker := func() *mq.Broker {
		bctx, bcancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer bcancel()
		b, err := mq.Connect(bctx, env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"))
		if err != nil {
			t.Fatalf("no RabbitMQ: %v", err)
		}
		return b
	}
	return st, rc, newBroker
}

func TestDistribution_SocketsOnDifferentGatewaysCompleteOneBattle(t *testing.T) {
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
	t1, _ := pool.Pick(randSource(1))
	t2, _ := pool.Pick(randSource(2))

	// One battle-session owner (its own broker).
	brokerS := newBroker()
	defer brokerS.Close()
	svc := session.New(session.Config{
		InstanceID: "sess-1", Dex: dex, Store: st, Cache: rc, Broker: brokerS,
	})
	go func() { _ = svc.Run(ctx) }()

	// Two independent gateways, each its own broker + Hub.
	brokerA, brokerB := newBroker(), newBroker()
	defer brokerA.Close()
	defer brokerB.Close()
	hubA := mustHub(t, brokerA)
	hubB := mustHub(t, brokerB)
	go func() { _ = hubA.Run(ctx) }()
	go func() { _ = hubB.Run(ctx) }()

	// Create the battle row the session will advance + complete.
	battleID := uuid.NewString()
	seed := uint64(20260620)
	tr1, _ := st.UpsertTrainer(ctx, "Red")
	tr2, _ := st.UpsertTrainer(ctx, "Blue")
	if err := st.CreateBattle(ctx, store.Battle{
		ID: battleID, Mode: "live_pvp", Seed: int64(seed),
		P1Trainer: tr1, P2Trainer: tr2, P1Name: "Red", P2Name: "Blue",
		Winner: -1, Status: "open",
	}); err != nil {
		t.Fatalf("create battle: %v", err)
	}

	// Gateway A publishes the session-start job (as the POST handler would).
	if err := brokerA.PublishLiveSession(ctx, messages.LiveSessionStart{
		BattleID: battleID, Mode: "live_pvp", Seed: seed,
		P1Name: "Red", P2Name: "Blue", Kinds: [2]string{"ws", "ws"},
	}); err != nil {
		t.Fatalf("publish session: %v", err)
	}

	// p1 is bridged by gateway A; p2 by gateway B — the whole point.
	_, framesA, err := hubA.SubscribeFrames(battleID, "p1")
	if err != nil {
		t.Fatalf("subscribe p1 frames: %v", err)
	}
	_, framesB, err := hubB.SubscribeFrames(battleID, "p2")
	if err != nil {
		t.Fatalf("subscribe p2 frames: %v", err)
	}

	// Each bridge announces attach and submits its team.
	publishAction(t, ctx, brokerA, battleID, "p1", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, brokerB, battleID, "p2", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, brokerA, battleID, "p1", messages.LivePhaseSubmit, t1, engine.Action{})
	publishAction(t, ctx, brokerB, battleID, "p2", messages.LivePhaseSubmit, t2, engine.Action{})

	// Drive each side to completion from its own gateway/broker.
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
			t.Fatal("battle did not complete within 60s across two gateways")
		}
	}

	// The authoritative record was written by the session owner.
	b, err := st.GetBattle(ctx, battleID)
	if err != nil {
		t.Fatalf("get battle: %v", err)
	}
	if b.Status != "completed" {
		t.Fatalf("battle status = %q, want completed", b.Status)
	}
	if b.Winner != 0 && b.Winner != 1 {
		t.Fatalf("winner = %d, want 0 or 1", b.Winner)
	}
}

func mustHub(t *testing.T, b *mq.Broker) *httpapi.Hub {
	t.Helper()
	eq, err := b.NewEventQueue()
	if err != nil {
		t.Fatalf("event queue: %v", err)
	}
	return httpapi.NewHub(eq)
}

func publishAction(t *testing.T, ctx context.Context, b *mq.Broker, battleID, slot, phase string, picks []engine.TeamPick, act engine.Action) {
	t.Helper()
	if err := b.PublishLiveAction(ctx, messages.LiveAction{
		BattleID: battleID, Slot: slot, Phase: phase, Picks: picks, Action: act,
	}); err != nil {
		t.Fatalf("publish action: %v", err)
	}
}

// driveSide acts as one honest WS client: it reads its slot's frames and replies
// with a legal action each time it's asked, until the battle ends.
func driveSide(ctx context.Context, b *mq.Broker, battleID, slot string, frames <-chan []byte, done chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case body, ok := <-frames:
			if !ok {
				return
			}
			var u protocol.MatchUpdate
			if json.Unmarshal(body, &u) != nil {
				continue
			}
			switch u.Type {
			case protocol.FrameState, protocol.FrameTurn:
				if u.View == nil {
					continue
				}
				_ = b.PublishLiveAction(ctx, messages.LiveAction{
					BattleID: battleID, Slot: slot, Turn: u.View.Turn,
					Phase: messages.LivePhaseAction, Action: legalAction(u.View),
				})
			case protocol.FrameEnd:
				close(done)
				return
			}
		}
	}
}

// legalAction is an honest client: it picks from the actual legal-action set the
// engine would accept for this view — the same ai.LegalActions a production agent
// uses — so it never submits a move that's out of PP, disabled, choice-locked, or
// otherwise restricted (any of which the engine rejects, stalling the turn loop).
// Among legal moves it prefers the one with the most remaining PP so a long
// battle doesn't needlessly burn down to Struggle; if only a switch (or Struggle)
// is legal, it takes the first legal action.
func legalAction(v *ai.View) engine.Action {
	legal := ai.LegalActions(*v)
	if len(legal) == 0 {
		return engine.Action{Kind: engine.ActionMove, Index: -1} // Struggle; should never happen
	}
	best, bestPP := legal[0], -1
	for _, a := range legal {
		if a.Kind == engine.ActionMove && a.Index >= 0 {
			if pp := v.Self.Team[v.Self.Active].Moves[a.Index].PP; pp > bestPP {
				best, bestPP = a, pp
			}
		}
	}
	return best
}
