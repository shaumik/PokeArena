//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/berserk.js.
//
// Berserk is not in this dataset; it is set on the fixtures anyway so the two
// singles cases run, and the boost never appearing is the finding. Drampa has no
// stand-in row — Dragonite is a Dragon body, and since both cases damage it with
// fixed-fraction or fixed-damage moves, nothing about the body affects the
// arithmetic. Multiscale is overwritten by the set for the same reason it would
// not have mattered.
//
// Super Fang is not in this dataset and is the subject of the first case — it is
// what puts the holder exactly at half HP — so it is kept and the missing-move
// failure is the finding.
//
// The second case's HP assertion is upstream's level-100 arithmetic
// (maxhp - 200 + maxhp/4) and does not transfer; at level 50 two Seismic Tosses
// are 100 damage, which would still cross the Sitrus threshold, so the port
// states that half as "the berry was eaten". Parental Bond is not modeled, so
// only one hit lands and the berry is not reached: that failure, not the
// vacuously correct Sp. Atk stage, is what the case reports.

func TestAbilitiesBerserk(t *testing.T) {
	describe(t, "Berserk", func(g *psg) {
		g.it("should activate prior to healing from Sitrus Berry", func(p *ps) {
			p.battle(
				team{{Species: "Drampa", As: "Dragonite", Item: "sitrusberry", Ability: "berserk",
					EVs: evs(map[string]int{"hp": 4}), Moves: mv("splash")}},
				team{{Species: "Wynaut", Ability: "compoundeyes", Moves: mv("superfang")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			drampa := p.mine()
			p.statStage(drampa, "spa", 1, "Berserk should fire on crossing half HP")
			p.equal(drampa.HP, drampa.MaxHP/2+drampa.MaxHP/4,
				"Berserk should resolve before the Sitrus Berry heals back over half")
		})

		g.it("should not activate prior to healing from Sitrus Berry after a multi-hit move", func(p *ps) {
			p.battle(
				team{{Species: "Drampa", As: "Dragonite", Item: "sitrusberry", Ability: "berserk",
					EVs: evs(map[string]int{"hp": 4}), Moves: mv("splash")}},
				team{{Species: "Wynaut", Ability: "parentalbond", Moves: mv("seismictoss")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			drampa := p.mine()
			p.statStage(drampa, "spa", 0, "Berserk should not fire between the hits of a multi-hit move")
			p.noItem(drampa, "the Sitrus Berry should still have been eaten once both hits landed")
		})

		g.skip("should not activate below 50% HP if it was damaged by Dragon Darts", "doubles")
	})
}
