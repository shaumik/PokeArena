//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/recycle.js.
//
// One substitution worth naming, because it changes what the case measures.
// Upstream leaves Snorlax's ability unset, which gives it Immunity — and an
// immune Snorlax is never poisoned, never eats the Lum Berry, and never gives
// Recycle anything to restore, so all three assertions hold whatever Recycle
// does. The port strips the ability instead, using upstream's own idiom for a
// body that must not interfere, so the berry is actually spent and the case
// asks its question.
//
// The nested Gen 4 block skips: this engine models one generation, and Gen 4
// Recycle restoring a *slot's* last item rather than a Pokemon's is a
// different rule.

func TestMovesRecycle(t *testing.T) {
	describe(t, "Recycle", func(g *psg) {
		g.it("should restore the user's last item", func(p *ps) {
			p.battle(
				team{{Species: "Snorlax", Ability: "noability", Item: "lumberry", Moves: mv("recycle")}},
				team{{Species: "Gengar", Moves: mv("toxic")}},
			)
			p.turn()
			p.turn()
			p.fullHP(p.mine(), "the Lum Berry should have cured each poison before it ticked")
			p.equal(p.mine().Item, "lumberry", "Recycle should have put the eaten Lum Berry back")
			p.noStatus(p.mine(), "")
		})
	})

	describe(t, "[Gen 4]", func(g *psg) {
		g.skip("should restore the slot's last item", "gen 4 mechanics")
	})
}
