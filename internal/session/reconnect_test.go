//go:build integration

package session_test

// Reconnect within the grace window. The abandon test covers the other half — a
// disconnect with no return is made terminal and not resurrected — so this pins
// the resume side: a slot whose socket blips and re-attaches under a fresh
// connection id (the real gateway mints one per WS) before the grace timer fires
// must NOT abandon the battle. The two together describe the disconnect-grace
// branch end to end.
//
// To make the assertion sharp and independent of how long a given battle happens
// to run, we FREEZE the battle mid-turn (stop both drivers) before the blip. A
// frozen battle cannot end on its own, so the only thing that could retire it
// inside the window is the grace timer. If the re-attach cancels that timer the
// battle is still running and owned a comfortable margin past the grace deadline;
// if it did not, the timer would have fired, ended the coordinator, released the
// lease, and marked the row terminal. We assert the battle survived.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/messages"
	"github.com/shaumik/PokeArena/internal/mq"
	"github.com/shaumik/PokeArena/internal/session"
	"github.com/shaumik/PokeArena/internal/store"
)

// publishConn announces a connection-scoped phase (attach/disconnect) for a slot.
// Unlike publishAction, it stamps Conn so the coordinator's reconnect identity —
// "ignore a stale disconnect, cancel grace on the same slot's new id" — is
// actually exercised rather than bypassed by the empty-id legacy path.
func publishConn(t *testing.T, ctx context.Context, b *mq.Broker, battleID, slot, phase, conn string) {
	t.Helper()
	if err := b.PublishLiveAction(ctx, messages.LiveAction{
		BattleID: battleID, Slot: slot, Phase: phase, Conn: conn,
	}); err != nil {
		t.Fatalf("publish %s (%s): %v", phase, conn, err)
	}
}

func TestReconnect_WithinGraceResumesBattle(t *testing.T) {
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
	t1, _ := pool.Pick(randSource(7))
	t2, _ := pool.Pick(randSource(8))

	// A short grace keeps the test quick: armed at the disconnect, it would retire
	// the frozen battle 1s later unless the re-attach cancels it first.
	const grace = 1 * time.Second
	brokerS := newBroker()
	defer brokerS.Close()
	svc := session.New(session.Config{
		InstanceID: "sess-reconnect", Dex: dex, Store: st, Cache: rc, Broker: brokerS,
		DisconnectGrace: grace,
	})
	stopSvc := startSession(ctx, svc)
	defer stopSvc()

	brokerA, brokerB := newBroker(), newBroker()
	defer brokerA.Close()
	defer brokerB.Close()
	hubA := mustHub(t, brokerA)
	hubB := mustHub(t, brokerB)
	go func() { _ = hubA.Run(ctx) }()
	go func() { _ = hubB.Run(ctx) }()

	battleID := uuid.NewString()
	seed := uint64(20260625)
	tr1, _ := st.UpsertTrainer(ctx, "Red")
	tr2, _ := st.UpsertTrainer(ctx, "Blue")
	if err := st.CreateBattle(ctx, store.Battle{
		ID: battleID, Mode: "live_pvp", Seed: int64(seed),
		P1Trainer: tr1, P2Trainer: tr2, P1Name: "Red", P2Name: "Blue",
		Winner: -1, Status: "open",
	}); err != nil {
		t.Fatalf("create battle: %v", err)
	}

	// This test deliberately leaves its battle frozen and "running". Before the
	// test ends, stop the owner and then finalize the row + drop the cached state,
	// so it doesn't linger as a "running" orphan that a later test's failover scan
	// would reclaim (cross-test interference). Runs first (LIFO) — before the early
	// defer stopSvc() / broker closes — and stops the owner itself so its yield
	// can't re-persist what we clear.
	defer func() {
		stopSvc()
		cctx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		_ = st.SetBattleStatus(cctx, battleID, "abandoned")
		_ = rc.DeleteState(cctx, battleID)
	}()

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

	// p1 attaches under a known connection id so the disconnect below matches it
	// (the strict-identity path, not the empty-id legacy one).
	const p1Conn1 = "conn-1"
	publishConn(t, ctx, brokerA, battleID, "p1", messages.LivePhaseAttach, p1Conn1)
	publishAction(t, ctx, brokerB, battleID, "p2", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, brokerA, battleID, "p1", messages.LivePhaseSubmit, t1, engine.Action{})
	publishAction(t, ctx, brokerB, battleID, "p2", messages.LivePhaseSubmit, t2, engine.Action{})

	// Drive both sides until the battle is genuinely mid-flight, each under its own
	// context so we can stop them to freeze the battle.
	ctxA, stopA := context.WithCancel(ctx)
	ctxB, stopB := context.WithCancel(ctx)
	go driveSide(ctxA, brokerA, battleID, "p1", framesA, make(chan struct{}, 1))
	go driveSide(ctxB, brokerB, battleID, "p2", framesB, make(chan struct{}, 1))

	if !waitFor(ctx, 15*time.Second, func() bool {
		s, err := rc.LoadState(ctx, battleID)
		return err == nil && s.Turn >= 1
	}) {
		t.Fatal("battle never reached turn 1")
	}

	// Freeze: with no driver replying, the coordinator parks waiting on actions, so
	// the battle stays "running" and owned until something retires it.
	stopA()
	stopB()
	if owner, err := rc.GetBattleOwner(ctx, battleID); err != nil {
		t.Fatalf("battle not owned while running: %v", err)
	} else if owner == "" {
		t.Fatal("battle has no owner while running")
	}

	// p1's socket drops (arms the grace), then re-attaches well within the window
	// under a NEW connection id (cancels the grace). FIFO on the per-battle action
	// queue guarantees the owner sees disconnect-then-reconnect in that order.
	publishConn(t, ctx, brokerA, battleID, "p1", messages.LivePhaseDisconnect, p1Conn1)
	time.Sleep(150 * time.Millisecond)
	publishConn(t, ctx, brokerA, battleID, "p1", messages.LivePhaseAttach, "conn-2")

	// Wait comfortably past the grace deadline (measured from the disconnect). A
	// canceled timer never fires; an uncanceled one would have retired the battle
	// at ~grace, releasing the lease and marking the row terminal.
	time.Sleep(grace + 600*time.Millisecond)

	if owner, err := rc.GetBattleOwner(ctx, battleID); err != nil {
		t.Fatalf("battle was abandoned despite a within-grace reconnect (owner lookup: %v)", err)
	} else if owner == "" {
		t.Fatal("battle lost its owner despite a within-grace reconnect")
	}
	b, err := st.GetBattle(ctx, battleID)
	if err != nil {
		t.Fatalf("get battle: %v", err)
	}
	if b.Status != "running" {
		t.Fatalf("battle status = %q, want running — a within-grace reconnect must not retire it", b.Status)
	}
}
