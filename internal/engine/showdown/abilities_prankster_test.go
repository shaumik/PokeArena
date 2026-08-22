//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/prankster.js.
//
// Prankster is not in this dataset, so the two cases that can be built are
// real questions rather than skips.
//
// Four of the seven turn on Prankster's Dark-type immunity, and this dex has
// no Dark-type at all — it is 80 Kanto species and the type did not exist in
// Gen 1 — so there is no body to point a boosted status move at. Two of those
// four are doubles as well. They skip.
//
// Murkrow and Sableye are not in the dex and their rows do not preserve what
// is needed here; Hypno is named directly instead, because what the priority
// case needs from the Prankster side is only that it is slower than the foe,
// which it is against Deoxys-Speed's stand-in Alakazam.
//
// The hint case has no exact counterpart: this engine emits prose, not
// Showdown's `|-hint|` protocol line, so there is nothing named "hint" to
// look for. It is ported as the two observable halves — a Fire-type is not
// burned, and nothing in the narration names Prankster — and the leak itself
// is not checkable here.

func TestAbilitiesPrankster(t *testing.T) {
	describe(t, "Prankster", func(g *psg) {
		g.it("should increase the priority of Status moves", func(p *ps) {
			p.battle(
				team{{Species: "Hypno", Ability: "prankster", Moves: mv("taunt")}},
				team{{Species: "Deoxys-Speed", Ability: "pressure", Moves: mv("calmmind")}},
			)
			p.makeChoices("move taunt", "move calmmind")
			p.statStage(p.foe(), "spa", 0, "Taunt should have landed before Calm Mind")
		})

		g.skip("should cause Status moves to fail against Dark Pokémon",
			"no Dark-type species is in this 80-species dex, so Prankster's Dark immunity cannot be built")
		g.skip("should cause bounced Status moves to fail against Dark Pokémon",
			"no Dark-type species is in this 80-species dex, so Prankster's Dark immunity cannot be built")
		g.skip("should not cause bounced Status moves to fail against Dark Pokémon if it is removed",
			"doubles")
		g.skip("should not cause Status moves forced by Encore to fail against Dark Pokémon",
			"no Dark-type species is in this 80-species dex, so Prankster's Dark immunity cannot be built")
		g.skip("should cause moves forced by Encore to fail against Dark Pokémon if the attacker intended to use a Status move",
			"doubles")

		g.it("should not leak the ability via hint if the target is immune to the Status move", func(p *ps) {
			p.battle(
				team{{Species: "Hypno", Ability: "prankster", Moves: mv("willowisp")}},
				team{{Species: "Ninetales", Ability: "pressure", Moves: mv("willowisp")}},
			)
			p.makeChoices("move willowisp", "move willowisp")
			p.noStatus(p.foe(), "a Fire-type cannot be burned")
			p.logLacks("Prankster", "Prankster should not leak the ability via hint")
		})
	})

	describe(t, "Prankster [Gen 6]", func(g *psg) {
		g.skip("should not cause Status moves to fail against Dark Pokémon", "gen 6 mechanics")
	})
}
