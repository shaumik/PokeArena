//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/synchronize.js.
//
// Sleep Talk is not in this dataset; Alakazam only needs a move that does
// nothing while Ralts paralyzes it, so Splash stands in.
//
// Ralts is not in this dex and has no stand-in row. Mew is the only other
// in-dex body carrying Synchronize, and it keeps the psychic typing Ralts
// shares — the case turns on the ability and on holding a Lum Berry, nothing
// else about the species.

func TestAbilitiesSynchronize(t *testing.T) {
	describe(t, "Synchronize", func(g *psg) {
		g.it("should complete before Lum Berry can trigger", func(p *ps) {
			p.battle(
				team{{Species: "alakazam", Ability: "synchronize", Item: "lumberry", Moves: mv("splash")}},
				team{{Species: "ralts", As: "Mew", Ability: "synchronize", Item: "lumberry", Moves: mv("glare")}},
			)
			p.turn()
			p.noItem(p.mine(), "Alakazam should not be holding an item")
			p.noItem(p.foe(), "Ralts should not be holding an item")
			p.notEqual(p.mine().Status, "paralysis", "Alakazam should not be paralyzed")
			p.notEqual(p.foe().Status, "paralysis", "Ralts should not be paralyzed")
		})
	})
}
