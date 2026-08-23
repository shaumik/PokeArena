//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/reflecttype.js.
//
// Neither Reflect Type nor Trick-or-Treat is in this dataset, so every case
// here stops at "move ... is not in this dataset". They are written out
// anyway: if the moves are ever added, they say what they have to do. Burn Up
// is in the dataset, but nothing in this engine turns its user typeless, so
// the "???" state all three cases are about does not exist here either.
//
// Upstream reads the copier's types through getTypes(). The harness has no
// type helper, so the port reads Type1 and Type2 off the Pokemon; a Pokemon
// with one type carries the second as empty. The first case's "unchanged"
// assertion is p.constant over the pair.
//
// Latias is only a body that copies types and is slowed to move last, so
// Alakazam builds instead — the Dragon half is not preserved and none of the
// three cases reads the copier's starting types, only what it ends with. The
// Lagging Tail stays: Reflect Type has to resolve after Burn Up, and Alakazam
// would otherwise outrun both foes. Arcanine and Moltres are in the dex.

func TestMovesReflectType(t *testing.T) {
	describe(t, "Reflect Type", func(g *psg) {
		g.it(`should fail when used against a Pokemon whose type is "???"`, func(p *ps) {
			p.battle(
				team{{Species: "Arcanine", Ability: "intimidate", Moves: mv("burnup")}},
				team{{
					Species: "Latias", As: "Alakazam", Ability: "levitate", Item: "laggingtail",
					Moves: mv("reflecttype"),
				}},
			)
			types := func() any { return string(p.foe().Type1) + "/" + string(p.foe().Type2) }
			p.constant(types, func() { p.makeChoices("move burnup", "move reflecttype") },
				"Reflect Type should fail against a target that has burned away its only type")
		})

		g.it(`should ignore the "???" type when used against a Pokemon whose type contains "???" and a non-added type`, func(p *ps) {
			p.battle(
				team{{
					Species: "Latias", As: "Alakazam", Ability: "levitate", Item: "laggingtail",
					Moves: mv("reflecttype", "trickortreat"),
				}},
				team{{Species: "Moltres", Ability: "pressure", Moves: mv("burnup")}},
			)
			p.makeChoices("move reflecttype", "move burnup")
			p.equal(p.mine().Type1, "flying", "the burnt-away Fire should not have come across")
			p.equal(p.mine().Type2, "", "the copier should be single-typed")

			p.makeChoices("move trickortreat", "move burnup")
			p.makeChoices("move reflecttype", "move burnup")
			p.equal(p.mine().Type1, "flying", "")
			p.equal(p.mine().Type2, "ghost", "the added Ghost should come across alongside Flying")
		})

		g.it(`should turn the "???" type into "Normal" when used against a Pokemon whose type is only "???" and an added type`, func(p *ps) {
			p.battle(
				team{{
					Species: "Latias", As: "Alakazam", Ability: "levitate", Item: "laggingtail",
					Moves: mv("reflecttype", "trickortreat"),
				}},
				team{{Species: "Arcanine", Ability: "intimidate", Moves: mv("burnup")}},
			)
			p.makeChoices("move trickortreat", "move burnup")
			p.makeChoices("move reflecttype", "move burnup")
			p.equal(p.mine().Type1, "normal", "a target left with nothing but an added type reads as Normal")
			p.equal(p.mine().Type2, "ghost", "")
		})
	})
}
