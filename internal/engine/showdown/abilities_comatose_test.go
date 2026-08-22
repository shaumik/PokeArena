//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/comatose.js.
//
// Comatose is not in this engine's ability set, so every case here is a real
// question and none of them are skipped. Komala is not in the dex and has no
// stand-in row, but it is only a Normal-typed body for the ability to sit on;
// Snorlax is the dex's Normal body and every set names the ability
// explicitly, so the body's own ability never matters. Smeargle goes through
// the stand-in table (Chansey) except in the last case, which needs real
// offense — see the note there.
//
// Sleep Talk and Snore are not in this dataset. That is the finding the
// fourth case exists to record, so it keeps them.
//
// The last case is the one whose translation is loose. Upstream reads base
// power straight off a `battle.onEvent('BasePower')` hook; this harness has no
// such hook, so the doubling is measured instead by hitting two identical
// bodies — one carrying Comatose, one bare — with the same move and comparing
// the HP lost. Four bodies are used so each takes exactly one hit and no
// reading is contaminated by an earlier one.

func TestAbilitiesComatose(t *testing.T) {
	describe(t, "Comatose", func(g *psg) {
		g.it("should make the user immune to status conditions", func(p *ps) {
			p.battle(
				team{{Species: "Komala", As: "Snorlax", Ability: "comatose", Moves: mv("shadowclaw")}},
				team{{Species: "Smeargle", Ability: "noguard", Moves: mv("spore", "glare", "willowisp", "toxic")}},
			)
			for _, foeMove := range []string{"move 1", "move 2", "move 3", "move 4"} {
				p.constant(func() any { return p.mine().Status },
					func() { p.makeChoices("move shadowclaw", foeMove) },
					"Comatose should refuse every non-volatile status")
			}
		})

		g.it("should not have its status immunity bypassed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Komala", As: "Snorlax", Ability: "comatose", Moves: mv("shadowclaw")}},
				team{{Species: "Smeargle", Ability: "moldbreaker", Moves: mv("spore", "glare", "willowisp", "toxic")}},
			)
			for _, foeMove := range []string{"move 1", "move 2", "move 3", "move 4"} {
				p.constant(func() any { return p.mine().Status },
					func() { p.makeChoices("move shadowclaw", foeMove) },
					"Mold Breaker should not open a Comatose holder up to status")
			}
		})

		g.it("should cause Rest to fail", func(p *ps) {
			p.battle(
				team{{Species: "Komala", As: "Snorlax", Ability: "comatose", Moves: mv("rest")}},
				team{{Species: "Smeargle", Ability: "technician", Moves: mv("aquajet")}},
			)
			p.hurts(p.mine(), func() { p.makeChoices("move rest", "move aquajet") },
				"Rest should have failed, leaving the Aqua Jet damage standing")
			p.constant(func() any { return p.mine().Status },
				func() { p.makeChoices("move rest", "move aquajet") },
				"a failed Rest should not put the user to sleep")
		})

		g.it("should allow the use of Snore and Sleep Talk as if the user were asleep", func(p *ps) {
			// Snore, Sleep Talk and Normalium Z are all absent from this
			// dataset; the fixture keeps them so the gap is named rather than
			// papered over.
			p.battle(
				team{{Species: "Komala", As: "Snorlax", Item: "normaliumz", Ability: "comatose",
					Moves: mv("snore", "sleeptalk", "brickbreak")}},
				team{{Species: "Smeargle", Moves: mv("endure")}},
			)
			p.hurts(p.foe(), func() { p.makeChoices("move snore", "move endure") },
				"Expected damage from Snore.")
			p.hurts(p.foe(), func() { p.makeChoices("move sleeptalk", "move endure") },
				"Expected damage from Sleep Talk calling Brick Break.")
		})

		g.it("should cause the user to be damaged by Dream Eater as if it were asleep", func(p *ps) {
			p.battle(
				team{{Species: "Komala", As: "Snorlax", Ability: "comatose", Moves: mv("shadowclaw")}},
				team{{Species: "Smeargle", Ability: "technician", Moves: mv("dreameater")}},
			)
			p.hurts(p.mine(), func() { p.makeChoices("move shadowclaw", "move dreameater") },
				"Dream Eater should treat a Comatose target as asleep")
		})

		g.it("should cause Wake-Up Slap and Hex to have doubled base power when used against the user", func(p *ps) {
			// Smeargle is built as Gengar here rather than through its
			// stand-in row: Chansey's offenses are low enough that a doubled
			// base power disappears inside the damage roll, and the whole
			// case is a magnitude comparison.
			p.battle(
				team{
					{Species: "Komala", As: "Snorlax", Ability: "comatose", Item: "ringtarget", Moves: mv("endure")},
					{Species: "Komala", As: "Snorlax", Ability: "noability", Item: "ringtarget", Moves: mv("endure")},
					{Species: "Komala", As: "Snorlax", Ability: "comatose", Item: "ringtarget", Moves: mv("endure")},
					{Species: "Komala", As: "Snorlax", Ability: "noability", Item: "ringtarget", Moves: mv("endure")},
				},
				team{{Species: "Smeargle", As: "Gengar", Ability: "technician", Moves: mv("hex", "wakeupslap")}},
			)

			p.makeChoices("move endure", "move hex")
			hexOnComatose := p.slot(0, 1).MaxHP - p.slot(0, 1).HP
			// Ring Target is what lets a Ghost move touch a Normal body at
			// all; without it both readings would be zero and the comparison
			// below would pass while measuring nothing.
			p.damaged(p.slot(0, 1), "Ring Target should have let Hex through")

			p.makeChoices("switch 2", "move hex")
			hexOnHealthy := p.slot(0, 2).MaxHP - p.slot(0, 2).HP
			p.atLeast(hexOnComatose, hexOnHealthy+hexOnHealthy/2,
				"Hex should hit a Comatose target for roughly double")

			p.makeChoices("switch 3", "move wakeupslap")
			slapOnComatose := p.slot(0, 3).MaxHP - p.slot(0, 3).HP
			p.makeChoices("switch 4", "move wakeupslap")
			slapOnHealthy := p.slot(0, 4).MaxHP - p.slot(0, 4).HP
			p.atLeast(slapOnComatose, slapOnHealthy+slapOnHealthy/2,
				"Wake-Up Slap should hit a Comatose target for roughly double")
		})
	})
}
