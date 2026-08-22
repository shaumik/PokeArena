//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/skillswap.js.
//
// Ferroseed has no stand-in row; Magneton is what the shared table uses for
// Ferrothorn, and the same reasoning holds here — Steel is preserved, Grass and
// Iron Barbs are not, and neither matters because every ability in this case is
// set explicitly and no damaging move is used. Wynaut goes to Hypno as usual.
//
// Schooling and Wonder Guard are not abilities this engine models. That is
// reported at team construction and is a finding rather than a reason to skip:
// the case exists precisely to say that Skill Swap must refuse them.
//
// Switch numbering. Showdown rotates a side's team on every switch, so its
// third `switch 3` names the Schooling body. This engine keeps team order
// stable, so the same body is `switch 2` here and the slot numbers below are
// the fixed positions in the team as written.
//
// `sleeptalk` is not in this dataset; `splash` stands in for it.

func TestMovesSkillSwap(t *testing.T) {
	describe(t, "Skill Swap", func(g *psg) {
		g.it("should not be able to Skill Swap certain abilities", func(p *ps) {
			p.battle(
				team{
					{Species: "wynaut", Ability: "moxie", Moves: mv("skillswap", "splash")},
					{Species: "wynaut", Ability: "schooling", Moves: mv("skillswap")},
					{Species: "wynaut", Ability: "wonderguard", Moves: mv("skillswap")},
				},
				team{
					{Species: "ferroseed", As: "Magneton", Ability: "overcoat", Moves: mv("skillswap", "splash")},
					{Species: "ferroseed", As: "Magneton", Ability: "schooling", Moves: mv("skillswap")},
					{Species: "ferroseed", As: "Magneton", Ability: "wonderguard", Moves: mv("skillswap")},
				},
			)
			if p.state() == nil {
				return
			}

			// user: Moxie; target: Overcoat; expected: success
			p.makeChoices("move skillswap", "move splash")
			p.hasAbility(p.mine(), "overcoat", "Skill Swap should have taken Overcoat")
			p.hasAbility(p.foe(), "moxie", "Skill Swap should have given away Moxie")

			// Skill Swap the abilities back
			p.makeChoices("move skillswap", "move splash")

			// user: Moxie; target: Schooling; expected: failure
			p.makeChoices("move skillswap", "switch 2")
			p.hasAbility(p.mine(), "moxie", "Skill Swap should not take Schooling")
			p.hasAbility(p.foe(), "schooling", "Skill Swap should not give Schooling away")

			// user: Moxie; target: Wonder Guard; expected: failure
			p.makeChoices("move skillswap", "switch 3")
			p.hasAbility(p.mine(), "moxie", "Skill Swap should not take Wonder Guard")
			p.hasAbility(p.foe(), "wonderguard", "Skill Swap should not give Wonder Guard away")

			// user: Wonder Guard; target: Moxie; expected: failure
			p.makeChoices("move splash", "move skillswap")
			p.hasAbility(p.mine(), "moxie", "a Wonder Guard user's Skill Swap should not take Moxie")
			p.hasAbility(p.foe(), "wonderguard", "a Wonder Guard user's Skill Swap should not give it away")

			// user: Schooling; target: Moxie; expected: failure
			p.makeChoices("move splash", "switch 2")
			p.makeChoices("move splash", "move skillswap")
			p.hasAbility(p.mine(), "moxie", "a Schooling user's Skill Swap should not take Moxie")
			p.hasAbility(p.foe(), "schooling", "a Schooling user's Skill Swap should not give it away")
		})
	})
}
