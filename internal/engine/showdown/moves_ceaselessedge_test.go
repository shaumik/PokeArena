//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/ceaselessedge.js.
//
// Ceaseless Edge is not in this dataset, so every live case here stops at
// "move ceaselessedge is not in this dataset". That absence is the finding, so
// the cases are written out in full rather than skipped: if the move is ever
// added, these say what it has to do.
//
// Substitutions, none of which the shared table covers. Samurott-Hisui is the
// Water-type physical attacker that learns the move; Blastoise is a Water body
// of the same role and the Dark half is not preserved, which nothing here
// reads — every assertion is about a Spikes layer, not about damage. Registeel
// is a Steel body that stands still and Magneton is this dex's only Steel one.
// Regieleki is the fast Electric body that has to get its Substitute or Protect
// up before the attack lands, and Electrode (Speed 150 against Blastoise's 78)
// keeps exactly that.
//
// The last case leans on No Guard making Sheer Cold hit as well: that is
// upstream's own arrangement, and it is what makes the case deterministic
// enough to run over every seed here.
//
// The doubles case is the one that cannot be re-expressed — its whole point is
// that the layer lands on the target's side rather than on the target's slot.
//
// `splash` stands in for upstream's `sleeptalk`, which is not in this dataset
// and is a do-nothing in every case that uses it.

func TestMovesCeaselessEdge(t *testing.T) {
	describe(t, "Ceaseless Edge", func(g *psg) {
		g.it("should set up Spikes on the side of the opponent", func(p *ps) {
			p.battle(
				team{{
					Species: "samurotthisui", As: "Blastoise", Ability: "noguard",
					Moves: mv("ceaselessedge"),
				}},
				team{{Species: "registeel", As: "Magneton", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.state().Sides[1].Conditions.Hazards.Spikes, 1,
				"Ceaseless Edge should have laid one layer of Spikes on the foe's side")
		})

		g.skip("should set up Spikes on the side of the opponent, not necessarily the target, in a double battle",
			"doubles")

		g.it("should still set up Spikes on the side of the opponent that is behind a Substitute", func(p *ps) {
			p.battle(
				team{{
					Species: "samurotthisui", As: "Blastoise", Ability: "noguard",
					Moves: mv("ceaselessedge"),
				}},
				team{{Species: "regieleki", As: "Electrode", Moves: mv("substitute")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.state().Sides[1].Conditions.Hazards.Spikes, 1,
				"a Substitute should not stop the Spikes half of the move")
		})

		g.it("should not set up Spikes if the move does not hit opponent or its Substitute", func(p *ps) {
			p.battle(
				team{{
					Species: "samurotthisui", As: "Blastoise", Ability: "noguard",
					Moves: mv("ceaselessedge"),
				}},
				team{{Species: "regieleki", As: "Electrode", Moves: mv("protect")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.state().Sides[1].Conditions.Hazards.Spikes, 0,
				"a move that never connected should leave no Spikes")
		})

		g.it("should not be bounced back by Magic Bounce", func(p *ps) {
			p.battle(
				team{{
					Species: "samurotthisui", As: "Blastoise", Ability: "noguard",
					Moves: mv("ceaselessedge"),
				}},
				team{{Species: "registeel", As: "Magneton", Ability: "magicbounce", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.state().Sides[0].Conditions.Hazards.Spikes, 0,
				"the Spikes should not have been reflected onto the user's side")
			p.equal(p.state().Sides[1].Conditions.Hazards.Spikes, 1,
				"the Spikes should have landed on the Magic Bounce holder's side anyway")
		})

		g.it("should have its Spikes prevented by Sheer Force", func(p *ps) {
			p.battle(
				team{{
					Species: "samurotthisui", As: "Blastoise", Ability: "sheerforce",
					Moves: mv("ceaselessedge"),
				}},
				team{{Species: "registeel", As: "Magneton", Ability: "noguard", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.state().Sides[1].Conditions.Hazards.Spikes, 0,
				"Sheer Force should have traded the Spikes away for the power boost")
		})

		g.it("should not set Spikes when the user faints from Rocky Helmet", func(p *ps) {
			p.battle(
				team{
					{
						Species: "samurotthisui", As: "Blastoise", Ability: "noguard", Item: "focussash",
						Moves: mv("ceaselessedge"),
					},
					{Species: "wynaut", Moves: mv("splash")},
				},
				team{{Species: "regieleki", As: "Electrode", Item: "rockyhelmet", Moves: mv("sheercold")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.fainted(p.mine(), "Focus Sash then Rocky Helmet should have killed the user during its own move")
			p.equal(p.state().Sides[1].Conditions.Hazards.Spikes, 0,
				"a user that faints mid-move should not get to lay the Spikes")
		})
	})
}
