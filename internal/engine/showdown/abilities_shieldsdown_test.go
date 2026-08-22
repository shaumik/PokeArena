//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/shieldsdown.js.
//
// Nothing here is ported. Shields Down is a forme change — the ability is
// Minior's Meteor and Core formes and the status immunity that comes with the
// first of them — and this engine models no formes. Minior is not in the dex
// and has no stand-in row either: this is the Shedinja-under-Wonder-Guard
// shape, where the species' identity is the mechanic, so both cases skip. The
// second case is about the coloured formes specifically, which doubles the
// reason.

func TestAbilitiesShieldsDown(t *testing.T) {
	describe(t, "Shields Down", func(g *psg) {
		g.skip("should be immune to status until below 50%",
			"Minior is not in this 80-species dex and Shields Down is a forme change, which is not modeled")
		g.skip("should be immune to status until below 50% in all formes",
			"Minior is not in this 80-species dex and Shields Down is a forme change, which is not modeled")
	})
}
