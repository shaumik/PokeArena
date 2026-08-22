//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/rollout.js.
//
// Upstream runs the same describe twice, once per move in ['Ice Ball',
// 'Rollout']; the closure below keeps the two blocks identical the way the
// loop does. Ice Ball is not in this dataset, so all five of its cases report
// the missing move — that is the finding, and the block is not skipped for it.
// The nested `Rollout Storage glitch` describe is emitted under both, exactly
// as upstream emits it; both copies are skips, so the duplicated ledger key
// never reaches the gaps table.
//
// # Reading Base Power out of damage
//
// Every case here asserts on a Base Power the original reads straight off
// battle.onEvent('BasePower'). There is no such hook, so each port measures the
// damage the move actually deals and compares one turn against another in the
// same battle. That is why the fixtures below differ from upstream's more than
// a stand-in table would explain:
//
//   - The defender uses Splash, not Recover. Move PP here is the dataset's base
//     value with no PP Ups, so eight Recovers do not exist; instead the target
//     is chosen bulky enough to sit through the whole chain, which also makes
//     each turn's damage a plain HP delta.
//   - The attacker holds Compound Eyes and the defender Battle Armor. Rollout
//     is 90% accurate and every hit can crit, and across eight turns and five
//     seeds either would rewrite a damage comparison for reasons the case is
//     not about. Compound Eyes puts accuracy over 100 and Battle Armor removes
//     the crit; upstream reaches for both abilities elsewhere in this same
//     file for the same reason.
//   - Steelix is built as Golduck rather than through its stand-in row. The row
//     is Onix, whose Defense drops Rollout to 1-5 HP a hit, and at that
//     magnitude the +2 term in the damage formula is larger than the doubling
//     the case is trying to see.
//   - Shuckle takes its stand-in row (Snorlax) everywhere except the five-turn
//     chain. A 16x Rollout off Snorlax's Attack does not fit inside any HP pool
//     in this dex, so there Shuckle is built as Chansey — the lowest Attack and
//     largest HP on the roster, which is the role Shuckle plays upstream.
//
// Two cases are skipped rather than translated: both turn on interrupting the
// chain at a chosen turn (a miss, an immobilization), and this harness has no
// accuracy or before-move hook and demands the result hold on every seed.

func TestMovesRollout(t *testing.T) {
	block := func(t *testing.T, g *psg, id string) {
		g.it("should double its Base Power every turn for five turns, then resets to 30 BP", func(p *ps) {
			p.battle(
				team{{Species: "Shuckle", As: "Chansey", Ability: "compoundeyes", Moves: mv(id)}},
				team{{Species: "Steelix", As: "Golduck", Ability: "battlearmor", Moves: mv("splash")}},
			)
			var d [8]int
			for i := range d {
				before := p.foe().HP
				p.makeChoices("move "+id, "move splash")
				d[i] = before - p.foe().HP
			}
			p.atLeast(d[0], 1, "the first turn of the chain should do damage at all")
			for i := 1; i < 5; i++ {
				p.atLeast(d[i], d[i-1]+1, "each turn of the chain should hit harder than the last")
			}
			p.atLeast(d[4], 8*d[0], "the fifth turn is a 16x Base Power hit")
			p.atMost(d[5], 2*d[0], "the sixth turn should be back to 30 BP")
			p.atLeast(d[6], d[5]+1, "the second chain should double again")
			p.atLeast(d[7], d[6]+1, "the second chain should double again")
		})

		g.skip("should reset its Base Power if the move misses",
			"the harness cannot force a miss on a chosen turn: there is no accuracy hook and every case must hold on all seeds")
		g.skip("should reset its Base Power if the Pokemon is immobilized",
			"the harness cannot force an immobilization on a chosen turn: paralysis and flinch are rolled, and no deterministic immobilizer is available mid-chain")

		g.it("should have double Base Power if the Pokemon used Defense Curl earlier", func(p *ps) {
			// Turn 1 is the un-curled reading; Defense Curl on turn 2 also
			// breaks the chain, so turn 3 starts a fresh 30 BP Rollout and any
			// difference between the two is the Defense Curl doubling.
			p.battle(
				team{{Species: "Shuckle", Ability: "compoundeyes", Moves: mv(id, "defensecurl")}},
				team{{Species: "Steelix", As: "Golduck", Ability: "battlearmor", Moves: mv("splash")}},
			)
			before := p.foe().HP
			p.makeChoices("move "+id, "move splash")
			plain := before - p.foe().HP
			p.makeChoices("move defensecurl", "move splash")
			before = p.foe().HP
			p.makeChoices("move "+id, "move splash")
			curled := before - p.foe().HP

			p.atLeast(plain, 1, "the un-curled Rollout should do damage at all")
			p.atLeast(curled, 3*plain/2, "Defense Curl should double Rollout's Base Power")
			p.atMost(curled, 5*plain/2, "Defense Curl should only double it")
		})

		g.it("should not be affected by Parental Bond", func(p *ps) {
			// Parental Bond adds a second, weaker hit; Rollout is exempt. With
			// no Base Power hook the exemption is read as "the same single
			// Rollout does the same damage with the ability as without", so the
			// case plays the same one turn twice, on the same seed.
			p.battle(
				team{{Species: "Shuckle", Ability: "compoundeyes", Moves: mv(id)}},
				team{{Species: "Steelix", As: "Golduck", Ability: "battlearmor", Moves: mv("splash")}},
			)
			before := p.foe().HP
			p.makeChoices("move "+id, "move splash")
			plain := before - p.foe().HP

			p.battle(
				team{{Species: "Shuckle", Ability: "parentalbond", Moves: mv(id)}},
				team{{Species: "Steelix", As: "Golduck", Ability: "battlearmor", Moves: mv("splash")}},
			)
			before = p.foe().HP
			p.makeChoices("move "+id, "move splash")
			bonded := before - p.foe().HP

			p.atLeast(plain, 1, "the plain Rollout should do damage at all")
			p.equal(bonded, plain, "Parental Bond should not add a hit to Rollout")
		})

		describe(t, "Rollout Storage glitch (Gen 7 / Gen 8DLC1)", func(g *psg) {
			g.skip("should delay the Rollout multiplier when hitting Disguise or Ice Face", "gen 7 mechanics")
			g.skip("should delay the Rollout multiplier when hitting multiple Disguise or Ice Face", "gen 7 mechanics")
			g.skip("should use the move's default BP when applying the modifier", "gen 7 mechanics")
			g.skip("should only apply the Rollout Storage boost to the first target of a spread move", "gen 7 mechanics")
		})
	}

	describe(t, "Ice Ball", func(g *psg) { block(t, g, "iceball") })
	describe(t, "Rollout", func(g *psg) { block(t, g, "rollout") })
}
