package usage

import (
	"math"
	"testing"
)

// TestAdd_FoldsAllBuckets: Add sums every bucket independently and mutates
// neither operand, so per-call usage can be folded into a running total.
func TestAdd_FoldsAllBuckets(t *testing.T) {
	a := Usage{InputTokens: 10, OutputTokens: 2, CacheReadTokens: 100, CacheWriteTokens: 5}
	b := Usage{InputTokens: 1, OutputTokens: 3, CacheReadTokens: 0, CacheWriteTokens: 7}
	got := a.Add(b)
	want := Usage{InputTokens: 11, OutputTokens: 5, CacheReadTokens: 100, CacheWriteTokens: 12}
	if got != want {
		t.Fatalf("Add = %+v, want %+v", got, want)
	}
	if (a != Usage{InputTokens: 10, OutputTokens: 2, CacheReadTokens: 100, CacheWriteTokens: 5}) {
		t.Fatalf("Add mutated its receiver: %+v", a)
	}
}

// TestTotalAndIsZero: Total counts every bucket; the zero value is free.
func TestTotalAndIsZero(t *testing.T) {
	if !(Usage{}).IsZero() {
		t.Fatal("zero Usage should be zero")
	}
	u := Usage{InputTokens: 10, OutputTokens: 2, CacheReadTokens: 100, CacheWriteTokens: 5}
	if u.IsZero() {
		t.Fatal("non-empty Usage reported zero")
	}
	if u.Total() != 117 {
		t.Fatalf("Total = %d, want 117", u.Total())
	}
}

// TestCost prices each bucket at its own rate — the whole reason the four
// counts are kept separate. Cached reads must cost far less than fresh input.
func TestCost(t *testing.T) {
	// $1 / 1M input, $5 / 1M output, $0.10 / 1M cache read, $1.25 / 1M cache write.
	p := Pricing{Input: 1.0, Output: 5.0, CacheRead: 0.10, CacheWrite: 1.25}
	u := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000, CacheWriteTokens: 1_000_000}
	got := u.Cost(p)
	want := 1.0 + 5.0 + 0.10 + 1.25
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("Cost = %.6f, want %.6f", got, want)
	}
	if (Usage{}).Cost(p) != 0 {
		t.Fatal("zero usage should cost nothing")
	}
}
