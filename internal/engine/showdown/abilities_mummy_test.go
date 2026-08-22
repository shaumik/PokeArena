//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/mummy.js.
//
// Mummy is not one of the abilities this engine models, so the first case
// reports that and is the file's finding. Cofagrigus is not in the dex and has
// no stand-in row; Gengar is built instead, which is all the case needs from
// it — a ghost body that gets hit by a contact move.
//
// Sleep Talk is not in this dataset and is only an idle move here, so it is
// Splash.

func TestAbilitiesMummy(t *testing.T) {
	describe(t, "Mummy", func(g *psg) {
		g.it("should set the attacker's ability to Mummy when the user is hit by a contact move", func(p *ps) {
			p.battle(
				team{{Species: "Cofagrigus", As: "Gengar", Ability: "mummy", Moves: mv("splash")}},
				team{{Species: "Mew", Ability: "synchronize", Moves: mv("aerialace")}},
			)
			p.turn()
			p.hasAbility(p.foe(), "mummy", "making contact with a Mummy holder should have overwritten Synchronize")
		})

		g.skip("should not change abilities that can't be suppressed",
			"Mimikyu is not in this 80-species dex, its stand-in row says Disguise is not among the things it preserves, "+
				"and Disguise is a forme change this engine does not model")

		g.skip("should not activate before all damage calculation is complete", "doubles")
	})
}
