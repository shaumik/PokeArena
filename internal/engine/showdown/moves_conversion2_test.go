//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/conversion2.js.
//
// Conversion 2 is not in this dataset, and neither are Electrify or Sleep Talk.
// All three are subjects rather than filler — the second half of the first case
// is precisely "Conversion 2 reads the move Sleep Talk called, not Sleep Talk",
// and the Electrify case is "Conversion 2 reads the retyped Tackle" — so every
// one of them stays in the fixture and the missing-move failures are the
// finding.
//
// Porygon2 becomes Porygon: the same line one stage down, Normal, which is the
// typing all three live cases start from and measure against. Shuckle resolves
// to Snorlax through the shared table and is only a body throwing Tackle.
//
// Upstream reads `pokemon.getTypes()[0]`; here that is `Type1`. Upstream's
// assertion is a set membership — Conversion 2 picks any type that resists the
// last move — and the port keeps it as one rather than pinning a single answer,
// since which of the resisting types is chosen is a roll.
//
// The Gen 5, Gen 4, Gen 3 and Gen 2 cases have no counterpart without a
// gen-mod layer.

func TestMovesConversion2(t *testing.T) {
	describe(t, "Conversion2", func(g *psg) {
		g.it("should change users type to resist", func(p *ps) {
			p.battle(
				team{{Species: "porygon2", As: "Porygon", Moves: mv("sleeptalk", "conversion2", "spore")}},
				team{
					{Species: "raticate", Moves: mv("tackle")},
					{Species: "zapdos", Moves: mv("thundershock", "sleeptalk")},
				},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move conversion2", "move tackle")
			afterTackle := psID(string(p.mine().Type1))
			p.ok(afterTackle == "rock" || afterTackle == "ghost" || afterTackle == "steel",
				"Conversion 2 should pick a type that resists the Normal-type Tackle")

			p.makeChoices("move spore", "switch 2")
			p.makeChoices("move conversion2", "move sleeptalk")
			afterSubmove := psID(string(p.mine().Type1))
			p.ok(afterSubmove == "electric" || afterSubmove == "grass" ||
				afterSubmove == "ground" || afterSubmove == "dragon",
				"should change type based on submove")
		})

		g.it("should respect the determined type of the last move", func(p *ps) {
			p.battle(
				team{{Species: "porygon2", As: "Porygon", Moves: mv("electrify", "conversion2")}},
				team{{Species: "shuckle", Moves: mv("tackle")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move electrify", "move tackle")
			p.makeChoices("move conversion2", "move tackle")
			got := psID(string(p.mine().Type1))
			p.ok(got == "electric" || got == "grass" || got == "ground" || got == "dragon",
				"Tackle should be considered Electric")
		})

		g.it("should fail if the last move was typeless", func(p *ps) {
			// The Assault Vest leaves Raticate with no selectable move, so it
			// Struggles, and Struggle is typeless.
			p.battle(
				team{{Species: "porygon2", As: "Porygon", Moves: mv("conversion2")}},
				team{{Species: "raticate", Item: "assaultvest", Moves: mv("taunt")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.mine().Type1, "normal", "there is no type to resist, so Conversion 2 should fail")
		})

		g.skip("should fail to change to a type the user already has", "gen 5 mechanics")
	})

	describe(t, "[Gen 4]", func(g *psg) {
		g.skip("should not fail if the last move was Struggle", "gen 4 mechanics")
	})

	describe(t, "[Gen 3]", func(g *psg) {
		g.skip("should not fail to change to a type the user already has", "gen 3 mechanics")
	})

	describe(t, "[Gen 2]", func(g *psg) {
		g.skip("should not succeed after moves that clear the last move used", "gen 2 mechanics")
	})
}
