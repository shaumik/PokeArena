//go:build showdown

package showdown

import (
	"strings"
	"testing"
)

// Ported from test/sim/moves/focuspunch.js.
//
// Chansey and Venusaur are both in this dex, so the seven modern cases are
// literal translations. Three do not come across: the "attacking move
// followed by a status move in one turn" case needs a second active slot, the
// pre-Gen-5 PP case needs a gen-mod layer, and the Dynamax case needs a
// gimmick this engine does not model.
//
// The last ported case ("should tighten focus after switches") needs Sleep
// Talk as inert filler for the switching side, and Sleep Talk is not in this
// dataset — so the case reports that gap rather than its own subject. Worth
// re-running once Sleep Talk lands: the engine emits its "tightening its
// focus" line at the top of ResolveTurn, ahead of the switch phase, which is
// the Gen 3-4 ordering this case says should have changed in Gen 5.

func TestMovesFocusPunch(t *testing.T) {
	describe(t, "Focus Punch", func(g *psg) {
		g.it("should cause the user to lose focus if hit by an attacking move", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Moves: mv("focuspunch")}},
				team{{Species: "Venusaur", Moves: mv("magicalleaf")}},
			)
			p.turn()
			p.fullHP(p.foe(), "a Focus Punch user hit by an attacking move should never land the punch")
		})

		g.it("should not cause the user to lose focus if hit by a status move", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Moves: mv("focuspunch")}},
				team{{Species: "Venusaur", Moves: mv("growl")}},
			)
			p.turn()
			p.damaged(p.foe(), "Growl deals no damage, so focus should hold")
		})

		g.it("should not cause the user to lose focus if hit while behind a substitute", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Moves: mv("substitute", "focuspunch")}},
				team{{Species: "Venusaur", Moves: mv("magicalleaf")}},
			)
			p.turn()
			p.makeChoices("move focuspunch", "")
			p.damaged(p.foe(), "damage absorbed by a Substitute should not break focus")
		})

		g.it("should cause the user to lose focus if hit by a move called by Nature Power", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Moves: mv("focuspunch")}},
				team{{Species: "Venusaur", Moves: mv("naturepower")}},
			)
			p.turn()
			p.fullHP(p.foe(), "the move Nature Power calls should break focus like any other attack")
		})

		g.it("should not cause the user to lose focus on later uses of Focus Punch if hit", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Moves: mv("focuspunch")}},
				team{{Species: "Venusaur", Moves: mv("magicalleaf", "growl")}},
			)
			p.turn()
			p.fullHP(p.foe(), "the first punch is lost to Magical Leaf")
			p.makeChoices("", "move growl")
			p.damaged(p.foe(), "losing focus once should not carry into the next turn")
		})

		g.skip("should cause the user to lose focus if hit by an attacking move followed by a status move in one turn",
			"doubles")

		g.it("should not deduct PP if the user lost focus", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Moves: mv("focuspunch")}},
				team{{Species: "Venusaur", Moves: mv("magicalleaf", "growl")}},
			)
			// Upstream holds a reference to the move slot; the slot is read
			// back out of the active each time here, which is the same thing
			// for a Pokemon that never leaves the field.
			maxPP := p.mine().Moves[0].MaxPP
			p.turn()
			p.equal(p.mine().Moves[0].PP, maxPP, "a punch cancelled by lost focus should cost no PP")
			p.makeChoices("", "move growl")
			p.equal(p.mine().Moves[0].PP, maxPP-1, "a punch that executes should cost one PP")
		})

		g.skip("should deduct PP if the user lost focus before Gen 5", "gen 4 mechanics")

		g.it("should display a message indicating the Pokemon is tightening focus", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Moves: mv("focuspunch")}},
				team{{Species: "Venusaur", Moves: mv("magicalleaf")}},
			)
			p.turn()
			p.logHas("is tightening its focus!", "Focus Punch should announce itself at the top of the turn")
		})

		g.skip("should not tighten the Pokemon's focus when Dynamaxing or already Dynamaxed", "Dynamax")

		g.it("should tighten focus after switches in Gen 5+", func(p *ps) {
			// Salamence and Wynaut go through their stand-in rows (Dragonite
			// and Hypno); neither identity matters, one side just has to
			// switch while the other charges a punch.
			p.battle(
				team{{Species: "salamence", Moves: mv("focuspunch")}},
				team{
					{Species: "mew", Moves: mv("sleeptalk")},
					{Species: "wynaut", Moves: mv("sleeptalk")},
				},
			)
			p.makeChoices("move focuspunch", "switch 2")
			// Upstream compares indices into the debug log. The prose log is
			// ordered the same way, so the question survives: does the charge
			// message land after the switch-in announcement?
			p.logHas("is tightening its focus!", "")
			p.logHas("Go, ", "")
			text := p.logText()
			p.ok(strings.Index(text, "is tightening its focus!") > strings.Index(text, "Go, "),
				"Focus Punch's charge message should occur after switches in Gen 5+")
		})

		g.skip("should tighten focus before switches in Gens 3-4", "gen 4 mechanics")
	})
}
