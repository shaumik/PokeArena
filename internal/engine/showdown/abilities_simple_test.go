//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/simple.js.
//
// Simple is not in this dataset, so the one modern case runs with the ability
// set and inert: the doubling never happens and the single boosts it reports
// are the finding. Bibarel has no stand-in row; Snorlax is a Normal body, which
// is all the case needs — what matters is that the Curse user is not a Ghost,
// since Curse is a different move on one.
//
// The whole 'Simple [Gen 4]' block is built with common.gen(4). There is no
// gen-mod layer here and Gen 4 Simple is a different mechanic — a multiplier on
// the stat rather than on the boost — so it skips as a block.

func TestAbilitiesSimple(t *testing.T) {
	describe(t, "Simple", func(g *psg) {
		g.it("should double all stat boosts", func(p *ps) {
			p.battle(
				team{{Species: "Bibarel", As: "Snorlax", Ability: "simple", Moves: mv("curse")}},
				team{{Species: "Gyarados", Ability: "moxie", Moves: mv("splash")}},
			)
			p.makeChoices("move curse", "move splash")
			target := p.mine()
			p.statStage(target, "atk", 2, "Simple should double Curse's Attack boost")
			p.statStage(target, "def", 2, "Simple should double Curse's Defense boost")
			p.statStage(target, "spe", -2, "Simple should double Curse's Speed drop")
		})
	})

	describe(t, "Simple [Gen 4]", func(g *psg) {
		g.skip("should double the effect of stat boosts", "gen 4 mechanics")
		g.skip("should double the effect of stat boosts passed by Baton Pass", "gen 4 mechanics")
		g.skip("should be suppressed by Mold Breaker", "gen 4 mechanics")
	})
}
