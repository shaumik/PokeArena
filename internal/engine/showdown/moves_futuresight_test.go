//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/futuresight.js.
//
// Future Sight is not in this dataset, so every live case here fails at team
// construction naming the missing move. They are written out in full rather
// than skipped: the move is the whole file, and a skip would record "no
// Girafarig" when the finding is "no Future Sight". Copycat, Final Gambit,
// Tail Glow, Lucky Chant and Eject Button are absent from the dataset as
// well, and the cases that name them report through the same channel.
//
// Upstream states its answers as absolute damage windows at level 100
// (`assert.bounded(damage, [46, 55])`), and none of those transfer to a
// level-50 engine built out of different bodies. Each such case is restated
// as the comparison the window stood in for — the same Future Sight measured
// twice, once with the effect under test and once without — at a ratio no
// damage roll can cross. Where upstream heals between hits to keep its target
// alive, the port drops the healing move: recovery inside the turn the damage
// is measured would swamp the difference being measured, and Chansey survives
// the two hits without it.
//
// Substitutions, since none of the upstream cast is in this dex:
//
//   - Wynaut, Blissey, Happiny, Scizor and Ho-Oh go through their stand-in
//     rows (Hypno, Chansey, Chansey, Magneton, Moltres).
//   - Sneasel and Liepard are both built as Persian, a fast Normal body. Dark
//     is lost in both, and neither case uses it — the one case that does turn
//     on the Dark immunity is skipped below.
//   - Girafarig is built as Hypno, which keeps the Psychic half that gives
//     Future Sight its STAB; Normal is lost and unused.
//   - Roggenrola is built as Golem and Deino as Snorlax; both are bench or
//     filler bodies.
//   - Manaphy is built as Vaporeon (Water, special attacker). Flapple is
//     built as Venusaur: what Flapple contributes is a special attacker that
//     is *not* Psychic, so its Future Sight goes un-STABbed, and Venusaur
//     preserves that.
//   - Shedinja is built as Butterfree with HP set to 1. Wonder Guard is not
//     what these two cases use it for; what they use is a body that faints
//     the moment it is exposed, which 1 HP behind a 4x Stealth Rock weakness
//     reproduces.
//   - Scizor in the Stomping Tantrum case is built as Snorlax rather than
//     through its stand-in row: that case needs a target that sits through
//     two measured hits without being chipped into a different damage regime.
//     It keeps upstream's Shell Armor so no critical hit widens the
//     comparison.
//
// `sleeptalk`, upstream's do-nothing, is not in this dataset either; `splash`
// stands in for it throughout.

