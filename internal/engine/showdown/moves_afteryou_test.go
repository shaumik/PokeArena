//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/afteryou.js.
//
// Three of the four cases need a second Pokemon on the field — two doubles and
// one free-for-all — and After You has no meaning with a single active per
// side, so they skip. The fourth is the singles case and is the one worth
// having: After You is supposed to fail outright in singles, either way round.
//
// After You is not in this dataset, so that case reports the missing move. That
// absence is the finding, which is why it is written out rather than skipped.
//
// Species: Wynaut resolves through the shared table to Hypno and Tyrogue to
// Hitmonlee. Neither typing matters — the case reads only whether the move
// failed.
//
// Upstream matches the protocol lines `|-fail|p1a: Wynaut` and
// `|-fail|p2a: Tyrogue`. This engine emits one prose failure line with no side
// marker in the text, so the port asserts the line appears twice instead, once
// per user.

func TestMovesAfterYou(t *testing.T) {
	describe(t, "After You", func(g *psg) {
		g.skip("should cause the targeted Pokemon to immediately move next", "doubles")

		g.skip("should only cause the target to move next, not run a submove", "doubles")

		g.skip("should work in a free-for-all",
			"free-for-all: this engine has exactly two sides")

		g.it("should fail in singles whether the user is faster or slower than its target", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("afteryou", "ember")}},
				team{{Species: "Tyrogue", Moves: mv("afteryou", "seismictoss")}},
			)
			p.makeChoices("move afteryou", "move seismictoss")
			p.makeChoices("move ember", "move afteryou")
			p.equal(p.logCount("But it failed!"), 2,
				"both After You attempts should have failed in singles")
		})
	})
}
