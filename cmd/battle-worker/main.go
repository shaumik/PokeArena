// Command battle-worker consumes quicksim jobs and simulates whole AI-vs-AI
// battles. It is a competing consumer — throughput scales by running more
// replicas.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/config"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
	"pokearena/internal/mq"
	"pokearena/internal/store"
)

type worker struct {
	dex    *domain.Dex
	store  *store.Store
	broker *mq.Broker
	budget time.Duration
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[battle-worker] ")

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dex, err := domain.LoadDex(envOr("DATA_DIR", "data"), cfg.DataVersion)
	if err != nil {
		log.Fatalf("load dataset: %v", err)
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("apply schema: %v", err)
	}
	broker, err := mq.Connect(ctx, cfg.RabbitURL)
	if err != nil {
		log.Fatalf("connect rabbitmq: %v", err)
	}
	defer broker.Close()

	w := &worker{dex: dex, store: st, broker: broker, budget: cfg.AITimeBudget}
	log.Printf("consuming %s", messages.QueueQuickSim)
	if err := broker.ConsumeJobs(ctx, messages.QueueQuickSim, 1, w.handle); err != nil && ctx.Err() == nil {
		log.Fatalf("consumer stopped: %v", err)
	}
}

func (w *worker) handle(ctx context.Context, body []byte) error {
	var job messages.QuickSimJob
	if err := json.Unmarshal(body, &job); err != nil {
		log.Printf("dropping malformed job: %v", err)
		return nil // permanent error — do not requeue
	}
	a1 := ai.NewHarness(w.dex, w.budget)
	a2 := ai.NewHarness(w.dex, w.budget)
	// Prefer the explicit picks (custom movesets/abilities from the builder)
	// when both sides supplied them; otherwise build from the bare lineups
	// with default movesets. Mixing one of each would be surprising, so it's
	// all-or-nothing.
	var st *engine.BattleState
	var err error
	if len(job.P1Picks) > 0 && len(job.P2Picks) > 0 {
		st, err = engine.NewBattleFromPicks(w.dex, job.BattleID, job.P1Name, job.P1Picks, job.P2Name, job.P2Picks, job.Seed)
	} else {
		st, err = engine.NewBattle(w.dex, job.BattleID, job.P1Name, job.P1Team, job.P2Name, job.P2Team, job.Seed)
	}
	if err != nil {
		log.Printf("dropping invalid battle %s: %v", job.BattleID, err)
		_ = w.store.SetBattleStatus(ctx, job.BattleID, "failed")
		return nil
	}
	if err := w.simulate(ctx, st, [2]*ai.Harness{a1, a2}); err != nil {
		log.Printf("battle %s failed: %v", job.BattleID, err)
		return err // transient — requeue (re-simulation is deterministic and idempotent)
	}
	log.Printf("battle %s done: winner=%d turns=%d", job.BattleID, st.Winner, st.Turn)
	return nil
}

// simulate runs the battle to completion, persisting each turn and emitting a
// turn-resolved event for spectators.
func (w *worker) simulate(ctx context.Context, st *engine.BattleState, agents [2]*ai.Harness) error {
	if err := w.store.SetBattleStatus(ctx, st.ID, "running"); err != nil {
		return err
	}
	_ = w.broker.PublishEvent(ctx, messages.EventBattleStarted, st.ID, messages.BattleStarted{BattleID: st.ID})

	for !st.Ended() {
		turnLog := engine.ResolveTurn(w.dex, st,
			[2]engine.Action{agents[0].Decide(st, 0), agents[1].Decide(st, 1)})

		// A loop, not an if: a replacement can die to entry hazards on the way
		// in, leaving the battle in PhaseReplace with the side still owing one.
		// The old single pass fell through and asked for choosing actions in a
		// replace phase; it also called AppendTurn twice with the same turn
		// number, and the store's ON CONFLICT DO NOTHING silently dropped the
		// second round — so a battle could be persisted with a replay that ends
		// mid-turn and no win line.
		// ctx and a progress guard, because this loop can spin. ResolveReplace
		// silently ignores an sw[i] that is not an ActionSwitch, and updatePhase
		// then re-derives PhaseReplace from the same fainted active — identical
		// state, forever, pegging a core and never acking the job. The old
		// single-pass shape fell through to AppendTurn(ctx, ...) every turn, so
		// a canceled ctx broke it out; a loop has to check for itself.
		for st.Phase == engine.PhaseReplace && ctx.Err() == nil {
			var sw [2]*engine.Action
			progressed := false
			for i := 0; i < 2; i++ {
				if !st.Replace[i] {
					continue
				}
				a := agents[i].Decide(st, i)
				if a.Kind == engine.ActionSwitch {
					progressed = true
				}
				sw[i] = &a
			}
			if !progressed {
				// Nobody offered a switch, so the next round would be identical.
				// Bail loudly rather than spin: an agent that cannot answer a
				// replace is a bug worth surfacing, not one to hide in a loop.
				return fmt.Errorf("battle %s turn %d: replace phase made no progress "+
					"(sides owing: %v) — an agent returned a non-switch action",
					st.ID, st.Turn, st.Replace)
			}
			turnLog = append(turnLog, engine.ResolveReplace(st, sw)...)
		}

		logJSON, _ := json.Marshal(turnLog)
		stateJSON, _ := json.Marshal(st)
		if err := w.store.AppendTurn(ctx, st.ID, st.Turn, logJSON, stateJSON); err != nil {
			return err
		}
		_ = w.broker.PublishEvent(ctx, messages.EventTurnResolved, st.ID, messages.TurnResolved{
			BattleID: st.ID, Turn: st.Turn, Log: turnLog, State: st,
		})
	}

	if err := w.store.CompleteBattle(ctx, st.ID, st.Winner, st.Turn); err != nil {
		return err
	}
	return w.broker.PublishEvent(ctx, messages.EventBattleCompleted, st.ID, messages.BattleCompleted{
		BattleID: st.ID, Winner: st.Winner, TurnCount: st.Turn,
	})
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
