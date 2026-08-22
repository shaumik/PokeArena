//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/fakeout.js.
//
// The whole file is singles and comes across.
//
// Species. Chansey, Venusaur and Primeape's neighbors are all in the dex.
// Blissey would resolve to Chansey through the shared table, which would put
// two Chanseys on one side; Wigglytuff is named instead so the two bodies stay
// distinguishable, and it is doing the same job — a Normal body that also knows
// Fake Out. Oricorio has no row and is only a Dancer holder, so Wigglytuff
// takes that role too. Hitmontop becomes Hitmonlee and Gallade becomes Machamp,
// both Fighting bodies; Machamp carries Steadfast natively.
//
// Sleep Talk is not in this dataset; Splash stands in where upstream uses it to
// spend a turn.
//
// Dancer is not modeled. It is kept in the fixture rather than stripped,
// because "a Dancer activation counts as the user having moved" is the whole
// subject of that case.
//
// Upstream's second `switch 2` relies on Showdown reordering a side's list so
// the active is always first. This engine keeps team slots fixed, so the step
// that brings the lead back is written as `switch 1`.

func TestMovesFakeOut(t *testing.T) {
	describe(t, "Fake Out", func(g *psg) {
		g.it("should flinch on the first turn out", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Ability: "naturalcure", Moves: mv("fakeout")}},
				team{{Species: "Venusaur", Ability: "overgrow", Moves: mv("swift")}},
			)
			p.makeChoices("move fakeout", "move swift")
			p.fullHP(p.mine(), "the flinch should have stopped Swift")
		})

		g.it("should not flinch on the second turn out", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Ability: "naturalcure", Moves: mv("fakeout")}},
				team{{Species: "Venusaur", Ability: "overgrow", Moves: mv("swift")}},
			)
			p.makeChoices("move fakeout", "move swift")
			p.fullHP(p.mine(), "the flinch should have stopped Swift")
			p.makeChoices("move fakeout", "move swift")
			p.damaged(p.mine(), "Fake Out should have failed on the second turn out")
		})

		g.it("should flinch after switching out and back in to refresh the move", func(p *ps) {
			p.battle(
				team{
					{Species: "Chansey", Ability: "naturalcure", Moves: mv("fakeout")},
					{Species: "Blissey", As: "Wigglytuff", Ability: "naturalcure", Moves: mv("fakeout")},
				},
				team{{Species: "Venusaur", Ability: "overgrow", Moves: mv("swift", "splash")}},
			)
			p.makeChoices("move fakeout", "move swift")
			p.makeChoices("switch 2", "move splash")
			p.makeChoices("switch 1", "move splash")
			p.makeChoices("move fakeout", "move swift")
			p.fullHP(p.mine(), "re-entering should have refreshed Fake Out")
		})

		g.it("should not flinch if the user has already used a Dancer move first", func(p *ps) {
			p.battle(
				team{
					{Species: "Chansey", Ability: "naturalcure", Moves: mv("fakeout")},
					{Species: "Oricorio", As: "Wigglytuff", Ability: "dancer", Moves: mv("fakeout")},
				},
				team{{Species: "Venusaur", Ability: "overgrow", Moves: mv("swift", "quiverdance")}},
			)
			p.makeChoices("switch 2", "move quiverdance")
			p.makeChoices("move fakeout", "move swift")
			p.damaged(p.mine(),
				"the Dancer activation counts as the user's first action, so Fake Out should have failed")
		})

		g.it("should not flinch if the target had prepared to Focus Punch", func(p *ps) {
			p.battle(
				team{{Species: "Hitmontop", As: "Hitmonlee", Ability: "steadfast", Moves: mv("fakeout")}},
				team{{Species: "Gallade", As: "Machamp", Ability: "steadfast", Moves: mv("focuspunch")}},
			)
			p.turn()
			p.statStage(p.foe(), "spe", 0, "no flinch, so Steadfast should not have fired")
		})
	})
}
