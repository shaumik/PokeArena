//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/healblock.js.
//
// Heal Block is not in this dataset, so every ported case reports the missing
// move rather than the healing it is supposed to block. That is the finding the
// file exists to record; the cases are written out in full so they measure the
// right thing the day the move lands.
//
// Belly Drum is missing too, and it is load-bearing in two of them — it is how
// upstream gets the target low enough for a recovery item or Water Absorb to
// want to fire.
//
// The Gen 5 and Gen 4 describes skip as blocks: this engine models one
// generation, and both blocks exist precisely to pin the older rules (Gen 5
// blocks the drain instead of the move, Gen 4 leaves items and abilities
// alone). The last case in the Gen 4 block is a doubles case that upstream
// left there by accident — it builds a current-generation battle — so it skips
// as doubles rather than as gen 4.
//
// Substitutions beyond the shared table: Hippowdon becomes Sandslash, a Ground
// body that is likewise unhurt by the sandstorm it puts up when the port sets
// Sand Stream; Pansage becomes Snorlax, which carries Gluttony itself; Cresselia
// becomes Slowbro, a bulky body slower than the Heal Block user; Quagsire
// becomes Vaporeon, which carries Water Absorb itself. None of the four is
// picked for its typing, and no case here turns on one.

func TestMovesHealBlock(t *testing.T) {
	describe(t, "Heal Block", func(g *psg) {
		g.it("should prevent Pokemon from gaining HP from residual recovery items", func(p *ps) {
			p.battle(
				team{{Species: "Hippowdon", As: "Sandslash", Ability: "sandstream", Moves: mv("healblock")}},
				team{{Species: "Spiritomb", Ability: "pressure", Item: "leftovers", Moves: mv("calmmind")}},
			)
			p.makeChoices("move healblock", "move calmmind")
			p.damaged(p.foe(), "Leftovers should not have undone the sandstorm chip")
		})

		g.it("should prevent Pokemon from consuming HP recovery items", func(p *ps) {
			p.battle(
				team{{Species: "Sableye", Ability: "prankster", Moves: mv("healblock")}},
				team{{Species: "Pansage", As: "Snorlax", Ability: "gluttony", Item: "berryjuice",
					Moves: mv("bellydrum")}},
			)
			p.makeChoices("move healblock", "move bellydrum")
			p.equal(p.foe().Item, "berryjuice", "the blocked Berry Juice should not have been eaten")
			max := p.foe().MaxHP
			p.equal(p.foe().HP, max-max/2, "the holder should be left on Belly Drum's half")
		})

		g.it("should disable the use of healing moves", func(p *ps) {
			p.battle(
				team{{Species: "Spiritomb", Ability: "pressure", Moves: mv("healblock")}},
				team{{Species: "Cresselia", As: "Slowbro", Ability: "levitate", Moves: mv("recover")}},
			)
			p.makeChoices("move healblock", "move recover")
			p.cantMove(1, "recover", "Recover should not be choosable under Heal Block")
		})

		g.it("should prevent Pokemon from using draining moves", func(p *ps) {
			// From Gen 6 on, Heal Block stops a drain move being used at all, so
			// the Heal Block user takes no damage — that untouched HP bar is the
			// whole assertion.
			p.battle(
				team{{Species: "Sableye", Ability: "prankster", Moves: mv("healblock")}},
				team{{Species: "Venusaur", Ability: "overgrow", Moves: mv("gigadrain")}},
			)
			p.makeChoices("move healblock", "move gigadrain")
			p.fullHP(p.mine(), "Giga Drain should not have been usable at all")
		})

		g.it("should prevent abilities from recovering HP", func(p *ps) {
			p.battle(
				team{{Species: "Sableye", Ability: "prankster", Moves: mv("healblock", "surf")}},
				team{{Species: "Quagsire", As: "Vaporeon", Ability: "waterabsorb",
					Moves: mv("bellydrum", "calmmind")}},
			)
			p.makeChoices("move healblock", "move bellydrum")
			hp := p.foe().HP
			p.makeChoices("move surf", "move calmmind")
			p.equal(p.foe().HP, hp, "Water Absorb should have healed nothing under Heal Block")
		})

		g.it("should prevent Leech Seed from healing HP", func(p *ps) {
			p.battle(
				team{{Species: "Starmie", Ability: "noguard", Moves: mv("healblock")}},
				team{{Species: "Venusaur", Ability: "overgrow", Moves: mv("substitute", "leechseed")}},
			)
			p.makeChoices("move healblock", "move substitute")
			hp := p.foe().HP
			p.makeChoices("move healblock", "move leechseed")
			p.equal(p.foe().HP, hp, "the seeder should gain nothing under Heal Block")
			p.damaged(p.mine(), "Leech Seed should still sap the seeded Pokemon")
		})

		g.skip("should not prevent the target from using Z-Powered healing status moves or healing from Z Power",
			"Z-moves")
	})

	describe(t, "Heal Block [Gen 5]", func(g *psg) {
		g.skip("should prevent Pokemon from gaining HP from residual recovery items", "gen 5 mechanics")
		g.skip("should prevent Pokemon from consuming HP recovery items", "gen 5 mechanics")
		g.skip("should disable the use of healing moves", "gen 5 mechanics")
		g.skip("should prevent abilities from recovering HP", "gen 5 mechanics")
		g.skip("should prevent draining moves from healing HP", "gen 5 mechanics")
		g.skip("should prevent Leech Seed from healing HP", "gen 5 mechanics")
	})

	describe(t, "Heal Block [Gen 4]", func(g *psg) {
		g.skip("should disable the use of healing moves", "gen 4 mechanics")
		g.skip("should block the effect of Wish", "gen 4 mechanics")
		g.skip("should prevent draining moves from healing HP", "gen 4 mechanics")
		g.skip("should allow HP recovery items to activate", "gen 4 mechanics")
		g.skip("should allow abilities that recover HP to activate", "gen 4 mechanics")
		g.skip("should prevent Leech Seed from healing HP", "gen 4 mechanics")
		g.skip("should fail independently on each target", "doubles")
	})
}
