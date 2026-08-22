//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/soulheart.js.
//
// All three cases are doubles, and they are about how many times the ability
// fires when several Pokemon faint at once — a question that cannot be asked
// with one active slot per side. Soul-Heart is not in this dataset either. The
// third case is marked it.skip upstream; it is recorded here so the case count
// still matches the original.

func TestAbilitiesSoulHeart(t *testing.T) {
	describe(t, "Soul-Heart", func(g *psg) {
		g.skip("should activate on each individual KO", "doubles")
		g.skip("should not activate if two Soul-Hearts are KOed on the same side", "doubles")
		g.skip("should activate an opposing Soul-Heart if the attacker's ally was first KOed in a spread move",
			"doubles (upstream skips this case too)")
	})
}
