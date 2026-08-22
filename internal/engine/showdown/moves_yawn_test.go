//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/yawn.js. Nothing here needed re-stating: Mew is
// in this dex, Ninjask comes through the shared table as Scyther, and Scyther
// keeps the one thing the last two cases depend on — outrunning Mew, so that
// Safeguard goes up before Yawn in one case and after it in the other.

func TestMovesYawn(t *testing.T) {
	describe(t, "Yawn", func(g *psg) {
		g.it("should put foes to sleep eventually", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Moves: mv("yawn", "splash")}},
				team{{Species: "Ninjask", Moves: mv("splash")}},
			)
			p.turn()
			p.noStatus(p.foe(), "Yawn should not put the target under on the turn it lands")
			p.makeChoices("move 2", "move 1")
			p.hasStatus(p.foe(), "slp", "the drowsy target should fall asleep at the end of the next turn")
		})

		g.it("should be blocked by Safeguard", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Moves: mv("yawn")}},
				team{{Species: "Ninjask", Moves: mv("safeguard")}},
			)
			p.turn()
			p.turn()
			p.noStatus(p.foe(), "Safeguard should refuse Yawn")
		})

		g.it("should be able to put foes to sleep through Safeguard if used first", func(p *ps) {
			p.battle(
				team{{Species: "Ninjask", Moves: mv("yawn")}},
				team{{Species: "Mew", Moves: mv("safeguard")}},
			)
			p.turn()
			p.turn()
			p.hasStatus(p.foe(), "slp",
				"Yawn landed before Safeguard went up, so the sleep it queued should still arrive")
		})
	})
}
