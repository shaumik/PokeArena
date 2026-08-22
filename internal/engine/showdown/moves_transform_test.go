//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/transform.js.
//
// Nothing in this file can pass: Transform has no entry in this dataset, and
// this engine has no forme layer for a copied species to live in. What the
// port controls is which of those two facts each case reports.
//
// Eleven of the fourteen cases in the base block lead with a Ditto, the
// species whose identity is the mechanic and which this port has no stand-in
// for by design — test/sim/abilities/imposter.js is skipped the same way, and
// for the same reason. Those eleven skip. The three that use Mew as the
// transformer are written out live, so the file records the missing move and
// not only the missing species.
//
// The four generation blocks skip whole. `Transform [Gen 9]` is nominally
// this engine's generation, but every case in it turns on Terastallization.
//
// Substitutions in the live cases:
//
//   - Scolipede is built as Venomoth, the same Bug/Poison. It is only a body
//     that boosts three stats for Transform to copy.
//   - Talonflame is built as Charizard, the other Fire/Flying in this dex —
//     that typing is what the Roost case reads back off the transformer.
//     Upstream's Talonflame outruns Mew on base Speed while Charizard ties
//     it, and a speed tie would decide by seed whether Roost had resolved
//     before Transform, so the Charizard here carries Speed EVs. The case is
//     only asking a question while Roost has already gone off.
//   - Roggenrola is built as Golem, a Rock body carrying Sturdy.
//
// Upstream's `sleeptalk` filler is not in this dataset; `splash` is this
// engine's do-nothing.

