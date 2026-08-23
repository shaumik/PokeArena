//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/fusion-combo.js.
//
// All four upstream cases are doubles, but the mechanic is not: Fusion Flare
// doubles when the move resolved immediately before it *this turn* was Fusion
// Bolt, and that predecessor is battle-wide, not side-wide. So the first case,
// where the ally is only there to be the previous mover, re-expresses exactly
// in singles: the foe holds the Lagging Tail it holds upstream, so it moves
// last, and its Fusion Flare follows our Fusion Bolt inside the same turn.
//
// The other three do not. One needs Instruct to make a single Pokemon move
// twice, one needs a third action wedged between the two fusion moves, and one
// needs the failed Fusion Bolt and the Fusion Flare to come from the same side.
//
// Neither Fusion Bolt nor Fusion Flare is in this dataset, so the live case
// fails at team construction naming the missing move — that absence is the
// finding. Upstream reads the base-power modifier out of a `BasePower` event
// hook, which has no counterpart here, so the port measures the damage instead
// and compares it against the same turn played without the Fusion Bolt: the
// doubling has to survive the 85-100% damage roll on both sides, hence the 1.6x
// threshold rather than 2x.

func TestMiscFusionCombo(t *testing.T) {
	describe(t, "Fusion Bolt + Fusion Flare", func(g *psg) {
		g.it("should boost the second move if the first was used immediately before it", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("fusionbolt", "splash")}},
				team{{Species: "Dragonite", Item: "laggingtail", Moves: mv("fusionflare")}},
			)
			p.makeChoices("move fusionbolt", "move fusionflare")
			boosted := p.mine().MaxHP - p.mine().HP

			p.battle(
				team{{Species: "Wynaut", Moves: mv("fusionbolt", "splash")}},
				team{{Species: "Dragonite", Item: "laggingtail", Moves: mv("fusionflare")}},
			)
			p.makeChoices("move splash", "move fusionflare")
			plain := p.mine().MaxHP - p.mine().HP

			p.atLeast(boosted, plain*16/10,
				"Fusion Flare after Fusion Bolt should hit for close to double")
		})

		g.skip("should boost the second move if the first was used by the same Pokemon", "doubles")
		g.skip("should not boost the second move if another move was used between them", "doubles")
		g.skip("should not boost the second move if the first move failed", "doubles")
	})
}
