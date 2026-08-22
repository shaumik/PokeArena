//go:build showdown

package showdown

import (
	"strings"
	"testing"
)

// Ported from test/sim/abilities/neutralizinggas.js.
//
// Neutralizing Gas itself is in this engine (Weezing carries it and the engine
// announces it), so the singles cases are real translations rather than
// placeholders. What varies is whether the *other* ability in each pair exists
// here; where it does not, the case is still ported and the comment says which
// direction that pushes the result.
//
// Substitutions worth knowing about:
//
//   - Lead switch-in abilities run on turn 1 in this engine rather than at
//     battle construction, so cases that upstream asserts immediately after
//     createBattle play one turn first.
//   - Toxic is 90% accurate and there is no rigged RNG here, so every case that
//     only needs the target to be badly poisoned sets Status: "tox" on the
//     fixture instead of playing the move out. The Pokemon upstream used for it
//     stays in the fixture as an inert body.
//   - Belly Drum is not in this dataset. In the Unburden and Gluttony cases it
//     is only the fixture's way of halving the holder's HP so a berry fires, so
//     the port starts the holder one point above half and lets a weak attack
//     cross the line. Sleep Talk is likewise absent and Splash stands in for it
//     wherever it is inert filler.
//   - Effective Speed is not readable from a port, so the two Unburden cases
//     read the doubling the only way that is left: off turn order, with a foe
//     whose Speed sits between the holder's normal and doubled figures.
//     Snorlax is the holder for that reason — it is the slowest in-dex body, so
//     both foes bracket it.
//
// Not ported: everything that needs a second active slot, a forme, primal
// reversion, Transform, or Terastallization.

