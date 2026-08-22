//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/struggle.js.
//
// Built with common.gen(4), and the body is a Shedinja: names_test.go
// deliberately withholds a stand-in for a species whose identity is the
// mechanic, and Shedinja under Wonder Guard is one of the named examples.
//
// Worth a reviewer's second look, because the case title says "and every other
// gen": the mechanic underneath is gen-independent (Taunt leaves a Pokemon
// whose only move is a status move with nothing but Struggle, and Struggle
// recoil finishes a 1 HP body). Restating it would mean dropping both the
// generation and the species the fixture is built from, so it is recorded as a
// skip rather than translated into a case about something else.

func TestMovesStruggle(t *testing.T) {
	describe(t, "Struggle", func(g *psg) {
		g.skip("should KO Shedinja in Gen 4 (and every other gen)",
			"gen 4 mechanics, and Shedinja is not in this 80-species dex — Wonder Guard is the mechanic, so it has no stand-in")
	})
}
