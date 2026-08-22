//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/miracleeye.js.
//
// Upstream rigs the two evasion cases with battle.boost and inspects the
// accuracy roll through battle.onEvent. Neither hook exists here, so both are
// re-expressed as play: the boost is put on with Double Team or taken off with
// Sweet Scent, and the claim about accuracy is stated as the outcome it
// implies — a move that must land every time lands on every seed. Adding those
// moves to the fixture is the only change; the stat stages the case is about
// are the same ones upstream sets by hand.
//
// Smeargle goes to Chansey and Forretress to Magneton through the shared
// table. `sleeptalk` is not in this dataset; `splash` stands in for it.

func TestMovesMiracleEye(t *testing.T) {
	describe(t, "Miracle Eye", func(g *psg) {
		g.skip(`should negate Psychic immunities`,
			"this 80-species Kanto dex has no Dark-type at all, so the Psychic immunity Miracle Eye lifts cannot be set up")

		g.it(`should ignore the effect of positive evasion stat stages`, func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Moves: mv("tackle", "miracleeye")}},
				team{{Species: "Forretress", As: "Magneton", Moves: mv("doubleteam")}},
			)
			if p.state() == nil {
				return
			}
			// Double Team puts on the +6 upstream sets directly. The first turn
			// does not count: this engine zeroes existing positive evasion at
			// the moment Miracle Eye lands, so that Double Team is spent
			// getting Miracle Eye up. Six more follow, and repeat uses of
			// Miracle Eye just fail, which keeps the user inert meanwhile.
			p.makeChoices("move miracleeye", "move doubleteam")
			for i := 0; i < 6; i++ {
				p.makeChoices("move miracleeye", "move doubleteam")
			}
			p.statStage(p.foe(), "evasion", 6, "the target should be at maximum evasion")

			p.makeChoices("move tackle", "move doubleteam")
			p.damaged(p.foe(), "Miracle Eye should ignore positive evasion boosts, so Tackle cannot miss")
		})

		g.it(`should not ignore the effect of negative evasion stat stages`, func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Moves: mv("zapcannon", "miracleeye", "sweetscent")}},
				team{{Species: "Zapdos", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move miracleeye", "move splash")
			// Three Sweet Scents are the -6 upstream sets directly. Zap Cannon's
			// 50% accuracy only becomes unmissable if that drop is still being
			// read through Miracle Eye.
			for i := 0; i < 3; i++ {
				p.makeChoices("move sweetscent", "move splash")
			}
			p.statStage(p.foe(), "evasion", -6, "the target should be at minimum evasion")

			p.makeChoices("move zapcannon", "move splash")
			p.damaged(p.foe(), "Miracle Eye should not ignore negative evasion drops, so Zap Cannon cannot miss")
		})
	})
}
