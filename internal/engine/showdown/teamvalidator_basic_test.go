//go:build showdown

package showdown

import "testing"

// Ported from test/sim/team-validator/basic.js.
//
// Nine of the twenty-four cases came across. They are the ones asking a
// question this engine's ValidateTeam actually asks: does the species exist,
// does the item exist, does the ability belong to the species, does the move
// exist and can this species learn it, is the nature real, is the EV spread
// inside its budget, and is the same move listed twice. The other fifteen ask
// about rules there is no layer for here — generation-specific IV and EV
// formats, Hidden Power types, event and Virtual Console provenance, tiers and
// banlists, happiness, Sketch, Mega Evolution, Dynamax and G-Max.
//
// Two substitutions the ports make on their own, both because the upstream
// name would be rejected for the wrong reason:
//
//   - Meowstic (in "should accept legal movesets") becomes Alakazam, which
//     learns Trick and Magic Coat. Upstream sets Prankster only because
//     Meowstic-M needs a legal ability for the set to validate at all; no
//     Kanto species has Prankster, so the port leaves the ability at the
//     species default. The moveset is what the case is named for and is what
//     survives.
//   - Corsola and Snore (in the duplicate-move case) become Snorlax and Body
//     Slam. Neither the species nor the move is in this dataset, so a literal
//     port would be rejected for an unrecognized name and would report a pass
//     without ever reaching the rule under test.
//
// Pikachu resolves through the stand-in table to Raichu, which keeps Static
// and learns all four of Agility, Protect, Thunder and Thunderbolt, so the
// legal-moveset case is a true positive rather than an accident.
//
// The describe is `Team Validator`, shared with the other files in
// test/sim/team-validator; the ledger key stays unique because no two of them
// use the same `it` name.

func TestTeamValidatorBasic(t *testing.T) {
	describe(t, "Team Validator", func(g *psg) {
		g.skip("should have valid formats to work with",
			"team validator: a format table is not a thing this engine has")

		g.it("should reject non-existent Pokemon", func(p *ps) {
			p.illegalTeam(team{
				{Species: "nonexistentPokemon", Moves: mv("thunderbolt"), EVs: evs(map[string]int{"hp": 1})},
			}, "a species with no dex entry should be refused")
		})

		g.it("should reject non-existent items", func(p *ps) {
			p.illegalTeam(team{
				{Species: "pikachu", Ability: "static", Item: "nonexistentItem",
					Moves: mv("thunderbolt"), EVs: evs(map[string]int{"hp": 1})},
			}, "an item outside the catalog should be refused")
		})

		g.it("should reject non-existent abilities", func(p *ps) {
			p.illegalTeam(team{
				{Species: "pikachu", Ability: "nonexistentAbility",
					Moves: mv("thunderbolt"), EVs: evs(map[string]int{"hp": 1})},
			}, "an ability this species does not have should be refused")
		})

		g.it("should reject non-existent moves", func(p *ps) {
			p.illegalTeam(team{
				{Species: "pikachu", Ability: "static", Moves: mv("nonexistentMove"),
					EVs: evs(map[string]int{"hp": 1})},
			}, "a move that is not in the dataset should be refused")
		})

		g.skip("should validate Gen 2 IVs",
			"team validator: per-generation IV rules and Hidden Power types are not rules this engine has")
		g.skip("should validate Gen 2 EVs",
			"team validator: per-generation EV rules are not a rule this engine has")
		g.skip("should validate Gen 7 IVs",
			"team validator: Hidden Power types are not a rule this engine has")
		g.skip("should enforce the 3 perfect IV minimum on legendaries with Gen 6+ origin",
			"team validator: origin-dependent IV minimums are not a rule this engine has")

		g.it("should reject non-existent natures", func(p *ps) {
			p.illegalTeam(team{
				{Species: "pikachu", Ability: "static", Moves: mv("thunderbolt"),
					Nature: "nonexistentNature", EVs: evs(map[string]int{"hp": 1})},
			}, "a nature that is not in the dataset should be refused")
		})

		g.skip("should reject invalid happiness values",
			"team validator: happiness is not a field this engine has")

		// Upstream's spread is illegal on its total rather than on any one
		// stat: 252 is the per-stat cap here too, and three of them is 756
		// against a 510 budget.
		g.it("should validate EVs", func(p *ps) {
			p.illegalTeam(team{
				{Species: "pikachu", Ability: "static", Moves: mv("thunderbolt"),
					EVs: evs(map[string]int{"hp": 252, "atk": 252, "def": 252})},
			}, "an EV spread over the budget should be refused")
		})

		g.it("should accept legal movesets", func(p *ps) {
			p.legalTeam(team{
				{Species: "pikachu", Ability: "static",
					Moves: mv("agility", "protect", "thunder", "thunderbolt"),
					EVs:   evs(map[string]int{"hp": 1})},
			}, "")

			// See the file header on why the ability is dropped here.
			p.legalTeam(team{
				{Species: "meowstic", As: "Alakazam", Moves: mv("trick", "magiccoat"),
					EVs: evs(map[string]int{"hp": 1})},
			}, "")
		})

		// Blast Burn, Frenzy Plant and Hydro Cannon are all in this dataset and
		// none of them is on Raichu's list, so the set is refused for being
		// unlearnable rather than for the unknown fourth move (Dragon Ascent is
		// not in the dataset at all).
		g.it("should reject illegal movesets", func(p *ps) {
			p.illegalTeam(team{
				{Species: "pikachu", Ability: "static",
					Moves: mv("blastburn", "frenzyplant", "hydrocannon", "dragonascent"),
					EVs:   evs(map[string]int{"hp": 1})},
			}, "moves this species cannot learn should be refused")
		})

		g.skip("should reject banned Pokemon",
			"team validator: tiers and banlists are not rules this engine has")
		g.skip("should validate Sketch",
			"team validator: Sketch provenance is not a rule this engine has")
		g.skip("should accept both ability types for Mega Evolutions",
			"mega evolution")
		g.skip("should reject newer Pokemon in older gens",
			"team validator: generation membership is not a rule this engine has")
		g.skip("should reject Max moves added directly to a Pokemon's moveset",
			"Dynamax")
		g.skip("should reject exclusive G-Max moves added directly to a Pokemon's moveset",
			"Dynamax")
		g.skip("should reject Gmax Pokemon from formats with Dynamax Clause",
			"Dynamax")

		// Only the first half ports. The second asks the same team to validate
		// under Pure Hackmons, which lifts the duplicate-move rule; this engine
		// has one rule set and no way to lift it.
		g.it("should not allow duplicate moves on the same set, except in hackmons", func(p *ps) {
			p.illegalTeam(team{
				{Species: "corsola", As: "Snorlax", Moves: mv("bodyslam", "bodyslam"),
					EVs: evs(map[string]int{"hp": 1})},
			}, "the same move listed twice should be refused")
		})

		g.skip("should accept VC moves only with Hidden ability and correct IVs",
			"team validator: Virtual Console transfer legality is not a rule this engine has")
		g.skip("should disallow past gen only moves in Gen 9",
			"team validator: generation membership is not a rule this engine has")
	})
}
