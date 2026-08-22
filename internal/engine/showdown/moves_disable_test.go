//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/disable.js.
//
// Wynaut and Drowzee take their stand-in rows (both Hypno); Spearow is built
// as Fearow, the same line one stage up, which keeps it faster than the
// Disable user so the target has a last move to lose. Mew and Muk are in
// this dex already.
//
// The `[Gen 1] Disable` block is nested inside `Disable` upstream; here it is
// a sibling describe, because the ledger keys on the immediate describe name.
// Its first case is not actually a Gen 1 battle — it calls plain
// common.createBattle — so it is ported for real and only the five that use
// common.gen(1) skip.
//
// "should interrupt consecutively executed moves" needs Sleep Talk as the
// second, unreachable move on the locked Pokemon, and Sleep Talk is not in
// this dataset; that case reports the missing move rather than the Outrage
// interaction.

func TestMovesDisable(t *testing.T) {
	describe(t, "Disable", func(g *psg) {
		g.it("should prevent the use of the target's last move", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("disable")}},
				team{{Species: "Spearow", As: "Fearow", Moves: mv("growl")}},
			)
			p.turn()
			// Upstream wraps the illegal choice in assert.cantMove; here the
			// choice set is read directly.
			p.cantMove(1, "growl", "Disable should bar the move Spearow just used")
		})

		g.it("should interrupt consecutively executed moves like Outrage", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("disable")}},
				team{{Species: "Spearow", As: "Fearow", Moves: mv("outrage", "sleeptalk")}},
			)
			p.turn()
			p.cantMove(1, "sleeptalk", "the Outrage lock should still hold the turn Disable lands")
			p.turn()
			p.cantMove(1, "outrage", "Disable should have broken the rampage and barred Outrage")
		})

		g.it("should not work successfully against Struggle", func(p *ps) {
			// The Assault Vest leaves Spearow no legal move but Struggle, and
			// Struggle is the thing Disable must refuse to name.
			p.battle(
				team{{Species: "Wynaut", Moves: mv("disable")}},
				team{{Species: "Spearow", As: "Fearow", Item: "assaultvest", Moves: mv("growl")}},
			)
			p.turn()
			p.logHas("But it failed!", "Disable should have failed against Struggle")
		})
	})

	describe(t, "[Gen 1] Disable", func(g *psg) {
		g.it("should fail if the opponent already has a move disabled", func(p *ps) {
			// Mew outspeeds Muk, so the first Disable fails for want of a last
			// move, the second names Tackle, and the third is the one the case
			// is about. Upstream's assertion — that the log records a failed
			// Disable — is satisfied by the first one too, so it is weaker
			// than its name suggests; the port keeps it as written.
			p.battle(
				team{{Species: "Mew", Moves: mv("disable")}},
				team{{Species: "Muk", Moves: mv("splash", "tackle")}},
			)
			p.makeChoices("", "move tackle")
			p.makeChoices("", "move splash")
			p.makeChoices("", "move splash")
			p.logHas("But it failed!", "Disable should fail if a move is already disabled")
		})

		g.skip("should work on the first turn so long as the opponent has move with PP", "gen 1 mechanics")
		g.skip("should fail if opponent has no moves with PP", "gen 1 mechanics")
		g.skip("should not select moves with 0 PP", "gen 1 mechanics")
		g.skip("should keep the slot disabled even if the move is replaced by Mimic", "gen 1 mechanics")
		g.skip("should keep the slot disabled even if the move is replaced by Transform", "gen 1 mechanics")
		g.skip("Disable should build Rage, even if it misses/fails", "gen 1 mechanics")
	})
}
