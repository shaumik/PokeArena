//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/mirrormove.js.
//
// The file is entirely gen 1 and gen 2. Both describe blocks are named for the
// generation they test, every fixture is built with `common.gen(1)` or
// `common.gen(2)`, and what the cases measure is generation-specific behavior
// that no longer exists: Gen 1 Hyper Beam's recharge rule, Gen 1 Mirror Move
// copying Metronome only when Metronome called a two-turn move, and the Gen 1
// and Gen 2 "has not seen an attack yet" failure. There is no gen-mod layer
// here, so the blocks skip whole.
//
// Mirror Move is also not in this dataset, but that is the lesser reason: even
// with the move present these cases would be asking a question about a
// generation this engine does not model.

func TestMovesMirrorMove(t *testing.T) {
	describe(t, "Mirror Move [Gen 1]", func(g *psg) {
		g.skip("[Gen 1] Mirror Move'd Hyper Beam should force a recharge turn after not KOing a Pokemon",
			"gen 1 mechanics")
		g.skip("[Gen 1] Mirror Move'd Hyper Beam should not force a recharge turn after KOing a Pokemon",
			"gen 1 mechanics")
		g.skip("[Gen 1] Mirror Move should fail when used by a Pokemon that has not seen the opponent use an attack",
			"gen 1 mechanics")
		g.skip("[Gen 1] Mirror Move should not copy the charging turn of a two-turn attack",
			"gen 1 mechanics")
		g.skip("[Gen 1] Mirror Move should not copy Metronome if Metronome calls a regular move",
			"gen 1 mechanics")
		g.skip("[Gen 1] Mirror Move should copy Metronome if Metronome calls a two-turn move",
			"gen 1 mechanics")
	})

	describe(t, "Mirror Move [Gen 2]", func(g *psg) {
		g.skip("[Gen 2] Mirror Move should fail when used by a Pokemon that has not seen the opponent use an attack",
			"gen 2 mechanics")
	})
}
