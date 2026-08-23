//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/faint-order.js.
//
// Nothing came across, and the reason is uniform: every case in the file is
// built with common.gen(N).createBattle, and in seven of the ten the *subject*
// is the difference between two generations — the same battle is run twice, in
// gen 4 and gen 5, or gen 5 and gen 7, and the assertion is that the winner
// differs. This engine models one generation and has no gen-mod layer, so
// there is no honest way to ask half of such a case.
//
// Two further obstacles apply on top of that, and both would have to be solved
// even if the generations were available. Every case after the first three uses
// Shedinja as a body that dies to anything; Shedinja has no stand-in, because
// its 1 HP is the mechanism rather than an incidental stat, and while HP: 1
// would reproduce the frailty it would not reproduce a Pokemon that cannot
// survive its own recoil at any HP. And three of the moves the file leans on —
// Sleep Talk, Shadow Sneak, Future Sight, Final Gambit — are not in this
// dataset.
//
// What the file is really pinning is the order in which the engine checks for a
// winner after a mutual KO. This engine answers that question one way for all
// causes: internal/engine/turn.go's updatePhase declares a draw whenever both
// sides are wiped in the same turn, with no faint-order tiebreak. That is
// visible from a gen-9 battle without any of these cases — the ported Perish
// Song case in misc_turn_order_test.go reaches it.

func TestMiscFaintOrder(t *testing.T) {
	describe(t, "Fainting", func(g *psg) {
		g.skip("should end the turn in Gen 1", "gen 1 mechanics")
		g.skip("should end the turn in Gen 3", "gen 3 mechanics")
		g.skip("should not end the turn in Gen 4", "gen 4 mechanics")
		g.skip("should check for a winner after an attack",
			"gen 4 mechanics — the case is a gen-4/gen-5 comparison, and Shedinja is not in this 80-species dex")
		g.skip("should check for a winner after recoil",
			"gen 4 mechanics — the case is a gen-4/gen-5 comparison, and Shedinja is not in this 80-species dex")
		g.skip("should check for a winner after Rough Skin",
			"gen 4 mechanics — the case is a gen-4/gen-6/gen-7 comparison, and Shedinja is not in this 80-species dex")
		g.skip("should check for a winner after future moves",
			"gen 7 mechanics — Shedinja is not in this 80-species dex and Future Sight is not in this dataset")
		g.skip("should check for a winner after Rocky Helmet",
			"gen 5 mechanics — the case is a gen-5/gen-7 comparison, and Shedinja is not in this 80-species dex")
		g.skip("should check for a winner after Destiny Bond",
			"gen 4 mechanics — the case is a gen-4/gen-5 comparison, and Shedinja is not in this 80-species dex")
		g.skip("should check for a winner after Final Gambit",
			"gen 5 mechanics — Shedinja is not in this 80-species dex and Final Gambit is not in this dataset")
	})
}
