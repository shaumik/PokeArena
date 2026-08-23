//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/skydrop.js.
//
// Sky Drop is not in this dataset, so all six ported cases report the missing
// move. They are written out anyway: Sky Drop's rules are almost entirely about
// what the held target may and may not do, and that is worth stating even while
// the move is absent.
//
// Eight of the nineteen cases in the main describe need a second active slot
// and skip as doubles. Three more skip for a mechanic this engine does not
// model at all — Mega Evolution, Stance Change's forme swap, and Wonder Guard
// on Shedinja, whose 1 HP is the case's whole subject. The twelfth,
// "should only make contact on the way down", skips because both of its
// contact punishers are out of reach: Aegislash has no dex entry or stand-in,
// King's Shield is not in the dataset, and names_test.go's Ferrothorn row says
// in as many words that Iron Barbs ports must skip.
//
// Substitutions beyond the shared table. Lairon becomes Rhydon: rock is
// preserved, steel is not, and neither case using it asserts a damage figure —
// what matters is that Rhydon weighs the same 120 kg as Lairon, so it stays on
// the light side of Sky Drop's 200 kg limit. Aggron is the reverse: the shared
// row sends it to Magneton, which is fine as the bench body in the trapping
// case but destroys the point of the weight case, so that one names Snorlax
// (460 kg) instead. Shedinja in the final case is a body that faints to its own
// Sticky Barb, which HP: 1 on an in-dex body reproduces exactly.
//
// Weight is not modeled here at all, so the 200 kg case is expected to report
// that rather than a Sky Drop rule. Sleep Talk, upstream's do-nothing, is
// Splash.
//
// The Gen 5 block skips as a generation, and the Sky Drop Glitch block inside
// it is disabled upstream as well (describe.skip) — it is gen 5 doubles
// reproducing a cartridge bug.

