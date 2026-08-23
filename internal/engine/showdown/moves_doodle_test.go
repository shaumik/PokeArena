//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/doodle.js.
//
// Nothing here survives. Doodle copies an ability onto the user *and its ally*,
// so there is no singles reading of any of the three cases — the second slot is
// the mechanic, not scenery. (Doodle is also absent from this move set, and
// Komala, Flutter Mane, Mudkip and Iron Hands from the dex, but the second
// active slot is what makes these unportable.)

func TestMovesDoodle(t *testing.T) {
	describe(t, "Doodle", func(g *psg) {
		g.skip("should replace the Abilities of the user and its ally with the Ability of its target", "doubles")
		g.skip("should fail against certain Abilities", "doubles")
		g.skip("should not fail if only the user has an unreplaceable Ability", "doubles")
	})
}
