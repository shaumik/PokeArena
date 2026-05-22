// Command ai-service consumes AI-decision jobs for live battles, runs the
// agent harness against the battle state in Redis, and publishes the chosen
// action. It scales independently of the gateway and the battle workers.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/config"
	"pokearena/internal/domain"
	"pokearena/internal/messages"
	"pokearena/internal/mq"
)

type aiService struct {
	dex    *domain.Dex
	cache  *cache.Cache
	broker *mq.Broker
	cfg    config.Config
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[ai-service] ")

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dex, err := domain.LoadDex(envOr("DATA_DIR", "data"), cfg.DataVersion)
	if err != nil {
		log.Fatalf("load dataset: %v", err)
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

	svc := &aiService{dex: dex, cache: rc, broker: broker, cfg: cfg}
	log.Printf("consuming %s (difficulty=%s)", messages.QueueAI, cfg.AIDifficulty)
	if err := broker.ConsumeJobs(ctx, messages.QueueAI, 1, svc.handle); err != nil && ctx.Err() == nil {
		log.Fatalf("consumer stopped: %v", err)
	}
}

// handle decides one action. AI jobs are time-sensitive: any failure is logged
// and the job is dropped (acked) rather than requeued — a stale decision is
// useless, and the gateway's own fallback covers a miss.
func (s *aiService) handle(ctx context.Context, body []byte) error {
	var job messages.AIJob
	if err := json.Unmarshal(body, &job); err != nil {
		log.Printf("dropping malformed job: %v", err)
		return nil
	}
	st, err := s.cache.LoadState(ctx, job.BattleID)
	if err != nil {
		log.Printf("job %s: battle state unavailable: %v", job.JobID, err)
		return nil
	}

	harness := ai.NewHarness(s.dex, job.Difficulty, s.cfg.AITimeBudget, s.cfg.AnthropicKey)
	action := harness.Decide(st, job.Side)

	if err := s.broker.PublishEvent(ctx, messages.EventAIDecided, job.BattleID, messages.AIDecided{
		JobID: job.JobID, BattleID: job.BattleID, Turn: job.Turn, Side: job.Side, Action: action,
	}); err != nil {
		log.Printf("job %s: publish failed: %v", job.JobID, err)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
