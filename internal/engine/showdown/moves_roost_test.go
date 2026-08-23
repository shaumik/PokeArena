//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/roost.js.
//
// Three of the seven cases are out of reach for the format or the roster: one
// is doubles, one needs Terastallization, and the pure-Flying case needs a
// species with no second type — this 80-species dex has none, and Trick-or-
// Treat, which the case uses to prove the substituted type is Normal rather
// than nothing, is not in the dataset either. The `Roost - DPP` describe is a
// generation mod and skips as a block.
//
// Aggron resolves to Magneton through the shared table; the row warns that rock
// is lost, and nothing here depends on it — Magneton is only the Mud-Slap user.
// Clefable, Dragonite and Aerodactyl are all in the dex as written.
//
// Hidden Power Grass is not in this dataset: there is one Hidden Power here and
// it has no type variants. Rather than lose four working assertions to a
// missing-move report about the plumbing, the port substitutes. In "should heal
// the user" the move is only chipping the Roost user, so plain Hidden Power
// does the job; in the type-suppression case the Grass typing is the point, so
// it becomes Energy Ball.
//
// Wonder Guard is not one of this engine's 118 abilities and is kept where
// upstream wrote it, so the case reports it. Worth knowing when reading the
// result: upstream leans on Wonder Guard to make "took damage" mean "was hit
// super-effectively". Without it the Energy Ball assertion is weak — Grass is
// 1x into Rock/Flying and 2x into Rock alone, and both are non-zero. The Mud
// Slap half is unaffected, because Ground into a Flying type is an immunity and
// Ground into bare Rock is not, so that assertion still separates the two
// states cleanly.

func TestMovesRoost(t *testing.T) {
	describe(t, "Roost", func(g *psg) {
		g.it("should fail if the user is at max HP", func(p *ps) {
			p.battle(
				team{{Species: "Clefable", Item: "leftovers", Ability: "unaware", Moves: mv("calmmind")}},
				team{{Species: "Dragonite", Item: "laggingtail", Ability: "multiscale", Moves: mv("roost")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move calmmind", "move roost")
			p.logHas("But it failed!", "Roost at full HP should fail rather than heal nothing quietly")
		})

		g.it("should heal the user", func(p *ps) {
			p.battle(
				team{{Species: "Clefable", Ability: "unaware", Moves: mv("calmmind", "hiddenpower")}},
				team{{Species: "Dragonite", Ability: "multiscale", Moves: mv("roost", "dragondance")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move hiddenpower", "move dragondance")
			p.damaged(p.foe(), "the Roost user needs damage to heal back")
			p.makeChoices("move calmmind", "move roost")
			p.fullHP(p.foe(), "Roost should have restored the user to full")
		})

		g.it("should suppress user's current Flying type if successful", func(p *ps) {
			p.battle(
				team{{
					Species: "Aggron", Item: "leftovers", Ability: "sturdy",
					Moves: mv("mudslap", "energyball"),
				}},
				team{{
					Species: "Aerodactyl", Item: "focussash", Ability: "wonderguard",
					Moves: mv("roost", "doubleedge"),
				}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mudslap", "move roost")
			// Roost fails at full HP, so the Flying type is never suppressed and
			// Mud Slap is still a Ground move into a Flying type.
			p.fullHP(p.foe(), "Mud Slap should not touch a Flying-type whose Roost failed")

			// Ensure that Aerodactyl has some damage
			p.makeChoices("move mudslap", "move doubleedge")

			p.makeChoices("move mudslap", "move roost")
			p.damaged(p.foe(), "a successful Roost drops Flying, so Mud Slap should hit bare Rock")

			// Ensure that Aerodactyl has some damage
			p.makeChoices("move mudslap", "move doubleedge")

			p.makeChoices("move energyball", "move roost")
			p.damaged(p.foe(), "a Grass attack should reach the roosting Rock-type")
		})

		g.skip("should suppress Flying type yet to be acquired this turn", "doubles")
		g.skip("should treat a pure Flying pokémon as Normal type",
			"no pure Flying-type species is in this 80-species dex, and Trick-or-Treat is not in the dataset")
		g.skip("should not remove Flying type during Terastallization", "Terastallization")
	})

	describe(t, "Roost - DPP", func(g *psg) {
		g.skip("should treat a pure Flying pokémon as `???` type", "gen 4 mechanics")
	})
}
