package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClaimBattleOwner_Exclusive(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	ok, err := c.ClaimBattleOwner(ctx, "B", "inst-1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v, want true/nil", ok, err)
	}
	// A different instance must not be able to claim while the lease is held.
	ok, err = c.ClaimBattleOwner(ctx, "B", "inst-2", time.Minute)
	if err != nil {
		t.Fatalf("second claim err: %v", err)
	}
	if ok {
		t.Fatal("second instance claimed a held lease — not exclusive")
	}
}

func TestRenewBattleOwner_HolderOnly(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	if _, err := c.ClaimBattleOwner(ctx, "B", "inst-1", time.Minute); err != nil {
		t.Fatalf("claim: %v", err)
	}

	ok, err := c.RenewBattleOwner(ctx, "B", "inst-1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("holder renew: ok=%v err=%v, want true/nil", ok, err)
	}
	// A non-holder renewing must fail and must not take the lease.
	ok, err = c.RenewBattleOwner(ctx, "B", "inst-2", time.Minute)
	if err != nil {
		t.Fatalf("non-holder renew err: %v", err)
	}
	if ok {
		t.Fatal("non-holder renewed a lease it does not hold")
	}
	owner, err := c.GetBattleOwner(ctx, "B")
	if err != nil || owner != "inst-1" {
		t.Fatalf("owner = %q err=%v, want inst-1", owner, err)
	}
}

func TestReleaseBattleOwner_HolderOnly(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	if _, err := c.ClaimBattleOwner(ctx, "B", "inst-1", time.Minute); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// A non-holder release is a no-op: inst-1 still owns it afterward.
	if err := c.ReleaseBattleOwner(ctx, "B", "inst-2"); err != nil {
		t.Fatalf("non-holder release err: %v", err)
	}
	if owner, _ := c.GetBattleOwner(ctx, "B"); owner != "inst-1" {
		t.Fatalf("owner after non-holder release = %q, want inst-1", owner)
	}

	// The holder's release frees the lease for the next claimant.
	if err := c.ReleaseBattleOwner(ctx, "B", "inst-1"); err != nil {
		t.Fatalf("holder release err: %v", err)
	}
	if _, err := c.GetBattleOwner(ctx, "B"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after release: %v, want ErrNotFound", err)
	}
	if ok, _ := c.ClaimBattleOwner(ctx, "B", "inst-2", time.Minute); !ok {
		t.Fatal("could not claim after holder released")
	}
}

func TestClaimBattleOwner_TTLExpiryFrees(t *testing.T) {
	c, mr := newTestCache(t)
	ctx := context.Background()
	if _, err := c.ClaimBattleOwner(ctx, "B", "inst-1", 5*time.Second); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Owner dies without releasing; the lease must expire and free up.
	mr.FastForward(6 * time.Second)
	if _, err := c.GetBattleOwner(ctx, "B"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after expiry: %v, want ErrNotFound", err)
	}
	if ok, _ := c.ClaimBattleOwner(ctx, "B", "inst-2", time.Minute); !ok {
		t.Fatal("a new instance could not take over an expired lease")
	}
}

func TestGetBattleOwner_NoOwner(t *testing.T) {
	c, _ := newTestCache(t)
	if _, err := c.GetBattleOwner(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
