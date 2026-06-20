package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Ownership lease for live battles.
//
// Exactly one battle-session instance coordinates a given live battle at a time
// (single-owner-per-entity — that's what keeps the turn loop lock-free). The
// work-queue election picks the first owner; this lease is the durable source of
// truth for "who owns this battle right now" and the hook for failover: when an
// owner dies, its lease expires and another instance can take over.
//
// Keys are "pvp:owner:{battleID}" holding the owning instance id, with a TTL the
// owner renews on a heartbeat. Renew and release are compare-and-set on the
// instance id so a stale ex-owner can never stomp the current one.

func ownerKey(battleID string) string { return "pvp:owner:" + battleID }

// ClaimBattleOwner attempts to acquire the lease for battleID. Returns true if
// this instance is now the owner, false if another instance already holds it.
// SET NX PX is a single atomic op — no read-then-write race.
func (c *Cache) ClaimBattleOwner(ctx context.Context, battleID, instanceID string, ttl time.Duration) (bool, error) {
	return c.rdb.SetNX(ctx, ownerKey(battleID), instanceID, ttl).Result()
}

// RenewBattleOwner extends the lease TTL, but only if this instance still holds
// it. Returns true on success; false means the lease was lost (expired and
// re-taken, or never held) and the caller must stop coordinating.
func (c *Cache) RenewBattleOwner(ctx context.Context, battleID, instanceID string, ttl time.Duration) (bool, error) {
	res, err := renewOwnerScript.Run(ctx, c.rdb,
		[]string{ownerKey(battleID)},
		instanceID, ttl.Milliseconds(),
	).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

// ReleaseBattleOwner drops the lease, but only if this instance holds it — a
// late release from an ex-owner must not free the current owner's lease. A
// no-op (and no error) when the key is gone or held by someone else.
func (c *Cache) ReleaseBattleOwner(ctx context.Context, battleID, instanceID string) error {
	return releaseOwnerScript.Run(ctx, c.rdb,
		[]string{ownerKey(battleID)},
		instanceID,
	).Err()
}

// GetBattleOwner returns the instance id currently holding the lease, or
// ErrNotFound if the battle has no owner (never claimed, or the lease expired).
func (c *Cache) GetBattleOwner(ctx context.Context, battleID string) (string, error) {
	v, err := c.rdb.Get(ctx, ownerKey(battleID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return v, err
}

// renewOwnerScript: extend the TTL only if the caller still holds the lease.
// KEYS[1]=owner key, ARGV[1]=instance id, ARGV[2]=ttl in ms. Returns 1|0.
var renewOwnerScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  return 1
end
return 0
`)

// releaseOwnerScript: delete the key only if the caller holds it.
// KEYS[1]=owner key, ARGV[1]=instance id.
var releaseOwnerScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)
