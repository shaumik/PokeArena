//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/accuracy.js.
//
// Neither case came across, for two different reasons.
//
// Both upstream cases read the accuracy number itself out of an
// `battle.onEvent('Accuracy', ...)` hook and assert on it. This engine has no
// such hook and exports nothing that computes an accuracy, so the only handle
// on accuracy from a port is the hit rate — and the whole subject here is a
// one-point rounding difference (93 vs 94, 97.5 rounding to 98), which no
// amount of seed sweeping can separate. Measuring "Sleep Powder almost always
// hits" instead would be a green case measuring something the original was not
// asking, which is worse than recording that the question cannot be put.
//
// The second case is doubles besides: it chains four accuracy modifiers held
// by four different Pokemon and turns on the raw-Speed order they apply in.

func TestMiscAccuracy(t *testing.T) {
	describe(t, "Accuracy", func(g *psg) {
		g.skip("should round half down when applying a modifier",
			"the engine exposes no accuracy hook, and a one-point difference in accuracy is not recoverable from hit rates")
		g.skip("should chain modifiers in order of the Pokemon's raw speed", "doubles")
	})
}
