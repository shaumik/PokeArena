//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/stoneaxe.js.
//
// Stone Axe is not in this dataset, so every live case here stops at "move
// stoneaxe is not in this dataset". That absence is the finding, so the cases
// are written out in full rather than skipped.
//
// Substitutions, none of which the shared table covers. Kleavor is the Rock
// physical attacker that learns the move and Kabutops is this dex's; the Bug
// half is not preserved and nothing here reads it, since every assertion is
// about the Stealth Rock layer rather than about damage. Registeel is a Steel
// body that stands still, and Magneton is the only Steel body here. Regieleki
// has to get its Substitute or Protect up before the attack lands, and
// Electrode (Speed 150 against Kabutops's 80) keeps that.
//
// The doubles case is the one that cannot be re-expressed: its point is that
// the rocks land on the target's side rather than on the target's slot.
//
// `splash` stands in for upstream's `sleeptalk`, which is not in this dataset
// and is a do-nothing wherever it appears here.

func TestMovesStoneAxe(t *testing.T) {
	describe(t, "Stone Axe", func(g *psg) {
		g.it("should set up Stealth Rock on the side of the opponent", func(p *ps) {
			p.battle(
				team{{Species: "kleavor", As: "Kabutops", Ability: "noguard", Moves: mv("stoneaxe")}},
				team{{Species: "registeel", As: "Magneton", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.ok(p.state().Sides[1].Conditions.Hazards.StealthRock,
				"Stone Axe should have set Stealth Rock on the foe's side")
		})

		g.skip("should set up Stealth Rock on the side of the opponent, not necessarily the target, in a double battle",
			"doubles")

		g.it("should still set up Stealth Rock on the side of the opponent that is behind a Substitute", func(p *ps) {
			p.battle(
				team{{Species: "kleavor", As: "Kabutops", Ability: "noguard", Moves: mv("stoneaxe")}},
				team{{Species: "regieleki", As: "Electrode", Moves: mv("substitute")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.ok(p.state().Sides[1].Conditions.Hazards.StealthRock,
				"a Substitute should not stop the Stealth Rock half of the move")
		})

		g.it("should not set up Stealth Rock if the move does not hit opponent or its Substitute", func(p *ps) {
			p.battle(
				team{{Species: "kleavor", As: "Kabutops", Moves: mv("stoneaxe")}},
				team{{Species: "regieleki", As: "Electrode", Moves: mv("protect")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.isFalse(p.state().Sides[1].Conditions.Hazards.StealthRock,
				"a move that never connected should leave no rocks")
		})

		g.it("should not be bounced back by Magic Bounce", func(p *ps) {
			p.battle(
				team{{Species: "kleavor", As: "Kabutops", Ability: "noguard", Moves: mv("stoneaxe")}},
				team{{Species: "registeel", As: "Magneton", Ability: "magicbounce", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.isFalse(p.state().Sides[0].Conditions.Hazards.StealthRock,
				"the rocks should not have been reflected onto the user's side")
			p.ok(p.state().Sides[1].Conditions.Hazards.StealthRock,
				"the rocks should have landed on the Magic Bounce holder's side anyway")
		})

		g.it("should have its Stealth Rock prevented by Sheer Force", func(p *ps) {
			p.battle(
				team{{Species: "kleavor", As: "Kabutops", Ability: "sheerforce", Moves: mv("stoneaxe")}},
				team{{Species: "registeel", As: "Magneton", Ability: "noguard", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.isFalse(p.state().Sides[1].Conditions.Hazards.StealthRock,
				"Sheer Force should have traded the rocks away for the power boost")
		})

		g.it("should not set Stealth Rock when the user faints from Rocky Helmet", func(p *ps) {
			p.battle(
				team{
					{
						Species: "kleavor", As: "Kabutops", Ability: "noguard", Item: "focussash",
						Moves: mv("stoneaxe"),
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
			p.isFalse(p.state().Sides[1].Conditions.Hazards.StealthRock,
				"a user that faints mid-move should not get to set the rocks")
		})
	})
}
