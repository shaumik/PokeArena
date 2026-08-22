//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/curse.js.
//
// Five cases in the first describe assert on
// `getMoveRequestData().moves[0].target` — the field Showdown's request payload
// uses to tell a client whether Curse is asking for a foe. This engine builds
// no request payload, so the four of them that survive are restated as the
// consequence that field predicts: a Ghost user pays half its max HP and curses
// the target, a non-Ghost user raises Attack and Defense and drops Speed. The
// translation is therefore looser than the original in one direction — it
// cannot tell "the client was asked for a target" from "the move behaved as if
// it had one" — and tighter in another, since it checks the effect as well.
//
// Trick-or-Treat, Soak, Reflect Type and Sleep Talk are not in this dataset.
// The first two are the subject of their cases and report themselves; Sleep
// Talk is upstream's do-nothing and is Splash here.
//
// Substitutions beyond the shared table: Trevenant is a Ghost body carrying
// Trick-or-Treat and becomes Gengar; Jellicent is a Water body carrying Soak
// and becomes Vaporeon, chosen slower than Gengar so the Curse still resolves
// first, as it does upstream against Jellicent's 60 Speed.
//
// The second describe is gen 5/6 doubles and triples throughout, so all five of
// its cases skip.

func TestMovesCurse(t *testing.T) {
	describe(t, "Curse", func(g *psg) {
		g.it("should request the Ghost target if the user is a known Ghost", func(p *ps) {
			p.battle(
				team{{Species: "Gengar", Moves: mv("curse")}},
				team{{Species: "Caterpie", Moves: mv("splash")}},
			)
			p.turn()
			p.equal(p.mine().HP, p.mine().MaxHP-p.mine().MaxHP/2,
				"a Ghost user pays half its max HP for Curse")
			p.equal(p.foe().HP, p.foe().MaxHP-p.foe().MaxHP/4,
				"the cursed target loses a quarter of its max HP each turn")
			p.statStage(p.mine(), "atk", 0, "the Ghost version of Curse changes no stats")
		})

		g.it("should request the Ghost target after the user becomes Ghost", func(p *ps) {
			// Lagging Tail keeps the Trick-or-Treat user moving last, so the
			// first Curse resolves while Rapidash is still pure Fire.
			p.battle(
				team{{Species: "Rapidash", Moves: mv("curse")}},
				team{{Species: "Trevenant", As: "Gengar", Item: "laggingtail", Moves: mv("trickortreat")}},
			)
			p.turn()
			p.statStage(p.mine(), "atk", 1, "a non-Ghost user boosts itself instead of cursing")
			p.fullHP(p.foe(), "the non-Ghost version of Curse does not touch the target")
			p.turn()
			p.logHas("was cursed!", "once Trick-or-Treat has made the user Ghost, Curse should curse the target")
		})

		g.it("should not request a target after the user stops being Ghost", func(p *ps) {
			p.battle(
				team{{Species: "Gengar", Moves: mv("curse")}},
				team{{Species: "Jellicent", As: "Vaporeon", Moves: mv("soak")}},
			)
			p.turn()
			p.logHas("was cursed!", "a Ghost user should curse the target")
			p.turn()
			p.statStage(p.mine(), "atk", 1,
				"once Soak has made the user pure Water, Curse should boost it instead")
		})

		g.it("should not request a target if the user is a known non-Ghost", func(p *ps) {
			p.battle(
				team{{Species: "Blastoise", Moves: mv("curse")}},
				team{{Species: "Caterpie", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.mine(), "atk", 1, "")
			p.statStage(p.mine(), "def", 1, "")
			p.statStage(p.mine(), "spe", -1, "")
			p.fullHP(p.foe(), "a non-Ghost Curse never reaches the foe")
			p.fullHP(p.mine(), "the non-Ghost version costs no HP")
		})

		g.skip("should not request a target if the user is an unknown non-Ghost",
			"Zoroark is not in this 80-species dex and Illusion, which is the whole subject of the case, is not modeled")

		g.it("should curse a non-Ghost user with Protean", func(p *ps) {
			// Greninja goes through its stand-in row (Poliwrath), which does not
			// carry Protean; the port sets the ability explicitly, as upstream
			// does. Protean is what the case measures, so if the engine has no
			// record of it that is the finding.
			p.battle(
				team{{Species: "Greninja", Ability: "protean", Moves: mv("curse", "spite")}},
				team{{Species: "Caterpie", Moves: mv("splash")}},
			)
			maxHP := p.mine().MaxHP
			residual := maxHP / 4
			p.turn()
			p.equal(p.mine().HP, maxHP-maxHP/2-residual, "Greninja should have Cursed itself")
			p.fullHP(p.foe(), "")

			p.makeChoices("move spite", "move splash")
			p.equal(p.mine().HP, maxHP-maxHP/2-residual*2, "Greninja should have taken Curse damage again")
			p.fullHP(p.foe(), "")
		})

		g.it("should curse the target if a Ghost user has Protean", func(p *ps) {
			p.battle(
				team{{Species: "Gengar", Ability: "protean", Moves: mv("curse")}},
				team{{Species: "Caterpie", Moves: mv("splash")}},
			)
			userMax := p.mine().MaxHP
			targetMax := p.foe().MaxHP
			residual := targetMax / 4
			p.turn()
			p.equal(p.mine().HP, userMax-userMax/2, "")
			p.equal(p.foe().HP, targetMax-residual, "")

			p.turn()
			p.equal(p.mine().HP, userMax-userMax/2, "a second Curse on an already-cursed target costs nothing")
			p.equal(p.foe().HP, targetMax-residual*2, "")
		})

		g.skip("should target either random opponent if the target is an ally", "doubles")
		g.skip("[Gen 7] should target the ally if the target is an ally", "gen 7 mechanics")
	})

	describe(t, "XY/ORAS Curse targeting when becoming Ghost the same turn", func(g *psg) {
		g.skip("should target an opponent in Doubles if the user is on left side and becomes Ghost the same turn",
			"doubles")
		g.skip("should target the ally in Doubles if the user is on right side and becomes Ghost the same turn",
			"doubles")
		g.skip("should target an opponent in Triples even if the user is on position 0", "triples")
		g.skip("should target an opponent in Triples even if the user is on position 1", "triples")
		g.skip("should target an opponent in Triples even if the user is on position 2", "triples")
	})
}
