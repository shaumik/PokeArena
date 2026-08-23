//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/substitute.js.
//
// The five common.gen(1) cases skip as a block: Gen 1 Substitute is a
// different move (recoil onto the target's doll, uncapped damage tracking,
// confusion self-hits routed at the foe's doll), and this engine has no
// gen-mod layer.
//
// Substitutions: Mewtwo, Dragonite and Hitmonlee/Hitmonchan are in this dex
// already. Zangoose is not and has no stand-in row; it is built as Kangaskhan,
// a normal-type physical body used on both sides exactly as upstream uses
// Zangoose on both sides — the case measures a drain against the doll's HP, so
// only the symmetry matters, not the species.
//
// Three cases name something this dataset does not have and therefore report
// that rather than the mechanic: Light of Ruin and Belly Drum are not in the
// move set, and Huge Power is not an ability this engine models. Per the port
// rules those are findings, so the cases are written for real.

func TestMovesSubstitute(t *testing.T) {
	describe(t, "Substitute", func(g *psg) {
		g.it("should deduct 25% of max HP, rounded down", func(p *ps) {
			p.battle(
				team{{Species: "Mewtwo", Ability: "pressure", Moves: mv("substitute")}},
				team{{Species: "Mewtwo", Ability: "pressure", Moves: mv("recover")}},
			)
			p.makeChoices("move substitute", "move recover")
			mon := p.mine()
			p.equal(mon.MaxHP-mon.HP, mon.MaxHP/4, "Substitute should cost a quarter of max HP, rounded down")
		})

		g.it("should not block the user's own moves from targeting itself", func(p *ps) {
			p.battle(
				team{{Species: "Mewtwo", Ability: "pressure", Moves: mv("substitute", "calmmind")}},
				team{{Species: "Mewtwo", Ability: "pressure", Moves: mv("recover")}},
			)
			p.makeChoices("move substitute", "move recover")
			p.makeChoices("move calmmind", "move recover")
			p.statStage(p.mine(), "spa", 1, "Calm Mind should reach its own user through its Substitute")
			p.statStage(p.mine(), "spd", 1, "Calm Mind should reach its own user through its Substitute")
		})

		g.it("should block damage from most moves", func(p *ps) {
			// Lagging Tail is what makes the doll go up before Psystrike lands,
			// as upstream. The holder's only HP loss should still be the setup
			// cost: overflow from a hit that breaks the doll is discarded.
			p.battle(
				team{{Species: "Mewtwo", Ability: "pressure", Moves: mv("substitute")}},
				team{{Species: "Mewtwo", Ability: "pressure", Item: "laggingtail", Moves: mv("psystrike")}},
			)
			p.makeChoices("move substitute", "move psystrike")
			mon := p.mine()
			p.equal(mon.MaxHP-mon.HP, mon.MaxHP/4, "the doll should have absorbed Psystrike entirely")
		})

		g.it("should not block recoil damage", func(p *ps) {
			p.battle(
				team{{Species: "Mewtwo", Ability: "pressure", Moves: mv("substitute", "doubleedge")}},
				team{{Species: "Mewtwo", Ability: "pressure", Moves: mv("nastyplot")}},
			)
			p.makeChoices("move substitute", "move nastyplot")
			p.makeChoices("move doubleedge", "move nastyplot")
			mon := p.mine()
			p.notEqual(mon.MaxHP-mon.HP, mon.MaxHP/4,
				"Double-Edge recoil should hit the user behind its own Substitute")
		})

		g.skip("should take specific recoil damage in Gen 1", "gen 1 mechanics")

		g.it("should cause recoil damage from an opponent's moves to be based on damage dealt to the substitute", func(p *ps) {
			// Light of Ruin is not in this dataset, so the case reports the
			// missing move rather than the recoil rule.
			p.battle(
				team{{Species: "Mewtwo", Ability: "pressure", Moves: mv("substitute")}},
				team{{Species: "Mewtwo", Ability: "noguard", Moves: mv("nastyplot", "lightofruin")}},
			)
			p.makeChoices("move substitute", "move nastyplot")
			p.makeChoices("move substitute", "move lightofruin")
			doll := p.mine().MaxHP / 4
			foe := p.foe()
			p.equal(foe.MaxHP-foe.HP, (doll+1)/2,
				"recoil should be half the damage the doll absorbed, not half the damage the move would have dealt")
		})

		g.it("should cause recovery from an opponent's draining moves to be based on damage dealt to the substitute", func(p *ps) {
			// Belly Drum is not in this dataset; it is upstream's way of making
			// Drain Punch certainly break the doll, so this case reports the
			// missing move rather than the drain rule.
			p.battle(
				team{{Species: "Zangoose", As: "Kangaskhan", Ability: "pressure", Moves: mv("substitute")}},
				team{{Species: "Zangoose", As: "Kangaskhan", Ability: "noguard", Moves: mv("bellydrum", "drainpunch")}},
			)
			p.makeChoices("move substitute", "move bellydrum")
			hp := p.foe().HP
			p.makeChoices("move substitute", "move drainpunch")
			doll := p.mine().MaxHP / 4
			p.equal(p.foe().HP-hp, (doll+1)/2,
				"the drain should be half the damage the doll absorbed, capped by the doll's HP")
		})

		g.it("should block most status moves targeting the user", func(p *ps) {
			// No Guard on the Substitute user is upstream's way of taking the
			// accuracy roll out of Hypnosis and Toxic; Lagging Tail keeps the
			// doll up before each status move resolves.
			p.battle(
				team{{Species: "Mewtwo", Ability: "noguard", Moves: mv("substitute")}},
				team{{
					Species: "Mewtwo", Ability: "pressure", Item: "laggingtail",
					Moves: mv("hypnosis", "toxic", "poisongas", "thunderwave", "willowisp"),
				}},
			)
			p.makeChoices("move substitute", "move 1")
			p.noStatus(p.mine(), "Hypnosis should not reach a Pokemon behind a Substitute")
			p.makeChoices("move substitute", "move 2")
			p.noStatus(p.mine(), "Toxic should not reach a Pokemon behind a Substitute")
			p.makeChoices("move substitute", "move 3")
			p.noStatus(p.mine(), "Poison Gas should not reach a Pokemon behind a Substitute")
			p.makeChoices("move substitute", "move 4")
			p.noStatus(p.mine(), "Thunder Wave should not reach a Pokemon behind a Substitute")
			p.makeChoices("move substitute", "move 5")
			p.noStatus(p.mine(), "Will-O-Wisp should not reach a Pokemon behind a Substitute")
		})

		g.it("should allow multi-hit moves to continue after the substitute fades", func(p *ps) {
			// Huge Power is not modeled here; upstream uses it only to be sure
			// the first Dual Chop hit breaks the doll, which this matchup
			// manages without it. The inert-ability finding is reported anyway.
			p.battle(
				team{{Species: "Dragonite", Ability: "noguard", Item: "focussash", Moves: mv("substitute", "roost")}},
				team{{Species: "Dragonite", Ability: "hugepower", Item: "laggingtail", Moves: mv("roost", "dualchop")}},
			)
			p.makeChoices("move substitute", "move roost")
			p.makeChoices("move roost", "move dualchop")
			p.damaged(p.mine(), "the second Dual Chop hit should land on the holder once the doll is gone")
		})

		g.skip("[Gen 1] should track what the actual damage would have been without the Substitute",
			"gen 1 mechanics")
		g.skip("[Gen 1] Substitute should not block secondary effect confusion if it is unbroken",
			"gen 1 mechanics")
		g.skip("[Gen 1] if a Pokemon with a Substitute hurts itself due to confusion and the target does not have a Substitute, there is no damage dealt.",
			"gen 1 mechanics")
		g.skip("[Gen 1] if a Pokemon with a Substitute hurts itself due to confusion and the target has a Substitute, the target's Substitute takes the damage.",
			"gen 1 mechanics")
	})
}
