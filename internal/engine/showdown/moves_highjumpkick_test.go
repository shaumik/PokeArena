//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/highjumpkick.js.
//
// Everything crosses. Gastly takes its stand-in row (Gengar, the same line
// evolved, still Ghost and so still immune to a Fighting move) and Hitmonlee
// and Dugtrio are both in this dex.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash. Memento
// is absent too and is load-bearing in the second case — it is how upstream
// removes the target before High Jump Kick resolves — so that case reports the
// missing move rather than the crash it is about.

func TestMovesHighJumpKick(t *testing.T) {
	describe(t, "High Jump Kick", func(g *psg) {
		g.it("should damage the user if it does not hit the target", func(p *ps) {
			p.battle(
				team{{Species: "Gastly", Ability: "levitate", Moves: mv("splash")}},
				team{{Species: "Hitmonlee", Ability: "limber", Moves: mv("highjumpkick")}},
			)
			p.hurts(p.foe(), func() { p.turn() },
				"a High Jump Kick a Ghost-type is immune to should still crash into the user")
		})

		g.it("should not damage the user if there was no target", func(p *ps) {
			// Singles upstream too: the second Dugtrio is only the replacement
			// for the one Memento removes.
			p.battle(
				team{
					{Species: "Dugtrio", Ability: "sandveil", Moves: mv("memento")},
					{Species: "Dugtrio", Ability: "sandveil", Moves: mv("memento")},
				},
				team{{Species: "Hitmonlee", Ability: "limber", Moves: mv("highjumpkick")}},
			)
			p.turn()
			p.fullHP(p.foe(), "with the target already gone the move should not resolve, so it cannot crash")
		})
	})
}
