// Command battle-session owns the coordinator for live battles (mode=live and
// mode=live_pvp). It is the dedicated tier the gateway hands a battle off to,
// mirroring how battle-worker owns Quick Sim execution. The orchestration lives
// in internal/session; this is the wiring.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/config"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/mq"
	"pokearena/internal/session"
	"pokearena/internal/store"

	"github.com/google/uuid"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[battle-session] ")

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
	rc, err := cache.New(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("connect redis: %v", err)
	}
	defer rc.Close()
	broker, err := mq.Connect(ctx, cfg.RabbitURL)
	if err != nil {
		log.Fatalf("connect rabbitmq: %v", err)
	}
	defer broker.Close()

	// The live opponent is the heuristic — our strongest programmatic bot (it
	// outranks every expectimax depth in the mirror round-robin) and
	// deterministic given the view. There is no knob: one canonical opponent.
	svc := session.New(session.Config{
		InstanceID: uuid.NewString(),
		Dex:        dex,
		Store:      st,
		Cache:      rc,
		Broker:     broker,
		AI:         &harnessAI{h: ai.NewHeuristicHarness(dex, cfg.AITimeBudget)},
	})
	if err := svc.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("session consumer stopped: %v", err)
	}
}

// harnessAI implements livebattle.AIDecider in-process, running the agent
// harness directly (like battle-worker) rather than offloading to ai-service.
// For a dedicated worker tier this is strictly better than a per-turn broker
// round-trip: lower latency, no correlation bookkeeping, and the harness's own
// time budget guarantees the turn never stalls.
type harnessAI struct{ h *ai.Harness }

func (a *harnessAI) Start(context.Context) {}
func (a *harnessAI) Decide(_ context.Context, st *engine.BattleState, side int) (engine.Action, string) {
	return a.h.Decide(st, side), ""
}

func (a *harnessAI) DecideReplace(_ context.Context, st *engine.BattleState, side int) engine.Action {
	return a.h.Decide(st, side)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
