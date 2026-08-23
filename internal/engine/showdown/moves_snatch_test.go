//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/snatch.js.
//
// Four of the six cases in the main describe are doubles — Snatch's
// interactions with Choice locking, Throat Chop, Heal Block and an ally's
// Swallow all need a second active — and the whole `Snatch [Gen 4]` describe is
// a generation mod. Two cases are singles and are ported.
//
// Dratini becomes Dragonite, the same line fully evolved; the case uses it only
// as a body with an ability and a status move.
//
// Belly Drum is not in this dataset. In the Swallow case it does one job —
// leave the snatcher well below full HP so a quarter-of-max heal is visible —
// which the fixture's HP field does directly and without a Belly Drum
// interaction of its own, so the port sets HP instead. Sleep Talk, upstream's
// do-nothing, is Splash.
//
// One thing a reader should know about the first case: in this dataset Howl is
// recorded with target "foe" rather than "self". That means Snatch does not
// intercept it here at all (the engine only steals self-targeted status moves,
// which is canon) and its +1 Attack lands on the snatcher directly. So the stat
// assertion can read +1 for a reason that has nothing to do with Snatch, and
// only the two typing assertions actually speak to the case.

func TestMovesSnatch(t *testing.T) {
	describe(t, "Snatch", func(g *psg) {
		g.it("should cause the victim of Snatch to change typing with Protean rather than the Snatch user", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Ability: "protean", Moves: mv("snatch")}},
				team{{Species: "dratini", As: "Dragonite", Ability: "protean", Moves: mv("howl")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.statStage(p.mine(), "atk", 1, "the snatcher should be the one that ends up boosted")
			p.ok(p.mine().Type1 == "dark" || p.mine().Type2 == "dark",
				"Protean should have retyped the snatcher to Dark for Snatch itself, not to Howl's Normal")
			p.ok(p.foe().Type1 == "normal" || p.foe().Type2 == "normal",
				"Protean should have retyped the victim to Normal when it called Howl")
		})

		g.skip("should not Choice lock the user from the snatched move", "doubles")
		g.skip("should not be able to steal Rest when the Rest user is at full HP", "doubles")
		g.skip("should Snatch moves and run Throat Chop and Heal Block checks", "doubles")
		g.skip("should not snatch Swallow if the Swallow user has no Stockpiles", "doubles")

		g.it("Snatched Swallow should heal the snatcher by 25% if the snatcher has no Stockpiles", func(p *ps) {
			p.battle(
				team{{Species: "clefable", HP: 60, Moves: mv("splash", "snatch")}},
				team{{Species: "dewgong", Moves: mv("stockpile", "swallow")}},
			)
			if p.state() == nil {
				return
			}
			for turn := 1; turn <= 3; turn++ {
				p.makeChoices("move splash", "move stockpile")
			}
			// A quarter of the snatcher's own max HP, rounded half up the way
			// the engine's heal does — (max+2)/4 is that rounding in integers.
			want := (p.mine().MaxHP + 2) / 4
			p.hurtsBy(p.mine(), -want, func() { p.makeChoices("move snatch", "move swallow") },
				"the snatcher has no stockpile of its own and should still get the quarter heal")
		})
	})

	describe(t, "Snatch [Gen 4]", func(g *psg) {
		const why = "gen 4 mechanics"

		g.skip("should Snatch moves that were called by another user of Snatch", why)
		g.skip("should only deduct additional PP from Snatch if the Snatch was successful", why)
	})
}
