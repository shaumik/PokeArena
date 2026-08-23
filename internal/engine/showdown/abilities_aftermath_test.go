//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/aftermath.js.
//
// Neither species is in this dex. Scyther replaces Galvantula as the Lunge
// user — what the case needs from the attacker is only that it makes contact
// and has enough HP left to read a quarter off. Electrode replaces Shiftry
// because it is the in-dex species that carries Aftermath natively.
//
// Upstream gets its one-shot KO from Lunge hitting Shiftry for 4x at level
// 100. Levels are fixed at 50 here and Electrode is neutral to Bug, so the
// same setup is arranged by starting Electrode at 1 HP: what the case is about
// is the Aftermath recoil after a contact KO, not how the KO was reached.
//
// Sleep Talk is not in this dataset; the Aftermath holder only has to be
// there to die, so Splash replaces it.

func TestAbilitiesAftermath(t *testing.T) {
	describe(t, "Aftermath", func(g *psg) {
		g.it("should hurt attackers by 1/4 their max HP when this Pokemon is KOed by a contact move", func(p *ps) {
			p.battle(
				team{{Species: "galvantula", As: "Scyther", Moves: mv("lunge")}},
				team{{Species: "shiftry", As: "Electrode", Ability: "aftermath", Moves: mv("splash"), HP: 1}},
			)
			p.turn()
			attacker := p.mine()
			p.equal(attacker.HP, attacker.MaxHP-attacker.MaxHP/4, "Aftermath should have taken a quarter of the attacker's max HP")
		})
	})
}
