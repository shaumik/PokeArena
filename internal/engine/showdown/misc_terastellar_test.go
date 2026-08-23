//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/terastellar.js.
//
// Nothing came across. Every case is a Stellar Terastallization: `set` has no
// teraType field, the choice grammar has no `terastallize` suffix, and nothing
// in this engine tracks a Tera type or the once-per-move-type ledger that the
// Stellar boost is counted against. The whole file measures that ledger — the
// 1.2x on the first non-STAB move of a type, the 2x on the first STAB one, and
// the drop back to the ordinary multiplier on the second use.
//
// No case survives partial translation. Each is a chain of damage figures where
// the *first* number is already the boosted one, so there is no unboosted
// baseline to keep, and every figure is a level-100 range that would not
// transfer to a level-50 engine in any event.
//
// Three other things would each have to be solved separately: Terapagos,
// Comfey, Krookodile, Tornadus and Steelix are outside this 80-species dex
// (Steelix has a stand-in, none of the others do); Tera Shift and Embody Aspect
// are forme-changing abilities; and Hyperspace Hole, Surging Strikes and Flip
// Turn are not in this dataset.
//
// The file is common.gen(9) throughout, which is this engine's own data
// generation, so the generation is not the obstacle here either.

func TestMiscTerastellar(t *testing.T) {
	describe(t, "Tera Stellar", func(g *psg) {
		g.skip("should increase the damage of non-STAB moves by 1.2x on the first use of that move type",
			"Terastallization")
		g.skip("should not have the once-per-type restriction when used by Terapagos",
			"Terastallization")
		g.skip("should not modify the Pokemon's base type for defensive purposes",
			"Terastallization")
		g.skip("should only be super-effective against opposing Terastallized targets",
			"Terastallization")
		g.skip("should not work with Adapatability",
			"Terastallization")
		g.skip("should increase the damage of all hits of a multi-hit move",
			"Terastallization")
		g.skip("should boost the base power of weaker moves on the first use of that move type to 60 BP",
			"Terastallization")
	})
}
