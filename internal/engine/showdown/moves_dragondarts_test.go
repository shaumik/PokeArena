//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/dragondarts.js.
//
// Dragon Darts is a spread move whose entire point is that it splits its two
// hits between two foes, so thirteen of the fourteen cases are doubles and
// skip. The one that survives is the singles fallback — both darts go into the
// only target there is.
//
// Neither Dragon Darts nor Stamina is in this dataset. Upstream counts the hits
// by counting Stamina's Defense boosts, and that shape is kept rather than
// swapped for a damage comparison: two engine gaps reported by name are worth
// more than one assertion that would go quiet the moment either landed.

func TestMovesDragonDarts(t *testing.T) {
	describe(t, "Dragon Darts", func(g *psg) {
		g.it("should hit twice in singles", func(p *ps) {
			p.battle(
				team{{Species: "Ninjask", Moves: mv("dragondarts")}},
				team{{Species: "Mew", Ability: "stamina", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.foe(), "def", 2, "Stamina should have fired once per dart")
		})

		g.skip("should hit each foe once in doubles", "doubles")
		g.skip("should hit the other foe twice if it misses against one", "doubles")
		g.skip("should hit itself and ally if it targets itself after Ally Switch", "doubles")
		g.skip("should hit both targets even if one faints", "doubles")
		g.skip("should hit the ally twice in doubles", "doubles")
		g.skip("should smart-target the foe that's not Protecting in Doubles", "doubles")
		g.skip("should be able to be redirected", "doubles")
		g.skip("should hit one target twice if the other is protected by an ability", "doubles")
		g.skip("should hit one target twice if the other is immunue", "doubles")
		g.skip("should hit one target twice if the other is semi-invulnerable", "doubles")
		g.skip("should hit one target twice if the other is fainted", "doubles")
		g.skip("should hit one target twice if the other is Dark type and Dragon Darts is Prankster boosted",
			"doubles")
		g.skip("should fail if both targets are fainted", "doubles")
	})
}
