//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/sheerforce.js.
//
// Sheer Force, Tauros, Lapras, Machamp and Scyther are all in this dataset, so
// most of the file ports literally. Lock On, Meteor Assault and Sleep Talk are
// not in the move set and Mummy is not in the ability set; the cases that need
// them keep them, because naming the gap is the point.
//
// Upstream pins the Life Orb cases with `hp === 262`, a level-100 absolute.
// The figure is 1/10 of max HP either way, so the port states the fraction —
// which is what the assertion means and what survives the level change.
//
// The two thaw cases set the freeze mid-battle, after a turn spent forcing a
// recharge. A ported set can only carry a starting status, so the freeze is
// arranged at team build instead. If Meteor Assault is ever added to this
// dataset that ordering will need revisiting: upstream freezes *after* the
// first turn precisely so the frozen Pokemon never rolls to thaw.

func TestAbilitiesSheerForce(t *testing.T) {
	describe(t, "Sheer Force", func(g *psg) {
		g.it("should not eliminate Life Orb recoil in a move with no secondary effects", func(p *ps) {
			p.battle(
				team{{Species: "Tauros", Ability: "sheerforce", Item: "lifeorb", Moves: mv("earthquake")}},
				team{{Species: "Lapras", Ability: "shellarmor", Item: "laggingtail", Moves: mv("rest")}},
			)
			tauros := p.mine()
			p.hurtsBy(tauros, tauros.MaxHP/10,
				func() { p.makeChoices("move earthquake", "move rest") },
				"Earthquake has no secondary, so Life Orb recoil should still be paid")
		})

		g.it("should eliminate secondary effects from moves", func(p *ps) {
			p.battle(
				team{{Species: "Tauros", Ability: "sheerforce", Moves: mv("zapcannon")}},
				team{{Species: "Machamp", Ability: "noguard", Moves: mv("bulkup")}},
			)
			p.makeChoices("move zapcannon", "move bulkup")
			p.noStatus(p.foe(), "Sheer Force should have removed Zap Cannon's paralysis")
		})

		g.it("should not eliminate Life Orb recoil if the ability is disabled/removed mid-attack", func(p *ps) {
			p.battle(
				team{{Species: "Tauros", Ability: "sheerforce", Item: "lifeorb", Moves: mv("lockon", "dynamicpunch")}},
				team{{Species: "Scyther", Ability: "mummy", Moves: mv("irondefense")}},
			)
			tauros := p.mine()
			p.makeChoices("move lockon", "move irondefense")
			before := tauros.HP
			p.makeChoices("move dynamicpunch", "move irondefense")
			p.isFalse(p.foe().Volatiles.Confusion != nil,
				"the secondary was already removed when the move started")
			p.equal(before-tauros.HP, tauros.MaxHP/10,
				"recoil is charged after the move, by which point Mummy has taken Sheer Force away")
		})

		g.it("should eliminate Life Orb recoil in a move with secondary effects", func(p *ps) {
			p.battle(
				team{{Species: "Tauros", Ability: "sheerforce", Item: "lifeorb", Moves: mv("bodyslam")}},
				team{{Species: "Lapras", Ability: "shellarmor", Item: "laggingtail", Moves: mv("rest")}},
			)
			p.makeChoices("move bodyslam", "move rest")
			p.fullHP(p.mine(), "a Sheer Force-boosted move should not charge Life Orb recoil")
		})

		g.it(`should not be possible to thaw a frozen target with a Sheer Force-boosted thawsTarget move`, func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Ability: "sheerforce", Moves: mv("splash", "scald")}},
				team{{Species: "shuckle", Moves: mv("meteorassault"), Status: "frz"}},
			)
			p.turn() // Meteor Assault forces a recharge next turn
			p.makeChoices("move scald", "")
			p.hasStatus(p.foe(), "frz", "a Sheer Force-boosted Scald has no thaw effect left to apply")
		})

		g.it(`should be possible to thaw a frozen target with a Sheer Force-boosted Fire-type move`, func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Ability: "sheerforce", Moves: mv("splash", "flamethrower")}},
				team{{Species: "shuckle", Moves: mv("meteorassault"), Status: "frz"}},
			)
			p.turn() // Meteor Assault forces a recharge next turn
			p.makeChoices("move flamethrower", "")
			p.noStatus(p.foe(), "a Fire-type move thaws by type, not by secondary effect")
		})
	})
}
