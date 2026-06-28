//go:build integration

package session_test

// Abandonment / failover-correctness tests.
//
// These exercise a subtle interaction the distribution work introduced: the
// failover scan reclaims any battle that is "running" in Postgres but unowned in
// Redis. A live battle that ENDS via a player disconnect leaves exactly that
// shape — the coordinator returns, the lease is released, but the row stays
// "running" and the state stays in Redis — so the scan resurrects a battle whose
// players are gone, into a coordinator that blocks forever.
//
// Needs real infra; built only under the `integration` tag.

import (
	"context"
	"errors"
	"testing"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
	"pokearena/internal/session"
	"pokearena/internal/store"

	"github.com/google/uuid"
)

// TestAbandon_DisconnectIsNotResurrected drives a live_pvp battle to ACTIVE, then
// disconnects a slot (ending the match the way a closed socket would). After the
// match ends, the failover scan must NOT resurrect the battle: it has no players
// to drive it and would block a coordinator goroutine + renew a lease forever.
//
// The fix is for an abandoned battle to be marked terminal (and its state
// cleared) so the scan no longer treats it as a reclaimable running battle.
func TestAbandon_DisconnectIsNotResurrected(t *testing.T) {
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

	// Short timings so the failover scan gets several chances to (mis)reclaim.
	brokerS := newBroker()
	defer brokerS.Close()
	svc := session.New(session.Config{
		InstanceID: "sess-abandon", Dex: dex, Store: st, Cache: rc, Broker: brokerS,
		LeaseTTL: 6 * time.Second, LeaseRenew: 2 * time.Second, ScanInterval: 1 * time.Second,
		// Short grace so the disconnect abandons within the test window; the
		// production default (DefaultDisconnectGrace) would outlast the assertion.
		DisconnectGrace: 100 * time.Millisecond,
	})
	defer startSession(ctx, svc)()

	battleID := uuid.NewString()
	tr1, _ := st.UpsertTrainer(ctx, "Red")
	tr2, _ := st.UpsertTrainer(ctx, "Blue")
	if err := st.CreateBattle(ctx, store.Battle{
		ID: battleID, Mode: "live_pvp", Seed: 99,
		P1Trainer: tr1, P2Trainer: tr2, P1Name: "Red", P2Name: "Blue",
		Winner: -1, Status: "open",
	}); err != nil {
		t.Fatalf("create battle: %v", err)
	}

	pub := newBroker()
	defer pub.Close()
	if err := pub.PublishLiveSession(ctx, messages.LiveSessionStart{
		BattleID: battleID, Mode: "live_pvp", Seed: 99,
		P1Name: "Red", P2Name: "Blue", Kinds: [2]string{"ws", "ws"},
	}); err != nil {
		t.Fatalf("publish session: %v", err)
	}

	// Wait until the session owns the battle (its action consumer is up), then
	// settle briefly so the durable action queue is declared+bound before we
	// publish onto it.
	if !waitFor(ctx, 5*time.Second, func() bool {
		_, err := rc.GetBattleOwner(ctx, battleID)
		return err == nil
	}) {
		t.Fatal("session never claimed ownership")
	}
	time.Sleep(800 * time.Millisecond)

	// Both slots attach and submit, driving the room to ACTIVE ("running").
	publishAction(t, ctx, pub, battleID, "p1", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, pub, battleID, "p2", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, pub, battleID, "p1", messages.LivePhaseSubmit, t1, engine.Action{})
	publishAction(t, ctx, pub, battleID, "p2", messages.LivePhaseSubmit, t2, engine.Action{})

	if !waitFor(ctx, 15*time.Second, func() bool {
		b, err := st.GetBattle(ctx, battleID)
		return err == nil && b.Status == "running"
	}) {
		t.Fatal("battle never reached running")
	}
	// State is persisted for the running battle.
	if _, err := rc.LoadState(ctx, battleID); err != nil {
		t.Fatalf("running battle has no persisted state: %v", err)
	}

	// Player p1's socket drops. The match ends.
	publishAction(t, ctx, pub, battleID, "p1", messages.LivePhaseDisconnect, nil, engine.Action{})

	// Give the coordinator time to wind down AND the failover scan several ticks
	// to (mis)reclaim the now-abandoned battle.
	time.Sleep(5 * time.Second)

	// The abandoned battle must NOT be owned by anyone — a live owner here means
	// the scan resurrected it into a coordinator that will block forever.
	if owner, err := rc.GetBattleOwner(ctx, battleID); err == nil {
		t.Fatalf("abandoned battle was resurrected — still owned by %q; the failover scan reclaimed a battle with no players", owner)
	} else if !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("unexpected owner lookup error: %v", err)
	}

	// And the row must be terminal, not left "running" forever.
	b, err := st.GetBattle(ctx, battleID)
	if err != nil {
		t.Fatalf("get battle: %v", err)
	}
	if b.Status == "running" {
		t.Fatalf("abandoned battle left in status %q — never marked terminal", b.Status)
	}
}

// waitFor polls cond every 100ms until it is true or the timeout elapses.
func waitFor(ctx context.Context, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
	return cond()
}
