//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/flowerveil.js.
//
// All three cases are doubles, and the ally is not incidental in any of them:
// the claim under test is what Flower Veil does to a *teammate*, and even the
// self-inflicted-drop case measures the holder and its ally side by side. Flower
// Veil is not in this dataset either, so a singles re-expression would pass for
// the wrong reason rather than measure anything. All three skip as doubles.

func TestAbilitiesFlowerVeil(t *testing.T) {
	describe(t, "Flower Veil", func(g *psg) {
		g.skip("should block status conditions and stat drops on Grass-type Pokemon and its allies", "doubles")
		g.skip("should not stop an ally from falling asleep when Yawn was already affecting it", "doubles")
		g.skip("should not block self-inflicted stat drops", "doubles")
	})
}
