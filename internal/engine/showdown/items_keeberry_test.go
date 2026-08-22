//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/keeberry.js.
//
// Double Iron Bash is not in this dataset, and it is the subject here rather
// than filler — the case exists to check that the berry raises Defense once the
// whole multi-hit move is over, not between its hits — so the move is kept and
// the missing-move failure is the finding.
//
// Upstream states the claim as an absolute level-100 damage window (56-68 with
// the berry, versus 47-57 if the second hit were reduced). Absolute figures do
// not transfer to level 50, so the port measures the same thing as a comparison:
// the identical fixture without the berry must take the same damage. Shell Armor
// is kept for the reason upstream keeps it — a crit would move the figure for an
// unrelated reason. Sleep Talk is inert filler for the holder and Splash stands
// in for it.

func TestItemsKeeBerry(t *testing.T) {
	describe(t, "Kee Berry", func(g *psg) {
		g.it("should activate after a multi-hit physical move", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("doubleironbash")}},
				team{{Species: "Alakazam", Item: "keeberry", Ability: "shellarmor", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			held := p.foe()
			withBerry := held.MaxHP - held.HP
			p.statStage(held, "def", 1, "the Kee Berry should have activated at all")

			p.battle(
				team{{Species: "Wynaut", Moves: mv("doubleironbash")}},
				team{{Species: "Alakazam", Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.turn()
			bare := p.foe()
			p.equal(withBerry, bare.MaxHP-bare.HP,
				"the Defense boost should land after the last hit, so neither hit is reduced")
		})
	})
}
