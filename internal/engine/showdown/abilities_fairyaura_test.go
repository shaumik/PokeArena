//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/fairyaura.js.
//
// Fairy Aura is not one of the abilities this engine models, which is what the
// ported case reports.
//
// Upstream states the case as an absolute damage window at level 100, which
// does not transfer. It is restated as the comparison the window stood for: the
// same Moonblast measured twice, once with the aura up and once after Gastro
// Acid has taken it away, with the boost expected to show as the clearly larger
// of the two. That comparison is loose — a critical hit on either measurement
// is worth more than the aura's 33%, and there is no way to hold the crit
// constant on a Fairy move — so the threshold is set well below the true ratio
// and the case is stated as "more", not "a third more".
//
// Mega Ampharos is only ever a Mold Breaker body upstream; Pinsir is built
// instead, which carries Mold Breaker itself and needs no mega stone. Xerneas
// is not in the dex and has no row; Clefable is built for it, a fairy body
// bulky enough to be hit twice.
//
// The Gen 7 block skips whole: this engine has one generation.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.

func TestAbilitiesFairyAura(t *testing.T) {
	describe(t, "Fairy Aura", func(g *psg) {
		g.it("should boost Mold Breaker moves", func(p *ps) {
			p.battle(
				team{{Species: "Ampharos-Mega", As: "Pinsir", Ability: "moldbreaker", Moves: mv("moonblast", "gastroacid")}},
				team{{Species: "Xerneas", As: "Clefable", Ability: "fairyaura", Moves: mv("splash")}},
			)
			xerneas := p.foe()
			p.makeChoices("move moonblast", "move splash")
			withAura := xerneas.MaxHP - xerneas.HP
			p.atLeast(withAura, 1, "Moonblast should have connected")

			p.makeChoices("move gastroacid", "move splash")
			p.logHas("ability was suppressed", "Gastro Acid should have taken Fairy Aura off the field")

			before := xerneas.HP
			p.makeChoices("move moonblast", "move splash")
			withoutAura := before - xerneas.HP
			p.atLeast(withAura*10, withoutAura*11,
				"a Mold Breaker Moonblast should still have been boosted by the aura")
		})
	})

	describe(t, "[Gen 7]", func(g *psg) {
		g.skip("should not boost Mold Breaker moves", "gen 7 mechanics")
	})
}
