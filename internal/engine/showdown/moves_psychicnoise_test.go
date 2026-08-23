//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/psychicnoise.js.
//
// Psychic Noise is in this dataset; the Heal Block effect it is supposed to
// leave behind is not modeled anywhere in the engine, which is what the first
// case is here to say.
//
// Wynaut takes its stand-in row (Hypno). Regieleki has none and becomes
// Jolteon — Electric and comfortably faster than the target, which is all the
// case asks; Transistor is not preserved and nothing reads it.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.
//
// The first assertion is upstream's "not at full HP", and it is a real test
// rather than a tautology: Psychic Noise lands first and Soft-Boiled would
// otherwise put the whole chip back, so the target ends the turn damaged only
// if the healing was blocked.

func TestMovesPsychicNoise(t *testing.T) {
	describe(t, "Psychic Noise", func(g *psg) {
		g.it("should prevent the target from healing, like Heal Block", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "battlearmor", Moves: mv("softboiled", "splash")}},
				team{{Species: "Regieleki", As: "Jolteon", Moves: mv("psychicnoise")}},
			)
			p.turn()
			p.damaged(p.mine(), "the blocked Soft-Boiled should not have undone Psychic Noise's chip")
			p.cantMove(0, "softboiled", "Psychic Noise should lock a healing move out of the choice set")
		})

		g.skip("should prevent the target's ally from healing it with Life Dew", "doubles")
	})
}
