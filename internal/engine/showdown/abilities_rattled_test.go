//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/rattled.js.
//
// Rattled is not one of the abilities this engine models, so both cases report
// that. Dunsparce is not in the dex and has no stand-in row; Lickitung is built
// instead, a normal-type body with nothing of its own to interfere. Incineroar
// resolves through the stand-in table to Arcanine, which the row names as an
// Intimidate body.
//
// Upstream asserts straight off the freshly built battle, because Showdown runs
// switch-in abilities during construction. This engine fires the leads'
// switch-in hooks at the top of turn 1 instead, so each case plays one turn
// first; nothing else happens on it.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.

func TestAbilitiesRattled(t *testing.T) {
	describe(t, "Rattled", func(g *psg) {
		g.it("should boost the user's Speed when Intimidated", func(p *ps) {
			p.battle(
				team{{Species: "Dunsparce", As: "Lickitung", Ability: "rattled", Moves: mv("splash")}},
				team{{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.mine(), "atk", -1, "Intimidate should still have cut Attack")
			p.statStage(p.mine(), "spe", 1, "and Rattled should have answered with a Speed boost")
		})

		g.it("should not boost the user's Speed if Intimidate failed to lower attack", func(p *ps) {
			p.battle(
				team{{Species: "Dunsparce", As: "Lickitung", Item: "clearamulet", Ability: "rattled", Moves: mv("splash")}},
				team{{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.mine(), "atk", 0, "Clear Amulet should have refused the Intimidate")
			p.statStage(p.mine(), "spe", 0, "so Rattled should have had nothing to answer")
		})
	})
}
