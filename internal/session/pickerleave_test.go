//go:build integration

package session_test

// Picker-room abandonment. The abandon test covers a slot dropping mid-battle
// (after "running"); this covers the OPEN phase, before either side has locked
// in: one player joins the room and leaves (a leave_room is just a WS close) while
// the other is still picking. The room must die, the row must be finalized as
// abandoned — never "running" — and the failover scan must leave it alone, since
// a battle that never started has no persisted state to reclaim.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/cache"
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/messages"
	"github.com/shaumik/PokeArena/internal/session"
	"github.com/shaumik/PokeArena/internal/store"
)

func TestPickerLeave_RoomAbandonedNotResurrected(t *testing.T) {
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

	// Short timings so the leave abandons quickly and the scan gets several ticks
	// to (wrongly) try to reclaim the dead room.
	brokerS := newBroker()
	defer brokerS.Close()
	svc := session.New(session.Config{
		InstanceID: "sess-pickerleave", Dex: dex, Store: st, Cache: rc, Broker: brokerS,
		LeaseTTL: 6 * time.Second, LeaseRenew: 2 * time.Second, ScanInterval: 1 * time.Second,
		DisconnectGrace: 100 * time.Millisecond,
	})
	defer startSession(ctx, svc)()

	battleID := uuid.NewString()
	tr1, _ := st.UpsertTrainer(ctx, "Red")
	tr2, _ := st.UpsertTrainer(ctx, "Blue")
	if err := st.CreateBattle(ctx, store.Battle{
		ID: battleID, Mode: "live_pvp", Seed: 7,
		P1Trainer: tr1, P2Trainer: tr2, P1Name: "Red", P2Name: "Blue",
		Winner: -1, Status: "open",
	}); err != nil {
		t.Fatalf("create battle: %v", err)
	}

	pub := newBroker()
	defer pub.Close()
	if err := pub.PublishLiveSession(ctx, messages.LiveSessionStart{
		BattleID: battleID, Mode: "live_pvp", Seed: 7,
		P1Name: "Red", P2Name: "Blue", Kinds: [2]string{"ws", "ws"},
	}); err != nil {
		t.Fatalf("publish session: %v", err)
	}

	// Wait until the owner has the battle and its action queue is bound.
	if !waitFor(ctx, 5*time.Second, func() bool {
		_, err := rc.GetBattleOwner(ctx, battleID)
		return err == nil
	}) {
		t.Fatal("session never claimed ownership")
	}
	time.Sleep(800 * time.Millisecond)

	// Both players show up in the room; p1 locks in a team, p2 bails before
	// submitting (the gateway turns a leave_room / closed socket into a disconnect).
	publishAction(t, ctx, pub, battleID, "p1", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, pub, battleID, "p2", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, pub, battleID, "p1", messages.LivePhaseSubmit, t1, engine.Action{})
	publishAction(t, ctx, pub, battleID, "p2", messages.LivePhaseDisconnect, nil, engine.Action{})

	// The room dies: ownership is released and the row is finalized.
	if !waitFor(ctx, 10*time.Second, func() bool {
		_, err := rc.GetBattleOwner(ctx, battleID)
		return errors.Is(err, cache.ErrNotFound)
	}) {
		t.Fatal("room was never abandoned — battle is still owned after a picker-room leave")
	}
	b, err := st.GetBattle(ctx, battleID)
	if err != nil {
		t.Fatalf("get battle: %v", err)
	}
	if b.Status == "open" || b.Status == "running" {
		t.Fatalf("abandoned room left in status %q — never finalized", b.Status)
	}

	// And it stays dead: several scan ticks must not resurrect a room with no
	// players and no persisted state.
	time.Sleep(4 * time.Second)
	if owner, err := rc.GetBattleOwner(ctx, battleID); err == nil {
		t.Fatalf("dead room was resurrected — now owned by %q", owner)
	} else if !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("unexpected owner lookup error: %v", err)
	}
}
