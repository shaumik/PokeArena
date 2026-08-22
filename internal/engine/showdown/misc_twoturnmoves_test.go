//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/twoturnmoves.js.
//
// The whole file is one `describe('Two Turn Moves [Gen 1]')` block built on
// `common.gen(1).createBattle`, and every case turns on a Gen 1 quirk — PP
// spent on the release turn rather than the charge turn, the Dig/Fly
// invulnerability glitch, a charge soft-locked by Haze, Wrap delaying a charge.
// This engine has gen 9 data and no gen-mod layer, so the block skips whole
// and each name is kept so the ledger still records it.

func TestMiscTwoTurnMoves(t *testing.T) {
	describe(t, "Two Turn Moves [Gen 1]", func(g *psg) {
		g.skip("charges the first turn, does damage and uses PP the second turn", "gen 1 mechanics")
		g.skip("move is paused when asleep or frozen", "gen 1 mechanics")
		g.skip("two-turn move ends if it fails due to Disable, does not use PP", "gen 1 mechanics")
		g.skip("if called by Metronome or Mirror Move, the calling move uses PP in the attacking turn", "gen 1 mechanics")
		g.skip("Dig/Fly dodges all attacks except for Swift, Transform, and Bide", "gen 1 mechanics")
		g.skip("Dig/Fly invulnerability glitch", "gen 1 mechanics")
		g.skip("should be soft-locked if it was woken up by Haze during the charging turn", "gen 1 mechanics")
		g.skip("should be delayed by trapping moves", "gen 1 mechanics")
	})
}
