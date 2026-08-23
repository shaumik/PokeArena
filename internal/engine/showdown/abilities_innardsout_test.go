//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/innardsout.js.
//
// Innards Out is not one of the abilities this engine models, which is what
// the first case reports.
//
// Neither species is in the dex. Breloom is built as Victreebel, which keeps
// the grass STAB that decides whether Bullet Seed gets through in the number
// of hits Loaded Dice guarantees. Azurill is built as Starmie: the case needs
// a body that Bullet Seed knocks out from full HP inside four hits and whose
// max HP is below the attacker's, so that "attacker loses exactly the target's
// max HP" is a statement that can be true at all. Azurill's own typing is not
// preserved and plays no part upstream.

func TestAbilitiesInnardsOut(t *testing.T) {
	describe(t, "Innards Out", func(g *psg) {
		g.it("should deal damage equal to the total damage of a multi-hit move", func(p *ps) {
			p.battle(
				team{{Species: "Breloom", As: "Victreebel", Ability: "noability", Item: "loadeddice", Moves: mv("bulletseed")}},
				team{{Species: "Azurill", As: "Starmie", Ability: "innardsout", Moves: mv("splash")}},
			)
			p.turn()
			p.fainted(p.foe(), "Bullet Seed should have knocked the Innards Out holder out")
			p.equal(p.mine().HP, p.mine().MaxHP-p.foe().MaxHP,
				"Innards Out should have charged the attacker the whole of the target's HP, not just the final hit")
		})

		g.skip("should not accumulate damage dealt to its allies by a spread move", "doubles")
	})
}
