package engine

import (
	"math"
	"testing"
)

// probability_test.go holds the helpers for testing effects that only happen
// some of the time, and the reason they exist.
//
// The tempting way to test a 30% rider is to find a seed that makes the roll
// land the way the test wants and assert the outcome: "seed 2 fires, seed 1
// does not". It reads as precise and it is fast, but what it pins is this
// generator's output at this point in this draw order — not the rule. Change
// the generator and the test fails while the engine is still right; keep the
// generator and the test passes even if the probability is wrong, because one
// seed cannot tell 30% from 25%.
//
// It also makes the tests useless as a specification. These tests are what a
// reimplementation is written against: you port the test, then make it pass.
// A test that says "seed 2 fires" cannot be ported without first reproducing
// splitmix64 and every draw the engine takes before this one — which is a
// heavy prerequisite for the question "does Cute Charm work".
//
// So a probability is tested as a probability: run it many times and check the
// rate. The guards around it — no contact, wrong gender, Inner Focus, a
// substitute in the way — are tested as absolutes over the same many runs,
// which is strictly stronger than one lucky seed.
//
// To find tests that depend on the generator rather than on the rules, perturb
// it and see what breaks:
//
//	// in NewRNG: return &RNG{state: seed ^ 0x9E3779B9}
//	go test ./internal/engine/ -count=1
//
// What should fail is the golden corpus and the draw-order tests that pin the
// RNG contract on purpose (TestFullGame_MatchesGolden,
// TestEffectSporeImmunityCheckSitsAfterBothRolls). Anything else that fails is
// describing the generator when it meant to describe the game.

// trials is the sample size for a rate measurement. Large enough that the
// bands below are tight, small enough that a battle per trial stays cheap.
const trials = 400

// fireRate runs fn once per seed and reports the fraction that fired.
func fireRate(t *testing.T, fn func(seed uint64) bool) float64 {
	t.Helper()
	fired := 0
	for seed := uint64(1); seed <= trials; seed++ {
		if fn(seed) {
			fired++
		}
	}
	return float64(fired) / float64(trials)
}

// assertRate checks that an effect fires about as often as it is documented
// to. The band is five standard deviations of the binomial, which a correct
// implementation will not fall outside in practice and a wrong probability
// (30% built as 25%, say) will.
func assertRate(t *testing.T, what string, want float64, fn func(seed uint64) bool) {
	t.Helper()
	got := fireRate(t, fn)
	band := 5 * math.Sqrt(want*(1-want)/float64(trials))
	if math.Abs(got-want) > band {
		t.Errorf("%s fired %.1f%% of %d attempts, want about %.0f%% (±%.1f)",
			what, 100*got, trials, 100*want, 100*band)
	}
}

// assertNever checks that an effect is refused every single time — the form
// every guard takes, and the half of a rider's contract that a seed-picked
// test proves least.
func assertNever(t *testing.T, what string, fn func(seed uint64) bool) {
	t.Helper()
	for seed := uint64(1); seed <= trials; seed++ {
		if fn(seed) {
			t.Fatalf("%s fired on attempt %d, and must never fire", what, seed)
		}
	}
}

// assertAlways is its mirror, for the branches canon makes certain.
func assertAlways(t *testing.T, what string, fn func(seed uint64) bool) {
	t.Helper()
	for seed := uint64(1); seed <= trials; seed++ {
		if !fn(seed) {
			t.Fatalf("%s did not fire on attempt %d, and must fire every time", what, seed)
		}
	}
}

// assertRateWithin is assertRate with the band written out instead of derived.
// Some rules do not come with a documented percentage — a measured "roughly
// half the time, and definitely not always or never" is the honest form of
// them — and some measurements ride on more than one roll, where a binomial
// band around a single probability would be too tight.
func assertRateWithin(t *testing.T, what string, lo, hi float64, seeds int, fn func(seed uint64) bool) {
	t.Helper()
	fired := 0
	for seed := uint64(1); seed <= uint64(seeds); seed++ {
		if fn(seed) {
			fired++
		}
	}
	got := float64(fired) / float64(seeds)
	if got < lo || got > hi {
		t.Errorf("%s fired %.1f%% of %d attempts, want within [%.0f%%, %.0f%%]",
			what, 100*got, seeds, 100*lo, 100*hi)
	}
}

// assertNeverOver and assertAlwaysOver are assertNever and assertAlways with
// the sample size chosen by the caller, for guards whose battles are expensive
// enough that a full sweep is not worth the seconds.
func assertNeverOver(t *testing.T, what string, seeds int, fn func(seed uint64) bool) {
	t.Helper()
	for seed := uint64(1); seed <= uint64(seeds); seed++ {
		if fn(seed) {
			t.Fatalf("%s fired on attempt %d, and must never fire", what, seed)
		}
	}
}

func assertAlwaysOver(t *testing.T, what string, seeds int, fn func(seed uint64) bool) {
	t.Helper()
	for seed := uint64(1); seed <= uint64(seeds); seed++ {
		if !fn(seed) {
			t.Fatalf("%s did not fire on attempt %d, and must fire every time", what, seed)
		}
	}
}
