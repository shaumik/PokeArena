//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/swordofruin.js.
//
// Sword of Ruin is not one of the abilities this engine models, so both cases
// report that.
//
// Upstream states each case as an absolute damage window at level 100, which
// does not transfer. Each is restated as the comparison the window stood for:
// the same attack measured twice, with Gastro Acid taking one side's Sword of
// Ruin off the field in between, and the aura's quarter off Defense expected to
// show as the larger of the two figures. The separation the damage roll leaves
// is modest — a 33% effect measured through rolls that span 85% to 100% — so
// the threshold is a plain "clearly more", not the ratio itself.
//
// Chien-Pao is not in the dex and has no stand-in row; Kabutops is built
// instead, a hard physical attacker so the two measurements are far enough
// apart to be worth comparing. Chien-Pao's dark/ice typing plays no part:
// neither Aerial Ace nor Storm Throw is either type.
//
// The second case has Sword of Ruin on both sides, so the defender cannot also
// carry the Shell Armor that keeps critical hits out of the first case's
// numbers. Storm Throw replaces Aerial Ace there instead: it always crits in
// this engine, which makes the crit a constant across both measurements rather
// than a coin flip that lands on one of them.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.

func TestAbilitiesSwordOfRuin(t *testing.T) {
	describe(t, "Sword of Ruin", func(g *psg) {
		g.it("should lower the Defense of all other Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Ability: "shellarmor", Moves: mv("splash", "gastroacid")}},
				team{{Species: "chienpao", As: "Kabutops", Ability: "swordofruin", Moves: mv("aerialace")}},
			)
			wynaut := p.mine()
			p.makeChoices("move splash", "move aerialace")
			withRuin := wynaut.MaxHP - wynaut.HP
			p.atLeast(withRuin, 1, "Aerial Ace should have connected")

			p.makeChoices("move gastroacid", "move aerialace")
			p.logHas("ability was suppressed", "Gastro Acid should have taken Sword of Ruin off the field")

			before := wynaut.HP
			p.makeChoices("move splash", "move aerialace")
			withoutRuin := before - wynaut.HP
			p.atLeast(withRuin*20, withoutRuin*21,
				"Sword of Ruin should have made the first hit the harder of the two")
		})

		g.it("should not lower the Defense of other Pokemon with the Sword of Ruin Ability", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Ability: "swordofruin", Moves: mv("splash")}},
				team{{Species: "chienpao", As: "Kabutops", Ability: "swordofruin", Moves: mv("stormthrow", "gastroacid")}},
			)
			wynaut := p.mine()
			p.makeChoices("move splash", "move stormthrow")
			bothHoldIt := wynaut.MaxHP - wynaut.HP
			p.atLeast(bothHoldIt, 1, "Storm Throw should have connected")

			p.makeChoices("move splash", "move gastroacid")
			p.logHas("ability was suppressed", "Gastro Acid should have taken the target's own Sword of Ruin away")

			before := wynaut.HP
			p.makeChoices("move splash", "move stormthrow")
			onlyAttackerHoldsIt := before - wynaut.HP
			p.atLeast(onlyAttackerHoldsIt*20, bothHoldIt*21,
				"a target holding Sword of Ruin itself should have been the one taking the smaller hit")
		})
	})
}
