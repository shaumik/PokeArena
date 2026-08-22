//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/sparklingaria.js.
//
// Wynaut takes its stand-in row (Hypno) and keeps Compound Eyes, which matters
// here: it lifts Will-O-Wisp past 100% accuracy so the burn the case is about
// lands under every seed. Chansey is in this dex already, and Sleep Talk — not
// in the dataset — is idle, so it is Splash.
//
// The port adds one assertion upstream leaves implicit: that the burn was
// really applied before Sparkling Aria is asked to cure it. Without it the
// case passes for free on any run where Will-O-Wisp failed, which is the one
// outcome a port must not produce.

func TestMovesSparklingAria(t *testing.T) {
	describe(t, "Sparkling Aria", func(g *psg) {
		g.it("should cure the target's burn", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "compoundeyes",
					Moves: mv("willowisp", "sparklingaria")}},
				team{{Species: "Chansey", Moves: mv("splash")}},
			)
			p.turn()
			p.hasStatus(p.foe(), "brn", "Will-O-Wisp should have burned Chansey first")
			p.makeChoices("move sparklingaria", "")
			p.noStatus(p.foe(), "Sparkling Aria should have cured the burn it landed on")
		})

		g.skip("should not cure the target's burn if the user fainted", "doubles")
	})
}
