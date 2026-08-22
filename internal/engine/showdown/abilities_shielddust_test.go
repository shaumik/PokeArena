//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/shielddust.js.
//
// Shield Dust is modeled and Venomoth carries it natively, so Venomoth is
// named directly wherever upstream reaches for Dustox — neither Dustox nor
// Ledian nor Talonflame has a stand-in row. Hitmonchan takes the two Ledian
// cases that want Iron Fist and a punching move, and Venomoth itself takes
// the case where Ledian is only the ability's owner. Moltres replaces
// Talonflame: fire/flying with Flame Body, which is what that case uses it
// for. Pinsir carries Mold Breaker natively.
//
// The item case is the one that changes shape. Upstream rigs every secondary
// to 100% through `battle.onEvent` and then asserts a single flinch did not
// happen; there is no such hook here, so it becomes a rate case: over two
// hundred seeds King's Rock must never flinch a Shield Dust holder. A 10%
// flinch that Shield Dust failed to block would show up about twenty times.
// Cotton Guard is not in this dataset — Iron Defense is the self-boost whose
// absence would report the flinch instead, and the flinch is read off the
// narration directly.
//
// Two cases are doubles and skip.

func TestAbilitiesShieldDust(t *testing.T) {
	describe(t, "Shield Dust", func(g *psg) {
		g.skip("should block secondary effects against the user", "doubles")

		g.it("should not block secondary effects that affect the user of the move", func(p *ps) {
			p.battle(
				team{{Species: "Hitmonchan", Ability: "ironfist", Moves: mv("poweruppunch")}},
				team{{Species: "Venomoth", Ability: "shielddust", Moves: mv("roost")}},
			)
			p.makeChoices("move poweruppunch", "move roost")
			p.statStage(p.mine(), "atk", 1, "Power-Up Punch boosts its own user, not the target")
		})

		g.itRate("should block added effects from items", 0.0, 0.0, 200, func(p *ps) bool {
			p.battle(
				team{{Species: "Moltres", Ability: "flamebody", Item: "kingsrock", Moves: mv("flamecharge")}},
				team{{Species: "Clefable", Ability: "shielddust", Moves: mv("irondefense")}},
			)
			p.makeChoices("move flamecharge", "move irondefense")
			p.statStage(p.mine(), "spe", 1, "Flame Charge's own boost is not a secondary on the target")
			return p.logCount("flinched") > 0
		})

		g.it("should block added effects from Fling", func(p *ps) {
			p.battle(
				team{{Species: "Hitmonchan", Ability: "ironfist", Item: "petayaberry", Moves: mv("fling")}},
				team{{Species: "Venomoth", Ability: "shielddust", Moves: mv("roost")}},
			)
			p.makeChoices("move fling", "move roost")
			p.statStage(p.foe(), "spa", 1, "the flung Petaya Berry is eaten by the target")
		})

		g.it("should not block secondary effects on attacks used by the Pokemon with the ability", func(p *ps) {
			p.battle(
				team{{Species: "Venomoth", Ability: "shielddust", Moves: mv("poweruppunch", "strugglebug")}},
				team{{Species: "Clefable", Ability: "unaware", Moves: mv("softboiled")}},
			)
			p.makeChoices("move poweruppunch", "move softboiled")
			p.statStage(p.mine(), "atk", 1, "")
			p.makeChoices("move strugglebug", "move softboiled")
			p.statStage(p.foe(), "spa", -1, "")
		})

		g.it("should be negated by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Pinsir", Ability: "moldbreaker", Moves: mv("strugglebug")}},
				team{{Species: "Venomoth", Ability: "shielddust", Moves: mv("roost")}},
			)
			p.makeChoices("move strugglebug", "move roost")
			p.statStage(p.foe(), "spa", -1, "Mold Breaker should switch Shield Dust off")
		})

		g.skip("should only prevent Sparkling Aria from curing burn if there is only one target", "doubles")
	})
}
