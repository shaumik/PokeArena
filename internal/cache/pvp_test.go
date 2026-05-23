package cache

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestCache wires the Cache against a miniredis instance. miniredis
// supports EVAL so the same Lua script that runs in production runs here;
// this is the whole reason we picked it over a hand-rolled fake.
func newTestCache(t *testing.T) (*Cache, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return &Cache{rdb: redis.NewClient(&redis.Options{Addr: mr.Addr()})}, mr
}

func TestGenerateToken_IsUnguessable(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if a == b {
		t.Fatal("two generated tokens collided — entropy source is broken")
	}
	// 32 bytes base64url with no padding = 43 chars. Pinning the length
	// catches an accidental change to pvpTokenBytes that would silently
	// shrink the security margin.
	if len(a) != 43 {
		t.Fatalf("token length = %d, want 43 (base64url of 32 bytes)", len(a))
	}
}

func TestClaimSlot_FirstClaimWins(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	if err := c.SavePvPTokens(ctx, "B", "p1tok", "p2tok"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := c.ClaimSlot(ctx, "B", SlotP1, "p1tok"); err != nil {
		t.Fatalf("first claim should succeed: %v", err)
	}
	if err := c.ClaimSlot(ctx, "B", SlotP1, "p1tok"); !errors.Is(err, ErrSlotTaken) {
		t.Fatalf("second claim: got %v, want ErrSlotTaken", err)
	}
}

func TestClaimSlot_RejectsWrongToken(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	_ = c.SavePvPTokens(ctx, "B", "p1tok", "p2tok")
	if err := c.ClaimSlot(ctx, "B", SlotP1, "wrong"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("got %v, want ErrInvalidToken", err)
	}
	// And the slot remains claimable by the right token afterward — a
	// failed attempt must not lock the slot.
	if err := c.ClaimSlot(ctx, "B", SlotP1, "p1tok"); err != nil {
		t.Fatalf("legitimate claim after failed attempt: %v", err)
	}
}

func TestClaimSlot_RejectsUnknownBattle(t *testing.T) {
	c, _ := newTestCache(t)
	if err := c.ClaimSlot(context.Background(), "missing", SlotP1, "anything"); !errors.Is(err, ErrSlotNotFound) {
		t.Fatalf("got %v, want ErrSlotNotFound", err)
	}
}

func TestClaimSlot_RejectsUnknownSlotName(t *testing.T) {
	c, _ := newTestCache(t)
	if err := c.ClaimSlot(context.Background(), "B", "p3", "anything"); !errors.Is(err, ErrSlotNotFound) {
		t.Fatalf("got %v, want ErrSlotNotFound", err)
	}
}

func TestClaimSlot_SlotsAreIndependent(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	_ = c.SavePvPTokens(ctx, "B", "p1tok", "p2tok")
	if err := c.ClaimSlot(ctx, "B", SlotP1, "p1tok"); err != nil {
		t.Fatalf("p1 claim: %v", err)
	}
	if err := c.ClaimSlot(ctx, "B", SlotP2, "p2tok"); err != nil {
		t.Fatalf("p2 claim must be independent of p1: %v", err)
	}
}

func TestReleaseSlot_AllowsReclaim(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	_ = c.SavePvPTokens(ctx, "B", "p1tok", "p2tok")
	_ = c.ClaimSlot(ctx, "B", SlotP1, "p1tok")
	if err := c.ReleaseSlot(ctx, "B", SlotP1); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := c.ClaimSlot(ctx, "B", SlotP1, "p1tok"); err != nil {
		t.Fatalf("reclaim after release: %v", err)
	}
}

func TestSavePvPTokens_HasTTL(t *testing.T) {
	c, mr := newTestCache(t)
	if err := c.SavePvPTokens(context.Background(), "B", "p1tok", "p2tok"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := mr.TTL("battle:B:slots"); got != stateTTL {
		t.Fatalf("ttl = %v, want %v — tokens must not outlive their battle", got, stateTTL)
	}
}

func TestPvPSlot_IndexMapping(t *testing.T) {
	if SlotP1.Index() != 0 {
		t.Fatalf("p1 index = %d, want 0", SlotP1.Index())
	}
	if SlotP2.Index() != 1 {
		t.Fatalf("p2 index = %d, want 1", SlotP2.Index())
	}
}
