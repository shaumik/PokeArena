//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/echoedvoice.js.
//
// Upstream detects the pierce with a Focus Sash on a level-2 Caterpie: the
// sound move only reaches the body if it kills it, and the spent Sash is the
// evidence. Levels are fixed at 50 here, so no single hit is lethal and a
// Focus Sash never fires. The port asserts the same thing one step earlier —
// the body behind the Substitute loses HP — which is what the Sash was
// standing in for. With the detector changed, the Lagging Tail and the Rest
// that arranged the lethal ordering come off too, and the target spends its
// second turn on Splash.
//
// Deoxys-Attack builds as Mewtwo and Caterpie as Butterfree, both through the
// shared table. Victory Star is not in this ability set and only ever supplied
// accuracy the case does not need, so the attacker goes in bare.
//
// The second case needs a spread move aimed at a slot that empties before it
// resolves, which has no singles reading, and the nested Gen 5 block is one
// generation too far back.

func TestMovesEchoedVoice(t *testing.T) {
	describe(t, "Echoed Voice", func(g *psg) {
		g.it("should pierce through substitutes", func(p *ps) {
			p.battle(
				team{{Species: "Deoxys-Attack", Ability: "noability", Moves: mv("splash", "echoedvoice")}},
				team{{Species: "Caterpie", Ability: "naturalcure", Moves: mv("substitute", "splash")}},
			)
			p.makeChoices("move splash", "move substitute")
			p.hurts(p.foe(), func() { p.makeChoices("move echoedvoice", "move splash") },
				"a sound move should reach the body behind a Substitute")
		})

		g.skip("should raise BP even if there is no target", "doubles")
	})

	describe(t, "[Gen 5]", func(g *psg) {
		g.skip("should not pierce through substitutes", "gen 5 mechanics")
	})
}
