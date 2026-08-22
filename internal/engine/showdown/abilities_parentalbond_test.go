//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/parentalbond.js.
//
// Parental Bond is not in this ability set; Kangaskhan itself is in the dex.
// Upstream reads every figure off a `battle.onEvent('ModifyDamage')` hook that
// collects one entry per hit, and this harness has no per-hit hook, so neither
// case can be stated the way it is written. The damage bands are level-100
// absolutes as well.
//
// What each case becomes:
//
//   - the first asserts the multi-hit announcement. A single-hit move struck
//     twice is a multi-hit move, and this engine announces the hit count, so
//     the line's absence is the gap.
//   - the second is a comparison. A second Kangaskhan with no ability is added
//     as the baseline, and both use Double Kick on the same target; if
//     Parental Bond ever does reach a multi-hit move, the two readings come
//     apart. The target holds Battle Armor, as upstream's does, which is also
//     what keeps a critical hit out of the comparison.
//
// Aggron is built as Snorlax rather than through its stand-in row (Magneton):
// the second case hits one target twice and Magneton does not survive that.
// The target's Rest becomes Splash for the same reason — upstream's cases are
// one turn long, so its Rest never resolves, while here it would heal away the
// damage being measured.
//
// The Electrium Z is dropped from both fixtures. It exists only for the Z-move
// case, which is out of scope, and no Z-crystal is in this item set.

func TestAbilitiesParentalBond(t *testing.T) {
	describe(t, "Parental Bond", func(g *psg) {
		g.it(`should cause single-hit attacks to strike twice, with the second hit dealing 0.25x damage`, func(p *ps) {
			p.battle(
				team{{Species: "Kangaskhan", Ability: "parentalbond", Moves: mv("thunderpunch", "doublekick")}},
				team{{Species: "Aggron", Ability: "battlearmor", Moves: mv("rest")}},
			)
			p.makeChoices("move thunderpunch", "move rest")
			p.damaged(p.foe(), "Thunder Punch should have landed")
			p.logHas(" time(s)!", "Parental Bond should have made Thunder Punch a two-hit move")
		})

		g.it(`should not have any effect on moves with multiple hits`, func(p *ps) {
			p.battle(
				team{
					{Species: "Kangaskhan", Ability: "parentalbond", Moves: mv("thunderpunch", "doublekick")},
					{Species: "Kangaskhan", Ability: "noability", Moves: mv("thunderpunch", "doublekick")},
				},
				team{{Species: "Aggron", As: "Snorlax", Ability: "battlearmor", Moves: mv("splash")}},
			)
			target := p.foe()

			before := target.HP
			p.makeChoices("move doublekick", "move splash")
			withBond := before - target.HP

			p.makeChoices("switch 2", "move splash")

			before = target.HP
			p.makeChoices("move doublekick", "move splash")
			withoutBond := before - target.HP

			p.atLeast(withoutBond, 1, "the baseline Double Kick should have landed")
			p.bounded(withBond, withoutBond*4/5, withoutBond*5/4,
				"Double Kick already hits twice, so Parental Bond should change nothing")
		})

		g.skip(`should not have any effect Z-Moves`, "Z-moves")
	})

	describe(t, "Parental Bond [Gen 6]", func(g *psg) {
		g.skip(`should cause single-hit attacks to strike twice, with the second hit dealing 0.5x damage`,
			"gen 6 mechanics")
		g.skip(`should not have any effect on moves with multiple hits`, "gen 6 mechanics")
	})
}
