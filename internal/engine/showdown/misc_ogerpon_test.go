//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/ogerpon.js.
//
// Nothing came across. Every case is a Terastallization case, and each one
// stacks two further mechanics on top of it that this engine also does not
// have: Ogerpon's masks are formes and Embody Aspect is the ability that
// changes with them, so "which type did it Terastallize into" and "which forme
// is it now" are the same question, asked of a layer that does not exist here.
//
// The first case is about Transform, which is not modeled and has no move in
// this dataset; the last Hackmons case needs Transform as well. Ogerpon,
// Terapagos, Silicobra, Seismitoad and Shedinja are all outside this
// 80-species dex, and Ivy Cudgel and Sticky Web are not in this dataset — but
// none of that is the binding constraint, the Tera layer is.
//
// The `[DLC1]` case is `it.skip` upstream, disabled there because the harness
// cannot build a battle for a mod with no format. It is recorded here so the
// count matches the original.

func TestMiscOgerpon(t *testing.T) {
	describe(t, "Ogerpon", func(g *psg) {
		g.skip("should reject the Terastallization choice while Transformed into Ogerpon",
			"Terastallization — and Transform is not modeled")
		g.skip("[DLC1] should accept the Terastallization choice, but not Terastallize while Transformed into Ogerpon",
			"Terastallization — the case is disabled upstream too")
	})

	describe(t, "[Hackmons] Ogerpon", func(g *psg) {
		g.skip("should keep permanent abilities after Terastallizing until it switches out",
			"Terastallization")
		g.skip("won't Terastallize into a type other than Fire, Grass, Rock or Water",
			"Terastallization")
		g.skip("can Terastallize into the type of another mask", "Terastallization")
		g.skip("Tera form can Terastallize", "Terastallization")
		g.skip("Tera form can Terastallize into the type of another mask", "Terastallization")
		g.skip("can Terastallize into any type if transformed, but it won't change form",
			"Terastallization — and Transform is not modeled")
		g.skip("Embody Aspect should not activate unless the user is Terastallized",
			"Terastallization — Embody Aspect is a forme-changing ability")
	})
}
