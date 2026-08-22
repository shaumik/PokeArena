//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/battlearmor.js.
//
// Upstream inspects the crit flag through a ModifyDamage event hook, which has
// no counterpart here. This engine announces a critical hit in the turn
// narration instead, so both cases assert on that line: absent when Battle
// Armor holds, present when Mold Breaker goes through it. Frost Breath always
// crits in this engine too (internal/engine/willcrit.go), so the line is the
// whole question.
//
// Cryogonal is not in the dex and has no stand-in row. Dewgong is built
// instead: an ice body to carry Frost Breath is all the case wants from it.
// Frost Breath is 90% accurate in this dataset, which is why upstream's No
// Guard and Zoom Lens both matter and are kept.
//
// In the first case Slowbro uses Splash rather than upstream's Quick Attack:
// a stray critical hit off Quick Attack would put the line in the log and fail
// a logLacks that has nothing to do with Battle Armor. The second case keeps
// Quick Attack, because Zoom Lens only applies when its holder moves second
// and the priority is what arranges that.

func TestAbilitiesBattleArmor(t *testing.T) {
	describe(t, "Battle Armor", func(g *psg) {
		g.it("should prevent moves from dealing critical hits", func(p *ps) {
			p.battle(
				team{{Species: "Slowbro", Ability: "battlearmor", Moves: mv("splash")}},
				team{{Species: "Cryogonal", As: "Dewgong", Ability: "noguard", Moves: mv("frostbreath")}},
			)
			p.makeChoices("move splash", "move frostbreath")
			p.damaged(p.mine(), "Frost Breath should still have connected")
			p.logLacks("A critical hit!", "Battle Armor should have taken the crit off Frost Breath")
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Slowbro", Ability: "battlearmor", Moves: mv("quickattack")}},
				team{{Species: "Cryogonal", As: "Dewgong", Ability: "moldbreaker", Item: "zoomlens", Moves: mv("frostbreath")}},
			)
			p.makeChoices("move quickattack", "move frostbreath")
			p.logHas("A critical hit!", "Mold Breaker should have let Frost Breath crit through Battle Armor")
		})
	})
}
