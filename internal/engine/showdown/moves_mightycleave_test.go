//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/mightycleave.js.
//
// Mighty Cleave is not in this dataset, and that report is the finding.
//
// Terrakion has no stand-in row; Kabutops is built instead, keeping the Rock
// typing and the physical-attacker role. Terrakion's Fighting half is lost and
// is not used — the assertion is only that the target took damage at all
// through its Protect. Entei's stand-in Arcanine keeps the Fire typing; Inner
// Focus is kept from the fixture, which also keeps Arcanine's own Intimidate
// out of the way.

func TestMovesMightyCleave(t *testing.T) {
	describe(t, "Mighty Cleave", func(g *psg) {
		g.it("should go through Protect", func(p *ps) {
			p.battle(
				team{{Species: "Terrakion", As: "Kabutops", Ability: "justified", Moves: mv("mightycleave")}},
				team{{Species: "Entei", Ability: "innerfocus", Moves: mv("protect")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.damaged(p.foe(), "Protect should not have stopped Mighty Cleave")
		})
	})
}
