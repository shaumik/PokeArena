//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/costar.js.
//
// Every case in the file is a doubles battle, which is unavoidable: Costar's
// whole mechanic is copying the ally's stat stages and crit volatiles on
// switch-in, and there is no ally in this engine. Costar is also not in this
// dataset, but the format is the blocker, so all three skip as doubles.

func TestAbilitiesCostar(t *testing.T) {
	describe(t, "Costar", func(g *psg) {
		g.skip("should copy the teammate's crit ratio on activation", "doubles")
		g.skip("should copy both positive and negative stat changes", "doubles")
		g.skip("should always activate later than Intimidate during simultaneous switch-ins", "doubles")
	})
}
