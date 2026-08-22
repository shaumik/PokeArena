//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/charge.js.
//
// The three Gen 9 cases come across; the Gen 8 block skips.
//
// Species. Kilowattrel is not in this dex and has no stand-in row; Zapdos is
// built instead, which keeps the Electric/Flying body and the Electric attacker
// the case needs. Dondozo resolves to Lapras through the shared table, and
// Lapras' Water typing is what makes the first case's knockout turn on the
// doubling. Baxcalibur is not in the dex either and is only a punching bag that
// has to survive four hits and put Electric Terrain up, so Chansey takes the
// role; the HP investment is the port's, so the bag survives whether or not
// Charge is still up on the last two hits.
//
// Sleep Talk is not in this dataset. Upstream uses it as a do-nothing, so Splash
// stands in.
//
// Upstream reads `volatiles['charge']`; this engine keeps the same flag as
// Volatiles.Charge, so those assertions transfer directly. Upstream's `auto`
// choices are written out as the move they resolve to, because this harness's
// default is the first legal action rather than Showdown's request order.

func TestMovesCharge(t *testing.T) {
	describe(t, "Charge", func(g *psg) {
		g.it("should double the base power of the next Electric attack", func(p *ps) {
			p.battle(
				team{{Species: "Kilowattrel", As: "Zapdos", Ability: "noability",
					Moves: mv("charge", "thunderbolt")}},
				team{{Species: "Dondozo", Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.makeChoices("move charge", "move splash")
			p.makeChoices("move thunderbolt", "move splash")
			p.fainted(p.foe(), "an undoubled Thunderbolt leaves this target alive")
		})

		g.it("should remain active until an Electric-type attack is used", func(p *ps) {
			p.battle(
				team{{Species: "Kilowattrel", As: "Zapdos", Ability: "noability",
					Moves: mv("charge", "agility", "airslash", "thunderbolt", "naturepower")}},
				team{{Species: "Baxcalibur", As: "Chansey", Ability: "noability",
					EVs: evs(map[string]int{"hp": 252}), Moves: mv("splash", "electricterrain")}},
			)
			mine := p.mine()

			p.makeChoices("move charge", "move splash")
			p.makeChoices("move agility", "move splash")
			p.ok(mine.Volatiles.Charge, "a stat move should not spend the charge")
			p.makeChoices("move airslash", "move splash")
			p.ok(mine.Volatiles.Charge, "a non-Electric attack should not spend the charge")
			p.makeChoices("move thunderbolt", "move splash")
			p.isFalse(mine.Volatiles.Charge, "the Electric attack should have spent the charge")
			p.makeChoices("move charge", "move electricterrain")
			p.makeChoices("move naturepower", "move splash")
			p.isFalse(mine.Volatiles.Charge,
				"Nature Power becomes Thunderbolt under Electric Terrain, so it should spend the charge")
		})

		g.it("should wear off after an Electric-type status move that is not Charge is used", func(p *ps) {
			p.battle(
				team{{Species: "Kilowattrel", As: "Zapdos", Ability: "noability",
					Moves: mv("charge", "thunderwave")}},
				team{{Species: "Baxcalibur", As: "Chansey", Ability: "noability", Moves: mv("splash")}},
			)
			mine := p.mine()

			p.makeChoices("move charge", "move splash")
			p.makeChoices("move charge", "move splash")
			p.ok(mine.Volatiles.Charge, "Charge should still be up after re-charging")
			p.makeChoices("move thunderwave", "move splash")
			p.isFalse(mine.Volatiles.Charge, "an Electric status move should spend the charge")
		})
	})

	describe(t, "Charge [Gen 8]", func(g *psg) {
		g.skip("should wear off after a move of any type is used", "gen 8 mechanics")
	})
}
