//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/uproar.js.
//
// Upstream detects the pierce with a Focus Sash on a level-2 Caterpie: the
// sound move only reaches the body if it kills it, and the spent Sash is the
// evidence. Levels are fixed at 50 here, so no single hit is lethal and a
// Focus Sash never fires. The port asserts the same thing one step earlier —
// the body behind the Substitute loses HP — which is what the Sash was
// standing in for. With the detector changed, the Lagging Tail and the Rest
// that arranged the lethal ordering come off too, and the target spends its
// second turn on Splash. Deoxys-Attack builds as Mewtwo and Caterpie as
// Butterfree, both through the shared table; Victory Star is not in this
// ability set and only ever supplied accuracy, so the attacker goes in bare.
//
// The Throat Chop case reads `volatiles['uproar']` upstream. This engine has
// no Uproar handling at all — the move is in the dataset but nothing reads it
// — so "the volatile is gone" is true before the case even starts, and
// asserting only that would leave a green case measuring nothing. The port
// therefore runs the control first: an Uproar with an inert foe must leave the
// user locked in (the engine's general locked-move volatile is the closest
// thing it has to Showdown's), and only then does the Throat Chop half mean
// what upstream meant by it.
//
// Zoroark and Corsola are a fast user and a slower foe; Persian and Omastar
// keep that relationship, which is what the case needs so that Uproar has
// started before Throat Chop lands. Neither Dark nor Corsola's bulk matters.
//
// The "Uproar [Gen 5]" block skips: one generation is modeled here.

func TestMovesUproar(t *testing.T) {
	describe(t, "Uproar", func(g *psg) {
		g.it("should pierce through substitutes", func(p *ps) {
			p.battle(
				team{{Species: "Deoxys-Attack", Ability: "noability", Moves: mv("splash", "uproar")}},
				team{{Species: "Caterpie", Ability: "naturalcure", Moves: mv("substitute", "splash")}},
			)
			p.makeChoices("move splash", "move substitute")
			p.hurts(p.foe(), func() { p.makeChoices("move uproar", "move splash") },
				"a sound move should reach the body behind a Substitute")
		})

		g.it("should end if the user is under the effect of Throat Chop", func(p *ps) {
			p.battle(
				team{{Species: "Zoroark", As: "Persian", Moves: mv("uproar", "splash")}},
				team{{Species: "Corsola", As: "Omastar", Moves: mv("throatchop", "splash")}},
			)
			p.makeChoices("move uproar", "move splash")
			p.ok(p.mine().Volatiles.LockedMove != nil,
				"Uproar should lock its user in, or the assertion below means nothing")

			p.battle(
				team{{Species: "Zoroark", As: "Persian", Moves: mv("uproar", "splash")}},
				team{{Species: "Corsola", As: "Omastar", Moves: mv("throatchop", "splash")}},
			)
			p.makeChoices("move uproar", "move throatchop")
			p.ok(p.mine().Volatiles.LockedMove == nil, "Throat Chop should end the Uproar")
		})
	})

	describe(t, "Uproar [Gen 5]", func(g *psg) {
		g.skip("should not pierce through substitutes", "gen 5 mechanics")
	})
}
