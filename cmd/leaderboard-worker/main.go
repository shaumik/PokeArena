// Command leaderboard-worker consumes battle-completed events and recomputes
// Elo ratings. It is an independent consumer: battles run whether or not it is
// up, and events queue durably until it recovers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os/signal"
	"syscall"

	"pokearena/internal/cache"
	"pokearena/internal/config"
	"pokearena/internal/messages"
	"pokearena/internal/mq"
	"pokearena/internal/store"

	"github.com/jackc/pgx/v5"
)

type leaderboard struct {
	store *store.Store
	cache *cache.Cache
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[leaderboard-worker] ")

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	lw := &leaderboard{store: st, cache: rc}
	log.Printf("consuming %s events", messages.EventBattleCompleted)
	err = broker.ConsumeEvents(ctx, messages.QueueLeaderboard,
		[]string{messages.EventBattleCompleted + ".*"}, 1, lw.handle)
	if err != nil && ctx.Err() == nil {
		log.Fatalf("consumer stopped: %v", err)
	}
}

func (l *leaderboard) handle(ctx context.Context, _ string, body []byte) error {
	var ev messages.BattleCompleted
	if err := json.Unmarshal(body, &ev); err != nil {
		log.Printf("dropping malformed event: %v", err)
		return nil
	}

	b, err := l.store.GetBattle(ctx, ev.BattleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // unknown battle — nothing to do
	}
	if err != nil {
		return err // transient — requeue
	}
	if b.Winner < 0 {
		return nil // not actually finished
	}

	// ApplyResult is idempotent: a redelivered event changes nothing.
	updates, err := l.store.ApplyResult(ctx, b.ID, b.P1Trainer, b.P2Trainer, b.Winner)
	if err != nil {
		return err
	}
	for _, u := range updates {
		if err := l.cache.SetRating(ctx, u.Name, u.Rating); err != nil {
			log.Printf("leaderboard cache update failed for %s: %v", u.Name, err)
		}
	}
	if len(updates) > 0 {
		log.Printf("battle %s: ratings updated %+v", ev.BattleID, updates)
	}
	return nil
}
