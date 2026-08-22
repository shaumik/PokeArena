//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/asone.js.
//
// The file's only case is a Transform test: Ditto copies Calyrex-Shadow and
// the question is whether the copied As One still projects Unnerve. Three
// separate things this engine does not have have to be present at once, so
// there is nothing left to measure and no substitution that would preserve
// the subject.

func TestAbilitiesAsOne(t *testing.T) {
	describe(t, "As One", func(g *psg) {
		g.skip("should work if the user is Transformed",
			"Transform is not modeled and Ditto has no stand-in by design; "+
				"Calyrex-Shadow is not in this 80-species dex and As One is not in the ability set")
	})
}
