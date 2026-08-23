//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/cheekpouch.js.
//
// Sleep Talk is not in this dataset. Both cases use it only as a move that
// does nothing while the other side acts, so Splash stands in for it; nothing
// either case measures depends on which inert move is chosen.
//
// Cheek Pouch itself is not in the ability set. The harness says so directly,
// and the failing HP assertion in the first case is the same gap seen from the
// other side.
//
// The second case's log assertion is the port of upstream's "no |-heal line
// appears anywhere". This engine narrates healing in prose rather than
// protocol, so the fragment is the word its heal lines share.

func TestAbilitiesCheekPouch(t *testing.T) {
	describe(t, "Cheek Pouch", func(g *psg) {
		g.it("should restore 1/3 HP to the user after eating a Berry", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "lumberry", Ability: "cheekpouch", Moves: mv("splash")}},
				team{{Species: "pichu", Moves: mv("nuzzle")}},
			)
			p.turn()
			p.fullHP(p.mine(), "the Berry's Cheek Pouch heal should have covered Nuzzle's damage")
		})

		g.it("should not activate if the user was at full HP", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "lumberry", Ability: "cheekpouch", Moves: mv("splash")}},
				team{{Species: "pichu", Moves: mv("glare")}},
			)
			p.turn()
			p.logLacks("restored", "nothing should have healed")
		})
	})
}
