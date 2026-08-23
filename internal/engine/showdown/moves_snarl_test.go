//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/snarl.js.
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
// The "Snarl [Gen 5]" block skips: one generation is modeled here.

func TestMovesSnarl(t *testing.T) {
	describe(t, "Snarl", func(g *psg) {
		g.it("should pierce through substitutes", func(p *ps) {
			p.battle(
				team{{Species: "Deoxys-Attack", Ability: "noability", Moves: mv("splash", "snarl")}},
				team{{Species: "Caterpie", Ability: "naturalcure", Moves: mv("substitute", "splash")}},
			)
			p.makeChoices("move Splash", "move Substitute")
			p.hurts(p.foe(), func() { p.makeChoices("move Snarl", "move Splash") },
				"a sound move should reach the body behind a Substitute")
		})
	})

	describe(t, "Snarl [Gen 5]", func(g *psg) {
		g.skip("should not pierce through substitutes", "gen 5 mechanics")
	})
}
