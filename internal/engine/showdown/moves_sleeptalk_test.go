//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/sleeptalk.js.
//
// Sleep Talk is not in this dataset, so the live case stops at "move sleeptalk
// is not in this dataset". It is written out anyway: if the move is ever
// added, it says what it has to do.
//
// Upstream also matches a `|cant` protocol line to show High Jump Kick was
// refused rather than merely missing. This engine emits no line for a submove
// Gravity forbids, so the port asserts the state instead — the target took no
// damage — and No Guard staying on the user is what rules the alternative out,
// since a No Guard High Jump Kick cannot miss.
//
// Breloom is a Grass/Fighting body that spores the user and puts Gravity up;
// Victreebel is Grass, shares its Speed tier exactly (so Spore still lands
// before Sleep Talk each turn), and the Fighting half is not used. The Gen 4
// case skips.

func TestMovesSleepTalk(t *testing.T) {
	describe(t, "Sleep Talk", func(g *psg) {
		g.it("should run conditions for submove", func(p *ps) {
			p.battle(
				team{{Species: "snorlax", Ability: "noguard", Moves: mv("sleeptalk", "highjumpkick")}},
				team{{Species: "breloom", As: "Victreebel", Moves: mv("spore", "gravity")}},
			)
			p.makeChoices("move sleeptalk", "move gravity")
			p.makeChoices("move sleeptalk", "move spore")
			p.fullHP(p.foe(), "Gravity forbids High Jump Kick, so Sleep Talk should not have landed it")
		})

		g.skip("should fail and lose PP on subsequent turns while Choice locked, prior to Gen 5",
			"gen 4 mechanics")
	})
}
