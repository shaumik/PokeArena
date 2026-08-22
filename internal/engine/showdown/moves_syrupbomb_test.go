//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/syrupbomb.js.
//
// Syrup Bomb is not in this dataset, so every case here stops at "move
// syrupbomb is not in this dataset". They are written out anyway: if the move
// is ever added, they say what it has to do.
//
// Upstream reads `applin.volatiles['syrupbomb']` directly. This engine's
// Volatiles carry no Syrup Bomb entry at all, so there is nothing to read;
// the port asserts the Speed stages the volatile's presence and expiry
// produce instead — three drops and then no fourth.
//
// Mirror Armor is not in this ability set either. The case is written as a
// live one rather than skipped, because the missing ability is itself the
// finding the port is here to record.
//
// Substitutions. Applin is a body that has to survive four turns of a 60 BP
// hit, which Snorlax does comfortably. Dipplin and Wynaut are the users, and
// only the Grass typing (STAB) is even arguably relevant; Venusaur keeps it,
// Hypno stands in for Wynaut through the shared table. Furret is a second body
// to switch into and Raticate is one. Sleep Talk is not in this dataset, so
// the bodies use Splash for their inert turns.

func TestMovesSyrupBomb(t *testing.T) {
	describe(t, "Syrup Bomb", func(g *psg) {
		g.it("should lower the opponent's Speed for 3 turns, but not remove its volatile until after 4 turns", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "noguard", Moves: mv("syrupbomb")}},
				team{{Species: "Applin", As: "Snorlax", Moves: mv("splash")}},
			)
			for i := 0; i < 3; i++ {
				p.turn()
			}
			p.statStage(p.foe(), "spe", -3, "three turns of Syrup Bomb should cost three Speed stages")

			p.turn()
			p.statStage(p.foe(), "spe", -3, "the fourth turn should expire the effect rather than drop Speed again")
		})

		g.it("should end if the source leaves the field", func(p *ps) {
			p.battle(
				team{
					{Species: "Dipplin", As: "Venusaur", Ability: "noguard", Moves: mv("syrupbomb")},
					{Species: "Furret", As: "Raticate", Moves: mv("splash")},
				},
				team{{Species: "Applin", As: "Snorlax", Moves: mv("splash")}},
			)
			p.turn()
			p.makeChoices("switch 2", "")
			p.statStage(p.foe(), "spe", -1, "the Speed drops should stop once the user leaves the field")
		})

		g.it("the stat changes should be reflected by Mirror Armor", func(p *ps) {
			p.battle(
				team{{Species: "Dipplin", As: "Venusaur", Ability: "noguard", Moves: mv("syrupbomb")}},
				team{{Species: "Corviknight", Ability: "mirrorarmor", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.mine(), "spe", -1, "Mirror Armor should send the Speed drop back at the user")
			p.statStage(p.foe(), "spe", 0, "the Mirror Armor holder should keep its own Speed")
		})
	})
}
