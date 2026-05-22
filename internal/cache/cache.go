// Package cache is the Redis layer. It holds only derived or ephemeral state:
// live battle state, the atomic turn-coordination hash, the leaderboard sorted
// set, and read-through caches. Everything here can be rebuilt from Postgres.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"pokearena/internal/engine"
)

const (
	stateTTL       = 3 * time.Hour
	turnTTL        = 3 * time.Hour
	leaderboardKey = "leaderboard"
)

// ErrNotFound is returned for a cache miss.
var ErrNotFound = errors.New("cache: not found")

// Cache wraps a Redis client.
type Cache struct {
	rdb *redis.Client
}

// New connects to Redis, retrying briefly so a service can start alongside it.
func New(ctx context.Context, url string) (*Cache, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	rdb := redis.NewClient(opt)
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		if err := rdb.Ping(ctx).Err(); err == nil {
			return &Cache{rdb: rdb}, nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("connect redis: %w", lastErr)
}

// Ping checks liveness.
func (c *Cache) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

// Close releases the client.
func (c *Cache) Close() error { return c.rdb.Close() }

// --- live battle state ---

func stateKey(id string) string { return "battle:" + id + ":state" }

// SaveState persists a live battle's state with a TTL.
func (c *Cache) SaveState(ctx context.Context, st *engine.BattleState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, stateKey(st.ID), b, stateTTL).Err()
}

// LoadState fetches a live battle's state. Returns ErrNotFound if it expired.
func (c *Cache) LoadState(ctx context.Context, id string) (*engine.BattleState, error) {
	b, err := c.rdb.Get(ctx, stateKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var st engine.BattleState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// DeleteState removes a live battle's state.
func (c *Cache) DeleteState(ctx context.Context, id string) error {
	return c.rdb.Del(ctx, stateKey(id)).Err()
}

// --- atomic turn coordination ---

func turnKey(id string, turn int) string {
	return fmt.Sprintf("battle:%s:turn:%d", id, turn)
}

// submitScript records one side's action and reports, atomically, whether the
// pair is now complete. Single-threaded Redis guarantees exactly one caller
// sees both sides present — that caller publishes the turn-resolution job.
var submitScript = redis.NewScript(`
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
if redis.call('HEXISTS', KEYS[1], '0') == 1 and redis.call('HEXISTS', KEYS[1], '1') == 1 then
  return 1
end
return 0
`)

// SubmitAction records a side's action for a turn and returns true when both
// sides have now submitted.
func (c *Cache) SubmitAction(ctx context.Context, battleID string, turn, side int, action engine.Action) (bool, error) {
	payload, err := json.Marshal(action)
	if err != nil {
		return false, err
	}
	n, err := submitScript.Run(ctx, c.rdb,
		[]string{turnKey(battleID, turn)},
		side, payload, int(turnTTL.Seconds())).Int()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// GetActions returns both submitted actions for a turn.
func (c *Cache) GetActions(ctx context.Context, battleID string, turn int) ([2]engine.Action, error) {
	var out [2]engine.Action
	m, err := c.rdb.HGetAll(ctx, turnKey(battleID, turn)).Result()
	if err != nil {
		return out, err
	}
	for k, v := range m {
		var a engine.Action
		if json.Unmarshal([]byte(v), &a) != nil {
			continue
		}
		switch k {
		case "0":
			out[0] = a
		case "1":
			out[1] = a
		}
	}
	return out, nil
}

// ClearTurn drops a resolved turn's coordination hash.
func (c *Cache) ClearTurn(ctx context.Context, battleID string, turn int) error {
	return c.rdb.Del(ctx, turnKey(battleID, turn)).Err()
}

// --- leaderboard sorted set ---

// RankEntry is one leaderboard row from the Redis sorted set.
type RankEntry struct {
	Name   string `json:"name"`
	Rating int    `json:"rating"`
}

// SetRating upserts a trainer's rating into the leaderboard sorted set.
func (c *Cache) SetRating(ctx context.Context, name string, rating int) error {
	return c.rdb.ZAdd(ctx, leaderboardKey, redis.Z{Score: float64(rating), Member: name}).Err()
}

// TopRatings returns the top n trainers by rating.
func (c *Cache) TopRatings(ctx context.Context, n int) ([]RankEntry, error) {
	zs, err := c.rdb.ZRevRangeWithScores(ctx, leaderboardKey, 0, int64(n-1)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]RankEntry, 0, len(zs))
	for _, z := range zs {
		name, _ := z.Member.(string)
		out = append(out, RankEntry{Name: name, Rating: int(z.Score)})
	}
	return out, nil
}

// --- read-through JSON cache ---

// SetJSON stores any value as JSON under key with a TTL.
func (c *Cache) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, key, b, ttl).Err()
}

// GetJSON loads a cached JSON value into v. Returns ErrNotFound on a miss.
func (c *Cache) GetJSON(ctx context.Context, key string, v any) error {
	b, err := c.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
