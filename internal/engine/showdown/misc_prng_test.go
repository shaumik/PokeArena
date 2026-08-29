//go:build showdown

package showdown

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/engine"
)

// Ported from test/sim/misc/prng.js.
//
// This file tests a library, not a battle, so it is the one port that reaches
// past p.battle and drives engine.RNG directly. The two generators are not the
// same shape and that governs what transfers:
//
//   - Showdown's PRNG takes a seed *string* and offers random(), randomChance
//     (numerator, denominator) and sample(array). This engine's is splitmix64
//     over a single uint64 with IntN, Range and Chance(percent).
//   - The four determinism / degenerate-probability cases transfer directly.
//   - The two rate cases transfer with the sample size scaled from 100 draws to
//     1000. Upstream can bound 100 draws inside [45, 55] because it pins one
//     seed; this port must hold on every seed, and 100 draws does not (seeds 1
//     to 5 give 43, 56, 56, 52, 45). Scaling by ten keeps the proportion
//     upstream asserted and states the same claim about the generator.
//   - randomChance's denominator has no counterpart: Chance takes a whole
//     percent, so 1/1 and 256/256 are the same call here, and the two
//     "identical to random(d) < n" invariants have no random(d) to be stated
//     against. Those two skip rather than being re-pointed at IntN(100), which
//     would only restate Chance's own implementation.
//   - sample() has no counterpart at all — the engine's RNG offers no
//     collection-picking primitive — so that whole describe block skips.

func TestMiscPRNG(t *testing.T) {
	describe(t, "PRNG", func(g *psg) {
		g.it("should always generate the same results off the same seed", func(p *ps) {
			results := make([]int, 0, 100)
			testAgainst := engine.NewRNG(p.seed)
			for i := 0; i < 100; i++ {
				results = append(results, testAgainst.IntN(1000))
			}
			for i := 0; i < 10; i++ {
				cur := engine.NewRNG(p.seed)
				for j := range results {
					n := cur.IntN(1000)
					p.equal(n, results[j], "a replayed generator diverged from the first run")
				}
			}
		})
	})

	describe(t, "randomChance(numerator=0, denominator=1)", func(g *psg) {
		g.it("should always return false", func(p *ps) {
			rng := engine.NewRNG(p.seed)
			for i := 0; i < 100; i++ {
				p.isFalse(rng.Chance(0), "a zero chance must never fire")
			}
		})
	})

	describe(t, "randomChance(numerator=1, denominator=1)", func(g *psg) {
		g.it("should always return true", func(p *ps) {
			rng := engine.NewRNG(p.seed)
			for i := 0; i < 100; i++ {
				p.ok(rng.Chance(100), "a certain chance must always fire")
			}
		})
	})

	describe(t, "randomChance(numerator=256, denominator=256)", func(g *psg) {
		// The same call as 1/1 above: Chance takes a percentage, so every way
		// of writing certainty collapses to Chance(100). Kept as its own case
		// because the ledger is keyed on the upstream name.
		g.it("should always return true", func(p *ps) {
			rng := engine.NewRNG(p.seed)
			for i := 0; i < 100; i++ {
				p.ok(rng.Chance(100), "a certain chance must always fire")
			}
		})
	})

	describe(t, "randomChance(numerator=1, denominator=2)", func(g *psg) {
		g.it("should return true 45-55% of the time", func(p *ps) {
			rng := engine.NewRNG(p.seed)
			trueCount := 0
			for i := 0; i < 1000; i++ {
				if rng.Chance(50) {
					trueCount++
				}
			}
			p.bounded(trueCount, 450, 550, "a coin flip over 1000 draws")
		})

		g.skip("should be identical to (random(2) == 0)",
			"the engine's Chance takes a whole percentage rather than a numerator and denominator, so there is no random(2) draw for this invariant to be stated against")
	})

	describe(t, "randomChance(numerator=217, denominator=256)", func(g *psg) {
		// 217/256 is 84.77%, and Chance only takes whole percents, so this is
		// Chance(85). The upstream bound of 80-90% is wide enough to cover the
		// rounding.
		g.it("should return true 80%-90% of the time", func(p *ps) {
			rng := engine.NewRNG(p.seed)
			trueCount := 0
			for i := 0; i < 1000; i++ {
				if rng.Chance(85) {
					trueCount++
				}
			}
			p.bounded(trueCount, 800, 900, "an 85% chance over 1000 draws")
		})

		g.skip("should be identical to (random(256) < 217)",
			"the engine's Chance takes a whole percentage rather than a numerator and denominator, so there is no random(256) draw for this invariant to be stated against")
	})

	describe(t, "sample", func(g *psg) {
		const noSample = "the engine's RNG offers IntN, Range and Chance only; there is no sample() to test"
		g.skip("should throw for a zero-item array", noSample)
		g.skip("should eventually throw for a very sparse array", noSample)
		g.skip("should eventually throw for a somewhat sparse array", noSample)
		g.skip("should return the only item in a single-item array", noSample)
		g.skip("should return items with equal probability for a five-item array", noSample)
		g.skip("should return items with weighted probability for a three-item array with duplicates", noSample)
		g.skip("should be identical to array[random(array.length)]", noSample)
	})
}
