//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/ateabilities.js.
//
// Upstream builds four identical describe blocks from a table
// (Refrigerate/Ice, Pixilate/Fairy, Aerilate/Flying, Galvanize/Electric). The
// port writes them out, with the interpolated `it` strings resolved to the
// text upstream actually produces.
//
// Genesect has no dex entry and no stand-in row. Pinsir is built in its place:
// what the user has to be here is a body that is neither Normal — so the move
// it fires gets no STAB before the conversion — nor any of the four -ate types,
// so none is gained after it. Pinsir is pure Bug, which is none of those. The
// ability is set explicitly, exactly as upstream sets it.
//
// The damage case cannot keep its figure. `assert.bounded(hp, [651 - 83,
// 651 - 70])` is a level-100 Blissey absorbing one roll, and this engine is
// fixed at level 50 with Chansey standing in, so the numbers say nothing. It is
// restated as a comparison: the same turn is played again with the ability
// stripped, and the converted move has to come out meaningfully ahead. The
// floor is 1.15x rather than the canonical 1.2x so that a rounding step cannot
// decide the case; it still separates "boosted" from "not boosted at all".
// Hyper Voice stays neutral against a Normal-type target both before and after
// the conversion, so the boost is the only thing the comparison can see.
//
// `sleeptalk`, upstream's do-nothing, is not in this dataset; `splash` stands
// in for it.

func TestMiscAteAbilities(t *testing.T) {
	// The two case bodies, shared by the four blocks the way upstream shares
	// them through its loop.
	becomesType := func(ability, typeName string) func(p *ps) {
		return func(p *ps) {
			p.battle(
				team{{Species: "Genesect", As: "Pinsir", Ability: ability, Moves: mv("hypervoice")}},
				team{{Species: "Gengar", Moves: mv("splash")}},
			)
			p.turn()
			p.damaged(p.foe(), "Hyper Voice should have become "+typeName+" and got past the Ghost's Normal immunity")
		}
	}

	boostsBy20 := func(ability string) func(p *ps) {
		return func(p *ps) {
			p.battle(
				team{{Species: "Genesect", As: "Pinsir", Ability: ability, Moves: mv("hypervoice")}},
				team{{Species: "Blissey", Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.turn()
			converted := p.foe().MaxHP - p.foe().HP

			// The same turn with the ability stripped, as the baseline the
			// comparison needs. Not an upstream battle.
			p.battle(
				team{{Species: "Genesect", As: "Pinsir", Ability: "noability", Moves: mv("hypervoice")}},
				team{{Species: "Blissey", Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.turn()
			plain := p.foe().MaxHP - p.foe().HP

			p.atLeast(converted, plain*115/100,
				"converting a Normal move should also add 20% to its power")
		}
	}

	describe(t, "Refrigerate", func(g *psg) {
		g.it("should make most Normal type moves become Ice type", becomesType("refrigerate", "Ice"))
		g.it("should boost the power of Normal type attacks by 20% when changing their type", boostsBy20("refrigerate"))
	})

	describe(t, "Pixilate", func(g *psg) {
		g.it("should make most Normal type moves become Fairy type", becomesType("pixilate", "Fairy"))
		g.it("should boost the power of Normal type attacks by 20% when changing their type", boostsBy20("pixilate"))
	})

	describe(t, "Aerilate", func(g *psg) {
		g.it("should make most Normal type moves become Flying type", becomesType("aerilate", "Flying"))
		g.it("should boost the power of Normal type attacks by 20% when changing their type", boostsBy20("aerilate"))
	})

	describe(t, "Galvanize", func(g *psg) {
		g.it("should make most Normal type moves become Electric type", becomesType("galvanize", "Electric"))
		g.it("should boost the power of Normal type attacks by 20% when changing their type", boostsBy20("galvanize"))
	})
}
