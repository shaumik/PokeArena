//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/stompingtantrum.js.
//
// # Reading Base Power out of damage
//
// Every case upstream asserts on a Base Power read off
// battle.onEvent('BasePower'). There is no such hook here, so each port takes
// a plain Tantrum as a baseline earlier in the same battle and compares the
// Tantrum under test against it: at least 1.5x for "should double", at most
// 1.5x for "should not double". The two ranges do not touch — a doubling
// lands near 1.9x once the damage formula's +2 term is accounted for — so the
// looseness costs nothing.
//
// The defenders hold Battle Armor for the same reason. A critical hit is 1.5x,
// which is exactly the size of the effect being measured, and over five seeds
// one would land often enough to make the comparisons meaningless. The one
// exception is the Water Absorb body in "a two-turn move fails for a different
// reason": there the crit-prone reading is the *doubled* one, where a crit can
// only push it further above the threshold.
//
// Sleep Talk, which upstream uses purely as an inert move, is not in this
// dataset; Splash stands in for it, which is upstream's own idiom for a body
// that must not interfere.
//
// Substitutions: Manaphy is built as Lapras (a bulky water body) except in the
// first case, which needs a genderless target so that Attract fails — there it
// is Mew, the bulkiest genderless species in this dex, with defensive EVs so it
// survives the whole sequence. Marowak-Alola is built as plain Marowak (the
// forme layer is not modeled and the case turns on the recharge, not the
// typing), Lycanroc-Midnight and Accelgor as inert bodies, Feebas as Seadra.
//
// Two things the port does not attempt: the Spore half of the first case,
// because sleep length is rolled here and a second Spore only fails while the
// target is still asleep; and the two doubles cases.

