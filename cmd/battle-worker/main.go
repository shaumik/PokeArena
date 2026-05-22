// Command battle-worker consumes quicksim jobs and simulates whole AI-vs-AI
// battles. It is a competing consumer — throughput scales by running more
// replicas.
package main

import (
	"context"
	"encoding/json"
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
	st, err := engine.NewBattle(w.dex, job.BattleID, job.P1Name, job.P1Team, job.P2Name, job.P2Team, job.Seed)
	if err != nil {
		log.Printf("dropping invalid battle %s: %v", job.BattleID, err)
		_ = w.store.SetBattleStatus(ctx, job.BattleID, "failed")
		return nil
	}
	if err := w.simulate(ctx, st, job); err != nil {
		log.Printf("battle %s failed: %v", job.BattleID, err)
		return err // transient — requeue (re-simulation is deterministic and idempotent)
	}
	log.Printf("battle %s done: winner=%d turns=%d", job.BattleID, st.Winner, st.Turn)
	return nil
}

// simulate runs the battle to completion, persisting each turn and emitting a
// turn-resolved event for spectators.
func (w *worker) simulate(ctx context.Context, st *engine.BattleState, job messages.QuickSimJob) error {
	if err := w.store.SetBattleStatus(ctx, st.ID, "running"); err != nil {
		return err
	}
	_ = w.broker.PublishEvent(ctx, messages.EventBattleStarted, st.ID, messages.BattleStarted{BattleID: st.ID})

	agents := [2]*ai.Harness{
		ai.NewHarness(w.dex, job.P1Difficulty, w.budget, ""), // quicksim never uses the LLM
		ai.NewHarness(w.dex, job.P2Difficulty, w.budget, ""),
	}

	for !st.Ended() {
		turnLog := engine.ResolveTurn(w.dex, st,
			[2]engine.Action{agents[0].Decide(st, 0), agents[1].Decide(st, 1)})

		if st.Phase == engine.PhaseReplace {
			var sw [2]*engine.Action
			for i := 0; i < 2; i++ {
				if st.Replace[i] {
					a := agents[i].Decide(st, i)
					sw[i] = &a
				}
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
