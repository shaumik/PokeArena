//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/damp.js.
//
// Politoed is not in the dex and has no stand-in row. Poliwrath is built
// instead — the same line's fully evolved form, water, and it carries Damp as
// one of its own abilities. Aron is likewise absent; Magneton takes its place
// as the Aftermath body, a steel type frail enough that Close Combat takes it
// out in one, which is the whole of Aron's job here.

func TestAbilitiesDamp(t *testing.T) {
	describe(t, "Damp", func(g *psg) {
		g.it("should prevent self-destruction moves from activating", func(p *ps) {
			p.battle(
				team{{Species: "Politoed", As: "Poliwrath", Ability: "damp", Moves: mv("calmmind")}},
				team{{Species: "Electrode", Ability: "static", Moves: mv("explosion")}},
			)
			p.makeChoices("move calmmind", "move explosion")
			p.fullHP(p.mine(), "Damp should have stopped the Explosion before it dealt damage")
			p.fullHP(p.foe(), "a blocked Explosion should not cost its user any HP either")
			p.logHas("! (Damp)", "the block should be announced")
		})

		g.it("should prevent Aftermath from activating", func(p *ps) {
			p.battle(
				team{{Species: "Poliwrath", Ability: "damp", Moves: mv("closecombat")}},
				team{{Species: "Aron", As: "Magneton", Ability: "aftermath", Moves: mv("leer")}},
			)
			p.makeChoices("move closecombat", "move leer")
			p.fullHP(p.mine(), "Damp should have stopped Aftermath's chip")
			p.fainted(p.foe(), "Close Combat should still have knocked the Aftermath holder out")
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Politoed", As: "Poliwrath", Ability: "damp", Moves: mv("calmmind")}},
				team{{Species: "Electrode", Ability: "moldbreaker", Moves: mv("explosion")}},
			)
			p.hurts(p.mine(), func() {
				p.makeChoices("move calmmind", "move explosion")
			}, "Mold Breaker should have let the Explosion through Damp")
			p.fainted(p.foe(), "the Explosion's user should have gone down with it")
		})
	})
}
