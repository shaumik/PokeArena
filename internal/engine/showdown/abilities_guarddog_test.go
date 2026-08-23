//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/guarddog.js.
//
// Guard Dog is not one of the abilities this engine models, so every case here
// reports that first.
//
// None of the four species is in the dex. Mabosstiff is built as Arcanine, the
// dex's other large dog and an ability-agnostic body for this; Sandile as
// Arbok, which carries Intimidate itself; Shinx as Raichu, an electric body
// that only has to use Roar. Azumarill has a stand-in row (Clefable) and is
// only ever the Pokemon that gets dragged in.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.

func TestAbilitiesGuardDog(t *testing.T) {
	describe(t, "Guard Dog", func(g *psg) {
		g.it("should nullify Intimidate and instead boost the Pokemon's Attack by 1", func(p *ps) {
			p.battle(
				team{{Species: "Mabosstiff", As: "Arcanine", Ability: "guarddog", Moves: mv("splash")}},
				team{{Species: "sandile", As: "Arbok", Ability: "intimidate", Moves: mv("splash")}},
			)
			// Lead switch-in abilities fire at the top of turn 1 in this
			// engine rather than at battle construction, so the turn is what
			// makes Intimidate happen at all.
			p.turn()
			p.statStage(p.mine(), "atk", 1, "Guard Dog should have turned the Intimidate into a boost")
		})

		g.it("should prevent phazing", func(p *ps) {
			p.battle(
				team{
					{Species: "Mabosstiff", As: "Arcanine", Ability: "guarddog", Moves: mv("splash")},
					{Species: "azumarill", Ability: "thickfat", Moves: mv("rollout")},
				},
				team{{Species: "shinx", As: "Raichu", Ability: "rivalry", Moves: mv("roar")}},
			)
			p.turn()
			p.species(p.mine(), "Arcanine", "Guard Dog should have refused the Roar")
		})

		g.it("should be bypassed by Mold Breaker", func(p *ps) {
			p.battle(
				team{
					{Species: "Mabosstiff", As: "Arcanine", Ability: "guarddog", Moves: mv("splash")},
					{Species: "azumarill", Ability: "thickfat", Moves: mv("rollout")},
				},
				team{{Species: "shinx", As: "Raichu", Ability: "moldbreaker", Moves: mv("roar")}},
			)
			p.turn()
			p.species(p.mine(), "Azumarill", "Mold Breaker should have dragged the Guard Dog holder out")
		})
	})
}
