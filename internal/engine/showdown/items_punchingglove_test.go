//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/punchingglove.js.
//
// Sleep Talk is not in this dataset and is pure filler on both sides here, so
// Splash stands in for it.
//
// Bodies: Wynaut and Happiny go through the stand-in table. Miltank is only a
// Rocky Helmet holder that has to survive a hit, which Chansey does. Weavile is
// only a Pickpocket body, so Persian carries the ability explicitly; Pickpocket
// itself is what the case is about and the harness reports whether the engine
// models it.
//
// The last case keeps Baneful Bunker, Obstruct and Spiky Shield even though
// none of the three is in this dataset — they are the subject of the case, not
// filler, so the missing-move failure is the finding.

func TestItemsPunchingGlove(t *testing.T) {
	describe(t, "Punching Glove", func(g *psg) {
		g.it("should prevent item effects triggered by contact from acting", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "punchingglove", Moves: mv("bulletpunch")}},
				team{{Species: "miltank", As: "Chansey", Item: "rockyhelmet", Moves: mv("splash")}},
			)
			p.turn()
			p.fullHP(p.mine(), "Attacker should not be hurt")
		})

		g.it("should not prevent item effects triggered by contact from acting if using non-punching contact move", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "punchingglove", Moves: mv("tackle")}},
				team{{Species: "miltank", As: "Chansey", Item: "rockyhelmet", Moves: mv("splash")}},
			)
			p.turn()
			p.damaged(p.mine(), "Attacker should be hurt")
		})

		g.it("should not activate on the opponent's moves", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "punchingglove", Moves: mv("splash")}},
				team{{Species: "happiny", Moves: mv("lunge")}},
			)
			p.turn()
			p.statStage(p.mine(), "atk", -1, "Attack should be lowered")
		})

		g.it("should stop Pickpocket", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "punchingglove", Moves: mv("bulletpunch")}},
				team{{Species: "weavile", As: "Persian", Ability: "pickpocket", Moves: mv("splash")}},
			)
			p.turn()
			p.equal(p.mine().Item, "punchingglove", "Attacker should not lose their item")
			p.noItem(p.foe(), "Target should not steal Punching Glove")
		})

		g.it("should block against Protecting effects with a contact side effect", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "punchingglove", Moves: mv("splash", "bulletpunch")}},
				team{{Species: "aggron", Moves: mv("splash", "banefulbunker", "obstruct", "spikyshield")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move bulletpunch", "move banefulbunker")
			p.turn()
			p.makeChoices("move bulletpunch", "move obstruct")
			p.turn()
			p.makeChoices("move bulletpunch", "move spikyshield")
			p.turn()
			wynaut := p.mine()
			p.noStatus(wynaut, "Wynaut should not have been poisoned by Baneful Bunker")
			p.statStage(wynaut, "def", 0, "Wynaut's Defense should not have been lowered by Obstruct")
			p.fullHP(wynaut, "Wynaut should not have lost HP from Spiky Shield")
		})
	})
}