func TestMovesStompingTantrum(t *testing.T) {
	describe(t, "Stomping Tantrum", func(g *psg) {
		g.it("should double its Base Power if the last move used on the previous turn failed", func(p *ps) {
			p.battle(
				team{{Species: "Marowak", Moves: mv("splash", "attract", "stompingtantrum")}},
				team{{Species: "Manaphy", As: "Mew", Ability: "battlearmor",
					EVs: evs(map[string]int{"hp": 252, "def": 252}), Moves: mv("splash")}},
			)
			p.makeChoices("move splash", "move splash")
			before := p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			plain := before - p.foe().HP

			p.makeChoices("move attract", "move splash")
			p.isFalse(p.foe().Volatiles.Attract, "Attract should fail against a genderless target")

			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			doubled := before - p.foe().HP

			p.atLeast(plain, 1, "the baseline Tantrum should do damage at all")
			p.atLeast(doubled, 3*plain/2, "a Tantrum after a failed move should be doubled")
		})

		g.it("should not double its Base Power if the last move used on the previous turn hit Protect", func(p *ps) {
			p.battle(
				team{{Species: "Marowak", Moves: mv("stompingtantrum")}},
				team{{Species: "Manaphy", As: "Lapras", Ability: "battlearmor", Moves: mv("protect", "splash")}},
			)
			before := p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			plain := before - p.foe().HP

			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move protect")
			blocked := before - p.foe().HP
			p.equal(blocked, 0, "Protect should have stopped the Tantrum outright")

			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			after := before - p.foe().HP

			p.atLeast(plain, 1, "the baseline Tantrum should do damage at all")
			p.atMost(after, 3*plain/2, "hitting Protect does not count as the move failing")
		})

		g.skip("should double its Base Power if the last move used was a spread move that partially hit Protect and otherwise failed",
			"doubles")

		g.it("should not double its Base Power if the last move used on the previous turn was a successful Celebrate", func(p *ps) {
			p.battle(
				team{{Species: "Snorlax", Moves: mv("stompingtantrum", "celebrate")}},
				team{{Species: "Manaphy", As: "Lapras", Ability: "battlearmor", Moves: mv("splash")}},
			)
			before := p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			plain := before - p.foe().HP

			p.makeChoices("move celebrate", "move splash")

			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			after := before - p.foe().HP

			p.atLeast(plain, 1, "the baseline Tantrum should do damage at all")
			p.atMost(after, 3*plain/2, "Celebrate succeeds, so the Tantrum after it is not doubled")
		})

		g.it(`should not double its Base Power if the last "move" used on the previous turn was a recharge`, func(p *ps) {
			// No Guard is upstream's, and it matters here: a missed Hyper Beam
			// would be a failed move and would double the Tantrum for the wrong
			// reason.
			p.battle(
				team{{Species: "Marowak-Alola", As: "Marowak", Ability: "noguard",
					Moves: mv("stompingtantrum", "hyperbeam")}},
				team{{Species: "Lycanroc-Midnight", As: "Lapras", Ability: "battlearmor", Moves: mv("splash")}},
			)
			before := p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			plain := before - p.foe().HP

			p.makeChoices("move hyperbeam", "move splash")
			p.makeChoices("", "move splash")
			p.logHas("must recharge!", "the turn after Hyper Beam should be a recharge")

			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			after := before - p.foe().HP

			p.atLeast(plain, 1, "the baseline Tantrum should do damage at all")
			p.atMost(after, 3*plain/2, "a recharge is not a failed move")
		})

		g.it("should not double its Base Power if the user dropped mid-Fly due to Smack Down", func(p *ps) {
			p.battle(
				team{{Species: "Magikarp", Moves: mv("stompingtantrum", "fly")}},
				team{{Species: "Wynaut", Ability: "battlearmor", Moves: mv("smackdown")}},
			)
			before := p.foe().HP
			p.makeChoices("move stompingtantrum", "move smackdown")
			plain := before - p.foe().HP

			// If Smack Down does not ground the user, turn three resolves the
			// Fly strike instead of the Tantrum and the damage comparison below
			// is measuring the wrong move; this is the assertion that says so.
			p.makeChoices("move fly", "move smackdown")
			p.ok(p.mine().Volatiles.Charging == nil, "Smack Down should have knocked the user out of Fly")

			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move smackdown")
			after := before - p.foe().HP

			p.atLeast(plain, 1, "the baseline Tantrum should do damage at all")
			p.atMost(after, 3*plain/2, "being grounded mid-Fly does not count as the move failing")
		})

		g.it("should double its Base Power if a two-turn move fails for a different reason", func(p *ps) {
			// The baseline is taken against a Battle Armor body and the doubled
			// reading against the Water Absorb body that makes Dive fail. Only
			// the baseline has to be crit-free: a crit on the second reading can
			// only push it further above the threshold.
			p.battle(
				team{{Species: "Magikarp", Moves: mv("stompingtantrum", "dive", "splash")}},
				team{
					{Species: "Wynaut", Ability: "battlearmor", Moves: mv("splash")},
					{Species: "Wynaut", Ability: "waterabsorb", Moves: mv("splash")},
				},
			)
			before := p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			plain := before - p.foe().HP

			p.makeChoices("move splash", "switch 2")
			p.makeChoices("move dive", "move splash")
			p.makeChoices("", "move splash")
			p.logHas("absorbed the", "Water Absorb should have swallowed the Dive")

			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			doubled := before - p.foe().HP

			p.atLeast(plain, 1, "the baseline Tantrum should do damage at all")
			p.atLeast(doubled, 3*plain/2, "a two-turn move that was absorbed is a failed move")
		})

		g.it("should double its Base Power on some failure conditions of Rest", func(p *ps) {
			// Three failures in one case upstream: Rest while already asleep
			// (Comatose), Rest at full HP, and Rest with Insomnia. Comatose is
			// not an ability this engine models, so the first third reports that
			// rather than the Rest rule.
			p.battle(
				team{
					{Species: "Magikarp", Ability: "comatose", Moves: mv("stompingtantrum", "rest", "splash")},
					{Species: "Feebas", As: "Seadra", Ability: "insomnia",
						Moves: mv("stompingtantrum", "rest", "splash")},
				},
				team{{Species: "Accelgor", As: "Snorlax", Ability: "battlearmor",
					EVs: evs(map[string]int{"hp": 252, "def": 252}), Moves: mv("splash", "nightshade")}},
			)
			before := p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			baseA := before - p.foe().HP

			p.makeChoices("move rest", "move splash")
			p.noStatus(p.mine(), "Rest should have failed rather than put the user to sleep")
			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			p.atLeast(baseA, 1, "the first baseline Tantrum should do damage at all")
			p.atLeast(before-p.foe().HP, 3*baseA/2, "Rest while already asleep fails, so the Tantrum doubles")

			p.makeChoices("switch 2", "move splash")
			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			baseB := before - p.foe().HP

			p.makeChoices("move rest", "move splash")
			p.noStatus(p.mine(), "Rest at full HP should have failed rather than put the user to sleep")
			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			p.atLeast(baseB, 1, "the second baseline Tantrum should do damage at all")
			p.atLeast(before-p.foe().HP, 3*baseB/2, "Rest at full HP fails, so the Tantrum doubles")

			// Night Shade takes the user off full HP so that Insomnia is the
			// only thing left for Rest to fail on.
			p.makeChoices("move splash", "move nightshade")
			p.makeChoices("move rest", "move splash")
			p.noStatus(p.mine(), "Insomnia should have made Rest fail rather than put the user to sleep")
			before = p.foe().HP
			p.makeChoices("move stompingtantrum", "move splash")
			p.atLeast(before-p.foe().HP, 3*baseB/2, "Rest with Insomnia fails, so the Tantrum doubles")
		})

		g.skip("should not double its Base Power on other failure conditions of Rest",
			"upstream leaves this case pending (it.skip)")
		g.skip("should not double its Base Power on most failed healing effects", "doubles")
		g.skip("should cause Gravity-negated moves to double in BP, even Z-moves", "Z-moves")
	})
}
