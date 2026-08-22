//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/normalize.js.
//
// Normalize is not in this dataset, so none of these are skips — each is a
// real question the report should carry.
//
// Upstream reads the move's type off a Color Change target, which turns into
// whatever type just hit it. Color Change is not modeled here either, and the
// harness has no way to read a type off a Pokémon, so each case reads the
// type off its *effect* instead: a Normal-type move cannot touch a Ghost, and
// the types the exceptions are supposed to keep are read as a
// super-effective hit on a body that resists Normal. Gengar is the Ghost,
// Golem (rock/ground: Normal 0.5x, Fighting 2x, Water 2x) and Blastoise
// (Normal 1x, Electric 2x) are the others.
//
// Delcatty is not in this dex; Persian is a Normal-type body of similar
// frailty with Normalize set explicitly, which is all the user side has to
// be. Latias is replaced by whichever target above makes the type visible.
//
// Grass Knot is in this dataset at power 0 — weight is not modeled — so the
// first case leans on the immunity rather than on the damage: it asserts the
// Ghost is untouched and that the narration says so.
//
// Three exceptions name a move or item this dataset does not have — Hidden
// Power Fighting, Techno Blast with a Douse Drive, Judgment with a Zap
// Plate — and are left naming them so the report says which piece is
// missing. The two that can be built (Weather Ball, Natural Gift) pass
// vacuously while Normalize is absent: there is nothing to override.
//
// The Gen 4 describe block is skipped as a block — this engine models one
// generation.

func TestAbilitiesNormalize(t *testing.T) {
	describe(t, "Normalize", func(g *psg) {
		g.it("should change most of the user's moves to Normal-type", func(p *ps) {
			p.battle(
				team{{Species: "Persian", Ability: "normalize", Moves: mv("grassknot")}},
				team{{Species: "Gengar", Ability: "noability", Moves: mv("endure")}},
			)
			p.makeChoices("move grassknot", "move endure")
			p.logHas("doesn't affect", "a Normalized Grass Knot should not reach a Ghost")
			p.fullHP(p.foe(), "a Normalized Grass Knot should not reach a Ghost")
		})

		g.it("should not change Hidden Power to Normal-type", func(p *ps) {
			p.battle(
				team{{Species: "Persian", Ability: "normalize", Moves: mv("hiddenpowerfighting")}},
				team{{Species: "Golem", Ability: "noability", Moves: mv("endure")}},
			)
			p.makeChoices("move hiddenpowerfighting", "move endure")
			p.logHas("super effective", "Hidden Power should stay Fighting-type")
		})

		g.it("should not change Techno Blast to Normal-type if the user is holding a Drive", func(p *ps) {
			p.battle(
				team{{Species: "Persian", Ability: "normalize", Item: "dousedrive", Moves: mv("technoblast")}},
				team{{Species: "Golem", Ability: "noability", Moves: mv("endure")}},
			)
			p.makeChoices("move technoblast", "move endure")
			p.logHas("super effective", "a Douse Drive should keep Techno Blast Water-type")
		})

		g.it("should not change Judgment to Normal-type if the user is holding a Plate", func(p *ps) {
			p.battle(
				team{{Species: "Persian", Ability: "normalize", Item: "zapplate", Moves: mv("judgment")}},
				team{{Species: "Blastoise", Ability: "noability", Moves: mv("endure")}},
			)
			p.makeChoices("move judgment", "move endure")
			p.logHas("super effective", "a Zap Plate should keep Judgment Electric-type")
		})

		g.it("should not change Weather Ball to Normal-type if sun, rain, or hail is an active weather", func(p *ps) {
			// Lagging Tail is upstream's way of making the ball land after the
			// weather is up; it is kept for the same reason.
			p.battle(
				team{{Species: "Persian", Ability: "normalize", Item: "laggingtail", Moves: mv("weatherball")}},
				team{{Species: "Gengar", Ability: "noability", Moves: mv("sunnyday")}},
			)
			p.makeChoices("move weatherball", "move sunnyday")
			p.equal(p.weather(), "sun", "Sunny Day should be up before the ball lands")
			p.damaged(p.foe(), "Weather Ball should stay Fire-type in sun, which a Ghost does not dodge")
		})

		g.it("should not change Natural Gift to Normal-type if the user is holding a Berry", func(p *ps) {
			p.battle(
				team{{Species: "Persian", Ability: "normalize", Item: "chopleberry", Moves: mv("naturalgift")}},
				team{{Species: "Golem", Ability: "noability", Moves: mv("endure")}},
			)
			p.makeChoices("move naturalgift", "move endure")
			p.logHas("super effective", "a Chople Berry should make Natural Gift Fighting-type")
		})
	})

	describe(t, "Normalize [Gen 4]", func(g *psg) {
		g.skip("should change most of the user's moves to Normal-type", "gen 4 mechanics")
		g.skip("should change Hidden Power to Normal-type", "gen 4 mechanics")
		g.skip("should change Judgment to Normal-type even if the user is holding a Plate", "gen 4 mechanics")
		g.skip("should change Weather Ball to Normal-type even if sun, rain, or hail is an active weather", "gen 4 mechanics")
	})
}
