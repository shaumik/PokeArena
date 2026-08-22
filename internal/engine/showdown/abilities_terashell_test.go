//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/terashell.js.
//
// Nothing in this file survives the port, and the reason is the same in every
// case: Tera Shell only works while the holder's current species is
// Terapagos-Terastal, so the species *is* the mechanic. Terapagos is not in
// this 80-species dex, Terastal is a forme this engine has no layer for, and
// Tera Shell is not in the ability set. Substituting a body would produce a
// case that measures a plain damage roll and calls it Tera Shell, which is
// worse than not porting it — the same reasoning names.go gives for refusing a
// stand-in for Ditto under Transform or Shedinja under Wonder Guard.
//
// Every case is therefore recorded as a skip rather than as a missing-move
// finding, which is the porting rule for an absent species. Several of them
// would also need Wicked Blow, Surging Strikes, Flower Trick, Soak, Forest's
// Curse, Transform, Lucky Chant, Future Sight, Super Fang, Counter or Final
// Gambit, none of which are in this dataset; that is a second reason, not the
// first one.
//
// The last case is `it.skip` upstream as well.

func TestAbilitiesTeraShell(t *testing.T) {
	describe(t, "Tera Shell", func(g *psg) {
		g.skip("should take not very effective damage when it is at full health",
			"Terapagos is not in this 80-species dex and Tera Shell is not modeled")

		g.skip("should not take precedence over immunities",
			"Terapagos is not in this 80-species dex and Tera Shell is not modeled")

		g.skip("should not activate if Terapagos already resists the move",
			"Terapagos is not in this 80-species dex and Tera Shell is not modeled")

		g.skip("All hits of multi-hit move should be not very effective",
			"Terapagos is not in this 80-species dex and Tera Shell is not modeled")

		g.skip("should be suppressed by Gastro Acid",
			"Terapagos is not in this 80-species dex and Tera Shell is not modeled")

		g.skip("should not work if the user's species is not currently Terapagos-Terastal",
			"Terapagos is not in this 80-species dex, and formes and Transform are not modeled")

		g.skip("should not weaken the damage from Struggle",
			"Terapagos is not in this 80-species dex and Tera Shell is not modeled")

		g.skip("should not continue to weaken attacks after taking damage from a Future attack",
			"Terapagos is not in this 80-species dex and Tera Shell is not modeled")

		g.skip("should activate, but not weaken, moves with fixed damage",
			"Terapagos is not in this 80-species dex and Tera Shell is not modeled; the case is it.skip upstream too")
	})
}
