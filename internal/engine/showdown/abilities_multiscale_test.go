//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/multiscale.js.
//
// Upstream states both cases as absolute damage windows at level 100
// ([15, 18] halved against [30, 36] whole), which do not transfer to a level-50
// engine. Each case is restated as the comparison those two windows were
// standing in for: the same attack measured once while Dragonite is at full HP
// and once after it has been chipped, and a ratio the damage roll cannot cross.
//
// Wicked Blow is not in this dataset. Storm Throw replaces it and does the same
// job — a physical move that always crits (internal/engine/willcrit.go), so the
// crit is a constant across both measurements instead of a coin flip that could
// move either one. Wynaut's stand-in Hypno hits far too softly for the two
// figures to be distinguishable, so the attacker is built as Machamp; nothing
// in the case depends on the attacker beyond "hits Dragonite for a measurable,
// survivable amount".
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.

func TestAbilitiesMultiscale(t *testing.T) {
	describe(t, "Multiscale", func(g *psg) {
		g.it("should halve damage when it is at full health", func(p *ps) {
			p.battle(
				team{{Species: "Dragonite", Ability: "multiscale", Moves: mv("splash")}},
				team{{Species: "Wynaut", As: "Machamp", Ability: "noability", Moves: mv("stormthrow")}},
			)
			dnite := p.mine()
			p.makeChoices("move splash", "move stormthrow")
			atFull := dnite.MaxHP - dnite.HP
			p.atLeast(atFull, 1, "Storm Throw should have connected")

			before := dnite.HP
			p.makeChoices("move splash", "move stormthrow")
			chipped := before - dnite.HP
			p.atLeast(chipped, atFull*3/2,
				"Multiscale should have halved the first hit and left the second alone")
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Dragonite", Ability: "multiscale", Moves: mv("splash")}},
				team{{Species: "Wynaut", As: "Machamp", Ability: "moldbreaker", Moves: mv("stormthrow")}},
			)
			dnite := p.mine()
			p.makeChoices("move splash", "move stormthrow")
			atFull := dnite.MaxHP - dnite.HP
			p.atLeast(atFull, 1, "Storm Throw should have connected")

			before := dnite.HP
			p.makeChoices("move splash", "move stormthrow")
			chipped := before - dnite.HP
			// Both measurements are whole hits now, so they should sit within
			// a damage roll of each other rather than a factor of two apart.
			p.atLeast(atFull*4, chipped*3,
				"Mold Breaker should have left the full-HP hit unhalved")
			p.atMost(atFull*4, chipped*5,
				"and no larger than the chipped one either")
		})
	})
}