func TestMovesTransform(t *testing.T) {
	describe(t, "Transform", func(g *psg) {
		g.skip("should copy the Pokemon's species",
			"Ditto is not in this 80-species dex and Transform is not modeled")

		g.skip("should copy all stats except HP",
			"Ditto is not in this 80-species dex and Transform is not modeled")

		g.it("should copy all stat changes", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Ability: "synchronize", Item: "laggingtail",
					Moves: mv("calmmind", "agility", "transform")}},
				team{{Species: "Scolipede", As: "Venomoth", Ability: "swarm",
					Moves: mv("honeclaws", "irondefense", "doubleteam")}},
			)
			p.makeChoices("move 1", "move 1")
			p.makeChoices("move 2", "move 2")
			p.makeChoices("move 3", "move 3")
			me, foe := p.mine(), p.foe()
			for _, stat := range []string{"atk", "def", "spa", "spd", "spe", "accuracy", "evasion"} {
				p.equal(p.stage(me, stat), p.stage(foe, stat),
					"Transform should have copied the "+stat+" stage")
			}
		})

		g.skip("should copy the target's focus energy status",
			"Ditto is not in this 80-species dex and Transform is not modeled")

		g.skip("should copy the target's moves with 5 PP each",
			"Ditto is not in this 80-species dex and Transform is not modeled")

		g.skip("should copy and activate the target's Ability",
			"Ditto is not in this 80-species dex and Transform is not modeled")

		g.skip("should copy, but not activate the target's Ability if it is the same as the user's pre-Transform",
			"Ditto is not in this 80-species dex and Transform is not modeled")

		g.skip("should not copy speed boosts from Unburden",
			"Ditto is not in this 80-species dex and Transform is not modeled")

		g.skip("should fail against Pokemon with a Substitute",
			"Ditto is not in this 80-species dex and Transform is not modeled")

		g.skip("should fail if either the user or target have Illusion active",
			"Ditto and Zoroark are not in this 80-species dex, and neither Transform nor Illusion is modeled")

		g.skip("should fail against transformed Pokemon",
			"Ditto is not in this 80-species dex and Transform is not modeled")

		g.skip("should copy the target's real type, even if the target is an Arceus",
			"Ditto and Arceus are not in this 80-species dex, and Transform, Multitype and plate formes are not modeled")

		g.it("should ignore the effects of Roost", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Ability: "synchronize", Moves: mv("seismictoss", "transform")}},
				team{{Species: "Talonflame", As: "Charizard", Ability: "flamebody",
					EVs: evs(map[string]int{"spe": 252}), Moves: mv("roost")}},
			)
			p.makeChoices("move seismictoss", "move roost")
			p.makeChoices("move transform", "move roost")
			me := p.mine()
			p.equal(string(me.Type1)+"/"+string(me.Type2), "fire/flying",
				"Transform should copy the target's real typing, not the one Roost suppressed for the turn")
		})

		g.it("should not announce that its ability was suppressed after Transforming", func(p *ps) {
			// Upstream reads the protocol for a `|-endability|`. This engine's
			// equivalent line is Gastro Acid's "ability was suppressed!", so
			// the port asserts that no such line is emitted.
			p.battle(
				team{{Species: "Mew", Ability: "synchronize", Moves: mv("transform")}},
				team{{Species: "roggenrola", As: "Golem", Ability: "sturdy", Moves: mv("splash")}},
			)
			p.turn()
			p.logLacks("ability was suppressed",
				"Transforming should not report the user's own ability as suppressed")
		})
	})

	describe(t, "Transform [Gen 9]", func(g *psg) {
		g.skip("should copy the target's old types, not the Tera Type",
			"Terastallization is not modeled and Ditto is not in this 80-species dex")

		g.skip("should keep the user's Tera Type when Terastallized",
			"Terastallization is not modeled and Ditto is not in this 80-species dex")

		g.skip("should fail against Ogerpon when the user is Terastallized",
			"Terastallization is not modeled and neither Ditto nor Ogerpon is in this 80-species dex")

		g.skip("should fail against Ogerpon when Ogerpon is Terastallized",
			"Terastallization is not modeled and neither Ditto nor Ogerpon is in this 80-species dex")

		g.skip("should prevent Pokemon transformed into Ogerpon from Terastallizing",
			"Terastallization is not modeled and neither Ditto nor Ogerpon is in this 80-species dex")

		g.skip("should not allow Pokemon transformed into Ogerpon to Terastallize later if they couldn't before transforming",
			"Terastallization is not modeled and neither Ditto nor Ogerpon is in this 80-species dex")

		g.skip("should not work if the user is Tera Stellar",
			"Terastallization is not modeled and Ditto is not in this 80-species dex")
	})

	describe(t, "Transform [Gen 5]", func(g *psg) {
		g.skip("should not copy the target's focus energy status", "gen 5 mechanics")
	})

	describe(t, "Transform [Gen 4]", func(g *psg) {
		g.skip("should revert Pokemon transformed into Giratina-Origin to Giratina-Alternate if not holding a Griseous Orb",
			"gen 4 mechanics")

		g.skip("should cause Pokemon transformed into Giratina-Alternate to become Giratina-Origin if holding a Griseous Orb",
			"gen 4 mechanics")

		g.skip("should cause Pokemon transformed into Arceus to become an Arceus forme corresponding to its held Plate",
			"gen 4 mechanics")

		g.skip("should succeed against a Substitute", "gen 4 mechanics")
	})

	describe(t, "Transform [Gen 1]", func(g *psg) {
		g.skip("should not send |-endability|", "gen 1 mechanics")

		g.skip("should copy the target's boosted stats", "gen 1 mechanics")

		g.skip("should copy the target's stats (except HP), even if different level", "gen 1 mechanics")

		g.skip("should copy the target's status-modified stats", "gen 1 mechanics")

		g.skip("should not re-apply status stat modifier after transforming", "gen 1 mechanics")

		g.skip("calls Metronome or Mirror Move, PP from the original base move slot is incremented",
			"gen 1 mechanics")

		g.skip("calls Metronome or Mirror Move, PP from the original base move slot is incremented (same with two-turn moves)",
			"gen 1 mechanics")
	})
}
