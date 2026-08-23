//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/revivalblessing.js.
//
// Revival Blessing is not in this dataset, so the three live cases here stop at
// "move revivalblessing is not in this dataset". That absence is the finding.
//
// Getting an ally fainted is scaffolding, not the subject, and upstream does it
// with Memento, which is not in this dataset either. Rather than bury the real
// finding under a second one, the lead starts at 1 HP and the opposing Pokemon
// knocks it out with Tackle — the same state one turn later, by a route this
// engine has.
//
// The revival target itself has no counterpart. Upstream answers a second
// request (`battle.makeChoices('switch corviknight', '')`) to say which fainted
// ally comes back; this engine has no such phase, so the port stops after the
// Revival Blessing turn and asserts on the team state instead of on the
// request. For the same reason the `requestState === 'switch'` half of the
// second case is dropped and only its title claim — that the user stays out —
// is checked.
//
// Team order does not change on a switch here, the way Showdown's
// `side.pokemon` array does, so the revived Pokemon is read back from slot 1
// rather than from upstream's index 1.
//
// Substitutions. Corviknight goes through the shared table to Magneton. Zoroark
// is the Revival Blessing user and is deliberately given Run Away upstream so
// Illusion cannot interfere; Persian is a plain Normal body doing the same job,
// and Dark is not preserved because nothing here reads it. Goodra becomes
// Dragonite.
//
// Upstream's Run Away and Gooey are both "an ability that does not interfere":
// Run Away is upstream's own idiom for it, and Gooey only fires on a contact
// move that never comes. This engine spells that "noability", and using it
// keeps the run from reporting two abilities that have nothing to do with
// Revival Blessing.
//
// The last two cases are doubles, and both are specifically about what happens
// when the revived Pokemon lands in an active slot, which this engine's single
// slot per side cannot express.

func TestMovesRevivalBlessing(t *testing.T) {
	describe(t, "Revival Blessing", func(g *psg) {
		g.it("should revive allies", func(p *ps) {
			p.battle(
				team{
					{Species: "corviknight", Ability: "noability", HP: 1, Moves: mv("splash")},
					{Species: "zoroark", As: "Persian", Ability: "noability", Moves: mv("revivalblessing")},
					{Species: "wynaut", Ability: "noability", Moves: mv("splash")},
				},
				team{{Species: "goodra", As: "Dragonite", Ability: "noability", Moves: mv("tackle")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move tackle")
			p.fainted(p.slot(0, 1), "the lead should have gone down before Revival Blessing is used")
			p.makeChoices("switch 2", "")
			p.makeChoices("move revivalblessing", "move tackle")

			alive := 0
			for i := range p.state().Sides[0].Team {
				if !p.state().Sides[0].Team[i].Fainted {
					alive++
				}
			}
			p.equal(alive, 3, "Revival Blessing should have put the fainted ally back on the roster")
			revived := p.slot(0, 1)
			p.equal(revived.HP, revived.MaxHP/2, "a revived Pokemon should come back at half its maximum HP")
		})

		g.it("should not actually switch the active Pokemon", func(p *ps) {
			p.battle(
				team{
					{Species: "corviknight", Ability: "noability", HP: 1, Moves: mv("splash")},
					{Species: "zoroark", As: "Persian", Ability: "noability", Moves: mv("revivalblessing")},
					{Species: "wynaut", Ability: "noability", Moves: mv("splash")},
				},
				team{{Species: "goodra", As: "Dragonite", Ability: "noability", Moves: mv("tackle")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move tackle")
			p.makeChoices("switch 2", "")
			p.makeChoices("move revivalblessing", "move tackle")
			p.species(p.mine(), "Persian", "the Revival Blessing user should still be the one out")
		})

		g.it("should let you revive even with one Pokemon remaining", func(p *ps) {
			p.battle(
				team{
					{Species: "corviknight", Ability: "noability", HP: 1, Moves: mv("splash")},
					{Species: "zoroark", As: "Persian", Ability: "noability", Moves: mv("revivalblessing")},
				},
				team{{Species: "goodra", As: "Dragonite", Ability: "noability", Moves: mv("tackle")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move tackle")
			p.makeChoices("switch 2", "")
			p.makeChoices("move revivalblessing", "move tackle")

			alive := 0
			for i := range p.state().Sides[0].Team {
				if !p.state().Sides[0].Team[i].Fainted {
					alive++
				}
			}
			p.equal(alive, 2, "the last fainted ally should still have been revivable")
		})

		g.skip("should send the Pokemon back in immediately if in an active slot in Doubles", "doubles")
		g.skip("shouldn't allow a fainted Pokemon to make its move the same turn after being revived", "doubles")
	})
}
