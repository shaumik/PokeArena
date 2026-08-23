//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/custapberry.js.
//
// Upstream puts the berry on a level-1 Wynaut and spends the first turn on a
// False Swipe that leaves it on 1 HP, which is what brings it into Custap range.
// Level is fixed at 50 here, so the harness's HP field arranges that state
// directly and the opening turn is dropped — it carried no part of the claim,
// and this engine's False Swipe does not spare the target, so keeping it would
// end the battle before the case starts. (That is a finding about False Swipe,
// not about the berry, and belongs to the move's own file.)
//
// Sleep Talk is not in this dataset and is the holder's idle move, so Splash
// stands in for it. Darkrai is only a second body that outspeeds the holder and
// hits it once; Gengar is in the dex and comfortably outspeeds Hypno, which is
// what the last turn needs.
//
// As upstream, the second case reads the *absence* of a second Growl: -1 rather
// than -2 says the berry was spent on the turn the opponent switched.

func TestItemsCustapBerry(t *testing.T) {
	describe(t, "Custap Berry", func(g *psg) {
		g.it("should cause the user to move first when it activates", func(p *ps) {
			p.battle(
				team{{Species: "gyarados", Moves: mv("falseswipe", "tackle")}},
				team{{Species: "wynaut", Item: "custapberry", HP: 1, Moves: mv("splash", "growl")}},
			)
			p.makeChoices("move tackle", "move growl")
			p.statStage(p.mine(), "atk", -1, "Custap should have let Growl land before the KO")
			p.fainted(p.foe(), "")
		})

		g.it("should activate even if the opponent switches out", func(p *ps) {
			p.battle(
				team{
					{Species: "gyarados", Moves: mv("falseswipe")},
					{Species: "darkrai", As: "Gengar", Moves: mv("tackle")},
				},
				team{{Species: "wynaut", Item: "custapberry", HP: 1, Moves: mv("splash", "growl")}},
			)
			p.makeChoices("switch 2", "move growl")
			p.makeChoices("move tackle", "move growl")
			p.statStage(p.mine(), "atk", -1,
				"the berry should have been spent on the switch turn, so the second Growl never lands")
			p.fainted(p.foe(), "")
		})

		// Upstream nests this describe inside Custap Berry; the ledger key keeps
		// the inner name verbatim.
		describe(t, "[Gen 4]", func(g *psg) {
			g.skip("should not activate if the opponent switches out", "gen 4 mechanics")
		})
	})
}
