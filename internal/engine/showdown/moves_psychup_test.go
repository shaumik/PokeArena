//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/psychup.js.
//
// Both live cases are written upstream as doubles, but only so that Psych Up
// can be aimed at an ally whose boosts the test set up by hand. Psych Up reads
// the same thing off whichever Pokemon it is pointed at, so the ally is
// incidental and both are re-expressed against the foe in singles. Nothing
// about what is copied changes.
//
// Palkia is not in this dex and has no stand-in row; it is only the reader, so
// Mew stands in (a psychic body with no relevant ability). Smeargle and Suicune
// resolve through the shared table; in the singles re-expression the two roles
// collapse into the single foe, so only one of them is needed.
//
// Sleep Talk is not in this dataset. Upstream uses it purely as a do-nothing,
// so Splash stands in wherever it appears.
//
// The four assertions in the first case run as one sequence, exactly as
// upstream writes them, so the two "should gain" halves only mean something if
// the two "should lose" halves before them have already taken the volatile
// away. That is upstream's own arrangement and it is kept, but it is worth
// knowing when reading a run: on an engine that never clears the volatile, the
// gain assertions pass for the wrong reason and only the lose ones report.
//
// The negative-stat half of the second case needs the foe at -2 Attack. Upstream
// gets there by aiming Feather Dance at a Pokemon that never boosted; in singles
// the foe has already used Swords Dance for the first half, so the port walks it
// back down with two Feather Dances (+2 → 0 → -2) and copies from there.
//
// The Gen 5 block skips whole.

func TestMovesPsychUp(t *testing.T) {
	describe(t, "Psych Up", func(g *psg) {
		g.it("should copy the opponent's crit ratio", func(p *ps) {
			p.battle(
				team{{
					Species: "Palkia", As: "Mew", Ability: "noability",
					Moves: mv("splash", "focusenergy", "psychup", "laserfocus"),
				}},
				team{{
					Species: "Suicune", Ability: "noability",
					Moves: mv("splash", "focusenergy", "laserfocus"),
				}},
			)
			mine := p.mine()

			p.makeChoices("move focusenergy", "move splash")
			p.makeChoices("move psychup", "move splash")
			p.isFalse(mine.Volatiles.FocusEnergy,
				"A pokemon should lose a Focus Energy boost if the target of Psych Up does not have a Focus Energy boost.")

			p.makeChoices("move splash", "move focusenergy")
			p.makeChoices("move psychup", "move splash")
			p.ok(mine.Volatiles.FocusEnergy,
				"A pokemon should gain a Focus Energy boost if the target of Psych Up has a Focus Energy boost.")

			p.makeChoices("move laserfocus", "move splash")
			p.makeChoices("move psychup", "move splash")
			p.isFalse(mine.Volatiles.LaserFocus,
				"A pokemon should lose a Laser Focus boost if the target of Psych Up does not have a Laser Focus boost.")

			p.makeChoices("move splash", "move laserfocus")
			p.makeChoices("move psychup", "move splash")
			p.ok(mine.Volatiles.LaserFocus,
				"A pokemon should gain a Laser Focus boost if the target of Psych Up has a Laser Focus boost.")
		})

		g.it("should copy both positive and negative stat changes", func(p *ps) {
			p.battle(
				team{{
					Species: "Palkia", As: "Mew", Ability: "noability",
					Moves: mv("splash", "psychup", "featherdance"),
				}},
				team{{
					Species: "Smeargle", Ability: "noability",
					Moves: mv("splash", "swordsdance"),
				}},
			)
			mine := p.mine()

			p.makeChoices("move splash", "move swordsdance")
			p.makeChoices("move psychup", "move splash")
			p.statStage(mine, "atk", 2,
				"A pokemon should copy the target's positive stat changes when using Psych Up.")

			p.makeChoices("move featherdance", "move splash")
			p.makeChoices("move featherdance", "move splash")
			p.statStage(p.foe(), "atk", -2, "the foe should be at -2 before the second copy")
			p.makeChoices("move psychup", "move splash")
			p.statStage(mine, "atk", -2,
				"A pokemon should copy the target's negative stat changes when using Psych Up.")
		})
	})

	describe(t, "Psych Up [Gen 5]", func(g *psg) {
		g.skip("should not copy the opponent's crit ratio", "gen 5 mechanics")
	})
}
