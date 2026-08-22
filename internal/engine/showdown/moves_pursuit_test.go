//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/pursuit.js.
//
// The three older-generation describe blocks skip whole — gen 9 data, no
// gen-mod layer — and are nested here the way they are nested upstream. Of the
// fourteen cases in the main block, seven are doubles or triples, and two turn
// on Mega Evolution and Terastallization; the four that survive are the
// singles core of the move: Pursuit does not linger past the turn it was
// chosen, it does not intercept a switch it did not cause, and a fully
// paralyzed pursuer never fires.
//
// Substitutions the stand-in table does not cover. Darkrai only has to outrun
// the pursuer and survive, so Persian stands in — base 115 Speed, and Normal,
// which is neutral to Dark in both directions. Emolga is a fast Volt Switch
// pivot, so Jolteon stands in — Electric, base 130 Speed, and likewise
// unrelated to Dark.
//
// Red Card is not in this item set. That is a finding rather than a porting
// decision, so the case stays live and the run records the missing item.
//
// Upstream's `sleeptalk` filler is not in this dataset either; `splash` is the
// do-nothing move.

func TestMovesPursuit(t *testing.T) {
	describe(t, "Pursuit", func(g *psg) {
		g.skip("should execute before the target switches out and after the user mega evolves",
			"mega evolution")
		g.skip("should execute before the target switches out and after the user Terastallizes",
			"Terastallization")

		g.it("should not repeat", func(p *ps) {
			// Upstream mega-evolves the Beedrill; the Mega is only there for
			// damage, and what the case measures is that the *second* turn's
			// switch draws no second Pursuit.
			p.battle(
				team{
					{Species: "Beedrill", Ability: "swarm", Moves: mv("pursuit")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("calmmind")},
				},
				team{
					{Species: "Clefable", Ability: "magicguard", Moves: mv("calmmind")},
					{Species: "Alakazam", Ability: "unaware", Moves: mv("calmmind")},
				},
			)
			p.makeChoices("move pursuit", "move calmmind")
			hpBeforeSwitch := p.foe().HP
			p.makeChoices("switch 2", "switch 2")
			p.equal(p.slot(1, 1).HP, hpBeforeSwitch,
				"a switch on a later turn should not draw the earlier Pursuit again")
		})

		g.skip("should not double in power or activate before a switch if targeting an ally",
			"doubles")
		g.skip("should activate on the first target switching out", "doubles")
		g.skip("should activate on the second target switching out, if the first fainted", "doubles")
		g.skip("should activate on a switching opponent even if targeting an ally", "doubles")

		g.it("should not double in power or activate before a switch triggered by Red Card", func(p *ps) {
			p.battle(
				team{{Species: "Steelix", Item: "redcard", Moves: mv("pursuit")}},
				team{
					{Species: "Darkrai", As: "Persian", Moves: mv("tackle")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
			)
			p.makeChoices("move pursuit", "")
			p.fullHP(p.slot(1, 1), "a Red Card switch should not be intercepted by Pursuit")
			p.damaged(p.foe(), "the dragged-in replacement should take Pursuit at its normal power")
		})

		g.it("should deal damage prior to attacker selecting a switch in after u-turn etc", func(p *ps) {
			// This engine resolves a pivot's replacement inside the move, so
			// upstream's separate `battle.choose('p2', 'switch 2')` request has
			// no counterpart; a two-Pokemon bench makes the replacement forced
			// either way. The species assertions name the built stand-ins.
			p.battle(
				team{{Species: "Parasect", Moves: mv("pursuit")}},
				team{
					{Species: "Emolga", As: "Jolteon", Moves: mv("voltswitch")},
					{Species: "Zapdos", Moves: mv("batonpass")},
				},
			)
			p.makeChoices("move Pursuit", "move voltswitch")
			p.damaged(p.slot(1, 1), "Pursuit should have caught the Volt Switch user before it left")
			p.species(p.foe(), "Zapdos", "")
			p.makeChoices("move Pursuit", "move batonpass")
			p.fullHP(p.slot(1, 2), "should not hit Pokemon that has used Baton Pass")
			p.species(p.foe(), "Jolteon", "")
			p.makeChoices("move Pursuit", "move voltswitch")
		})

		g.skip("should only activate before switches on adjacent foes", "triples")
		g.skip("should not be redirected if activated by a switch", "doubles")

		// Upstream pins the full-paralysis roll with `forceRandomChance` and
		// spends a turn landing Thunder Wave. There is no such hook here, so
		// the pursuer starts paralyzed and the roll is measured: the foe leaves
		// untouched exactly when the 25% full-paralysis chance lands. A rate of
		// 0 would mean paralysis never stops the interception; a rate of 1
		// would mean the interception never happens at all.
		g.itRate("should be able to be paralyzed to prevent activation", 0.15, 0.35, 300, func(p *ps) bool {
			p.battle(
				team{{Species: "Tyranitar", Moves: mv("pursuit"), Status: "par"}},
				team{
					{Species: "Jolteon", Moves: mv("thunderwave")},
					{Species: "Clefable", Moves: mv("calmmind")},
				},
			)
			p.makeChoices("move pursuit", "switch 2")
			jolteon := p.slot(1, 1)
			return jolteon.HP == jolteon.MaxHP
		})

		g.skip("should not activate if Encored into Pursuit", "doubles")
		g.skip("should not activate other move if Encored out of Pursuit", "doubles")

		describe(t, "[Gen 4]", func(g *psg) {
			g.skip("should continue the switch", "gen 4 mechanics")
			g.skip("should not activate if the user is asleep at the beginning of the turn",
				"gen 4 mechanics")
			g.skip("should be able to be paralyzed to prevent activation", "gen 4 mechanics")
			g.skip("should activate if Encored into Pursuit", "gen 4 mechanics")
			g.skip("should not activate other move if Encored out of Pursuit", "gen 4 mechanics")
		})

		describe(t, "[Gen 3]", func(g *psg) {
			g.skip("should continue the switch", "gen 3 mechanics")
			g.skip("should only activate on the targeted opponent", "gen 3 mechanics")
			g.skip("should not activate on a switching opponent if targeting an ally",
				"gen 3 mechanics")
		})

		describe(t, "[Gen 2]", func(g *psg) {
			g.skip("should continue the switch", "gen 2 mechanics")
			g.skip("should try to activate even if the user is asleep at the beginning of the turn",
				"gen 2 mechanics")
			g.skip("should be able to be paralyzed to prevent activation", "gen 2 mechanics")
		})
	})
}
