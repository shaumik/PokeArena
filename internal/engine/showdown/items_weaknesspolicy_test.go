//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/weaknesspolicy.js.
//
// Weakness Policy is modeled, so the three singles cases are live. The doubles
// case skips.
//
// Species. Lucario, Zygarde and Aron have no stand-in row. Machamp replaces
// Lucario as a Fighting body — all the first and third cases need is a Fighting
// move landing on a Normal target, super effective in one case and fixed damage
// in the other. Dragonite replaces Zygarde, which keeps the Dragon typing Dragon
// Tail has to be super effective against; Magneton replaces Aron and keeps its
// Steel typing, and is only the Pokemon Dragon Tail drags in. Blissey goes
// through its stand-in row.
//
// Sleep Talk is not in this dataset and is inert filler on the holder in the
// last case, so Splash stands in for it.

func TestItemsWeaknessPolicy(t *testing.T) {
	describe(t, "Weakness Policy", func(g *psg) {
		g.it("should be triggered by super effective hits", func(p *ps) {
			p.battle(
				team{{Species: "Lucario", As: "Machamp", Ability: "justified", Moves: mv("aurasphere")}},
				team{{Species: "Blissey", Ability: "naturalcure", Item: "weaknesspolicy", Moves: mv("softboiled")}},
			)
			p.makeChoices("move aurasphere", "move softboiled")
			holder := p.foe()
			p.noItem(holder, "the policy should have been spent")
			p.statStage(holder, "atk", 2, "")
			p.statStage(holder, "spa", 2, "")
		})

		g.skip("should respect individual type effectivenesses in doubles", "doubles")

		g.it("should not be triggered by fixed damage moves", func(p *ps) {
			p.battle(
				team{{Species: "Lucario", As: "Machamp", Ability: "justified", Moves: mv("seismictoss")}},
				team{{Species: "Blissey", Ability: "naturalcure", Item: "weaknesspolicy", Moves: mv("softboiled")}},
			)
			p.makeChoices("move seismictoss", "move softboiled")
			holder := p.foe()
			p.holdsItem(holder, "a fixed-damage move has no effectiveness to answer")
			p.statStage(holder, "atk", 0, "")
			p.statStage(holder, "spa", 0, "")
		})

		g.it("should trigger before forced switching moves", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "compoundeyes", Moves: mv("dragontail")}},
				team{
					{Species: "Zygarde", As: "Dragonite", Item: "weaknesspolicy", Moves: mv("splash")},
					{Species: "Aron", As: "Magneton", Moves: mv("splash")},
				},
			)
			p.turn()
			// The holder is dragged out by the same move that triggers the
			// policy, so the assertion reads its team slot rather than the active.
			p.noItem(p.slot(1, 1), "the policy should have fired before the drag-out")
		})
	})
}
