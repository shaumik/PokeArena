//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/lansatberry.js.
//
// Lansat Berry is not in this item set. The first case is about the berry, so it
// keeps it and the missing-item failure is the finding. Sleep Talk is not in the
// dataset either and is the holder's idle move, so Splash stands in for it.
// Aggron goes through the stand-in table to Magneton, which keeps the Steel half
// the Fighting attacker is aimed at; Lucario is only that attacker, so Machamp
// carries Aura Sphere and upstream's Adaptability.
//
// The second case measures the crit ratio passed to each hit of Triple Kick
// through battle.onEvent and then pins an exact level-100 HP figure. There is no
// event hook here and the figure does not transfer, so it skips; Triple Kick is
// not in this dataset either.

func TestItemsLansatBerry(t *testing.T) {
	describe(t, "Lansat Berry", func(g *psg) {
		g.it("should apply a Focus Energy effect when consumed", func(p *ps) {
			p.battle(
				team{{Species: "Aggron", Ability: "sturdy", Item: "lansatberry", Moves: mv("splash")}},
				team{{Species: "Lucario", As: "Machamp", Ability: "adaptability", Moves: mv("aurasphere")}},
			)
			if p.state() == nil {
				return
			}
			holder := p.mine()
			p.makeChoices("move splash", "move aurasphere")
			p.noItem(holder, "the berry should have been eaten in the pinch")
			p.ok(holder.Volatiles.FocusEnergy, "the berry should leave a Focus Energy effect behind")
		})

		g.skip("should start to apply the effect even in middle of an attack",
			"the case reads the crit ratio of each hit through battle.onEvent, which this "+
				"harness has no counterpart for, and then pins an exact level-100 HP figure; "+
				"Triple Kick is not in this dataset either")
	})
}
