//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/clearbody.js.
//
// Species. Tentacruel and Arbok are both in the dex with the abilities the
// original wanted. Haxorus is not; Pinsir replaces it as the only in-dex body
// carrying Mold Breaker of its own. Metagross is not either — Magneton stands
// in, a grounded body Sticky Web would reach, with Clear Body set explicitly
// exactly as upstream sets it. Malamar becomes Hypno, which keeps the psychic
// half of its typing; no case here turns on the dark half.
//
// Moves. Sticky Web, Topsy-Turvy and Belly Drum are not in this dataset, so
// the three cases built on them report the missing move, and that report is
// the finding. Sleep Talk is absent too and is replaced by Splash wherever it
// was only an idle turn.
//
// Abilities on the Arbok side. Upstream uses Unnerve as an ability that does
// nothing; this engine registers Unnerve but implements nothing for it, and
// the harness reports an inert ability as a failure — which would sink two
// Clear Body cases on a fact about Unnerve. They use "noability", the port's
// idiom for a body that must not interfere, and the Unnerve gap is recorded
// by the Unnerve port where it belongs. The Swagger case instead uses No
// Guard: Swagger is 85% accurate and every case here replays over five seeds,
// so left alone it would be measuring the accuracy roll. Neither substitution
// touches stat changes.

func TestAbilitiesClearBody(t *testing.T) {
	describe(t, "Clear Body", func(g *psg) {
		g.it("should negate stat drops from opposing effects", func(p *ps) {
			p.battle(
				team{{Species: "Tentacruel", Ability: "clearbody", Moves: mv("recover")}},
				team{{Species: "Arbok", Ability: "intimidate",
					Moves: mv("acidspray", "leer", "scaryface", "charm", "confide")}},
			)
			// Arbok's Intimidate fires on the way in, so the first assertion
			// already covers a switch-in drop as well as the move's.
			drops := []struct{ move, stat string }{
				{"acidspray", "spd"},
				{"leer", "def"},
				{"scaryface", "spe"},
				{"charm", "atk"},
				{"confide", "spa"},
			}
			for _, d := range drops {
				p.makeChoices("move recover", "move "+d.move)
				p.statStage(p.mine(), d.stat, 0, "Clear Body should have refused the drop")
			}
			for _, d := range drops {
				p.statStage(p.mine(), d.stat, 0, "no stat should have ended the sequence lowered")
			}
		})

		g.it("should not negate stat drops from the user's moves", func(p *ps) {
			p.battle(
				team{{Species: "Tentacruel", Ability: "clearbody", Moves: mv("superpower")}},
				team{{Species: "Arbok", Ability: "noability", Moves: mv("coil")}},
			)
			p.makeChoices("move superpower", "move coil")
			p.statStage(p.mine(), "atk", -1, "Superpower's own drop should land")
			p.statStage(p.mine(), "def", -1, "Superpower's own drop should land")
		})

		g.it("should not negate stat boosts from opposing moves", func(p *ps) {
			p.battle(
				team{{Species: "Tentacruel", Ability: "clearbody", Moves: mv("splash")}},
				team{{Species: "Arbok", Ability: "noguard", Moves: mv("swagger")}},
			)
			p.makeChoices("move splash", "move swagger")
			p.statStage(p.mine(), "atk", 2, "Swagger's boost is not a drop and should go through")
		})

		g.it("should not negate absolute stat changes", func(p *ps) {
			p.battle(
				team{{Species: "Tentacruel", Ability: "clearbody", Moves: mv("coil")}},
				team{{Species: "Malamar", As: "Hypno", Ability: "noability", Moves: mv("topsyturvy")}},
			)
			p.makeChoices("move coil", "move topsyturvy")
			p.statStage(p.mine(), "atk", -1, "Topsy-Turvy sets a stage rather than lowering it")
			p.statStage(p.mine(), "def", -1, "Topsy-Turvy sets a stage rather than lowering it")
			p.statStage(p.mine(), "accuracy", -1, "Topsy-Turvy sets a stage rather than lowering it")
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Tentacruel", Ability: "clearbody", Moves: mv("recover")}},
				team{{Species: "Haxorus", As: "Pinsir", Ability: "moldbreaker", Moves: mv("growl")}},
			)
			p.makeChoices("move recover", "move growl")
			p.statStage(p.mine(), "atk", -1, "Mold Breaker should read past Clear Body")
		})

		g.it("should be suppressed by Mold Breaker if it is forced out by a move", func(p *ps) {
			p.battle(
				team{
					{Species: "Metagross", As: "Magneton", Ability: "clearbody", Moves: mv("splash")},
					{Species: "Metagross", As: "Magneton", Ability: "clearbody", Moves: mv("splash")},
				},
				team{{Species: "Haxorus", As: "Pinsir", Ability: "moldbreaker", Moves: mv("roar", "stickyweb")}},
			)
			p.makeChoices("move splash", "move stickyweb")
			p.makeChoices("move splash", "move roar")
			// Upstream's third choice is "switch 2": Showdown moves the active
			// Pokemon to index 0 of the team array, so after the Roar its
			// "slot 2" is the Metagross that started the battle. This engine
			// does not reorder, so the same Pokemon is slot 1 here.
			p.makeChoices("switch 1", "default")
			p.statStage(p.mine(), "spe", -1, "Sticky Web set by a Mold Breaker should reach Clear Body")
		})

		g.it("should not take priority over a stat being at -6", func(p *ps) {
			p.battle(
				team{{Species: "Dragapult", Ability: "clearbody", Moves: mv("bellydrum", "splash")}},
				team{{Species: "Malamar", As: "Hypno", Moves: mv("topsyturvy", "growl")}},
			)
			p.turn()
			p.makeChoices("move splash", "move growl")
			p.statStage(p.mine(), "atk", -6, "Belly Drum inverted by Topsy-Turvy bottoms the stage out")
			// Upstream reads the protocol line |-unboost|...|atk|0. The prose
			// equivalent is the engine's "won't go" refusal, which is what an
			// already-floored stat says instead of Clear Body's block.
			p.logHas("won't go", "a floored stat should refuse the drop on its own, not via Clear Body")
		})
	})
}
