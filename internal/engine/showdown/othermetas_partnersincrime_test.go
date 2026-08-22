//go:build showdown

package showdown

import "testing"

// Ported from test/sim/othermetas/partnersincrime.js.
//
// Nothing came across. Partners in Crime is a doubles format whose whole
// premise is that each active Pokémon also runs its ally's ability, so there is
// no singles reading of any case in the file: all three watch two actives share
// an innate and then argue about the order the shared copies fire in, which
// needs a second active slot to be a question at all.
//
// Even setting the format aside, every body here is chosen for an ability this
// engine does not reach — Incineroar's Intimidate against a White Herb,
// Pincurchin's Electric Surge, Iron Valiant's and Iron Hands' Quark Drive,
// Corviknight's Mirror Armor, Shedinja's Wonder Guard, Stonjourner's Power
// Spot — and none of those species has a dex entry or a stand-in row.

func TestOtherMetasPartnersInCrime(t *testing.T) {
	describe(t, "Partners in Crime", func(g *psg) {
		g.skip("should activate shared abilities at the same time as other abilities",
			"doubles")
		g.skip("should activate shared abilities for each ally when only the original holder switches in",
			"doubles")
		g.skip("should not activate ally's innates if the partner faints on switch-in",
			"doubles")
	})
}
