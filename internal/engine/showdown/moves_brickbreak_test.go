//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/brickbreak.js.
//
// Upstream reads `battle.p2.sideConditions['reflect']`; here that is
// `p.state().Sides[1].Conditions.Reflect`, which is nil when the screen is
// down. Ninjask is only the body holding the screen up and resolves to Scyther
// through the shared table; Mew and Gengar are both in the dex.
//
// Electrify is not in this dataset. It is the subject of its case — the point
// is that a Ghost-type stops being immune once the incoming move has been
// retyped — so it stays and the missing-move failure is the finding.
//
// Two upstream cases share the string "should break Reflect against a Ghost
// type whose type immunity is being ignored"; both are kept, because the
// strings are the ledger key and rewriting one would lose the link to its
// original.

func TestMovesBrickBreak(t *testing.T) {
	describe(t, "Brick Break", func(g *psg) {
		g.it("should break Reflect", func(p *ps) {
			p.battle(
				team{{Species: "mew", Moves: mv("brickbreak", "splash")}},
				team{{Species: "ninjask", Moves: mv("reflect", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move reflect")
			p.ok(p.state().Sides[1].Conditions.Reflect != nil, "Reflect should be up before Brick Break")

			p.makeChoices("move brickbreak", "move splash")
			p.ok(p.state().Sides[1].Conditions.Reflect == nil, "Brick Break should have broken Reflect")
		})

		g.it("should not break Reflect when used against a Ghost-type", func(p *ps) {
			p.battle(
				team{{Species: "mew", Moves: mv("brickbreak", "splash")}},
				team{{Species: "gengar", Moves: mv("reflect", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move reflect")
			p.ok(p.state().Sides[1].Conditions.Reflect != nil, "Reflect should be up before Brick Break")

			p.makeChoices("move brickbreak", "move splash")
			p.ok(p.state().Sides[1].Conditions.Reflect != nil,
				"a Fighting move a Ghost-type is immune to should leave the screen alone")
		})

		g.skip("should break Reflect when used against a Ghost-type in Gen 4 or earlier",
			"gen 4 mechanics")

		g.it("should break Reflect against a Ghost type whose type immunity is being ignored", func(p *ps) {
			p.battle(
				team{{Species: "mew", Moves: mv("brickbreak", "splash")}},
				team{{Species: "gengar", Item: "ringtarget", Moves: mv("reflect", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move reflect")
			p.ok(p.state().Sides[1].Conditions.Reflect != nil, "Reflect should be up before Brick Break")

			p.makeChoices("move brickbreak", "move splash")
			p.ok(p.state().Sides[1].Conditions.Reflect == nil,
				"a Ring Target holder has no immunity left to hide the screen behind")
		})

		g.it("should break Reflect against a Ghost type whose type immunity is being ignored", func(p *ps) {
			p.battle(
				team{{Species: "mew", Ability: "scrappy", Moves: mv("brickbreak", "splash")}},
				team{{Species: "gengar", Moves: mv("reflect", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move reflect")
			p.ok(p.state().Sides[1].Conditions.Reflect != nil, "Reflect should be up before Brick Break")

			p.makeChoices("move brickbreak", "move splash")
			p.ok(p.state().Sides[1].Conditions.Reflect == nil,
				"Scrappy removes the immunity, so the screen should break")
		})

		g.it("should break Reflect against a Ghost type if it has been electrified", func(p *ps) {
			p.battle(
				team{{Species: "mew", Moves: mv("brickbreak", "splash")}},
				team{{Species: "gengar", Moves: mv("reflect", "electrify")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move reflect")
			p.ok(p.state().Sides[1].Conditions.Reflect != nil, "Reflect should be up before Brick Break")

			p.makeChoices("move brickbreak", "move electrify")
			p.ok(p.state().Sides[1].Conditions.Reflect == nil,
				"an electrified Brick Break is no longer a Fighting move a Ghost-type can ignore")
		})

		g.skip("should break the foe's Reflect when used against an ally in Gen 3",
			"gen 3 mechanics")
	})
}
