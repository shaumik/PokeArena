//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/abilityshield.js.
//
// Ability Shield is not one of this dataset's 128 items, so every case that
// holds one fails at team construction naming the item. That single gap is what
// the file records; the fixtures are still written out in full so they measure
// the right thing on the day the item lands.
//
// Because of that, each case guards on the battle having been built and then
// asserts the state change only. Upstream's `log.includes('Ability Shield')`
// checks have no counterpart either way: the engine has no Ability Shield
// string at all, so both a logHas and a logLacks on it would be an assertion
// that could never mean anything.
//
// Species. Weezing-Galar has no stand-in row; Kanto Weezing takes its place and
// is the better body anyway — it carries Levitate and Neutralizing Gas natively,
// which is every ability the upstream file asks of it. Wynaut goes through its
// stand-in (Hypno) except in the four Sturdy cases, where the case needs a body
// that a single hit would knock out so Sturdy has something to save it from.
// Upstream gets that from `level: 5`; level is fixed at 50 here, so the port
// names Onix — the frailest Sturdy body in the dex — and has Weezing throw a
// Surf, which Onix is four times weak to. Gastly goes through its stand-in
// (Gengar), which keeps the Ghost typing the Earth Power half of the Mold
// Breaker case needs.
//
// Abilities. Shadow Tag is not modeled and upstream only wants "an ability to
// be overwritten", so the port uses Forewarn, which Hypno has and the engine
// implements. Mummy, the ability-changing ability the file is actually about,
// is kept and reports itself.

