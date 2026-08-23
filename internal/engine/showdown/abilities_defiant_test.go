//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/defiant.js.
//
// Pawniard is not in the dex and has no stand-in row; Primeape is built
// instead, which carries Defiant as one of its own abilities.
//
// The case is three opponent-inflicted stat drops in a row, each of which
// should answer with +2 Attack, for +6. Two of upstream's three moves are not
// in this dataset — Fire Lash and Silk Trap — so the three drops are Fake
// Tears, Leer and Sand Attack instead. What has to survive is that all three
// come from the opponent, that none of them lowers Attack (which would net off
// against Defiant's own boost and change the total), and that all three are
// sure hits, which upstream's three also were. Silk Trap's contact clause is
// lost, so the tackle-into-a-protect turn becomes another idle turn.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.

func TestAbilitiesDefiant(t *testing.T) {
	describe(t, "Defiant", func(g *psg) {
		g.it("should raise the user's attack when lowered by an opponent", func(p *ps) {
			p.battle(
				team{{Species: "pawniard", As: "Primeape", Ability: "defiant", Moves: mv("splash")}},
				team{{Species: "wynaut", Ability: "noability", Moves: mv("faketears", "leer", "sandattack")}},
			)
			p.makeChoices("move splash", "move faketears")
			p.makeChoices("move splash", "move leer")
			p.makeChoices("move splash", "move sandattack")

			p.statStage(p.mine(), "atk", 6, "three drops from the opponent should be three Defiant boosts")
			p.logHas("Defiant raised its Attack sharply", "each boost should be announced")
		})

		g.skip("should not raise the user's attack when lowered by itself or an ally", "doubles")
	})
}
