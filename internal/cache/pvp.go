package cache

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const pvpTokenBytes = 32

// Slot errors. ClaimSlot returns these as distinct values so the gateway can
// log them differently, but the gateway must collapse them to a single
// opaque message before returning to the client — otherwise we leak whether
// a battle exists, whether a token was valid, or whether a slot is taken.
var (
	ErrInvalidToken = errors.New("cache: invalid join token")
	ErrSlotTaken    = errors.New("cache: slot already claimed")
	ErrSlotNotFound = errors.New("cache: no such slot or battle")
)

// PvPSlot identifies one of the two trainer slots in a live_pvp battle.
// Stringly-typed because the value flows through URLs and JSON; an int there
// would be too easy to typo without the compiler's help.
type PvPSlot string

const (
	SlotP1 PvPSlot = "p1"
	SlotP2 PvPSlot = "p2"
)

// Valid reports whether s is one of the two known slot names.
func (s PvPSlot) Valid() bool { return s == SlotP1 || s == SlotP2 }

// Index returns the 0/1 position this slot occupies in engine action arrays.
func (s PvPSlot) Index() int {
	if s == SlotP2 {
		return 1
	}
	return 0
}

// GenerateToken returns a CSPRNG-backed base64url token suitable for a join
// URL. Callers must persist it via SavePvPTokens and must never log it — it
// is the only capability a WS client needs to claim a slot.
func GenerateToken() (string, error) {
	b := make([]byte, pvpTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("cache: read entropy: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pvpKey(id string) string { return "battle:" + id + ":slots" }

// SavePvPTokens registers the two join tokens for a new live_pvp battle. The
// hash inherits the live-state TTL so tokens cannot outlive their battle: a
// pvp battle that's never claimed is reaped together with its state.
func (c *Cache) SavePvPTokens(ctx context.Context, battleID, p1Token, p2Token string) error {
	key := pvpKey(battleID)
	if err := c.rdb.HSet(ctx, key, map[string]any{
		"p1_token": p1Token,
		"p2_token": p2Token,
	}).Err(); err != nil {
		return err
	}
	return c.rdb.Expire(ctx, key, stateTTL).Err()
}

// ClaimSlot atomically validates the token and marks the slot as claimed.
// First WS to call this for a slot wins; subsequent calls return ErrSlotTaken
// even with the right token.
//
// The validity check and the claim must happen inside a single Redis op —
// a CAS done from Go (HGET, compare, HSET) would race two clients claiming
// the same slot in the small window between the read and the write. The Lua
// script below is the smallest correct primitive.
func (c *Cache) ClaimSlot(ctx context.Context, battleID string, slot PvPSlot, token string) error {
	if !slot.Valid() {
		return ErrSlotNotFound
	}
	res, err := claimSlotScript.Run(ctx, c.rdb,
		[]string{pvpKey(battleID)},
		string(slot), token,
	).Result()
	if err != nil {
		return err
	}
	switch res {
	case "ok":
		return nil
	case "unknown":
		return ErrSlotNotFound
	case "invalid":
		return ErrInvalidToken
	case "taken":
		return ErrSlotTaken
	default:
		return fmt.Errorf("cache: unexpected claim response: %v", res)
	}
}

// claimSlotScript: validate the token and set the claimed flag in one
// atomic step. Args: KEYS[1]=hash key, ARGV[1]=slot name (p1|p2),
// ARGV[2]=token. Returns one of: "ok" | "unknown" | "invalid" | "taken".
//
// The claimed flag is stored as "1" today; when disconnect-grace lands it
// will hold a per-session identifier so only the original claimant can
// reconnect during the grace window.
var claimSlotScript = redis.NewScript(`
local stored = redis.call('HGET', KEYS[1], ARGV[1] .. '_token')
if not stored then return 'unknown' end
if stored ~= ARGV[2] then return 'invalid' end
local already = redis.call('HGET', KEYS[1], ARGV[1] .. '_claimed')
if already then return 'taken' end
redis.call('HSET', KEYS[1], ARGV[1] .. '_claimed', '1')
return 'ok'
`)

// DeletePvPTokens removes the slot hash. Called when a battle ends so tokens
// don't linger past their useful life.
func (c *Cache) DeletePvPTokens(ctx context.Context, battleID string) error {
	return c.rdb.Del(ctx, pvpKey(battleID)).Err()
}