func TestMovesSkyDrop(t *testing.T) {
	describe(t, "Sky Drop", func(g *psg) {
		g.it("should prevent its target from moving when it is caught by the effect", func(p *ps) {
			p.battle(
				team{{Species: "Aerodactyl", Moves: mv("skydrop")}},
				team{{Species: "Lairon", As: "Rhydon", Moves: mv("tackle")}},
			)
			p.turn()
			p.fullHP(p.mine(), "a Pokemon held by Sky Drop should not get to attack")
			p.turn()
			p.damaged(p.mine(), "once dropped it should be free to attack again")
		})

		g.it("should prevent its target from switching out when it is caught by the effect", func(p *ps) {
			p.battle(
				team{{Species: "Aerodactyl", Moves: mv("skydrop")}},
				team{
					{Species: "Lairon", As: "Rhydon", Moves: mv("tackle")},
					{Species: "Aggron", Moves: mv("tackle")},
				},
			)
			p.turn()
			p.trapped(1, "a Pokemon held by Sky Drop should not be able to switch")
		})

		g.skip("should prevent both the user and the target from being forced out when caught by the effect",
			"doubles")
		g.skip("should prevent both the user and the target from being forced out by Eject Button", "doubles")
		g.skip("should prevent its target from using Mega Evolution when it is caught by the effect",
			"mega evolution")
		g.skip("should prevent its target from activating Stance Change when it is caught by the effect",
			"formes")
		g.skip("should free its target and allow it to move if the user faints", "doubles")

		g.it("should pick up Flying-type Pokemon but do no damage", func(p *ps) {
			// Salamence goes through its stand-in row (Dragonite), which keeps
			// the dragon/flying typing this case turns on.
			p.battle(
				team{{Species: "Aerodactyl", Moves: mv("skydrop")}},
				team{{Species: "Salamence", Moves: mv("tackle")}},
			)
			p.turn()
			p.fullHP(p.mine(), "a Flying-type is still picked up, so it cannot attack on the way up")
			p.makeChoices("move skydrop", "move tackle")
			p.fullHP(p.foe(), "a Flying-type takes no damage from the drop")
		})

		g.skip("should pick up non-Flying weak Wonder Guard Pokemon but do no damage",
			"Shedinja is not in this 80-species dex and Wonder Guard is not modeled")
		g.skip("should only make contact on the way down",
			"Aegislash is not in this 80-species dex, King's Shield is not in this dataset, and the Ferrothorn stand-in row says Iron Barbs ports must skip")

		g.it("should fail if the target has a Substitute", func(p *ps) {
			p.battle(
				team{{Species: "Aerodactyl", Moves: mv("splash", "skydrop")}},
				team{{Species: "Lairon", As: "Rhydon", Moves: mv("substitute", "tackle")}},
			)
			p.turn()
			p.makeChoices("move skydrop", "move tackle")
			p.damaged(p.mine(), "Sky Drop should have failed against the Substitute, leaving the Tackle free to land")
		})

		g.it("should fail if the target is heavier than 200kg", func(p *ps) {
			// Weight is not modeled, so this is expected to report that rather
			// than a Sky Drop rule; Snorlax is named because it is the heaviest
			// body in this dex and so states the case honestly either way.
			p.battle(
				team{{Species: "Aerodactyl", Moves: mv("skydrop")}},
				team{{Species: "Aggron", As: "Snorlax", Moves: mv("tackle")}},
			)
			p.turn()
			p.damaged(p.mine(), "Sky Drop should have failed on a target over 200 kg, leaving it free to attack")
		})

		g.skip("should fail if used against an ally", "doubles")
		g.skip("should hit its picked-up target even if its position changed with Ally Switch", "doubles")
		g.skip("should hit its target even if Follow Me would have otherwise redirected it", "doubles")
		g.skip("should cause most moves aimed at the user or target to miss", "doubles")
		g.skip("should be canceled by Gravity and allow the target to use its move", "doubles")

		g.it("should not suppress Speed Boost", func(p *ps) {
			// This case reports Speed Boost as unknown, and that part of the
			// report is not a real gap: abilities.go registers "speed-boost",
			// but the harness recovers a kebab slug by searching the abilities
			// in-dex species carry, and no species in these 80 has it. Triage
			// the Sky Drop half only.
			p.battle(
				team{{Species: "Aerodactyl", Moves: mv("skydrop")}},
				team{{Species: "Mew", Ability: "speedboost", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.foe(), "spe", 1, "being held by Sky Drop should not stop Speed Boost")
		})

		g.it("should not claim to have dropped a Pokemon if it is already fainted", func(p *ps) {
			// Upstream reads this off the protocol line Sky Drop emits when it
			// frees a Pokemon. This engine emits no Sky Drop line at all, so the
			// port asserts the other half of the sentence — that the move
			// announces a plain failure — and that the replacement is untouched.
			p.battle(
				team{
					{Species: "Shedinja", As: "Chansey", Item: "stickybarb", HP: 1, Moves: mv("splash")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Aerodactyl", Moves: mv("skydrop")}},
			)
			p.turn()
			p.makeChoices("switch 2", "")
			p.turn()
			p.logHas("But it failed!", "Sky Drop with nothing left to drop should simply fail")
			p.fullHP(p.mine(), "the replacement was never picked up and should be untouched")
		})
	})

	describe(t, "Sky Drop [Gen 5]", func(g *psg) {
		g.skip("should not fail even if the target is heavier than 200kg", "gen 5 mechanics")
	})

	describe(t, "Sky Drop Glitch", func(g *psg) {
		g.skip("should prevent the target from moving or switching", "gen 5 mechanics")
		g.skip("should prevent the user from being forced out", "gen 5 mechanics")
		g.skip("should end when the user switches out", "gen 5 mechanics")
		g.skip("should end when the user faints", "gen 5 mechanics")
		g.skip("should end when the user completes another two-turn move", "gen 5 mechanics")
	})
}
