//go:build showdown

package showdown

import (
	"strings"
	"testing"
)

// Ported from test/sim/items/lumberry.js.
//
// Sleep Talk is not in this dataset and is inert filler for the berry holder in
// every case, so Splash stands in for it.
//
// Golurk is only a No Guard body for Dynamic Punch; Machamp is the in-dex No
// Guard user. Shuckle's stand-in Snorlax is a body that survives, but Snorlax is
// not bulky enough at level 50 to be sure of living through a No Guard Dynamic
// Punch, so the confusion case names Slowbro instead — it resists Fighting and
// takes the hit comfortably, which is all the case asks of the target. The
// Poison Touch case keeps Snorlax but strips its ability: Immunity would refuse
// the poison outright and the case would measure nothing.
//
// Upstream forces the 30% Poison Touch roll with forceRandomChance. There is no
// such hook here, so that case is measured over many seeds instead: the cure
// must show up at Poison Touch's own rate, and the holder must never be left
// poisoned. Upstream also reads the debug log for the ability's status line;
// this engine's narration is asserted on instead.

func TestItemsLumBerry(t *testing.T) {
	describe(t, "Lum Berry", func(g *psg) {
		g.it("should heal a non-volatile status condition", func(p *ps) {
			p.battle(
				team{{Species: "Rapidash", Moves: mv("inferno")}},
				team{{Species: "Machamp", Ability: "noguard", Item: "lumberry", Moves: mv("splash")}},
			)
			p.makeChoices("move inferno", "move splash")
			p.noStatus(p.foe(), "the Lum Berry should have eaten the burn")
		})

		g.it("should cure confusion", func(p *ps) {
			p.battle(
				team{{Species: "Golurk", As: "Machamp", Ability: "noguard", Moves: mv("dynamicpunch")}},
				team{{Species: "Shuckle", As: "Slowbro", Ability: "noability", Item: "lumberry",
					Moves: mv("splash")}},
			)
			p.makeChoices("move dynamicpunch", "move splash")
			p.ok(p.foe().Volatiles.Confusion == nil, "the Lum Berry should have cured the confusion")
		})

		g.skip("should be eaten immediately when the holder gains a status condition",
			"the case reaches into the Outrage user's locked-move duration to line the "+
				"rampage's last turn up with Baneful Bunker, which this harness cannot do, "+
				"and Baneful Bunker is not in this dataset either")

		g.itRate("should cure Poison from Poison Touch before being knocked off", 0.15, 0.45, 200,
			func(p *ps) bool {
				p.battle(
					team{{Species: "Wynaut", Ability: "poisontouch", Moves: mv("knockoff")}},
					team{{Species: "Shuckle", Ability: "noability", Item: "lumberry", Moves: mv("splash")}},
				)
				p.turn()
				p.noStatus(p.foe(), "the berry should be eaten before Knock Off takes it")
				p.noItem(p.foe(), "Knock Off should leave the holder itemless either way")
				return strings.Contains(p.lastTurnText(), "was cured of its")
			})

		g.skip("should cure Poison and confusion after Poison Puppeteer activation",
			"Pecharunt is not in this 80-species dex and Poison Puppeteer is not modeled")
	})
}
