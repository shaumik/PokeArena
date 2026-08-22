//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/magicroom.js.
//
// Four of the six cases come across. Mega Evolution and Primal Reversion are
// both forme changes and neither is modeled, so those two skip; Lopunnite and
// the Red Orb are not in the item set either.
//
// Species. Lopunny resolves to Kangaskhan and Deoxys-Speed to Alakazam through
// the shared table; both are used only as fast Normal / Psychic bodies here.
// Golem is in the dex.
//
// Belly Drum and Sea Incense are not in this dataset, so those two cases report
// the missing name rather than the Magic Room behavior behind it. That absence
// is the finding, which is why they are written out rather than skipped.
//
// The "items that disable moves" case is restated. Upstream reads
// `lastMove.id === 'struggle'` to show the Assault Vest left the holder with
// nothing legal; this engine's Struggle carries no move id, so it never becomes
// the holder's last move and the assertion could not be written that way
// honestly. What the case is about — whether the status move is choosable — is
// exactly what cantMove / canMove ask, so the port asks that on either side of
// the Magic Room going up and keeps upstream's second assertion as it stands.

func TestMovesMagicRoom(t *testing.T) {
	describe(t, "Magic Room", func(g *psg) {
		g.it("should negate residual healing events", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Item: "leftovers", Moves: mv("bellydrum")}},
				team{{Species: "Golem", Moves: mv("magicroom")}},
			)
			lopunny := p.mine()
			p.turn()
			p.equal(lopunny.HP, (lopunny.MaxHP+1)/2,
				"Leftovers should not have healed back any of the Belly Drum payment")
		})

		g.it("should prevent items from being consumed", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Item: "chopleberry", Moves: mv("magicroom")}},
				team{{Species: "Golem", Moves: mv("lowkick")}},
			)
			p.turn()
			p.holdsItem(p.mine(), "the Chople Berry should not have been eaten under Magic Room")
		})

		g.it("should ignore the effects of items that disable moves", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Item: "assaultvest", Moves: mv("protect")}},
				team{{Species: "Golem", Moves: mv("magicroom")}},
			)
			p.cantMove(0, "protect", "the Assault Vest should bar the holder's only status move")
			p.makeChoices("", "move magicroom")
			p.canMove(0, "protect", "Magic Room should suppress the Assault Vest")
			p.makeChoices("move protect", "move magicroom")
			p.equal(p.mine().Volatiles.LastMoveID, "protect",
				"the status move should have gone through under Magic Room")
		})

		g.it("should cause Fling to fail", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Item: "seaincense", Moves: mv("fling")}},
				team{{Species: "Deoxys-Speed", Moves: mv("magicroom")}},
			)
			p.turn()
			p.holdsItem(p.mine(), "Fling should not have been able to throw a suppressed item")
		})

		g.skip("should not prevent Mega Evolution", "mega evolution")

		g.skip("should not prevent Primal Reversion",
			"formes: Primal Reversion is not modeled and the Red Orb is not in the item set")
	})
}
