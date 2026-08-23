//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/allyswitch.js.
//
// Ally Switch swaps the user with its ally, so six of the seven cases need a
// second active slot and skip; two of those are also triples on a Gen 6 mod,
// and one is a Gen 8 mod. Only one case has a half this engine can ask at all:
// "should not work in formats where you do not control allies" opens with a
// singles battle and expects the move to fail. That half is ported and the
// multi and free-for-all halves after it are dropped, since neither format
// exists here.
//
// Ally Switch itself is not in this 538-move dataset, which is the finding that
// case reports. It is left as a real case rather than a skip because "there is
// no ally to switch with, so it fails" is a rule singles can state, and the
// engine currently cannot state it at all.

func TestMovesAllySwitch(t *testing.T) {
	describe(t, "Ally Switch", func(g *psg) {
		g.skip("should cause the Pokemon to switch sides in a double battle", "doubles")
		g.skip("should not work if the user is in the center of a Triple Battle", "triples")
		g.skip("should swap Pokemon on the edges of a Triple Battle", "triples")

		g.it("should not work in formats where you do not control allies", func(p *ps) {
			// Only upstream's first battle, the singles one, is built. The multi
			// and free-for-all battles that follow it need formats this engine
			// has no concept of.
			p.battle(
				team{{Species: "Wynaut", Moves: mv("allyswitch")}},
				team{{Species: "Pichu", Moves: mv("swordsdance")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.logHas("But it failed!", "Ally Switch has no ally to swap with in singles and should fail")
		})

		g.skip("should have a chance to fail when used successively", "doubles")
		g.skip("[Gen 8] should not have a chance to fail when used successively", "gen 8 mechanics")
		g.skip("should not use the protection counter when determining if the move should fail", "doubles")
	})
}
