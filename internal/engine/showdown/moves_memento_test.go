//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/memento.js.
//
// Memento is not in this 538-move dataset. The three non-Z cases are written
// out rather than skipped, because that absence is the finding.
//
// Species. Whimsicott has no stand-in row and is only the Memento user, so it
// is built as Clefable (Fairy is the half it shares; nothing here reads it).
// Landorus is the body that comes in afterwards and becomes Sandslash, which is
// what the table uses for Landorus-Therian. Wynaut and Corviknight go through
// the table as usual.
//
// Prankster is not modeled here, and the Substitute case only wants it so the
// Substitute is up before Memento lands. Hypno already outspeeds Clefable, so
// the ability is dropped and the speed tier does the same job.
//
// Upstream reads `battle.requestState`; the equivalent state here is the
// battle's phase plus whether the user actually fainted, and both are asserted.
//
// `sleeptalk` is not in this dataset; `splash` stands in for it.

func TestMovesMemento(t *testing.T) {
	describe(t, "Memento", func(g *psg) {
		g.it(`should cause the user to faint even if the target has Clear Body`, func(p *ps) {
			p.battle(
				team{
					{Species: "whimsicott", As: "Clefable", Moves: mv("memento")},
					{Species: "landorus", As: "Sandslash", Moves: mv("splash")},
				},
				team{{Species: "wynaut", Ability: "clearbody", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.fainted(p.slot(0, 1), "Memento should faint its user even when Clear Body refused the drops")
			p.ok(p.state().Phase == "replace", "the fainted user should owe a replacement")
		})

		g.it(`should not cause the user to faint if used into Substitute`, func(p *ps) {
			p.battle(
				team{
					{Species: "whimsicott", As: "Clefable", Moves: mv("memento")},
					{Species: "landorus", As: "Sandslash", Moves: mv("splash")},
				},
				team{{Species: "wynaut", Ability: "noability", Moves: mv("substitute")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.notFainted(p.slot(0, 1), "Memento blocked by a Substitute should not faint its user")
			p.isFalse(p.state().Phase == "replace", "no replacement should be owed")
		})

		g.it(`should cause the user to faint after stat drops from Mirror Armor`, func(p *ps) {
			p.battle(
				team{
					{Species: "whimsicott", As: "Clefable", Moves: mv("memento")},
					{Species: "landorus", As: "Sandslash", Moves: mv("splash")},
				},
				team{{Species: "corviknight", Ability: "mirrorarmor", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			whimsicott := p.slot(0, 1)
			p.statStage(whimsicott, "atk", -2, "Mirror Armor should have sent the Attack drop back")
			p.statStage(whimsicott, "spa", -2, "Mirror Armor should have sent the Sp. Atk drop back")
			p.fainted(whimsicott, "Memento should still faint its user")
			p.ok(p.state().Phase == "replace", "the fainted user should owe a replacement")
		})

		g.skip(`should set the Z-Memento healing flag even if the Memento itself was not successful`, "Z-moves")
	})
}
