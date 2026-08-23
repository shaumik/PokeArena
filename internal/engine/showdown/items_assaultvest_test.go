//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/assaultvest.js.
//
// Both cases came across. Abra and Lopunny go through their stand-in rows
// (Alakazam and Kangaskhan); the second case turns on the Trick user moving
// first, which the Iron Ball preserves by halving the Calm Mind user's Speed
// exactly as it does upstream.

func TestItemsAssaultVest(t *testing.T) {
	describe(t, "Assault Vest", func(g *psg) {
		g.it("should disable the use of Status moves", func(p *ps) {
			p.battle(
				team{{Species: "Abra", Ability: "synchronize", Moves: mv("teleport")}},
				team{{Species: "Abra", Ability: "synchronize", Item: "assaultvest", Moves: mv("teleport")}},
			)
			// Upstream's assert.cantMove wraps the makeChoices that would throw;
			// here the choice set is read directly. The unvested mirror is
			// asserted too, because "Teleport is unchoosable" is worth nothing
			// unless it is choosable without the vest.
			p.cantMove(1, "teleport", "Assault Vest should bar the holder from selecting Teleport")
			p.canMove(0, "teleport", "the Pokemon without the vest should still be able to select Teleport")
		})

		g.it("should not prevent the use of Status moves", func(p *ps) {
			// Klutz switches the vest off, so Trick is selectable; the Iron Ball
			// halves the target's Speed, so the Trick lands before the Calm Mind
			// it must not retroactively cancel. That order is the case.
			p.battle(
				team{{Species: "Lopunny", Ability: "klutz", Item: "assaultvest", Moves: mv("trick")}},
				team{{Species: "Abra", Ability: "synchronize", Item: "ironball", Moves: mv("calmmind")}},
			)
			p.canMove(0, "trick", "Klutz should switch the vest off, leaving Trick selectable")
			p.makeChoices("move trick", "move calmmind")
			p.statStage(p.foe(), "spa", 1, "")
			p.statStage(p.foe(), "spd", 1, "")
		})
	})
}
