//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/psyblade.js.
//
// Psyblade is not in this dataset, so both cases report the missing move
// rather than the terrain boost they are about.
//
// Species. Gallade becomes Machamp, a Fighting physical attacker with no
// bearing on a Psychic-typed hit's multiplier. Miraidon becomes Raichu, and
// with it goes Hadron Engine — the ability is only upstream's way of getting
// Electric Terrain onto the field, so the port puts the terrain up with the
// move instead and strips the ability rather than reporting one the case does
// not care about.
//
// Assertion. Upstream reads an absolute damage window, [157, 186], off a
// level-100 battle; this engine is fixed at level 50, so the window has no
// counterpart. The port measures the same claim as a comparison instead: the
// identical attack under the identical seed, once with the terrain up and once
// without. A 1.5x base power cannot be swallowed by the damage roll's 85-100%
// spread, so "harder with the terrain" is a real reading of the rule even
// though it is looser than the original's window.

func TestMovesPsyblade(t *testing.T) {
	describe(t, "Psyblade", func(g *psg) {
		g.it("should have its base power multiplied by 1.5 in Electric Terrain", func(p *ps) {
			var boosted, plain int

			p.battle(
				team{{
					Species: "Gallade", As: "Machamp", Ability: "steadfast",
					Moves: mv("splash", "psyblade"),
				}},
				team{{
					Species: "Miraidon", As: "Raichu", Ability: "noability",
					Moves: mv("electricterrain", "splash"),
				}},
			)
			p.makeChoices("move splash", "move electricterrain")
			p.equal(p.terrain(), "electric", "the terrain should be up before the measured hit")
			before := p.foe().HP
			p.makeChoices("move psyblade", "move splash")
			boosted = before - p.foe().HP

			p.battle(
				team{{
					Species: "Gallade", As: "Machamp", Ability: "steadfast",
					Moves: mv("splash", "psyblade"),
				}},
				team{{
					Species: "Miraidon", As: "Raichu", Ability: "noability",
					Moves: mv("electricterrain", "splash"),
				}},
			)
			p.makeChoices("move splash", "move splash")
			before = p.foe().HP
			p.makeChoices("move psyblade", "move splash")
			plain = before - p.foe().HP

			p.atLeast(boosted, plain+1, "Electric Terrain should have made Psyblade hit harder")
		})

		g.it("should have its base power multiplied by 1.5 in Electric Terrain even if the user or the target isn't grounded", func(p *ps) {
			var boosted, plain int

			p.battle(
				team{{
					Species: "Gallade", As: "Machamp", Ability: "steadfast", Item: "airballoon",
					Moves: mv("splash", "psyblade"),
				}},
				team{{
					Species: "Miraidon", As: "Raichu", Ability: "noability", Item: "airballoon",
					Moves: mv("electricterrain", "splash"),
				}},
			)
			p.makeChoices("move splash", "move electricterrain")
			p.equal(p.terrain(), "electric", "the terrain should be up before the measured hit")
			before := p.foe().HP
			p.makeChoices("move psyblade", "move splash")
			boosted = before - p.foe().HP

			p.battle(
				team{{
					Species: "Gallade", As: "Machamp", Ability: "steadfast", Item: "airballoon",
					Moves: mv("splash", "psyblade"),
				}},
				team{{
					Species: "Miraidon", As: "Raichu", Ability: "noability", Item: "airballoon",
					Moves: mv("electricterrain", "splash"),
				}},
			)
			p.makeChoices("move splash", "move splash")
			before = p.foe().HP
			p.makeChoices("move psyblade", "move splash")
			plain = before - p.foe().HP

			p.atLeast(boosted, plain+1,
				"neither Air Balloon should have kept Psyblade off the terrain's boost")
		})
	})
}
