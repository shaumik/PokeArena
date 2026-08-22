//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/flameorb.js.
//
// Ursaring is only a Guts body holding the orb; Snorlax carries the ability
// explicitly. Breloom is the Bullet Seed user whose job is to make the lead
// faint, so that the orb holder comes in as a forced replacement rather than on
// a turn of its own — that is the timing the first case is about. Victreebel
// keeps the Grass typing, but the port gives it Skill Link rather than
// Technician: at level 50 the Seaking standing in for Magikarp survives a
// two-hit Bullet Seed behind its Focus Sash, and a guaranteed five hits is what
// makes the lead faint on every seed.

func TestItemsFlameOrb(t *testing.T) {
	describe(t, "Flame Orb", func(g *psg) {
		g.it("should not trigger when entering battle", func(p *ps) {
			p.battle(
				team{
					{Species: "Magikarp", Ability: "swiftswim", Item: "focussash", Moves: mv("splash")},
					{Species: "Ursaring", As: "Snorlax", Ability: "guts", Item: "flameorb",
						Moves: mv("protect")},
				},
				team{{Species: "Breloom", As: "Victreebel", Ability: "skilllink", Moves: mv("bulletseed")}},
			)
			p.makeChoices("move splash", "move bulletseed")
			p.fainted(p.slot(0, 1), "the lead has to faint for the orb holder to enter as a replacement")
			p.makeChoices("switch 2", "")
			p.notEqual(p.mine().Status, "burn", "the orb should not fire on the way in")
		})

		g.it("should trigger after one turn", func(p *ps) {
			p.battle(
				team{{Species: "Ursaring", As: "Snorlax", Ability: "guts", Item: "flameorb",
					Moves: mv("protect")}},
				team{{Species: "Magikarp", Ability: "swiftswim", Moves: mv("splash")}},
			)
			target := p.mine()
			p.sets(func() any { return target.Status }, "burn", func() {
				p.makeChoices("move protect", "move splash")
			}, "the orb should burn its holder at the end of the turn")
		})
	})
}
