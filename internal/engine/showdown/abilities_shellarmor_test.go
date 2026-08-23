//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/shellarmor.js. Same file as battlearmor.js
// with the ability swapped, and it is translated the same way: upstream reads
// the crit flag out of a ModifyDamage hook, this engine announces the critical
// hit in the narration, so the log line is what each case asserts on.
//
// Cryogonal is not in the dex and has no stand-in row; Dewgong is built
// instead, an ice body to carry Frost Breath. Frost Breath is 90% accurate in
// this dataset, so upstream's No Guard and Zoom Lens both still earn their
// place.
//
// The first case gives Slowbro Splash rather than Quick Attack: a stray crit
// off Quick Attack would satisfy the log fragment for a reason that has
// nothing to do with Shell Armor. The second keeps Quick Attack, because Zoom
// Lens only applies when its holder moves second.

func TestAbilitiesShellArmor(t *testing.T) {
	describe(t, "Shell Armor", func(g *psg) {
		g.it("should prevent moves from dealing critical hits", func(p *ps) {
			p.battle(
				team{{Species: "Slowbro", Ability: "shellarmor", Moves: mv("splash")}},
				team{{Species: "Cryogonal", As: "Dewgong", Ability: "noguard", Moves: mv("frostbreath")}},
			)
			p.makeChoices("move splash", "move frostbreath")
			p.damaged(p.mine(), "Frost Breath should still have connected")
			p.logLacks("A critical hit!", "Shell Armor should have taken the crit off Frost Breath")
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Slowbro", Ability: "shellarmor", Moves: mv("quickattack")}},
				team{{Species: "Cryogonal", As: "Dewgong", Ability: "moldbreaker", Item: "zoomlens", Moves: mv("frostbreath")}},
			)
			p.makeChoices("move quickattack", "move frostbreath")
			p.logHas("A critical hit!", "Mold Breaker should have let Frost Breath crit through Shell Armor")
		})
	})
}
