//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/smellingsalts.js.
//
// Upstream spends a turn on Thunder Wave to paralyze the target. Thunder Wave
// is 90% accurate, so a literal port would miss on some seeds and the case has
// to hold on all five; the paralysis is set on the fixture instead, which is
// what the harness's Status field is for. Thunder Wave stays in the moveset so
// the port still diffs against its original, but it is never chosen.
//
// Meloetta is not in this dex. Chansey is built instead: Normal, so Smelling
// Salts keeps its STAB, and it genuinely carries Serene Grace. Its Attack is
// far below Meloetta's, which only matters in that the target survives more
// comfortably — the assertion is about the status, not the damage.

func TestMovesSmellingSalts(t *testing.T) {
	describe(t, "Smelling Salts", func(g *psg) {
		g.it("should cure a paralyzed target", func(p *ps) {
			p.battle(
				team{{
					Species: "Meloetta", As: "Chansey", Ability: "serenegrace",
					Moves: mv("smellingsalts", "thunderwave"),
				}},
				team{{Species: "Dragonite", Ability: "multiscale", Status: "par", Moves: mv("roost")}},
			)
			p.makeChoices("move smellingsalts", "move roost")
			p.notEqual(p.foe().Status, "paralysis", "Smelling Salts should have cured the paralysis of the target it hit")
		})
	})
}
