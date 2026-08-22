//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/clearsmog.js.
//
// The whole file is singles and comes across intact.
//
// Species. Amoonguss is not in this dex and has no stand-in row; Venusaur is
// built instead, which keeps the Grass/Poison body the case uses as the Clear
// Smog thrower. Sableye resolves through the shared table to Gengar. Steelix
// resolves to Onix there, and that row is unusable in the immunity case — it
// says outright that the Steel typing is lost, and Steel is the immunity being
// tested — so Magneton is named instead. Arcanine and Primeape are both in the
// dex, with Intimidate and Anger Point native.
//
// Prankster is not modeled and is incidental in both cases that name it: Gengar
// is faster than Venusaur, so Calm Mind and Substitute already resolve before
// Clear Smog without any priority. It is stripped rather than left in the
// fixture, so a report about these cases is about Clear Smog.
//
// The Anger Point case needs a guaranteed critical hit. Focus Energy (+2
// stages) plus the Scope Lens (+1) reaches the always-crit stage, so it is
// deterministic here without a rigged RNG.

func TestMovesClearSmog(t *testing.T) {
	describe(t, "Clear Smog", func(g *psg) {
		g.it("should remove all stat boosts from the target", func(p *ps) {
			p.battle(
				team{{Species: "Amoonguss", As: "Venusaur", Ability: "regenerator",
					Moves: mv("clearsmog")}},
				team{{Species: "Sableye", Ability: "noability", Moves: mv("calmmind")}},
			)
			p.makeChoices("move clearsmog", "move calmmind")
			p.statStage(p.foe(), "spa", 0, "Clear Smog should have wiped the Calm Mind")
			p.statStage(p.foe(), "spd", 0, "Clear Smog should have wiped the Calm Mind")
		})

		g.it("should not remove stat boosts from a target behind a substitute", func(p *ps) {
			p.battle(
				team{{Species: "Amoonguss", As: "Venusaur", Ability: "regenerator",
					Moves: mv("clearsmog", "toxic")}},
				team{{Species: "Sableye", Ability: "noability",
					Moves: mv("substitute", "calmmind")}},
			)
			p.makeChoices("move toxic", "move substitute")
			p.makeChoices("move clearsmog", "move calmmind")
			p.statStage(p.foe(), "spa", 1, "a substitute should have kept the boosts")
			p.statStage(p.foe(), "spd", 1, "a substitute should have kept the boosts")
		})

		g.it("should not remove stat boosts if the target is immune to its attack type", func(p *ps) {
			p.battle(
				team{{Species: "Amoonguss", As: "Venusaur", Ability: "regenerator",
					Item: "laggingtail", Moves: mv("clearsmog")}},
				team{{Species: "Steelix", As: "Magneton", Ability: "noability",
					Moves: mv("irondefense")}},
			)
			p.makeChoices("move clearsmog", "move irondefense")
			p.statStage(p.foe(), "def", 2, "a Steel body is immune to Poison, so the boost should stand")
		})

		g.it("should not remove stat boosts from the user", func(p *ps) {
			p.battle(
				team{{Species: "Amoonguss", As: "Venusaur", Ability: "regenerator",
					Moves: mv("clearsmog")}},
				team{{Species: "Arcanine", Ability: "intimidate", Moves: mv("morningsun")}},
			)
			p.makeChoices("move clearsmog", "move morningsun")
			p.statStage(p.mine(), "atk", -1, "Clear Smog should not touch its own user's stages")
		})

		g.it("should trigger before Anger Point activates during critical hits", func(p *ps) {
			p.battle(
				team{{Species: "Amoonguss", As: "Venusaur", Ability: "regenerator",
					Item: "scopelens", Moves: mv("focusenergy", "clearsmog")}},
				team{{Species: "Primeape", Ability: "angerpoint", Moves: mv("bulkup")}},
			)
			p.makeChoices("move focusenergy", "move bulkup")
			p.statStage(p.foe(), "atk", 1, "")
			p.statStage(p.foe(), "def", 1, "")

			p.makeChoices("move clearsmog", "move bulkup")
			p.statStage(p.foe(), "atk", 6, "Anger Point should max Attack after Clear Smog has cleared it")
			p.statStage(p.foe(), "def", 0, "Clear Smog should have cleared the Defense boost")
		})
	})
}
