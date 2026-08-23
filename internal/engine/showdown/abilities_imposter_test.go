//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/imposter.js.
//
// Nothing here is ported. Every case puts a Ditto on the field, and Ditto is
// the species whose identity is the mechanic — the port has no stand-in for it
// by design, for the same reason it has none for Arceus under Multitype. The
// mechanic is missing twice over: Transform, and with it Imposter, is on this
// engine's not-modeled list.

func TestAbilitiesImposter(t *testing.T) {
	describe(t, "Imposter", func(g *psg) {
		g.skip("should Transform into the opposing Pokemon facing it",
			"Ditto is not in this 80-species dex and Transform is not modeled")
		g.skip("should be blocked by substitutes",
			"Ditto is not in this 80-species dex and Transform is not modeled")
		g.skip("should not activate if Skill Swapped",
			"Ditto is not in this 80-species dex and Transform is not modeled")
	})
}
