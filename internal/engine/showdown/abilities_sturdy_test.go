//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/sturdy.js.
//
// Sturdy is modeled, and every case here turns on a hit that would be lethal
// from full HP. Upstream arranges that with a level 1 body; level is fixed at
// 50 here, so the same relationship comes out of type effectiveness instead.
// Pinsir's Earthquake is 4x on Magneton and overkills it by nearly a factor
// of two, which is a clean lethal hit with no secondary and no accuracy roll
// to spoil the measurement — Paras, Aron and Stufful all become that Magneton
// (none of the three is in the dex). Golem carries Sturdy natively and takes
// the OHKO-move case, where nothing has to be survivable.
//
// Reshiram and Turboblaze are not modeled. Mold Breaker is, Pinsir carries it
// natively, and it is the ability the case actually names — so the suppressed
// case is the surviving case with one ability changed and nothing else, which
// is a stronger pairing than upstream's two different attackers. Charizard's
// sun goes with it: Earthquake does not need it, and Flamethrower's 10% burn
// would finish off the 1 HP survivor on some seeds.
//
// Three cases put Sturdy on Shedinja, whose 1 max HP is the entire mechanic —
// recoil, residual damage and confusion damage are all lethal to it and to
// nothing else. There is no stand-in for that and no way to set a max HP, so
// those skip. The False Swipe case skips for the level reason: at level 50 no
// False Swipe in this dex comes near a full-HP KO, so the case would pass
// without measuring anything.
//
// Sleep Talk is not in this dataset; Splash replaces it wherever it was
// standing in for "do nothing".

func TestAbilitiesSturdy(t *testing.T) {
	describe(t, "Sturdy", func(g *psg) {
		g.it("should give the user an immunity to OHKO moves", func(p *ps) {
			p.battle(
				team{{Species: "Golem", Ability: "sturdy", Moves: mv("splash")}},
				team{{Species: "Kyogre", Ability: "noguard", Moves: mv("sheercold")}},
			)
			p.makeChoices("move splash", "move sheercold")
			p.fullHP(p.mine(), "Sturdy should make Sheer Cold bounce off entirely")
		})

		g.it("should allow its user to survive an attack from full HP", func(p *ps) {
			p.battle(
				team{{Species: "Magneton", Ability: "sturdy", Moves: mv("splash")}},
				team{{Species: "Pinsir", Ability: "noability", Moves: mv("earthquake")}},
			)
			p.makeChoices("move splash", "move earthquake")
			p.equal(p.mine().HP, 1, "Sturdy should leave exactly 1 HP")
			p.logHas("hung on with Sturdy", "")
		})

		g.skip("should allow its user to survive a confusion damage hit from full HP",
			"Shedinja is not in this 80-species dex and its 1 max HP is the mechanic")
		g.skip("should not trigger on recoil damage",
			"Shedinja is not in this 80-species dex and its 1 max HP is the mechanic")
		g.skip("should not trigger on residual damage",
			"Shedinja is not in this 80-species dex and its 1 max HP is the mechanic")

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Magneton", Ability: "sturdy", Moves: mv("splash")}},
				team{{Species: "Pinsir", Ability: "moldbreaker", Moves: mv("earthquake")}},
			)
			p.makeChoices("move splash", "move earthquake")
			p.fainted(p.mine(), "Mold Breaker should switch Sturdy off")
		})

		g.it("should trigger before Focus Sash", func(p *ps) {
			p.battle(
				team{{Species: "Pinsir", Ability: "noability", Moves: mv("earthquake")}},
				team{{Species: "Magneton", Ability: "sturdy", Item: "focussash", Moves: mv("splash")}},
			)
			p.turn()
			p.equal(p.foe().HP, 1, "the hit should have been clamped to 1 HP")
			p.holdsItem(p.foe(), "Sturdy should have taken the hit, leaving the Sash unspent")
		})

		g.it("should not trigger when the user also uses Endure", func(p *ps) {
			p.battle(
				team{{Species: "Pinsir", Ability: "noability", Moves: mv("earthquake")}},
				team{{Species: "Magneton", Ability: "sturdy", Moves: mv("endure")}},
			)
			p.turn()
			p.equal(p.foe().HP, 1, "Endure should have left it at 1 HP")
			p.logLacks("hung on with Sturdy", "Sturdy should not activate.")
		})

		g.skip("should not trigger when the user is damaged to 1 HP from False Swipe",
			"level is fixed at 50, and no False Swipe in this dex threatens a full-HP KO to suppress")
	})
}
