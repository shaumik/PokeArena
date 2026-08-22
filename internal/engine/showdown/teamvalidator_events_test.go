//go:build showdown

package showdown

import "testing"

// Ported from test/sim/team-validator/events.js.
//
// Nothing came across. Every case in the file asks where a Pokémon came from —
// which event distribution, which generation, which in-game trade — and then
// asks whether the moves, ability, nature, IVs, level and shininess on the set
// are consistent with that origin. This engine's validator asks a much smaller
// question: are the six slots real species with real moves, and do they satisfy
// the Species, Item, Evasion and OHKO clauses. It has no event table, no
// generation axis, no Hidden Ability flag, no level (fixed at 50), and no
// concept of shininess, so there is no rule here for a port to exercise.
//
// The species make it moot a second time over: Garchomp, Spinda, Boldore,
// Kecleon, Greninja-Ash, Urshifu, Cosmoem, Zeraora, Heatran, Regirock, Zapdos,
// Diancie, Nidoking, Espeon, Dracovish, Dragonite, Mandibuzz, Volcarona, Arceus
// and Basculin-Blue-Striped either have no dex entry or resolve to a stand-in
// whose whole point is that it preserves typing rather than provenance.
//
// The describe is `Team Validator`, shared with the other files in
// test/sim/team-validator; the ledger key stays unique because no two of them
// use the same `it` name.

func TestTeamValidatorEvents(t *testing.T) {
	describe(t, "Team Validator", func(g *psg) {
		g.skip("should require Hidden Ability status to match event moves",
			"team validator: event legality is not a rule this engine has")
		g.skip("should handle Dream World moves",
			"team validator: Dream World move provenance is not a rule this engine has")
		g.skip("should reject mutually incompatible Dream World moves",
			"team validator: Dream World move provenance is not a rule this engine has")
		g.skip("should consider Dream World Abilities as Hidden based on Gen 5 data",
			"team validator: Hidden Ability provenance is not a rule this engine has")
		g.skip("should properly validate Greninja-Ash",
			"team validator: event legality is not a rule this engine has")
		g.skip("should not allow evolutions of Shiny-locked events to be Shiny",
			"team validator: shininess is not a rule this engine has")
		g.skip("should not allow events to use moves only obtainable in a previous generation",
			"team validator: transfer legality is not a rule this engine has")
		g.skip("should accept event Pokemon with oldgen tutor moves and HAs in formats with Ability Patch",
			"team validator: tutor-move provenance is not a rule this engine has")
		g.skip("should validate the Diancie released with zero perfect IVs",
			"team validator: event IV spreads are not a rule this engine has")
		g.skip("should not allow Gen 1 JP events",
			"team validator: event legality is not a rule this engine has")
		g.skip("should allow Gen 2 events of Gen 1 Pokemon to learn moves exclusive to Gen 1",
			"team validator: transfer legality is not a rule this engine has")
		g.skip("should allow Gen 2 events that evolve into Gen 1 Pokemon to learn moves exclusive to Gen 1",
			"team validator: transfer legality is not a rule this engine has")
		g.skip("should allow Gen 2 events in Gen 1 Tradebacks OU",
			"team validator: tradeback formats are not a rule this engine has")
		g.skip("should allow use of a Hidden Ability if the format has the item Ability Patch",
			"team validator: Hidden Ability provenance is not a rule this engine has")
		g.skip("should allow evolved Pokemon obtainable from events at lower levels than they could otherwise be obtained",
			"team validator: level caps are not a rule this engine has")
		g.skip("should force Gen 4 Arceus to have max 100 EVs in any one stat and only multiples of 10",
			"team validator: per-generation EV limits are not a rule this engine has")
		g.skip("should allow Hall of Origin Arceus with Full Arceus Clause",
			"team validator: custom clauses are not a rule this engine has")
		g.skip("should properly validate Rock Head Basculin-Blue-Striped in gen5bw1",
			"team validator: in-game trade provenance is not a rule this engine has")
	})
}
