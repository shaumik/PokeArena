//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/opportunist.js.
//
// Opportunist is not one of the abilities this engine models, so both ported
// cases report that. Espathra is not in the dex and has no stand-in row;
// Hypno is built instead, a psychic body with nothing of its own to interfere.
// Primeape is in the dex and carries Anger Point itself, and Storm Throw
// always crits in this engine, so the second case's trigger is exact rather
// than seed-dependent.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.

func TestAbilitiesOpportunist(t *testing.T) {
	describe(t, "Opportunist", func(g *psg) {
		g.it("should not cause an infinite loop with itself", func(p *ps) {
			p.battle(
				team{{Species: "Espathra", As: "Hypno", Ability: "Opportunist", Moves: mv("calmmind")}},
				team{{Species: "Espathra", As: "Hypno", Ability: "Opportunist", Moves: mv("splash")}},
			)
			p.makeChoices("move calmmind", "move splash")
			p.statStage(p.mine(), "spa", 1, "Calm Mind's own boost")
			p.statStage(p.foe(), "spa", 1, "Opportunist should copy it once and stop there")
		})

		g.it("should copy Anger Point", func(p *ps) {
			p.battle(
				team{{Species: "Espathra", As: "Hypno", Ability: "Opportunist", Moves: mv("stormthrow")}},
				team{{Species: "Primeape", Ability: "Anger Point", Moves: mv("splash")}},
			)
			p.makeChoices("move stormthrow", "move splash")
			p.statStage(p.foe(), "atk", 6, "Storm Throw always crits, so Anger Point should have maxed Attack")
			p.statStage(p.mine(), "atk", 6, "Opportunist should have copied the whole of it")
		})

		g.skip("should only copy the effective boost after the +6 cap", "doubles")
	})
}
