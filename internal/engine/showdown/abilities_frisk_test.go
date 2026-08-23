//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/frisk.js.
//
// Nothing is ported. Frisk itself is modeled here — the engine announces
// "frisked ... and found its ..." on switch-in — but neither upstream case is
// about a single opposing Pokemon. The first asserts that *both* foes' items
// are revealed and the second that a Neutralizing Gas lead keeps Frisk quiet
// for a whole opposing side of three, one of which faints mid-turn. Both are
// doubles-shaped in the assertion, not just in the fixture, so re-stating them
// in singles would be writing different cases.

func TestAbilitiesFrisk(t *testing.T) {
	describe(t, "Frisk", func(g *psg) {
		g.skip("should reveal opposing Pokemon's items", "doubles")
		g.skip("should not reveal opposing fainted Pokemon's items", "doubles")
	})
}
