//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/bide.js.
//
// The whole file is one `[Gen 1] Bide` describe and every case is built with
// common.gen(1).createBattle, so the block skips as a generation. This is not
// only the missing gen-mod layer: RBY Bide is a different move from the modern
// one — it stores the *last damage the battle recorded* rather than damage the
// user took, keeps accumulating across the foe's switches, and pauses its
// counter while the user is asleep, frozen or disabled. Bide is in this
// dataset, so a modern-mechanics file for it would still be worth having; none
// of these seven cases is that file.

func TestMovesBide(t *testing.T) {
	describe(t, "[Gen 1] Bide", func(g *psg) {
		const why = "gen 1 mechanics"

		g.skip("should be possible to roll two-turn Bide", why)
		g.skip("should be possible to roll three-turn Bide", why)
		g.skip("should damage Substitute with Bide damage", why)
		g.skip("should accumulate damage as the opponent switches or uses moves that don't reset lastDamage", why)
		g.skip("should not zero out accumulated damage when an enemy faints (Desync Clause Mod)", why)
		g.skip("should pause Bide's duration when asleep or frozen", why)
		g.skip("should pause Bide's duration when disabled", why)
	})
}
