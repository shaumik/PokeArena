//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/thunderwave.js.
//
// Hippowdon has no stand-in row; Sandslash is built instead. It is the pure
// Ground body the case needs and it genuinely carries Sand Force, so the
// fixture keeps the ability upstream wrote. Ground's Electric immunity is the
// only thing the case measures, and Sandslash has it for the same reason
// Hippowdon does.

func TestMovesThunderWave(t *testing.T) {
	describe(t, "Thunder Wave", func(g *psg) {
		g.it("should not ignore natural type immunities", func(p *ps) {
			p.battle(
				team{{Species: "Jolteon", Ability: "quickfeet", Moves: mv("thunderwave")}},
				team{{Species: "Hippowdon", As: "Sandslash", Ability: "sandforce", Moves: mv("slackoff")}},
			)
			p.makeChoices("move thunderwave", "move slackoff")
			p.noStatus(p.foe(), "a Ground type should be immune to Thunder Wave")
		})
	})
}
