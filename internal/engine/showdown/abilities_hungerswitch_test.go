//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/hungerswitch.js.
//
// Nothing here survives. Morpeko is not in this 80-species dex and has no
// stand-in, Hunger Switch is not in the ability set, and every case is stated
// as an assertion about which forme is out — Morpeko or Morpeko-Hangry.
// Alternating between two formes is the whole ability, and forme changes are
// not modeled, so a substituted body would answer every case with "still the
// same species" for a reason unrelated to Hunger Switch.
//
// The last two cases are additionally about Terastallization.

func TestAbilitiesHungerSwitch(t *testing.T) {
	describe(t, "Hunger Switch", func(g *psg) {
		g.skip("should alternate forms every turn",
			"Morpeko is not in this 80-species dex and Hunger Switch's forme alternation is not modeled")
		g.skip("should revert back to the base form when switched out",
			"Morpeko is not in this 80-species dex and Hunger Switch's forme alternation is not modeled")
		g.skip("should stop activating when Morpeko Terastallizes", "Terastallization")
		g.skip("should maintain its form when Terastallized, even when switched out", "Terastallization")
	})
}
