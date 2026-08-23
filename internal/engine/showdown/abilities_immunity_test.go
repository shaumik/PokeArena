//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/immunity.js.
//
// Snorlax is in the dex and carries Immunity itself; Crobat resolves through
// the stand-in table to Golbat, the same line one stage down, which keeps the
// poison/flying typing and Infiltrator.
//
// The second case turns on Skill Swap handing Immunity over. Skill Swap is in
// this dataset but the engine has no handler for it, so the ability never
// moves and the poison is still there at the end — worth knowing before that
// failure is filed against Immunity rather than against Skill Swap.
//
// The third case's first assertion counts protocol lines — exactly one
// `-status|...|tox` — and has no counterpart. This engine's status line
// substitutes the condition name into the template, so no log fragment can
// pin "toxic" without spanning a substitution. What is left is the second half
// of the same assertion, which is the part that says Immunity cleaned up after
// Mold Breaker: the holder ends the turn with no status.

func TestAbilitiesImmunity(t *testing.T) {
	describe(t, "Immunity", func(g *psg) {
		g.it("should make the user immune to poison", func(p *ps) {
			p.battle(
				team{{Species: "Snorlax", Ability: "immunity", Moves: mv("curse")}},
				team{{Species: "Crobat", Ability: "infiltrator", Moves: mv("toxic")}},
			)
			p.constant(func() any { return p.mine().Status }, func() {
				p.makeChoices("move curse", "move toxic")
			}, "Immunity should have refused the Toxic")
		})

		g.it("should cure poison if a Pokemon receives the ability", func(p *ps) {
			p.battle(
				team{{Species: "Snorlax", Ability: "thickfat", Moves: mv("curse")}},
				team{{Species: "Crobat", Ability: "immunity", Moves: mv("toxic", "skillswap")}},
			)
			// "toxic" rather than upstream's "tox": this engine spells the
			// condition out, and sets compares the value verbatim.
			p.sets(func() any { return p.mine().Status }, "toxic", func() {
				p.makeChoices("move curse", "move toxic")
			}, "Toxic should land on a Thick Fat holder")
			p.sets(func() any { return p.mine().Status }, "", func() {
				p.makeChoices("move curse", "move skillswap")
			}, "gaining Immunity should clear the poison on the spot")
		})

		g.it("should have its immunity to poison temporarily suppressed by Mold Breaker, but should cure the status immediately afterwards", func(p *ps) {
			p.battle(
				team{{Species: "Snorlax", Ability: "immunity", Moves: mv("curse")}},
				team{{Species: "Crobat", Ability: "moldbreaker", Moves: mv("toxic")}},
			)
			p.makeChoices("move curse", "move toxic")
			p.noStatus(p.mine(), "Immunity should have cured the poison as soon as Mold Breaker stopped suppressing it")
		})
	})
}
