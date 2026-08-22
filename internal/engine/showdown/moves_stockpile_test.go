//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/stockpile.js.
//
// The case is about bookkeeping: Stockpile hands out +1 Def / +1 Sp. Def per
// stack, and Spit Up must give back only the stages that actually landed, not
// one per stack. Upstream arranges the asymmetry with `battle.boost({def: 4,
// spd: 5})`, a direct write with no counterpart here, so the port reaches the
// same +4 / +5 through play: Cosmic Power four times for +4 / +4, then Charge
// for the fifth Sp. Def stage. The two Stockpiles then take Def from +4 to +6
// (both land) and Sp. Def from +5 to +6 (only the first lands), which is the
// state the assertion reads back after Spit Up.
//
// The intermediate stage check is setup verification, not an extra case: if
// the boost moves do not reach +4 / +5 the real assertion would be measuring
// something else, and the failure should say so.
//
// Seviper and Zangoose are not in this dex. Arbok keeps Seviper's Poison
// typing and genuinely carries Shed Skin; Snorlax is a Normal body that
// carries Immunity, which is all upstream's Zangoose is. `splash` stands in
// for `sleeptalk`, which is not in this dataset and is pure filler here.

func TestMovesStockpile(t *testing.T) {
	describe(t, "Stockpile", func(g *psg) {
		g.it("should keep track of how many boosts to each defense stat were successful", func(p *ps) {
			p.battle(
				team{{Species: "Seviper", As: "Arbok", Ability: "shedskin",
					Moves: mv("stockpile", "spitup", "cosmicpower", "charge")}},
				team{{Species: "Zangoose", As: "Snorlax", Ability: "immunity", Moves: mv("splash")}},
			)
			for i := 0; i < 4; i++ {
				p.makeChoices("move cosmicpower", "move splash")
			}
			p.makeChoices("move charge", "move splash")
			p.statStage(p.mine(), "def", 4, "setup: four Cosmic Powers should have reached +4 Defense")
			p.statStage(p.mine(), "spd", 5, "setup: four Cosmic Powers and a Charge should have reached +5 Sp. Def")

			p.makeChoices("move stockpile", "move splash")
			p.makeChoices("move stockpile", "move splash")
			p.makeChoices("move spitup", "move splash")

			p.statStage(p.mine(), "def", 4, "both Stockpile Defense boosts landed, so Spit Up should give back both")
			p.statStage(p.mine(), "spd", 5, "only one Stockpile Sp. Def boost landed, so Spit Up should give back only one")
		})
	})
}
