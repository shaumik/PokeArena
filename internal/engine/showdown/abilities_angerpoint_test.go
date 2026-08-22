//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/angerpoint.js.
//
// Anger Point is in this dataset and Primeape carries it, so all four cases run
// as written. Cryogonal and Haxorus have no stand-in rows; Lapras is an Ice body
// and Dragonite a Dragon one, and neither case turns on anything else about the
// attacker — only on whether the hit crits, which Frost Breath, Storm Throw and
// Focus Energy plus Scope Lens each guarantee on their own.

func TestAbilitiesAngerPoint(t *testing.T) {
	describe(t, "Anger Point", func(g *psg) {
		g.it("should maximize Attack when hit by a critical hit", func(p *ps) {
			p.battle(
				team{{Species: "Cryogonal", As: "Lapras", Ability: "noguard", Moves: mv("frostbreath")}},
				team{{Species: "Primeape", Ability: "angerpoint", Moves: mv("endure")}},
			)
			p.turn()
			p.statStage(p.foe(), "atk", 6, "a critical hit should max the Anger Point holder's Attack")
		})

		g.it("should maximize Attack when hit by a critical hit even if the foe has Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Haxorus", As: "Dragonite", Ability: "moldbreaker", Item: "scopelens",
					Moves: mv("focusenergy", "falseswipe")}},
				team{{Species: "Primeape", Ability: "angerpoint", Moves: mv("defensecurl")}},
			)
			p.makeChoices("move focusenergy", "move defensecurl")
			p.makeChoices("move falseswipe", "move defensecurl")
			p.statStage(p.foe(), "atk", 6, "Mold Breaker should not stop Anger Point")
		})

		g.it("should not maximize Attack when dealing a critical hit", func(p *ps) {
			p.battle(
				team{{Species: "Cryogonal", As: "Lapras", Ability: "noguard", Moves: mv("endure")}},
				team{{Species: "Primeape", Ability: "angerpoint", Moves: mv("stormthrow")}},
			)
			p.makeChoices("move endure", "move stormthrow")
			p.statStage(p.mine(), "atk", 0, "the Pokemon that was crit does not have Anger Point")
			p.statStage(p.foe(), "atk", 0, "Anger Point should not fire for a crit its holder dealt")
		})

		g.it("should not maximize Attack when behind a substitute", func(p *ps) {
			p.battle(
				team{{Species: "Cryogonal", As: "Lapras", Ability: "noguard", Item: "laggingtail",
					Moves: mv("frostbreath")}},
				team{{Species: "Primeape", Ability: "angerpoint", Moves: mv("substitute")}},
			)
			p.makeChoices("move frostbreath", "move substitute")
			p.statStage(p.foe(), "atk", 0, "a crit on the Substitute should not reach Anger Point")
		})
	})
}
