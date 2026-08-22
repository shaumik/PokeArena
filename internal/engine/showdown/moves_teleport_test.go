//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/teleport.js.
//
// Wynaut and Pichu both take their stand-in rows (Hypno and Raichu); nothing
// in the case reads either species.
//
// The single upstream case builds two battles, a singles one and a doubles
// one, and asks the same question of each. Only the first half is ported; the
// doubles half has no counterpart here and is dropped rather than mistranslated
// into a second singles battle that would repeat the first.

func TestMovesTeleport(t *testing.T) {
	describe(t, "Teleport", func(g *psg) {
		g.it("should fail to switch the user out if no Pokemon can be switched in", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Moves: mv("teleport")}},
				team{{Species: "pichu", Moves: mv("swordsdance")}},
			)
			p.turn()
			p.logHas("But it failed!", "Teleport should fail outright with nobody to switch to")
		})
	})
}
