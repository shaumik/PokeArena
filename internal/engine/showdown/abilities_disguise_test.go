//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/disguise.js.
//
// Nothing here is ported. Disguise is a forme change — the ability's whole
// observable behavior is Mimikyu turning into Mimikyu-Busted — and this
// engine models no formes and no forme changes. Mimikyu is not in the dex
// either, and its stand-in row says in as many words that Disguise is not
// among the things Gengar preserves, so substituting would produce a green
// case measuring nothing. This is the Shedinja-under-Wonder-Guard shape: the
// species' identity *is* the mechanic, so the file skips.
//
// Every case here puts Mimikyu on the field, including the entry-hazard one,
// so the skip is whole-file rather than case by case. Two cases are also
// Gen 7 battles and one also needs Transform, which is likewise not modeled;
// those reasons are named where they apply.

func TestAbilitiesDisguise(t *testing.T) {
	describe(t, "Disguise", func(g *psg) {
		g.skip("should block damage from one move",
			"Mimikyu is not in this 80-species dex and Disguise is a forme change, which is not modeled; also a gen 7 battle")
		g.skip("should only block damage from the first hit of a move",
			"Mimikyu is not in this 80-species dex and Disguise is a forme change, which is not modeled")
		g.skip("should bust Disguise on self-hit confusion",
			"Mimikyu is not in this 80-species dex and Disguise is a forme change, which is not modeled")
		g.skip("should not block damage from weather effects",
			"Mimikyu is not in this 80-species dex and Disguise is a forme change, which is not modeled")
		g.skip("should not block damage from entry hazards",
			"Mimikyu is not in this 80-species dex and Disguise is a forme change, which is not modeled")
		g.skip("should not block status moves or damage from status",
			"Mimikyu is not in this 80-species dex and Disguise is a forme change, which is not modeled")
		g.skip("should not block secondary effects from damaging moves",
			"Mimikyu is not in this 80-species dex and Disguise is a forme change, which is not modeled; also a gen 7 battle")
		g.skip("should cause Counter to deal 1 damage if it blocks a move",
			"Mimikyu is not in this 80-species dex and Disguise is a forme change, which is not modeled")
		g.skip("should not trigger critical hits while active",
			"Mimikyu is not in this 80-species dex and Disguise is a forme change, which is not modeled")
		g.skip("should not work while Transformed",
			"Mimikyu is not in this 80-species dex and neither Disguise nor Transform is modeled")
	})
}
