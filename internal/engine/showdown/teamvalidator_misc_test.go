//go:build showdown

package showdown

import "testing"

// Ported from test/sim/team-validator/misc.js.
//
// Nothing came across. The file is level legality and forme legality, and this
// engine has neither axis: level is fixed at 50 with no field on `set` to
// change it, and there is no forme layer, so "battle-only forme", "dexited
// forme", "cosmetic forme" and "CAP Pokemon" all name distinctions the dex
// does not draw. What is left — which move a Pokemon could have carried from
// which game, which encounter it came from, whether a Gen 3 tutor move and a
// Gen 4 ability can coexist — is provenance, and this engine's learn check is
// one flat list per species with no generation on it.
//
// The last case is not a team-validation case at all: it counts the species a
// format's unbanlist admits and compares that with the rule table, so it needs
// both a tier table and a rule table.
//
// Upstream nests the `Hackmons formes` describe inside `Team Validator`,
// between the Gen 3 tutor case and the Pokemon GO cases. The port makes them
// siblings, matching how the rest of this corpus handles a nested describe;
// the ledger key uses the innermost name either way.
//
// The outer describe is `Team Validator`, shared with the other files in
// test/sim/team-validator; the ledger key stays unique because no two of them
// use the same `it` name.

func TestTeamValidatorMisc(t *testing.T) {
	describe(t, "Team Validator", func(g *psg) {
		g.skip("should allow Shedinja to take exactly one level-up move from Ninjask in gen 3-4",
			"Shedinja is not in this 80-species dex and level-up move provenance is not modeled")
		g.skip("should correctly enforce levels on Pokémon with unusual encounters in RBY",
			"team validator: level is fixed at 50")
		g.skip("should correctly enforce per-game evolution restrictions",
			"team validator: per-game evolution restrictions are not a rule this engine has")
		g.skip("should prevent Pokemon that don't evolve via level-up and evolve from a Pokemon that does evolve via level-up from being underleveled.",
			"team validator: level is fixed at 50")
		g.skip("should require Pokémon transferred from Gens 1 and 2 to be above Level 2",
			"team validator: level is fixed at 50")
		g.skip("should enforce Gen 1 minimum levels",
			"team validator: level is fixed at 50")
		g.skip("should correctly enforce Shell Smash as a sketched move for Necturna prior to Gen 9",
			"team validator: Sketch provenance is not a rule this engine has")
		g.skip("should prevent Pokemon from having a Gen 3 tutor move and a Gen 4 ability together without evolving",
			"team validator: move and ability provenance is not a rule this engine has")

		g.skip("should allow various (underleveled) from Pokemon GO",
			"team validator: level is fixed at 50")
		g.skip("should disallow Pokemon from Pokemon GO knowing incompatible moves",
			"team validator: move provenance is not a rule this engine has")
		g.skip("should check for legal combinations of prevo/evo-exclusive moves",
			"team validator: move provenance is not a rule this engine has")
		g.skip("should should validate exactly as many species as are in the unbanlist for 35 Pokes",
			"team validator: tiers and unbanlists are not rules this engine has")
		g.skip("should allow moves learned via HOME relearner",
			"team validator: move provenance is not a rule this engine has")
	})

	describe(t, "Hackmons formes", func(g *psg) {
		g.skip("should reject battle-only formes in Gen 9, even in Hackmons", "formes")
		g.skip("should also reject battle-only dexited formes in Gen 9 Hackmons", "formes")
		g.skip("should not allow Xerneas with a hacked Ability in Gen 9 Hackmons", "formes")
		g.skip("should allow various other hacked formes in Gen 9 Hackmons", "formes")
		g.skip("should not allow old gen-exclusive formes in Gen 9 Hackmons", "formes")
		g.skip("should not allow CAP Pokemon in Gen 9 Hackmons",
			"team validator: CAP Pokemon are not in this 80-species dex")
		g.skip("should allow battle-only formes in Hackmons before Gen 9", "formes")
	})
}
