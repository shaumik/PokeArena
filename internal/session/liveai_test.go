//go:build integration

package session_test

// Live mode (human vs in-process AI) across the real stack. Unlike the live_pvp
// tests, one slot is driven by the coordinator's own AIDecider rather than a
// remote socket — so this is the only full-infra test that exercises the AI side
// wiring (session.Config.AI, the "ai" kind, the live-mode auto-submit). The AI is
// deterministic and self-contained (no LLM, no ai-service), so the battle plays
// to a definite winner every run.

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

// legalAIDecider plays the in-process AI side with the engine's first legal
// action. The coordinator hands it the real BattleState, so first-legal is enough
// to drive any battle to completion — the same property the in-memory coordinator
// test relies on, here with the production session wiring.
type legalAIDecider struct{}

func (legalAIDecider) Start(context.Context) {}
func (legalAIDecider) Decide(_ context.Context, st *engine.BattleState, side int) (engine.Action, string) {
	return engine.LegalActions(st, side)[0], ""
}

func (legalAIDecider) DecideReplace(_ context.Context, st *engine.BattleState, side int) engine.Action {
	return engine.LegalActions(st, side)[0]
}

func TestLive_WSVersusInProcessAI_Completes(t *testing.T) {
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
	human, _ := pool.Pick(randSource(5))
	aiTeam, _ := pool.Pick(randSource(6))

	// The session owner wires the deterministic in-process AI for the "ai" slot.
	brokerS := newBroker()
	defer brokerS.Close()
	svc := session.New(session.Config{
		InstanceID: "sess-live", Dex: dex, Store: st, Cache: rc, Broker: brokerS,
		AI: legalAIDecider{},
	})
	defer startSession(ctx, svc)()

	// One gateway bridges the lone human slot (p1). p2 is AI and never bridges.
	brokerA := newBroker()
	defer brokerA.Close()
	hubA := mustHub(t, brokerA)
	go func() { _ = hubA.Run(ctx) }()

	battleID := uuid.NewString()
	seed := uint64(31415926)
	tr1, _ := st.UpsertTrainer(ctx, "Red")
	tr2, _ := st.UpsertTrainer(ctx, "CPU")
	if err := st.CreateBattle(ctx, store.Battle{
		ID: battleID, Mode: "live", Seed: int64(seed),
		P1Trainer: tr1, P2Trainer: tr2, P1Name: "Red", P2Name: "CPU",
		Winner: -1, Status: "open",
	}); err != nil {
		t.Fatalf("create battle: %v", err)
	}

	if err := brokerA.PublishLiveSession(ctx, messages.LiveSessionStart{
		BattleID: battleID, Mode: "live", Seed: seed,
		P1Name: "Red", P2Name: "CPU", Kinds: [2]string{"ws", "ai"}, AITeam: aiTeam,
	}); err != nil {
		t.Fatalf("publish session: %v", err)
	}

	// Settle before driving: wait until the owner has claimed the battle and let
	// its per-battle action queue finish binding. The first attach/submit we
	// publish lands on a durable queue; if it races ahead of the bind it is lost,
	// and recovery then waits a full 20s steady-state resync — long enough to look
	// like a stall. The short pause closes that window so the run starts promptly.
	if !waitFor(ctx, 5*time.Second, func() bool {
		_, e := rc.GetBattleOwner(ctx, battleID)
		return e == nil
	}) {
		t.Fatal("session never claimed ownership")
	}
	time.Sleep(300 * time.Millisecond)

	_, framesA, err := hubA.SubscribeFrames(battleID, "p1")
	if err != nil {
		t.Fatalf("subscribe p1 frames: %v", err)
	}

	// Only the human attaches and submits; the AI slot auto-submits its roster.
	publishAction(t, ctx, brokerA, battleID, "p1", messages.LivePhaseAttach, nil, engine.Action{})
	publishAction(t, ctx, brokerA, battleID, "p1", messages.LivePhaseSubmit, human, engine.Action{})

	done := make(chan struct{})
	go driveSide(ctx, brokerA, battleID, "p1", framesA, done)

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("ws-vs-AI battle did not complete within 60s")
	}

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
