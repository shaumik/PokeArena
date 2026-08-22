//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/destinybond.js.
//
// Two substitutions run through the whole modern block. Gastly takes its
// stand-in row (Gengar, the only Ghost in this dex), and Metagross is built
// as Hypno: what the cases need from it is a slower Psychic-type body whose
// Psychic is lethal, and Clear Body is set on the fixture exactly as upstream
// sets it. Because Gengar is far bulkier than the Gastly upstream lets die to
// one Psychic, the bonded Pokemon starts at 1 HP — "this Psychic kills me" is
// the only property of Gastly any of these cases uses.
//
// The three older-generation blocks skip whole: Gen 6's non-consecutive rule,
// Gen 4's bond surviving a switch-out, and Gen 2's speed-dependent version
// are all differences from gen 9 that this engine has no layer to express.

func TestMovesDestinyBond(t *testing.T) {
	describe(t, "Destiny Bond", func(g *psg) {
		g.it("should fail if used consecutively", func(p *ps) {
			// Upstream builds the identical battle twice in this case, which
			// buys nothing here: the harness already replays every case over
			// several seeds.
			p.battle(
				team{
					{Species: "Gastly", As: "Gengar", Ability: "levitate", HP: 1, Moves: mv("destinybond")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("calmmind")},
				},
				team{
					{Species: "Metagross", As: "Hypno", Ability: "clearbody", Moves: mv("psychic", "calmmind")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("calmmind")},
				},
			)
			p.makeChoices("move destinybond", "move calmmind")
			p.makeChoices("move destinybond", "move psychic")
			p.fainted(p.mine(), "")
			p.notFainted(p.foe(), "a second Destiny Bond in a row should fail, leaving the attacker alive")
		})

		g.it("should not fail after Protect usage", func(p *ps) {
			p.battle(
				team{
					{Species: "Gastly", As: "Gengar", Ability: "levitate", HP: 1, Moves: mv("destinybond", "protect")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("calmmind")},
				},
				team{
					{Species: "Metagross", As: "Hypno", Ability: "clearbody", Moves: mv("psychic", "calmmind")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("calmmind")},
				},
			)
			p.makeChoices("move protect", "move calmmind")
			p.makeChoices("move destinybond", "move psychic")
			p.fainted(p.mine(), "")
			p.fainted(p.foe(), "Protect does not count as a Destiny Bond, so the bond should still take the attacker")
		})

		g.it("should be removed the next turn if a fast user is asleep", func(p *ps) {
			// Upstream's Hypno carries Insomnia, which nothing here depends
			// on — Hypnosis is aimed at the foe. No Guard replaces it because
			// Hypnosis is 60% accurate in this dataset and the case needs the
			// bonded Pokemon reliably asleep on the following turn; over the
			// harness's seed sweep a coin flip would measure the coin.
			p.battle(
				team{
					{Species: "Gastly", As: "Gengar", Ability: "levitate", HP: 1, Moves: mv("destinybond", "spite")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("calmmind")},
				},
				team{
					{Species: "Hypno", Ability: "noguard", Item: "laggingtail", Moves: mv("psychic", "hypnosis")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("calmmind")},
				},
			)
			p.makeChoices("move destinybond", "move hypnosis")
			p.makeChoices("move destinybond", "move psychic")
			p.fainted(p.mine(), "")
			p.notFainted(p.foe(), "a bond the sleeping user could not renew should be gone by the time the attack lands")
		})
	})

	describe(t, "Destiny Bond [Gen 6]", func(g *psg) {
		g.skip("should not fail if used consecutively", "gen 6 mechanics")
		g.skip("should end the effect before the user switches out", "gen 6 mechanics")
	})

	describe(t, "Destiny Bond [Gen 4]", func(g *psg) {
		g.skip("should not end the effect before the user switches out", "gen 4 mechanics")
	})

	describe(t, "Destiny Bond [Gen 2]", func(g *psg) {
		g.skip("should end the effect before the user switches out if it is faster than the Pursuit user",
			"gen 2 mechanics")
	})
}
