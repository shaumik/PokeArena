//go:build showdown

package showdown

import (
	"strings"
	"testing"
)

// Ported from test/sim/moves/thousandarrows.js.
//
// Thousand Arrows is not in this dataset, so every case here reports that
// gap before it gets to its own subject. The cases are still written out in
// full: the move is the whole file, and a skip would record "no Zygarde"
// when the finding is "no Thousand Arrows".
//
// Substitutions, since none of the upstream cast is in this dex:
//
//   - Zygarde is built as Sandslash — a Ground body is all the attacker has
//     to be. Upstream keeps it at level 1 so its victims survive several
//     hits; this engine is fixed at level 50, so the targets take real
//     damage. Nothing here measures a damage figure, but a case that ran
//     long enough to KO would end early.
//   - Tropius is built as Pidgeot, a plain Flying body. Grass is lost and
//     the case does not use it.
//   - Ho-Oh takes its stand-in row (Moltres), which keeps Fire/Flying.
//   - Cresselia and Eelektross are both built as Weezing, the only Levitate
//     species in this dex. Ground is 2x on Poison exactly as it is on
//     Electric, so the Weakness Policy case still measures a super-effective
//     hit; Cresselia's Psychic typing is not preserved and is not used.
//   - Donphan is built as Marowak (Ground, and bulky enough to survive the
//     hit the case reads an item off).
//   - Regieleki is built as Jolteon, a pure Electric body.
//   - Stunfisk is built as Dugtrio: the case turns on the Ground type's
//     immunity to an Electrified move, and the Electric half is not used.
//   - Dusknoir is built as Gengar, the only Ghost in this dex.
//
// Wonder Guard, Normalize and Electrify are all absent from this engine and
// report themselves in the cases that name them.