func TestMovesFutureSight(t *testing.T) {
	describe(t, "Future Sight", func(g *psg) {
		g.skip("should damage in two turns, ignoring Protect, affected by Dark immunities",
			"no Dark-type species is in this 80-species dex, and the case turns on Sneasel's Dark immunity to the Psychic Future Sight aimed at it")

		g.it("should fail when already active for the target's position", func(p *ps) {
			// Upstream reads the protocol line after the move; this engine
			// says "But it failed!" and Splash resolves silently, so nothing
			// else in the battle can produce that line.
			p.battle(
				team{{Species: "Sneasel", As: "Persian", Moves: mv("splash")}},
				team{{Species: "Girafarig", As: "Hypno", Moves: mv("futuresight")}},
			)
			p.turn()
			p.turn()
			p.logHas("But it failed!",
				"a second Future Sight should fail while the first is still pending")
		})

		g.skip("[Gen 2] should damage in two turns, ignoring Protect", "gen 2 mechanics")

		g.it("should not double Stomping Tantrum for exiting normally", func(p *ps) {
			// Upstream's window ([19, 23], "38-45 if it were doubled") is a
			// doubling check stated as a number. Here the same Stomping
			// Tantrum is measured on a clean turn and then on the turn after
			// Future Sight, and the second must not be near twice the first.
			// The pending hit lands at the end of turn 4, after both
			// measurements.
			p.battle(
				team{{Species: "Wynaut", Moves: mv("futuresight", "stompingtantrum")}},
				team{{Species: "Scizor", As: "Snorlax", Ability: "shellarmor", Moves: mv("splash")}},
			)
			foe := p.foe()
			before := foe.HP
			p.makeChoices("move stompingtantrum", "move splash")
			plain := before - foe.HP

			p.makeChoices("move futuresight", "move splash")

			before = foe.HP
			p.makeChoices("move stompingtantrum", "move splash")
			afterFutureSight := before - foe.HP

			p.atMost(afterFutureSight, plain*3/2,
				"Future Sight leaving normally is not a failed move, so Stomping Tantrum should not double")
		})

		g.it("should not trigger Eject Button", func(p *ps) {
			// `requestState === 'move'` is upstream's way of saying no forced
			// switch was queued; here that reads as the holder still being the
			// one out.
			p.battle(
				team{{Species: "Wynaut", Moves: mv("futuresight")}},
				team{
					{Species: "Scizor", Item: "ejectbutton", Moves: mv("splash")},
					{Species: "Roggenrola", As: "Golem", Moves: mv("splash")},
				},
			)
			p.turn()
			p.turn()
			p.turn()
			p.species(p.foe(), "Scizor", "Eject Button should not fire on Future Sight's delayed hit")
		})

		g.it("should be able to set Future Sight against an empty target slot", func(p *ps) {
			p.battle(
				team{
					{Species: "Shedinja", As: "Butterfree", HP: 1, Moves: mv("finalgambit")},
					{Species: "Roggenrola", As: "Golem", Moves: mv("splash")},
				},
				team{{Species: "Wynaut", Moves: mv("splash", "futuresight")}},
			)
			p.makeChoices("", "move futuresight")
			p.makeChoices("switch 2", "")
			p.turn()
			p.turn()
			p.damaged(p.mine(),
				"Future Sight set against a slot that emptied should land on whoever occupies it")
		})

		g.it("its damaging hit should not count as copyable for Copycat", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("splash", "futuresight")}},
				team{{Species: "Liepard", As: "Persian", Moves: mv("splash", "copycat")}},
			)
			p.makeChoices("move futuresight", "")
			p.turn()
			p.turn()
			p.makeChoices("", "move copycat")
			p.turn()
			p.turn()
			p.fullHP(p.mine(), "Copycat should not have been able to copy Future Sight's delayed hit")
		})

		g.it("should only cause the user to take Life Orb recoil on its damaging turn", func(p *ps) {
			// The recoil is a fraction of max HP and transfers; upstream's
			// damage window ([30, 35], "22-27 without the Life Orb") does not,
			// and there is no second Future Sight in this fixture to compare
			// against, so the port only asks that the hit landed.
			p.battle(
				team{{Species: "wynaut", Item: "lifeorb", Moves: mv("futuresight")}},
				team{{Species: "mew", Moves: mv("splash")}},
			)
			p.turn()
			me := p.mine()
			p.fullHP(me, "the user should not take Life Orb recoil on Future Sight's starting turn")
			p.turn()
			p.turn()
			p.equal(me.HP, me.MaxHP-me.MaxHP/10,
				"the user should take Life Orb recoil on Future Sight's damaging turn")
			p.damaged(p.foe(), "the delayed hit should have landed")
		})

		g.skip("[Gen 4] should not be affected by Life Orb", "gen 4 mechanics")

		g.it("should not be affected by Life Orb if not the original user", func(p *ps) {
			p.battle(
				team{
					{Species: "wynaut", Item: "lifeorb", Moves: mv("futuresight")},
					{Species: "liepard", As: "Persian", Item: "lifeorb", Moves: mv("splash")},
				},
				team{{Species: "mew", Moves: mv("splash")}},
			)
			p.turn()
			p.turn()
			p.makeChoices("switch 2", "move splash")
			p.fullHP(p.slot(0, 2),
				"the Life Orb of a Pokemon that did not use Future Sight should not fire")
			p.damaged(p.foe(), "the delayed hit should still have landed")
		})

		g.it("should not cause the user to change typing on either its starting or damaging turn", func(p *ps) {
			p.battle(
				team{{Species: "roggenrola", As: "Golem", Ability: "protean",
					Moves: mv("futuresight", "splash")}},
				team{{Species: "mew", Moves: mv("splash")}},
			)
			me := p.mine()
			p.turn()
			p.notEqual(me.Type1, "psychic", "Protean should not change type on Future Sight's starting turn")
			p.notEqual(me.Type2, "psychic", "Protean should not change type on Future Sight's starting turn")
			p.turn()
			p.turn()
			p.notEqual(me.Type1, "psychic", "Protean should not change type on Future Sight's damaging turn")
			p.notEqual(me.Type2, "psychic", "Protean should not change type on Future Sight's damaging turn")
		})

		g.it("should be boosted by Terrain only if Terrain is active on the damaging turn", func(p *ps) {
			// Psychic Terrain is up for the first delayed hit and has expired
			// by the second, which is upstream's [46, 55] against [36, 43]
			// stated as a comparison instead of two windows. The 1.3x boost
			// cannot be crossed by a damage roll in either direction, so the
			// first hit is strictly the larger.
			p.battle(
				team{{Species: "Blissey", Ability: "shellarmor", Moves: mv("splash")}},
				team{{Species: "Wynaut", Ability: "psychicsurge", Moves: mv("splash", "futuresight")}},
			)
			me := p.mine()
			p.makeChoices("", "move futuresight")
			p.turn()
			before := me.HP
			p.turn()
			withTerrain := before - me.HP

			p.makeChoices("", "move futuresight")
			p.turn()
			before = me.HP
			p.turn()
			withoutTerrain := before - me.HP

			p.atLeast(withTerrain, withoutTerrain+1,
				"only the hit that resolved under Psychic Terrain should have been boosted")
		})

		g.skip("should be boosted by Terrain even if the user is not on the field, as long as the user is not Flying-type",
			"no Psychic/Flying species is in this 80-species dex, and the case reads its answer off two absolute damage windows belonging to two different users, which substituted bodies at level 50 cannot reproduce as a comparison")

		g.it("should not ignore the target's screens, even when the user is not active on the field", func(p *ps) {
			// Both of upstream's windows are the same ([18, 21] twice): the
			// screen applies whether or not the user is still out. Light Clay
			// keeps it up for all eight turns this needs.
			p.battle(
				team{{Species: "Blissey", Ability: "shellarmor", Item: "lightclay",
					Moves: mv("splash", "lightscreen")}},
				team{
					{Species: "Wynaut", Moves: mv("splash", "futuresight")},
					{Species: "deino", As: "Snorlax", Moves: mv("splash")},
				},
			)
			me := p.mine()
			p.makeChoices("move lightscreen", "move futuresight")
			p.turn()
			before := me.HP
			p.turn()
			userOnField := before - me.HP

			p.makeChoices("", "move futuresight")
			p.turn()
			before = me.HP
			p.makeChoices("", "switch 2")
			userBenched := before - me.HP

			p.atMost(userBenched, userOnField*3/2,
				"Light Screen should still halve a Future Sight whose user has left the field")
		})

		g.it("should not consider the user's item or Ability when the user is not active", func(p *ps) {
			// [70, 84] with Adaptability and Choice Specs against [46, 55]
			// without them, restated as the ratio: the hit that resolves after
			// the user has switched out must be clearly the smaller.
			p.battle(
				team{{Species: "Blissey", Ability: "shellarmor", Moves: mv("splash")}},
				team{
					{Species: "Wynaut", Ability: "adaptability", Item: "choicespecs",
						Moves: mv("futuresight")},
					{Species: "Deino", As: "Snorlax", Ability: "powerspot", Moves: mv("splash")},
				},
			)
			me := p.mine()
			p.turn()
			p.turn()
			before := me.HP
			p.turn()
			userOnField := before - me.HP

			p.turn()
			p.turn()
			before = me.HP
			p.makeChoices("", "switch 2")
			userBenched := before - me.HP

			p.atMost(userBenched, userOnField*9/10,
				"a benched user's item and Ability should not reach its Future Sight")
		})

		g.it("should not ignore the target's Unaware", func(p *ps) {
			// Upstream reads [60, 71] and notes the hit would be 236-278 if
			// Unaware were ignored. Here the same Future Sight is measured
			// unboosted and then behind a Tail Glow that Simple doubles to +6;
			// against an Unaware target the two should be the same hit, and a
			// critical hit cannot carry the second past twice the first.
			p.battle(
				team{{Species: "Manaphy", As: "Vaporeon", Ability: "simple",
					Moves: mv("tailglow", "futuresight", "splash")}},
				team{{Species: "Ho-Oh", Ability: "unaware", Moves: mv("luckychant")}},
			)
			foe := p.foe()
			p.makeChoices("move futuresight", "")
			p.makeChoices("move splash", "")
			before := foe.HP
			p.makeChoices("move splash", "")
			plain := before - foe.HP

			p.makeChoices("move tailglow", "")
			p.makeChoices("move futuresight", "")
			p.makeChoices("move splash", "")
			before = foe.HP
			p.makeChoices("move splash", "")
			boosted := before - foe.HP

			p.atMost(boosted, plain*2,
				"Unaware should read the Future Sight user's Sp. Atk unboosted")
		})

		g.skip("should use the user's most recent Special Attack stat if the user is on the field",
			"Aegislash is not in this 80-species dex and Stance Change formes are not modeled")

		g.skip("should use the user's most recent Special Attack stat, even if the user is not on the field",
			"upstream skips this case itself; Aegislash is not in this 80-species dex and Stance Change formes are not modeled")

		g.it("should only use Sp. Atk stat boosts/drops if the user is on the field", func(p *ps) {
			// [113, 134] at +2 against [57, 68] at +0 — a factor of two, which
			// the port states as the ratio. Ho-Oh's Recover is dropped so the
			// healing does not land inside a measured turn; Moltres survives
			// both hits without it.
			p.battle(
				team{
					{Species: "Flapple", As: "Venusaur", Moves: mv("futuresight", "nastyplot", "splash")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Ho-Oh", Ability: "shellarmor", Moves: mv("splash")}},
			)
			foe := p.foe()
			p.makeChoices("move futuresight", "")
			p.makeChoices("move nastyplot", "")
			before := foe.HP
			p.makeChoices("move splash", "")
			userOnField := before - foe.HP

			p.makeChoices("move futuresight", "")
			p.makeChoices("switch wynaut", "")
			before = foe.HP
			p.turn()
			userBenched := before - foe.HP

			p.atMost(userBenched, userOnField*3/4,
				"the Sp. Atk boosts of a user that has left the field should not apply")
		})

		g.skip("should never resolve when used on turn 254 or later",
			"upstream pokes battle.turn = 255 directly; this harness cannot start a battle near the turn counter's limit, and playing 254 turns is not a test")

		g.skip("should target particular slots in Doubles", "doubles")

		g.it("should do nothing if no Pokemon is present to take damage from the Future attack", func(p *ps) {
			// The Shedinja stand-in is at 1 HP, so Stealth Rock kills it on
			// entry and the slot is empty when the delayed hit comes due.
			// Magic Guard on the returning body keeps Stealth Rock out of the
			// full-HP assertion, exactly as upstream uses it.
			p.battle(
				team{
					{Species: "Wynaut", Ability: "magicguard", Moves: mv("splash")},
					{Species: "Shedinja", As: "Butterfree", HP: 1, Moves: mv("splash")},
				},
				team{{Species: "Happiny", Moves: mv("stealthrock", "futuresight")}},
			)
			p.makeChoices("", "move futuresight")
			p.makeChoices("", "move stealthrock")
			p.makeChoices("switch 2", "")
			p.makeChoices("switch 1", "")
			p.turn()
			p.fullHP(p.mine(),
				"a Future Sight that came due against an empty slot should do nothing")
		})
	})
}