func TestAbilitiesNeutralizingGas(t *testing.T) {
	describe(t, "Neutralizing Gas", func(g *psg) {
		// movedFirst reads turn order off the narration, which is the only
		// window this harness has onto effective Speed.
		movedFirst := func(p *ps) bool {
			txt := p.lastTurnText()
			mine := strings.Index(txt, p.mine().Name+" used")
			foe := strings.Index(txt, p.foe().Name+" used")
			return mine >= 0 && foe >= 0 && mine < foe
		}

		g.it("should prevent switch-in abilities from activating", func(p *ps) {
			p.battle(
				team{{Species: "Gyarados", Ability: "intimidate", Moves: mv("splash")}},
				team{{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("toxicspikes")}},
			)
			// Upstream reads the stage straight off the fresh battle; here the
			// leads' switch-in hooks run on the first turn, so one turn passes.
			p.turn()
			p.statStage(p.foe(), "atk", 0, "Neutralizing Gas should have stopped Intimidate on the lead")
			p.logLacks("Intimidate cuts", "Intimidate should not have announced itself")
		})

		g.it("should ignore damage-reducing abilities", func(p *ps) {
			// Upstream asserts on p1's Substitute, which is Weezing — the
			// attacker, which never has one. The port reads the Substitute on
			// the Filter holder, which is what the fixture is built to show:
			// unreduced Sludge Bomb leaves Mr. Mime under the quarter of max HP
			// that Substitute costs, so the move fails.
			p.battle(
				team{{Species: "Weezing", Ability: "neutralizinggas", Item: "expertbelt", Moves: mv("sludgebomb")}},
				team{{Species: "Mr. Mime", Ability: "filter", Item: "laggingtail", Moves: mv("substitute")}},
			)
			p.makeChoices("move sludgebomb", "move substitute")
			p.isFalse(p.foe().Volatiles.Substitute != nil,
				"Filter should have been off, leaving Mr. Mime too low to put up a Substitute")
		})

		g.it("should negate self-healing abilities", func(p *ps) {
			// Poison Heal is not in this engine's ability set, so the case
			// cannot tell a negated Poison Heal from an absent one — it passes
			// either way. Kept as a real case so it starts meaning something if
			// the ability is ever added. Machamp is a poisonable Fighting body
			// for Breloom, which is Grass/Fighting.
			p.battle(
				team{{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("splash")}},
				team{{Species: "Breloom", As: "Machamp", Ability: "poisonheal", Moves: mv("swordsdance"), Status: "tox"}},
			)
			p.makeChoices("move splash", "move swordsdance")
			p.isFalse(p.foe().HP == p.foe().MaxHP, "the badly poisoned Pokemon should have lost HP, not gained it")
		})

		g.it("should negate abilities that suppress item effects", func(p *ps) {
			// Alakazam carries Magic Guard natively, so the Reuniclus stand-in
			// keeps the one thing the case turns on.
			p.battle(
				team{{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("reflect")}},
				team{{Species: "Reuniclus", As: "Alakazam", Ability: "magicguard", Item: "lifeorb", Moves: mv("shadowball")}},
			)
			p.makeChoices("move reflect", "move shadowball")
			p.isFalse(p.foe().HP == p.foe().MaxHP, "Magic Guard should have been off, so Life Orb recoil applies")
		})

		g.it("should negate abilities that modify boosts", func(p *ps) {
			// Sleep Talk is not in this dataset and cannot be substituted here:
			// the fixture's target is asleep, so Sleep Talk is the only way it
			// gets to use Superpower at all. The missing move is the finding.
			p.battle(
				team{{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("spore")}},
				team{{Species: "Shuckle", Ability: "contrary", Moves: mv("sleeptalk", "superpower")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move spore", "move sleeptalk")
			p.statStage(p.foe(), "atk", -1, "Contrary should have been off, so Superpower lowers Attack")
		})

		g.it("should negate abilities that activate on switch-out", func(p *ps) {
			// Chansey rather than Starmie for Corsola: both carry Natural Cure,
			// but the case needs the Natural Cure holder to be slower than
			// Weezing so the poison lands before the U-turn, and Starmie is not.
			// Upstream decorates the benched Magikarp with Rattled, which this
			// engine does not model; nothing here reads it, so it is left off
			// rather than turned into a finding that would hide this one.
			p.battle(
				team{
					{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("splash")},
					{Species: "Type: Null", As: "Snorlax", Ability: "battlearmor", Moves: mv("facade")},
				},
				team{
					{Species: "Corsola", As: "Chansey", Ability: "naturalcure", Moves: mv("uturn"), Status: "tox"},
					{Species: "Magikarp", Moves: mv("splash")},
				},
			)
			p.makeChoices("move splash", "move uturn")
			p.makeChoices("switch 2", "switch 1")
			p.hasStatus(p.foe(), "tox", "Natural Cure should have been off when it switched out")
		})

		g.it("should negate abilities that modify move type", func(p *ps) {
			p.battle(
				team{{Species: "Gengar", Ability: "neutralizinggas", Moves: mv("laserfocus")}},
				team{{Species: "Sylveon", Ability: "pixilate", Moves: mv("hypervoice")}},
			)
			p.makeChoices("move laserfocus", "move hypervoice")
			// Pixilate is not modeled, so this passes whether or not the
			// suppression works; what it pins is that Hyper Voice stays Normal
			// and so cannot touch a Ghost.
			p.fullHP(p.mine(), "Hyper Voice should have stayed Normal and missed the Ghost entirely")
		})

		g.it("should negate abilities that damage the attacker", func(p *ps) {
			// Iron Barbs is not in this engine's ability set, so the harness
			// reports it and the reading below can only pass — a negated Iron
			// Barbs and an absent one look identical from here. Kept as a real
			// case because the missing ability is the finding. Weezing-Galar is
			// built as plain Weezing; the Galar forme's Fairy half plays no part.
			p.battle(
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "neutralizinggas", Moves: mv("payback")}},
				team{{Species: "Ferrothorn", Ability: "ironbarbs", Moves: mv("rockpolish")}},
			)
			p.makeChoices("move payback", "move rockpolish")
			p.fullHP(p.mine(), "Iron Barbs should have been off, so contact costs the attacker nothing")
		})

		g.skip("should negate Primal weather Abilities",
			"formes — primal reversion is a forme change, and neither the orbs nor Desolate Land are in this dataset")

		g.skip("should not activate Imposter if Neutralizing Gas leaves the field",
			"Ditto is not in this 80-species dex and Transform/Imposter is not modeled")

		g.it("should prevent Unburden's activation when it is active on the field", func(p *ps) {
			// Snorlax's 235 max HP puts the Sitrus line at 117, so 118 is one
			// point above it and the foe's Tackle is what crosses it. The
			// readable proxy for "Unburden did not activate" is the volatile the
			// engine arms on item loss; upstream reads the Speed it would have
			// produced, which a port cannot see.
			p.battle(
				team{{Species: "Wynaut", As: "Snorlax", Ability: "unburden", Item: "sitrusberry", HP: 118, Moves: mv("splash")}},
				team{
					{Species: "Pancham", As: "Weezing", Ability: "neutralizinggas", Moves: mv("tackle", "splash")},
					{Species: "Whismur", As: "Vileplume", Moves: mv("splash")},
				},
			)
			p.makeChoices("move splash", "move tackle")
			p.noItem(p.mine(), "the Sitrus Berry should have been eaten")
			p.isFalse(p.mine().Volatiles.Unburden,
				"Neutralizing Gas should have kept Unburden from arming on the berry")

			p.makeChoices("move splash", "switch 2")
			p.isFalse(p.mine().Volatiles.Unburden,
				"Unburden missed its window and must not arm when Neutralizing Gas leaves")
		})

		g.it("should negate Unburden when Neutralizing Gas enters the field", func(p *ps) {
			// Snorlax is 50 Speed here and 100 with Unburden up; Vileplume (70)
			// and Weezing (80) both sit between the two, so whichever of them is
			// out, turn order says whether the doubling is in effect. Upstream
			// checks the same three states with getStat('spe'), which a port
			// cannot reach. The extra turns are the cost of the probe: a turn
			// spent switching cannot also be a turn that reports move order.
			p.battle(
				team{{Species: "Wynaut", As: "Snorlax", Ability: "unburden", Item: "sitrusberry", HP: 118, Moves: mv("splash")}},
				team{
					{Species: "Whismur", As: "Vileplume", Moves: mv("tackle", "splash")},
					{Species: "Pancham", As: "Weezing", Ability: "neutralizinggas", Moves: mv("splash")},
				},
			)
			p.makeChoices("move splash", "move tackle")
			p.noItem(p.mine(), "the Sitrus Berry should have been eaten")

			p.makeChoices("move splash", "move splash")
			p.ok(movedFirst(p), "Unburden should have doubled Speed with no Neutralizing Gas out")

			p.makeChoices("move splash", "switch 2")
			p.makeChoices("move splash", "move splash")
			p.isFalse(movedFirst(p), "Neutralizing Gas should have taken the Unburden doubling away")

			p.makeChoices("move splash", "switch 1")
			p.makeChoices("move splash", "move splash")
			p.ok(movedFirst(p), "the doubling should be back once Neutralizing Gas leaves")
		})

		g.skip("should cause Illusion to instantly wear off when Neutralizing Gas enters the field",
			"Zoroark is not in this 80-species dex and Illusion is not modeled")

		g.skip("should cause Slow Start to instantly wear off/restart when Neutralizing Gas leaves/enters the field",
			"Regigigas is not in this 80-species dex and Slow Start is not modeled")

		g.it("should not cause Gluttony to instantly eat Berries when Neutralizing Gas leaves the field", func(p *ps) {
			// Snorlax carries Gluttony natively. At 235 max HP the Aguav Berry's
			// own line is 58 and Gluttony's is 117, so 93 HP after the first
			// Tackle is a spot only Gluttony reaches — which is what makes the
			// three readings mean something.
			p.battle(
				team{{Species: "Wynaut", As: "Snorlax", Ability: "gluttony", Item: "aguavberry", HP: 118, Moves: mv("splash")}},
				team{
					{Species: "Pancham", As: "Weezing", Ability: "neutralizinggas", Moves: mv("tackle", "splash")},
					{Species: "Whismur", As: "Vileplume", Moves: mv("tackle", "splash")},
				},
			)
			p.makeChoices("move splash", "move tackle")
			p.holdsItem(p.mine(), "Neutralizing Gas should have kept Gluttony from reaching the berry")

			p.makeChoices("move splash", "switch 2")
			p.holdsItem(p.mine(), "Gluttony must not eat the berry the moment Neutralizing Gas leaves")

			p.makeChoices("move splash", "move tackle")
			p.noItem(p.mine(), "Gluttony should get its chance back on the next HP drop")
		})

		g.it("should not trigger twice if negated then replaced", func(p *ps) {
			// Intrepid Sword is not in this engine's ability set, so the entry
			// boost this case counts never happens and both readings come back
			// at 0. The failure is that gap, not the double-trigger the case is
			// really about.
			p.battle(
				team{{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("splash")}},
				team{{Species: "Wynaut", Ability: "intrepidsword", Moves: mv("gastroacid", "simplebeam")}},
			)
			p.makeChoices("move splash", "move gastroacid")
			p.statStage(p.foe(), "atk", 1, "suppressing the gas should have let Intrepid Sword through once")

			p.makeChoices("move splash", "move simplebeam")
			p.statStage(p.foe(), "atk", 1, "Intrepid Sword should not have run a second time")
		})

		g.skip("should not re-trigger Unnerve if the ability was already triggered before", "doubles")

		g.it("should not announce Neutralizing Gas has worn off, if multiple are active simultenously", func(p *ps) {
			// Upstream checks that no |-end| line was emitted at all; the prose
			// analog is the one wear-off sentence this engine has.
			p.battle(
				team{{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("splash")}},
				team{
					{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("splash")},
					{Species: "Wynaut", Ability: "intrepidsword", Moves: mv("splash")},
				},
			)
			p.makeChoices("move splash", "switch 2")
			p.logLacks("The effects of Neutralizing Gas wore off",
				"one Neutralizing Gas is still on the field, so nothing wore off")
			// Intrepid Sword is not modeled, so this reading passes for the
			// wrong reason today; it is kept because it is half the claim.
			p.statStage(p.foe(), "atk", 0, "the remaining gas should still be suppressing the newcomer")
		})

		g.skip("should not prevent Ice Face from blocking damage nor reform Ice Face when leaving the field",
			"Eiscue is not in this 80-species dex, Ice Face is not modeled and neither are formes")

		g.skip("should delay the activation of Cud Chew", "doubles")

		g.skip("should not work if it was obtained via Transform",
			"Ditto is not in this 80-species dex and Transform is not modeled")

		g.it("should not reactivate abilities that were protected by Ability Shield", func(p *ps) {
			// Ability Shield is not in this dataset, which is what this case is
			// about, so the item is kept and the missing-item failure is the
			// finding. Porygon carries Download natively; against Weezing's
			// higher Defense it is the Sp. Atk side that rises.
			p.battle(
				team{{Species: "Porygon", Ability: "download", Item: "abilityshield", Moves: mv("splash")}},
				team{
					{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("splash")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move splash")
			p.makeChoices("move splash", "switch 2")
			p.statStage(p.mine(), "spa", 1, "Download was shielded, so it must not run again when the gas leaves")
		})

		g.skip("should not reactivate instances of Embody Aspect that had previously activated",
			"Terastallization")
	})

	describe(t, "Ability reactivation order", func(g *psg) {
		g.skip("should cause entrance Abilities to reactivate in order of Speed", "doubles")
		g.skip("should cause non-entrance abilities to be active immediately", "doubles")
		g.skip("should not give Unnerve priority in activation", "doubles")
	})
}
