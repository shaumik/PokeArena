//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/lightningrod.js.
//
// Redirection is the bulk of this file and redirection needs an ally, so five
// of the eight cases are doubles or triples and skip. What is left is the
// single-target half: the Electric immunity and the +1 Sp. Atk that comes with
// it.
//
// The first case is written as a gen-6 battle upstream only because it is
// paired with the gen-6/gen-7 split in the two cases after it. Manectric is
// pure Electric, so it is not already immune, and gen 6 and gen 9 give the
// same answer; it is ported as a live case. The case that genuinely turns on
// the split — no boost for an already-immune body before Gen 7 — has no gen-6
// layer to run against and skips.
//
// Species. Manectric has no stand-in; Raichu is the dex's other Electric body
// carrying Lightning Rod, so the ability under test is the species' own.
// Rhydon is in the dex and already carries it.
//
// Moves. Sleep Talk is not in this dataset; Splash stands in as the idle move,
// which is all it is doing upstream.

func TestAbilitiesLightningRod(t *testing.T) {
	describe(t, "Lightning Rod", func(g *psg) {
		g.it("should grant immunity to Electric-type moves and boost Special Attack by 1 stage", func(p *ps) {
			p.battle(
				team{{Species: "Manectric", As: "Raichu", Ability: "lightningrod", Moves: mv("splash")}},
				team{{Species: "Jolteon", Ability: "static", Moves: mv("thunderbolt")}},
			)
			p.makeChoices("move splash", "move thunderbolt")
			p.fullHP(p.mine(), "Lightning Rod should absorb the Electric move")
			p.statStage(p.mine(), "spa", 1, "absorbing it should raise Sp. Atk one stage")
			p.logHas("drew in the attack", "Lightning Rod should have announced the absorb")
		})

		g.skip("should not boost Special Attack if the user is already immune to Electric-type moves in gen 6-",
			"gen 6 mechanics")

		g.it("should boost Special Attack if the user is already immune to Electric-type moves in gen 7+", func(p *ps) {
			p.battle(
				team{{Species: "Rhydon", Ability: "lightningrod", Moves: mv("splash")}},
				team{{Species: "Jolteon", Ability: "static", Moves: mv("thunderbolt")}},
			)
			p.makeChoices("move splash", "move thunderbolt")
			p.fullHP(p.mine(), "Ground is already immune to Electric")
			p.statStage(p.mine(), "spa", 1, "Lightning Rod should still boost a body that was already immune")
		})

		g.skip("should redirect single-target Electric-type attacks to the user if it is a valid target", "triples")
		g.skip("should redirect to the fastest Pokemon with the ability", "doubles")
		g.skip("should redirect to the Pokemon having the ability longest", "doubles")
		g.skip("should not redirect if another Pokemon has used Follow Me", "doubles")
		g.skip("should have its Electric-type immunity and its ability to redirect moves suppressed by Mold Breaker",
			"doubles")
	})
}
