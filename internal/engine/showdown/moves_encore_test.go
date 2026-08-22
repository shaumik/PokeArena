//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/encore.js.
//
// Only the first case is a singles battle. Seven of the remaining eight set
// gameType: 'doubles' and are about things a second active slot creates —
// Encore handing a move the priority of the move it replaced, Encore
// redirecting a self-targeting move at an ally, Focus Punch and Shell Trap
// being Encored across a spread of targets — and the last is a common.gen(2)
// battle.
//
// Whimsicott has no stand-in row; it is built as Clefable, which keeps the
// fairy half of its typing and, more to the point, still outspeeds Slowbro so
// the Encore lands before the move it is meant to replace. Prankster is not
// needed for that and Clefable's own ability is stripped so a Cute Charm proc
// on the Tackle cannot muddy the result. Sleep Talk is not in this dataset and
// is doing nothing upstream but filling a slot; Splash stands in.
//
// The nested describe is named `[Gen 2]` upstream with no move in the name, so
// that is the ledger key it gets — short enough that another port could
// collide with it.

func TestMovesEncore(t *testing.T) {
	describe(t, "Encore", func(g *psg) {
		g.it("should cause the target to be forced to repeat its move", func(p *ps) {
			p.battle(
				team{{Species: "slowbro", Moves: mv("tackle", "irondefense")}},
				team{{Species: "whimsicott", As: "Clefable", Ability: "noability", Moves: mv("encore", "splash")}},
			)
			p.makeChoices("move irondefense", "move splash")
			p.makeChoices("move tackle", "move encore")
			p.fullHP(p.foe(), "Encore should have replaced the Tackle with the Iron Defense it locked in")
			p.cantMove(0, "tackle", "Encore should leave the target no move but the one it locked")
		})

		g.skip("should cause the target to move with its Encored attack at the priority of the originally selected move once",
			"doubles")
		g.skip("should cause the target to move with its Encored attack at the priority of the originally selected move once and get blocked when appropriate",
			"doubles")
		g.skip("should not affect Focus Punch if the the user's decision is not changed", "doubles")
		g.skip("should make Focus Punch always succeed if it changes the user's decision", "doubles")
		g.skip("should not affect Shell Trap if the user's decision is not changed", "doubles")
		g.skip("should make Shell Trap always fail if the user's decision is changed", "doubles")
		g.skip("should not cause self-targeting moves to redirect to the opponent", "doubles")
	})

	describe(t, "[Gen 2]", func(g *psg) {
		g.skip("Encore succeeds when used against an opponent that last attacked before the Encore user switched in",
			"gen 2 mechanics")
	})
}
