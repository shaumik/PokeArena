//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/sheercold.js.
//
// Deoxys-Speed takes its stand-in row (Alakazam, psychic and fast) and keeps
// No Guard so the OHKO's accuracy is not what the case measures.
//
// Arceus-Ice becomes Lapras. Upstream needs an Ice-type target and builds one
// out of Arceus, Multitype and the Icicle Plate — none of which is in this
// engine — where Lapras simply is one. The scaffolding is dropped with the
// species; the Water half it brings instead has no bearing on an Ice-type's
// immunity to Sheer Cold.
//
// The Gen 6 block skips as a block: it exists to pin the older rule that Ice
// types could be frozen out by Sheer Cold, and this engine models one
// generation.

func TestMovesSheerCold(t *testing.T) {
	describe(t, "Sheer Cold", func(g *psg) {
		g.it("should not affect Ice-type Pokémon", func(p *ps) {
			p.battle(
				team{{Species: "Deoxys-Speed", Ability: "noguard", Moves: mv("sheercold")}},
				team{{Species: "Arceus-Ice", As: "Lapras", Moves: mv("calmmind")}},
			)
			p.makeChoices("move sheercold", "move calmmind")
			p.notFainted(p.foe(), "an Ice-type should be immune to Sheer Cold")
		})
	})

	describe(t, "Sheer Cold [Gen 6]", func(g *psg) {
		g.skip("should affect Ice-type Pokémon", "gen 6 mechanics")
	})
}
