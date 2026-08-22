//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/cudchew.js.
//
// Cud Chew is not one of this engine's 118 abilities, so both surviving cases
// report it, and that report is the finding.
//
// Three of the five cases are doubles and turn on what happens to the ally as
// well as the holder, so they skip.
//
// Tauros is in this dex. Toxicroak is not: Muk stands in, keeping the poison
// typing, and is given No Guard because Toxic is 90% accurate in this dataset
// with no poison-type exception, and a case replayed over five seeds would
// otherwise be measuring the accuracy roll. Belly Drum is not in this dataset
// and the case that halves its holder's HP with it reports that too; Sleep
// Talk is replaced by Splash for the idle turns.

func TestAbilitiesCudChew(t *testing.T) {
	describe(t, "Cud Chew", func(g *psg) {
		g.it("should re-activate a berry, eaten in the previous turn", func(p *ps) {
			p.battle(
				team{{Species: "tauros", Ability: "cudchew", Item: "lumberry", Moves: mv("splash")}},
				team{{Species: "toxicroak", As: "Muk", Ability: "noguard", Moves: mv("toxic")}},
			)
			tauros := p.mine()
			p.turn()
			p.noStatus(tauros, "the Lum Berry should have cured the first poisoning")
			p.turn()
			p.noStatus(tauros, "Cud Chew should have brought the berry back for the second")
			p.turn()
			p.hasStatus(tauros, "tox", "the berry is gone for good after the second helping")
		})

		g.skip("should re-activate a berry flung in the previous turn, for both the attacker and the target",
			"doubles")
		g.skip("should not re-activate a berry eaten by Bug Bite, for either the attacker or the target",
			"doubles")
		g.skip("should not be prevented by Unnerve", "doubles")

		g.it("should still activate in the following turn if the berry was consumed during residuals", func(p *ps) {
			p.battle(
				team{{Species: "tauros", Ability: "cudchew", Item: "sitrusberry", Moves: mv("bellydrum")}},
				team{{Species: "toxicroak", As: "Muk", Ability: "noguard", Moves: mv("toxic")}},
			)
			tauros := p.mine()
			p.turn()
			p.turn()
			p.atLeast(tauros.HP, tauros.MaxHP*3/4+1, "Tauros should have eaten its berry twice")
		})
	})
}
