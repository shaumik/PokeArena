//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/myceliummight.js.
//
// Mycelium Might is not one of the abilities this engine models, so all three
// cases report that.
//
// None of the four species is in the dex or has a stand-in row. Pyukumuku is
// built as Slowbro (a water body), Orthworm as Magneton (a steel body), Bonsly
// as Onix (rock, and slow, which is what makes the Quick Claw question worth
// asking) and Regieleki as Jolteon (electric and comfortably faster than Onix).
//
// The first case keeps Sleep Talk even though it is not in this dataset: the
// mechanic under test is what happens to a move *called by* a status move, so
// there is nothing to substitute it with, and the missing move is itself the
// finding.
//
// The two Quick Claw cases are upstream's `forceRandomChance`, which has no
// counterpart here. They become rate measurements instead: the status-move case
// says Quick Claw must never fire, over 200 seeds; the non-status case says it
// fires at about Quick Claw's own 20%.

func TestAbilitiesMyceliumMight(t *testing.T) {
	describe(t, "Mycelium Might", func(g *psg) {
		g.it("should cause attacks called by empowered status moves to ignore abilities", func(p *ps) {
			p.battle(
				team{{Species: "Pyukumuku", As: "Slowbro", Ability: "myceliummight", Moves: mv("sleeptalk", "earthquake")}},
				team{{Species: "Orthworm", As: "Magneton", Ability: "eartheater", Moves: mv("spore")}},
			)
			p.turn()
			p.damaged(p.foe(), "the Earthquake called by Sleep Talk should have gone through Earth Eater")
		})

		g.itRate("should never trigger your own quick claw if using a status move", 1.0, 1.0, 200, func(p *ps) bool {
			p.battle(
				team{{Species: "Bonsly", As: "Onix", Ability: "myceliummight", Item: "quickclaw", Moves: mv("spore")}},
				team{{Species: "Regieleki", As: "Jolteon", Ability: "noability", Moves: mv("falseswipe")}},
			)
			p.turn()
			// The holder taking damage is the observable form of "it moved
			// second": had Quick Claw fired, Spore would have landed first and
			// the sleeping attacker would never have swung.
			return p.mine().HP < p.mine().MaxHP
		})

		g.itRate("should be able to trigger your own quick claw if using a non-status move", 0.10, 0.32, 200, func(p *ps) bool {
			p.battle(
				team{{Species: "Bonsly", As: "Onix", Ability: "myceliummight", Item: "quickclaw", Moves: mv("tackle")}},
				team{{Species: "Regieleki", As: "Jolteon", Ability: "noability", Moves: mv("spore")}},
			)
			p.turn()
			// The foe taking damage means the slower holder got in first, which
			// only Quick Claw can arrange.
			return p.foe().HP < p.foe().MaxHP
		})
	})
}
