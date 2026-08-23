//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/belch.js.
//
// Species. Swalot has no stand-in row; Muk is the dex's other pure-Poison body,
// so Belch keeps its STAB and Sticky Hold is still available where a case wants
// it. Registeel is only a Glare body holding a Lagging Tail and is built as
// Magneton, the table's usual steel substitute. Rotom is only a Will-O-Wisp
// body and is built as Gengar, which is fast enough to burn the Swalot before it
// moves — the ordering upstream gets from Rotom's own speed.
//
// Upstream reads `lastMove.id` to show that Belch was or was not selectable.
// This harness can ask the legality question directly (cantMove / canMove), so
// the first case asserts both that and the move actually taken.
//
// U-turn. Upstream answers a forced-switch request after the pivot; this engine
// resolves a self-switch inside the same turn and picks the replacement itself,
// so the last case switches back on the following turn instead.

func TestMovesBelch(t *testing.T) {
	describe(t, "Belch", func(g *psg) {
		g.it(`should be disabled if the user has not consumed a berry`, func(p *ps) {
			p.battle(
				team{{Species: "Swalot", As: "Muk", Item: "lumberry", Moves: mv("belch", "stockpile")}},
				team{{Species: "Registeel", As: "Magneton", Item: "laggingtail", Moves: mv("glare")}},
			)
			if p.state() == nil {
				return
			}
			p.cantMove(0, "belch", "Belch should not be choosable before the user has eaten a berry")
			p.makeChoices("move stockpile", "move glare")
			p.equal(p.mine().Volatiles.LastMoveID, "stockpile", "")

			p.noItem(p.mine(), "Glare should have made the Lum Berry go off")
			p.canMove(0, "belch", "eating the Lum Berry should unlock Belch")
			p.makeChoices("move belch", "move glare")
			p.equal(p.mine().Volatiles.LastMoveID, "belch", "")
		})

		g.it("should count berries as consumed with Bug Bite or Pluck", func(p *ps) {
			p.battle(
				team{{Species: "Swalot", As: "Muk", Ability: "gluttony", Item: "salacberry", Moves: mv("belch", "bugbite")}},
				team{{Species: "Swalot", As: "Muk", Ability: "gluttony", Item: "salacberry", Moves: mv("belch", "pluck")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move bugbite", "move pluck")
			p.makeChoices("move belch", "move belch")
			p.equal(p.mine().Volatiles.LastMoveID, "belch",
				"eating the foe's berry with Bug Bite should unlock Belch")
			p.equal(p.foe().Volatiles.LastMoveID, "belch",
				"eating the foe's berry with Pluck should unlock Belch")
		})

		g.it("should count berries as consumed when they are Flung", func(p *ps) {
			p.battle(
				team{{Species: "Swalot", As: "Muk", Ability: "gluttony", Moves: mv("belch", "stockpile")}},
				team{{Species: "Machamp", Ability: "noguard", Item: "salacberry", Moves: mv("fling")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move stockpile", "move fling")
			p.makeChoices("move belch", "move fling")
			p.equal(p.mine().Volatiles.LastMoveID, "belch",
				"a berry eaten off a Fling should unlock Belch")
		})

		g.it("should still count berries as consumed after switch out", func(p *ps) {
			p.battle(
				team{
					{Species: "Swalot", As: "Muk", Item: "lumberry", Moves: mv("belch", "uturn")},
					{Species: "Swalot", As: "Muk", Moves: mv("toxic")},
				},
				team{{Species: "Rotom", As: "Gengar", Ability: "noability", Moves: mv("rest", "willowisp")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move uturn", "move willowisp")
			p.noItem(p.slot(0, 1), "the burn should have made the Lum Berry go off before the pivot")
			p.makeChoices("switch 1", "move willowisp")
			p.makeChoices("move belch", "move willowisp")
			p.equal(p.mine().Volatiles.LastMoveID, "belch",
				"the memory of a consumed berry should survive switching out")
		})
	})
}
