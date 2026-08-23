//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/furycutter.js.
//
// Upstream's three damage windows are level-100 figures, so the port asserts
// the doubling between consecutive hits instead: each hit against the previous
// one, allowing for the damage roll landing differently in the two figures, so
// "twice" survives as [165%, 240%].
//
// Lucky Chant is not in this dataset, and it was doing real work upstream —
// keeping a critical hit from distorting the comparison. Shell Armor on the
// target preserves exactly that and nothing else, and the target uses Splash
// for its inert turn. Compound Eyes stays where upstream put it, since Fury
// Cutter's 95% accuracy would otherwise let a miss reset the counter.

func TestMovesFuryCutter(t *testing.T) {
	describe(t, "Fury Cutter", func(g *psg) {
		g.it("should double in power with each successful hit", func(p *ps) {
			p.battle(
				team{{Species: "kangaskhan", Ability: "shellarmor", Moves: mv("splash")}},
				team{{Species: "wynaut", Ability: "compoundeyes", Moves: mv("furycutter")}},
			)
			kang := p.mine()
			before := kang.HP
			p.turn()
			first := before - kang.HP
			before = kang.HP
			p.turn()
			second := before - kang.HP
			before = kang.HP
			p.turn()
			third := before - kang.HP

			p.atLeast(first, 1, "the first Fury Cutter should have done damage")
			if first > 0 {
				p.bounded(100*second/first, 165, 240, "the second Fury Cutter should hit twice as hard")
			}
			if second > 0 {
				p.bounded(100*third/second, 165, 240, "the third Fury Cutter should hit twice as hard again")
			}
		})

		g.skip("should double in power with each successful hit (Gen 3)", "gen 3 mechanics")
	})
}
