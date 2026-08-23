//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/roughskin.js.
//
// Two substitutions, neither touching the subject. Shedinja is the Rough Skin
// holder here with its own ability overwritten, so it is a body and not the
// Wonder Guard species the port rules reserve; Snorlax takes the slot because
// it is normal-typed (Nuzzle's paralysis can land, which is the secondary the
// case is about) and survives the hit. Pachirisu is the attacker and is not in
// this dex; Jolteon keeps both the electric typing and the Volt Absorb
// upstream wrote on it.
//
// Rough Skin is not in this ability set, so the recoil the case counts never
// happens; the harness names the ability and the HP assertion below is the
// same finding stated as a number.
//
// Sleep Talk is not in this dataset either; the holder only has to stand
// there, so Splash replaces it.

func TestAbilitiesRoughSkin(t *testing.T) {
	describe(t, "Rough Skin", func(g *psg) {
		g.it("should not activate twice on moves with secondary effects", func(p *ps) {
			p.battle(
				team{{Species: "Shedinja", As: "Snorlax", Ability: "roughskin", Moves: mv("splash")}},
				team{{Species: "Pachirisu", As: "Jolteon", Ability: "voltabsorb", Moves: mv("nuzzle")}},
			)
			p.makeChoices("", "move nuzzle")
			pachi := p.foe()
			p.equal(pachi.HP, pachi.MaxHP-pachi.MaxHP/8, "Rough Skin should have taken exactly one eighth, once")
		})
	})
}