func TestItemsAbilityShield(t *testing.T) {
	describe(t, "Ability Shield", func(g *psg) {
		g.it("should protect the holder's ability against ability-changing moves", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "forewarn", Item: "abilityshield", Moves: mv("splash")}},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "levitate", Moves: mv("worryseed")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.hasAbility(p.mine(), "forewarn", "the shield should have refused Worry Seed")
		})

		g.it("should protect the holder's ability against ability-changing abilities", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "forewarn", Item: "abilityshield", Moves: mv("tackle")}},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "mummy", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.hasAbility(p.mine(), "forewarn", "the shield should have refused Mummy")
		})

		g.it("should only protect the holder", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "mummy", Item: "abilityshield", Moves: mv("splash")}},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "levitate", Moves: mv("tackle")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.hasAbility(p.foe(), "mummy", "the shield protects its holder, not the Pokemon that touched it")
		})

		g.it("should protect the holder's ability against Neutralizing Gas", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", As: "Onix", Ability: "sturdy", Item: "abilityshield", Moves: mv("splash")}},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "neutralizinggas", Moves: mv("surf")}},
			)
			if p.state() == nil {
				return
			}
			// Upstream reads the gas out of the log before the first turn. This
			// engine fires the leads' switch-in hooks at the top of turn 1, so
			// both halves are checked after it.
			p.makeChoices("move splash", "move surf")
			p.logHas("Neutralizing Gas filled the area", "Neutralizing Gas should still announce itself")
			p.equal(p.mine().HP, 1, "the holder should survive on Sturdy")
		})

		g.it("should protect the holder's ability against Mold Breaker", func(p *ps) {
			p.battle(
				team{
					{Species: "Wynaut", As: "Onix", Ability: "sturdy", Item: "abilityshield", Moves: mv("splash")},
					{Species: "Gastly", Ability: "levitate", Item: "abilityshield", Moves: mv("splash")},
				},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "moldbreaker", Moves: mv("surf", "earthpower")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move surf")
			p.equal(p.mine().HP, 1, "the holder should survive on Sturdy")
			p.makeChoices("switch gastly", "move earthpower")
			p.fullHP(p.mine(), "Levitate should still make the holder ungrounded")
		})

		g.it("should protect the holder's ability against Gastro Acid", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", As: "Onix", Ability: "sturdy", Item: "abilityshield", Moves: mv("splash")}},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "levitate", Moves: mv("gastroacid", "surf")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move gastroacid")
			p.makeChoices("move splash", "move surf")
			p.equal(p.mine().HP, 1, "Gastro Acid should not have reached Sturdy through the shield")
		})

		g.it("should not unsuppress the holder's ability if Ability Shield is acquired after Gastro Acid has been used", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", As: "Onix", Ability: "sturdy", Moves: mv("splash")}},
				team{{
					Species: "Weezing-Galar", As: "Weezing", Ability: "levitate", Item: "abilityshield",
					Moves: mv("gastroacid", "trick", "surf"),
				}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move gastroacid")
			p.makeChoices("move splash", "move trick")
			p.makeChoices("move splash", "move surf")
			p.fainted(p.mine(), "a shield picked up after the fact should not bring Sturdy back")
		})

		g.it("should unsuppress the holder's ability if Ability Shield is acquired after Neutralizing Gas has come into effect", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", As: "Onix", Ability: "sturdy", Moves: mv("splash")}},
				team{{
					Species: "Weezing-Galar", As: "Weezing", Ability: "neutralizinggas", Item: "abilityshield",
					Moves: mv("trick", "surf"),
				}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move trick")
			p.makeChoices("move splash", "move surf")
			p.equal(p.mine().HP, 1, "the shield should lift the gas the moment the holder takes it")
		})

		g.it("should not be suppressed by Klutz", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "klutz", Item: "abilityshield", Moves: mv("tackle")}},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "mummy", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.hasAbility(p.mine(), "klutz", "Klutz should not switch off the shield protecting Klutz")
		})

		g.it("should protect the holder's ability against Skill Swap", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "forewarn", Item: "abilityshield", Moves: mv("splash")}},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "levitate", Moves: mv("skillswap")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.hasAbility(p.mine(), "forewarn", "the holder should keep its ability")
			p.hasAbility(p.foe(), "levitate", "a refused Skill Swap should leave the user's ability alone too")
		})

		g.it("should protect the holder's ability against Skill Swap, even if used by the holder", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "forewarn", Item: "abilityshield", Moves: mv("skillswap")}},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "levitate", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.hasAbility(p.mine(), "forewarn", "the holder should keep its ability")
			p.hasAbility(p.foe(), "levitate", "a refused Skill Swap should leave the target's ability alone too")
		})

		g.it("should not trigger holder's Intimidate if Ability Shield is acquired after entrance, while Neutralizing Gas is in effect", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "intimidate", Moves: mv("splash")}},
				team{{
					Species: "Weezing-Galar", As: "Weezing", Ability: "neutralizinggas", Item: "abilityshield",
					Moves: mv("trick"),
				}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.statStage(p.foe(), "atk", 0, "Intimidate is spent on entry and should not fire again")
		})

		g.it("should not trigger holder's Trace", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "trace", Item: "abilityshield", Moves: mv("splash")}},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "levitate", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			// Upstream reads this straight off the freshly built battle; here the
			// leads' switch-in hooks run at the top of turn 1, so the turn has to
			// happen before Trace would have copied anything.
			p.turn()
			p.notEqual(p.mine().Ability, "levitate", "the shield should stop Trace from overwriting itself")
		})

		g.it("should not trigger holder's Trace even after losing the item", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "trace", Item: "abilityshield", Moves: mv("splash")}},
				team{{Species: "Weezing-Galar", As: "Weezing", Ability: "levitate", Moves: mv("trick")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.notEqual(p.mine().Ability, "levitate", "Trace does not get a second entry when the shield leaves")
		})

		g.skip("should not prevent Imposter from changing the holder's ability",
			"Ditto is not in this 80-species dex and Transform/Imposter are not modeled")
		g.skip("should not prevent forme changes from changing the holder's ability",
			"formes — Ogerpon is not in this dex, and neither forme changes nor Terastallization are modeled")
	})
}
