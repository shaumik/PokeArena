//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/weight.js.
//
// Nothing came across. Weight is not modeled: no species carries a weight, and
// the two consumers of one are absent for that reason — Float Stone is left out
// of the item set with the note "weight isn't modeled" in items_field.go, and
// Grass Knot, the move every case in this file measures, ships in the dataset
// at power 0.
//
// That last detail is why these are skips rather than cases written to report
// their own gap. Upstream reads the answer off a `battle.onEvent('BasePower')`
// hook, which the harness has no counterpart for, so the only restatement
// available is a damage comparison — and Grass Knot at 0 BP deals the same
// nothing whatever the target weighs, so the comparison would be between two
// zeroes. A case built anyway would go red naming Heavy Metal, which is a true
// but misleading finding: the missing ability is downstream of the missing
// weight, and fixing it alone would change nothing.
//
// The gaps the file names, for the record: Heavy Metal and Light Metal are not
// in the 118 implemented abilities, Float Stone is not in the 128 items,
// Autotomize is not in the 538 moves, and Simisear, Simisage, Registeel, Lairon
// and Aegislash are outside this 80-species dex with no stand-in.

func TestMiscWeight(t *testing.T) {
	describe(t, "Heavy Metal", func(g *psg) {
		g.skip("should double the weight of a Pokemon", "weight is not modeled")
		g.skip("should be negated by Mold Breaker", "weight is not modeled")
	})

	describe(t, "Light Metal", func(g *psg) {
		g.skip("should halve the weight of a Pokemon", "weight is not modeled")
		g.skip("should be negated by Mold Breaker", "weight is not modeled")
	})

	describe(t, "Float Stone", func(g *psg) {
		g.skip("should halve the weight of a Pokemon", "weight is not modeled")
	})

	describe(t, "Autotomize", func(g *psg) {
		g.skip("should reduce the weight of a Pokemon by 100 kg with each use",
			"weight is not modeled")
		g.skip("should factor into weight before Heavy Metal does", "weight is not modeled")
		g.skip("should reset after a forme change", "weight is not modeled")
	})
}
