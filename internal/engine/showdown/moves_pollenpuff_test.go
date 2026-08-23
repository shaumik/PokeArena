//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/pollenpuff.js.
//
// Pollen Puff is in this dataset, but every case in the file is about the half
// of the move this engine cannot have: what happens when it is aimed at an
// ally. There is one active slot per side here, so "heals an ally through a
// Substitute", "the ally is immune to it", and the whole Heal Block group
// ("can the user even select Pollen Puff at its partner") have no singles form
// to be re-expressed in — the target selection *is* the mechanic. The damaging
// half of the move, which does survive into singles, is not what any of these
// cases measure.
//
// The two Z-move cases would be out of scope twice over.

func TestMovesPollenPuff(t *testing.T) {
	describe(t, "Pollen Puff", func(g *psg) {
		g.skip("should heal allies through Substitute, but not damage opponents through Substitute", "doubles")
		g.skip("should not heal a Pokemon if they have natural type immunity to Pollen Puff", "doubles")
	})

	describe(t, "interaction of Heal Block and Pollen Puff", func(g *psg) {
		g.skip("should prevent the user from targeting an ally with Pollen Puff while the user is affected by Heal Block",
			"doubles")
		g.skip("should not prevent the user from targeting an ally with Z-Pollen Puff while the user is affected by Heal Block",
			"Z-moves")
		g.skip("should not prevent the user from targeting an ally with Pollen Puff while the target is affected by Heal Block at move selection, but it should fail at move execution",
			"doubles")
		g.skip("should prevent the user from successfully using Pollen Puff into an ally if the user becomes affected by Heal Block mid-turn",
			"doubles")
		g.skip("should not prevent the user from using Z-Pollen Puff into an ally", "Z-moves")
	})
}
