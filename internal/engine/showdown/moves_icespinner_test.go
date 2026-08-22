//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/icespinner.js.
//
// Psychic Surge is not modeled, so the terrain is put up with the move of the
// same name on the turn before — which is all either live case needs, since
// both only require the terrain to be standing when Ice Spinner resolves.
// Registeel has no stand-in row and is built as Magneton, the table's usual
// Steel body.
//
// Shedinja has no stand-in on purpose (Wonder Guard is its identity), but the
// case does not use Wonder Guard: it uses Shedinja as something that dies to
// the contact recoil of its own attack. A Hypno started at 1 HP is the same
// shape, and is what the Knock Off port does for the same upstream trick.
//
// The two `it.skip` cases are recorded as skips so the case count matches the
// original.
//
// `sleeptalk` is not in this dataset; `splash` stands in for it.

func TestMovesIceSpinner(t *testing.T) {
	describe(t, "Ice Spinner", func(g *psg) {
		g.it(`should remove Terrains if the user is active and on the field`, func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Moves: mv("icespinner", "splash")}},
				team{{Species: "registeel", As: "Magneton", Moves: mv("psychicterrain", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move psychicterrain")
			p.equal(p.terrain(), "psychic", "the terrain should be standing before Ice Spinner")
			p.makeChoices("move icespinner", "move splash")
			p.equal(p.terrain(), "", "Ice Spinner should have swept the terrain away")
		})

		g.skip(`should not remove Terrains if the user faints from Life Orb`, "pending upstream (it.skip)")

		g.it(`should not remove Terrains if the user faints from Rocky Helmet`, func(p *ps) {
			p.battle(
				team{
					{Species: "shedinja", As: "Hypno", Ability: "noability", HP: 1, Moves: mv("icespinner", "splash")},
					{Species: "wynaut", Moves: mv("splash")},
				},
				team{{Species: "registeel", As: "Magneton", Item: "rockyhelmet", Moves: mv("psychicterrain", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move psychicterrain")
			p.makeChoices("move icespinner", "move splash")
			p.fainted(p.slot(0, 1), "the Rocky Helmet recoil should have killed the Ice Spinner user")
			p.equal(p.terrain(), "psychic", "a user that dies mid-move should not clear the terrain")
		})

		g.skip(`should not remove Terrains if the user is forced out via Red Card`, "pending upstream (it.skip)")
	})
}
