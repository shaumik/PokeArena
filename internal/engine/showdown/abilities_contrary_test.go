//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/contrary.js.
//
// Contrary is not in this dataset. It is set on the fixtures anyway so the cases
// run: the first and third will report the uninverted stat change, which is the
// finding. The fourth case asks for the uninverted result and so passes here
// without measuring anything, since there is no inversion to suppress.
//
// Spinda and Serperior have no stand-in rows. Persian is a fast Normal body with
// nothing that interferes, which is all Spinda is used for; Tangela keeps
// Serperior's pure Grass typing. Growlithe resolves to Arcanine, which carries
// Intimidate.
//
// Topsy-Turvy and Belly Drum are not in this dataset and each is the subject of
// its case — the second turns on an absolute stat change existing at all, the
// third on Belly Drum's maximize — so both are kept and the missing-move
// failures are the finding.

func TestAbilitiesContrary(t *testing.T) {
	describe(t, "Contrary", func(g *psg) {
		g.it("should invert relative stat changes", func(p *ps) {
			p.battle(
				team{{Species: "Spinda", As: "Persian", Ability: "contrary", Moves: mv("superpower")}},
				team{{Species: "Dragonite", Ability: "multiscale", Moves: mv("dragondance")}},
			)
			p.makeChoices("move superpower", "move dragondance")
			contraryMon := p.mine()
			p.statStage(contraryMon, "atk", 1, "Contrary should turn Superpower's Attack drop into a boost")
			p.statStage(contraryMon, "def", 1, "Contrary should turn Superpower's Defense drop into a boost")
		})

		g.it("should not invert absolute stat changes", func(p *ps) {
			p.battle(
				team{{Species: "Serperior", As: "Tangela", Ability: "contrary", Moves: mv("leechseed")}},
				team{{Species: "Growlithe", Ability: "intimidate", Moves: mv("topsyturvy")}},
			)
			p.makeChoices("move leechseed", "move topsyturvy")
			p.statStage(p.mine(), "atk", -1,
				"Topsy-Turvy sets the stage outright, so Contrary should leave the inverted Intimidate boost negative")
		})

		g.it("should invert Belly Drum's maximizing Attack", func(p *ps) {
			p.battle(
				team{{Species: "Spinda", As: "Persian", Ability: "contrary", Moves: mv("bellydrum")}},
				team{{Species: "Dragonite", Ability: "multiscale", Moves: mv("dragondance")}},
			)
			p.makeChoices("move bellydrum", "move dragondance")
			p.statStage(p.mine(), "atk", -6, "Contrary should turn Belly Drum's maximize into a minimize")
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Spinda", As: "Persian", Ability: "contrary", Moves: mv("tackle")}},
				team{{Species: "Dragonite", Ability: "moldbreaker", Moves: mv("growl")}},
			)
			p.makeChoices("move tackle", "move growl")
			p.statStage(p.mine(), "atk", -1, "Mold Breaker should let Growl drop Attack normally")
		})
	})
}