func TestMovesThousandArrows(t *testing.T) {
	describe(t, "Thousand Arrows", func(g *psg) {
		g.it("should hit Flying-type Pokemon and remove their Ground immunity", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Moves: mv("thousandarrows", "earthquake")}},
				team{{Species: "Tropius", As: "Pidgeot", Moves: mv("synthesis")}},
			)
			p.turn()
			p.damaged(p.foe(), "Thousand Arrows should reach a Flying type")
			p.makeChoices("move earthquake", "")
			p.damaged(p.foe(), "a grounded Flying type should then be hit by Earthquake")
		})

		g.it("should ignore type effectiveness on the first hit against Flying-type Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Moves: mv("thousandarrows")}},
				team{{Species: "Ho-Oh", Item: "weaknesspolicy", Moves: mv("recover")}},
			)
			p.turn()
			p.statStage(p.foe(), "atk", 0, "the grounding hit is neutral, so the Weakness Policy should not fire")
			p.statStage(p.foe(), "spa", 0, "")
			p.turn()
			p.statStage(p.foe(), "atk", 2, "the second hit lands on a grounded target and is super effective")
			p.statStage(p.foe(), "spa", 2, "")
		})

		g.it("should not ignore type effectiveness on the first hit against Flying-type Pokemon with Ring Target", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Moves: mv("thousandarrows")}},
				team{{Species: "Ho-Oh", Ability: "wonderguard", Item: "ringtarget", Moves: mv("recover")}},
			)
			p.turn()
			p.damaged(p.foe(), "Ring Target should let the hit through Wonder Guard as a super-effective one")
		})

		g.it("should not ground or deal neutral damage to Flying-type Pokemon holding an Iron Ball", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Moves: mv("thousandarrows", "earthquake")}},
				team{{Species: "Ho-Oh", Item: "ironball", Moves: mv("recover", "trick")}},
			)
			p.turn()
			// Upstream reads the protocol line right after the move; the
			// closest honest equivalent is the narration of the turn just
			// resolved, since a whole-log check would still see this hit on
			// every later turn.
			p.isFalse(strings.Contains(p.lastTurnText(), "It's super effective!"),
				"an Iron Ball holder is already grounded, so this hit should not be super effective")
			hp := p.foe().HP
			p.damaged(p.foe(), "")

			p.makeChoices("move earthquake", "move trick")
			p.equal(p.foe().HP, hp, "having traded the Iron Ball away, the Flying type should be out of Earthquake's reach")
		})

		g.it("should not ground or deal neutral damage to Flying-type Pokemon affected by Gravity", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Moves: mv("thousandarrows", "sleeptalk")}},
				team{{Species: "Ho-Oh", Moves: mv("recover", "gravity")}},
			)
			p.makeChoices("move sleeptalk", "move gravity")
			for i := 0; i < 5; i++ {
				if p.state() == nil || p.state().PseudoWeather.Gravity == nil {
					break
				}
				p.turn()
				p.ok(strings.Contains(p.lastTurnText(), "It's super effective!"),
					"Gravity already grounds the target, so Thousand Arrows should be super effective")
			}
			p.turn()
			p.isFalse(strings.Contains(p.lastTurnText(), "It's super effective!"),
				"the first Thousand Arrows after Gravity ends spends itself on the grounding")
			p.turn()
			p.ok(strings.Contains(p.lastTurnText(), "It's super effective!"),
				"the second Thousand Arrows after Gravity ends is super effective")
		})

		g.it("should hit Pokemon with Levitate and remove their Ground immunity", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Moves: mv("thousandarrows", "earthquake")}},
				team{{Species: "Cresselia", As: "Weezing", Moves: mv("recover")}},
			)
			p.turn()
			p.damaged(p.foe(), "Thousand Arrows should reach a Levitate holder")
			p.makeChoices("move earthquake", "")
			p.damaged(p.foe(), "a grounded Levitate holder should then be hit by Earthquake")
		})

		g.it("should hit non-Flying-type Pokemon with Levitate with standard type effectiveness", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Moves: mv("thousandarrows")}},
				team{{Species: "Eelektross", As: "Weezing", Ability: "levitate", Item: "weaknesspolicy", Moves: mv("sleeptalk")}},
			)
			p.turn()
			p.statStage(p.foe(), "atk", 2, "the effectiveness cap applies only to Flying types, so this hit is super effective")
			p.statStage(p.foe(), "spa", 2, "")
		})

		g.it("should hit Pokemon with Air Balloon", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Moves: mv("thousandarrows")}},
				team{{Species: "Donphan", As: "Marowak", Item: "airballoon", Moves: mv("sleeptalk")}},
			)
			p.turn()
			p.damaged(p.foe(), "an Air Balloon should not keep Thousand Arrows out")
			p.noItem(p.foe(), "the balloon should have popped")
		})

		g.it("should hit Electric-type Wonder Guard Pokemon holding an Air Balloon", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Moves: mv("thousandarrows")}},
				team{{Species: "Regieleki", As: "Jolteon", Ability: "wonderguard", Item: "airballoon", Moves: mv("sleeptalk")}},
			)
			p.turn()
			p.damaged(p.foe(), "the hit is super effective through the balloon, so Wonder Guard should not block it")
		})

		g.it("should not hit Ground-type Pokemon when affected by Electrify", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Moves: mv("thousandarrows")}},
				team{{Species: "Stunfisk", As: "Dugtrio", Moves: mv("electrify")}},
			)
			p.turn()
			p.fullHP(p.foe(), "an Electrified Thousand Arrows is an Electric move and a Ground type is immune")
		})

		g.it("should not hit Ghost-type Pokemon when affected by Normalize", func(p *ps) {
			p.battle(
				team{{Species: "Zygarde", As: "Sandslash", Ability: "normalize", Moves: mv("thousandarrows")}},
				team{{Species: "Dusknoir", As: "Gengar", Moves: mv("sleeptalk")}},
			)
			p.turn()
			p.fullHP(p.foe(), "a Normalized Thousand Arrows is a Normal move and a Ghost type is immune")
		})
	})
}
