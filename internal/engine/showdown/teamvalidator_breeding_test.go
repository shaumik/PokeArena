//go:build showdown

package showdown

import "testing"

// Ported from test/sim/team-validator/breeding.js.
//
// Nothing came across. Every case asks whether a moveset could have been bred:
// which egg moves a father could have carried, which combinations are mutually
// exclusive because no single father has both, which chains route through a
// third species, which moves survive a trade back to an earlier generation,
// and at what level a bred Pokemon can legally appear. This engine's learn
// check is one flat list per species — `sp.Moves`, read out of
// data/pokedex.json — with no egg group, no father, no generation axis and no
// level (fixed at 50), so there is no provenance for any of these to be
// checked against.
//
// The last two also turn on Pokemon that only exist as a forme or an event
// distribution (Greninja-Bond, Pikachu-Alola, Ursaluna-Bloodmoon), neither of
// which this engine models.
//
// Where a stand-in row does exist — Blissey, Skarmory, Snorlax, Salamence,
// Tyranitar, Chansey, Marowak, Weezing, Azumarill, Staryu — it promises typing
// and an ability, never a learnset, which is the whole subject here.
//
// The describe is `Team Validator`, shared with the other files in
// test/sim/team-validator; the ledger key stays unique because no two of them
// use the same `it` name.

func TestTeamValidatorBreeding(t *testing.T) {
	describe(t, "Team Validator", func(g *psg) {
		g.skip("should validate Shedinja's egg moves correctly",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should properly exclude egg moves for Baby Pokemon and their evolutions",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should disallow 4 egg moves on move evolutions before gen 6",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should disallow egg moves with male-only Hidden Abilities",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should disallow Pokemon in Little Cup that can't be bred to be level 5",
			"team validator: level caps are not a rule this engine has")
		g.skip("should reject illegal egg move combinations",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should allow chain breeding",
			"team validator: breeding chains are not a rule this engine has")
		g.skip("should accept this chainbreed on Snorlax",
			"team validator: breeding chains are not a rule this engine has")
		g.skip("should allow trading back Gen 2 egg moves if compatible with Gen 1",
			"team validator: transfer legality is not a rule this engine has")
		g.skip("should disallow trading back an egg move not in gen 1",
			"team validator: transfer legality is not a rule this engine has")
		g.skip("should properly resolve egg moves for Pokemon with pre-evolutions that don't have Hidden Abilities",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should allow Nidoqueen to have egg moves",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should properly handle HA Dragonite with Extreme Speed",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should disallow low-level female-only Pokemon with illegal (level up) egg moves/egg move combinations",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should disallow illegal (level up) egg move combinations involving moves that can't be tradebacked",
			"team validator: transfer legality is not a rule this engine has")
		g.skip("should disallow illegal (level up) egg moves/egg move combinations involving Pomeg glitch and Gen 4 abilities",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should allow previously illegal level up egg moves in Gen 7",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should allow Pomeg glitch with event egg moves",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should disallow illegal egg move combinations containing past gen universal moves",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should allow complex chainbred sets",
			"team validator: breeding chains are not a rule this engine has")
		g.skip("should reject Volbeat with both Lunge and Dizzy Punch in Gen 7",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should allow level 5 Indeedee-M with Disarming Voice",
			"team validator: egg-move legality is not a rule this engine has")
		g.skip("should allow egg moves on event formes in Gen 9",
			"formes")
		g.skip("should not allow egg moves on event formes before Gen 9",
			"formes")
		g.skip("should not allow egg Pokemon below level 5 in Gens 2-3",
			"team validator: level caps are not a rule this engine has")
	})
}
