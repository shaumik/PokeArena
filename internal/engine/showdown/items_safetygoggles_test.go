//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/safetygoggles.js.
//
// Both upstream cases assert on the protocol reveal (`|item: Safety Goggles|`).
// This engine has no reveal protocol, but it does name the refuser in the
// immunity line the powder block emits ("It doesn't affect X... (Safety
// Goggles)"), so the ports match that fragment — it is the same claim, that the
// goggles and not something else are credited with stopping the move.
//
// Species: Tapu Koko, Amoonguss and Yveltal are not in this dex and have no
// stand-in row, so each case names an in-dex body instead. Raichu keeps Tapu
// Koko's Electric typing and puts the terrain up with the move, since Electric
// Surge is not modeled; Vileplume keeps Amoonguss's grass/poison and only has
// to throw a powder move; Aerodactyl replaces Yveltal purely as a body that is
// neither Grass nor an Overcoat holder, those being the two things that would
// otherwise take the credit away from the goggles.
//
// Sleep Talk is not in this dataset and is inert filler in both cases (its user
// is awake, so it fails), so Splash stands in for it.

func TestItemsSafetyGoggles(t *testing.T) {
	describe(t, "Safety Goggles", func(g *psg) {
		g.it("should be revealed if Terrain is also active", func(p *ps) {
			// Electric Surge is not modeled, so the terrain goes up on turn 1
			// and the powder move comes on turn 2 — otherwise the goggles would
			// be credited before the terrain the case is about existed.
			p.battle(
				team{{Species: "Raichu", Ability: "noability", Item: "safetygoggles", Moves: mv("electricterrain", "splash")}},
				team{{Species: "Vileplume", Moves: mv("splash", "spore")}},
			)
			p.makeChoices("move electricterrain", "move splash")
			p.equal(p.terrain(), "electric", "the terrain the case needs should be up before the powder move")
			p.makeChoices("move splash", "move spore")
			p.logHas("Safety Goggles", "the goggles and not the terrain should be credited with refusing Spore")
			p.noStatus(p.mine(), "Spore should not have landed")
		})

		// Upstream pins the accuracy roll with forceRandomChance:false so Sleep
		// Powder would have missed, then checks the goggles are still revealed.
		// There is no such hook here, so the same claim is measured instead: over
		// 200 seeds — which necessarily include the ones where the 75%-accurate
		// Sleep Powder would have missed — the goggles are always the reason the
		// move did nothing.
		g.itRate("should be revealed if the move would have missed", 1.0, 1.0, 200, func(p *ps) bool {
			p.battle(
				team{{Species: "Aerodactyl", Item: "safetygoggles", Moves: mv("splash")}},
				team{{Species: "Venusaur", Moves: mv("sleeppowder")}},
			)
			p.turn()
			return p.logCount("Safety Goggles") > 0
		})
	})
}
