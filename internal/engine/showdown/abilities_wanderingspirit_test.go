//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/wanderingspirit.js.
//
// Wandering Spirit is not one of the abilities this engine models, which is
// what the one ported case reports.
//
// Neither species is in the dex. Decidueye is built as Venusaur, which carries
// the Overgrow the case swaps away; Runerigus as Gengar, a ghost body to hold
// Wandering Spirit. Shadow Sneak is not in this dataset either, so the contact
// ghost move is Shadow Claw — same type, also contact, and the priority Shadow
// Sneak brings plays no part here.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.

func TestAbilitiesWanderingSpirit(t *testing.T) {
	describe(t, "Wandering Spirit", func(g *psg) {
		g.it("should exchange abilities with an attacker that makes contact", func(p *ps) {
			p.battle(
				team{{Species: "Decidueye", As: "Venusaur", Ability: "overgrow", Moves: mv("shadowclaw")}},
				team{{Species: "Runerigus", As: "Gengar", Ability: "wanderingspirit", Moves: mv("splash")}},
			)
			p.makeChoices("move shadowclaw", "move splash")
			p.hasAbility(p.mine(), "wanderingspirit", "the attacker should have come away with Wandering Spirit")
			p.hasAbility(p.foe(), "overgrow", "and the holder with the attacker's ability")
		})

		g.skip("should not activate while Dynamaxed", "Dynamax")

		g.skip("should not swap with Wonder Guard",
			"Shedinja is not in this 80-species dex and Wonder Guard is not modeled")
	})
}
