//go:build showdown

package showdown

import "testing"

// Ported from test/sim/team-validator/formes.js.
//
// Nothing came across. Each case picks a species with more than one forme and
// asks the validator to tell the formes apart — which moves a particular forme
// can have learned, which forme an ambiguous name resolves to, whether two
// formes count as the same species under Species Clause, and which tier each
// forme sits in. This engine has no forme layer at all, so there is no second
// forme for any of these to be distinguished from, and no tier table for the
// Zamazenta case to consult; its clauses are Species, Item, Evasion, OHKO and
// Sleep.
//
// Necrozma, Deoxys, Rockruff, Lycanroc, Toxtricity, Rotom, Kyurem, Hoopa,
// Zacian, Zamazenta and Unown have no dex entries; the one stand-in row that
// touches this file, Deoxys to Mewtwo, explicitly does not preserve the forme
// stat spreads, which is the whole subject of the Gen 3 Deoxys case.
//
// The describe is `Team Validator`, shared with the other files in
// test/sim/team-validator; the ledger key stays unique because no two of them
// use the same `it` name.

func TestTeamValidatorFormes(t *testing.T) {
	describe(t, "Team Validator", func(g *psg) {
		g.skip("should validate Necrozma formes correctly", "formes")
		g.skip("should reject Ultra Necrozma where ambiguous", "formes")
		g.skip("should handle Deoxys formes in Gen 3", "formes")
		g.skip("should correctly validate USUM Rockruff", "formes")
		g.skip("should reject Pokemon that cannot obtain moves in a particular forme", "formes")
		g.skip("should tier Zacian and Zamazenta formes separately", "formes")
		g.skip("should validate Unown formes in Gen 2 based on DVs", "formes")
	})
}
