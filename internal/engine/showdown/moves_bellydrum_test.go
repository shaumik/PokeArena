//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/bellydrum.js.
//
// Belly Drum is not in this 538-move dataset. Both cases are written out rather
// than skipped, because that absence is the finding.
//
// Species. Linoone has no stand-in row and is used upstream as a Normal-type
// body that the foe outspeeds; Snorlax is Normal, and slow enough that Machamp
// lands Close Combat before Belly Drum in the second case, which is the whole
// setup there. Terrakion becomes Machamp: Rock is lost, but the case only needs
// a Fighting attacker strong enough to take the user to 1 HP behind Sturdy.
//
// The Z-Belly Drum block skips as a block: Z-moves are not modeled.

func TestMovesBellyDrum(t *testing.T) {
	describe(t, "Belly Drum", func(g *psg) {
		g.it("should reduce the user's HP by half of their maximum HP, then boost their Attack to maximum", func(p *ps) {
			p.battle(
				team{{Species: "Linoone", As: "Snorlax", Ability: "limber", Moves: mv("bellydrum")}},
				team{{Species: "Terrakion", As: "Machamp", Ability: "justified", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			user := p.mine()
			p.makeChoices("move bellydrum", "move bulkup")
			p.equal(user.HP, (user.MaxHP+1)/2, "Belly Drum should cost half the user's max HP, rounded up")
			p.statStage(user, "atk", 6, "Belly Drum should max the user's Attack")
		})

		g.it("should fail if the user's HP is less than half of their maximum HP", func(p *ps) {
			p.battle(
				team{{Species: "Linoone", As: "Snorlax", Ability: "sturdy", Moves: mv("bellydrum")}},
				team{{Species: "Terrakion", As: "Machamp", Ability: "justified", Moves: mv("closecombat")}},
			)
			if p.state() == nil {
				return
			}
			user := p.mine()
			p.makeChoices("move bellydrum", "move closecombat")
			p.equal(user.HP, 1, "Sturdy should have left the user on 1 HP")
			p.statStage(user, "atk", 0, "Belly Drum should fail below half HP")
		})
	})

	describe(t, "Z-Belly Drum", func(g *psg) {
		g.skip("should heal the user, then reduce their HP by half their max HP and boost the user's Attack to maximum",
			"Z-moves")
		g.skip("should not fail even if the user's HP is less than half of their maximum HP", "Z-moves")
	})
}
