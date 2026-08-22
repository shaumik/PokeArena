//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/trumpcard.js.
//
// Upstream reads the move's base power straight off a `BasePower` event and
// compares the list to [40, 50, 60, 80, 200]. There is no such hook here, so
// the port measures the damage each use does instead and states the two things
// that list is really claiming: every use hits at least as hard as the one
// before it, and the last-PP use is a step change rather than another
// increment. Anything that ignores PP fails both.
//
// Species. Eevee resolves to Vaporeon and Lugia to Articuno through the shared
// table; Articuno is used only as a body with Recover and enough special bulk
// to be hit five times, which is what the row promises. Komala is not in the
// dex and is only the caller of Sleep Talk, so Snorlax takes the role.
//
// Upstream's Run Away and Comatose are dropped for "noability". Run Away exists
// here but is inert by design, and Comatose is not modeled; both would be
// reported as findings and would bury the one these cases are about.
//
// The second case also needs a way to write a PP figure into a move slot, which
// the harness does not have, so it spends the PP by using the move: with 5 PP,
// three ordinary uses leave exactly the two upstream measures. Sleep Talk is
// not in this dataset, so that case reports the missing move first.
//
// The Gen 4 Custap case skips.

func TestMovesTrumpCard(t *testing.T) {
	describe(t, "Trump Card", func(g *psg) {
		g.it("should power-up the less PP the move has", func(p *ps) {
			p.battle(
				team{{Species: "Eevee", Ability: "noability", Moves: mv("trumpcard")}},
				team{{Species: "Lugia", Ability: "multiscale", Moves: mv("recover")}},
			)
			foe := p.foe()
			var dmg [5]int
			for i := 0; i < 5; i++ {
				p.makeChoices("move trumpcard", "move recover")
				// The foe heals, so the HP missing at the end of a turn is that
				// turn's Trump Card and nothing else: Articuno outspeeds
				// Vaporeon, so its Recover resolves first and puts it back at
				// full (half its max HP covers every use but the last) before
				// the hit being measured lands. Subtracting a running total
				// would measure the *difference* between consecutive uses
				// against a target that never stays damaged.
				dmg[i] = foe.MaxHP - foe.HP
			}
			for i := 1; i < 5; i++ {
				p.atLeast(dmg[i], dmg[i-1],
					"each Trump Card should hit at least as hard as the one before it")
			}
			p.atLeast(dmg[4], 3*dmg[0],
				"the last-PP use jumps from 80 to 200 base power, so it should dwarf the first")
		})

		g.it("should get its base power calculated from a move calling it", func(p *ps) {
			p.battle(
				team{{Species: "Komala", As: "Snorlax", Ability: "noability",
					Moves: mv("sleeptalk", "trumpcard")}},
				team{{Species: "Lugia", Ability: "multiscale", Moves: mv("recover")}},
			)
			foe := p.foe()
			for i := 0; i < 3; i++ {
				p.makeChoices("move trumpcard", "move recover")
			}
			taken := foe.MaxHP - foe.HP
			p.makeChoices("move sleeptalk", "move recover")
			penultimate := (foe.MaxHP - foe.HP) - taken
			taken += penultimate
			p.makeChoices("move sleeptalk", "move recover")
			last := (foe.MaxHP - foe.HP) - taken
			p.atLeast(last, 2*penultimate,
				"a Trump Card called by Sleep Talk should still read the caller's remaining PP")
		})

		g.skip("should work if called via Custap Berry in Gen 4", "gen 4 mechanics")
	})
}
