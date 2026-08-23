//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/ragefist.js.
//
// Rage Fist is in this dataset, but nothing like Showdown's timesAttacked
// counter is, so "how many times has the user been hit" is not readable from a
// port. Every case is therefore restated as a damage comparison between two
// fixtures that differ only in the hits the user took, and the ratio between
// them stands in for the base-power figure upstream asserts. The absolute
// windows upstream uses would not transfer regardless: it plays at level 100
// and this engine is fixed at 50.
//
// Umbreon is upstream's Shell Armor body, there so that a crit cannot move the
// damage figure. Lapras carries Shell Armor natively and is bulky enough to sit
// through the whole exchange; the Dark typing that made Umbreon resist a Ghost
// move is lost, which scales both sides of every comparison equally and so
// leaves the ratios alone. Sleep Talk is not in this dataset and is inert filler
// here, so Splash stands in for it.
//
// The two doubles cases are not ported.

func TestMovesRageFist(t *testing.T) {
	describe(t, "Rage Fist", func(g *psg) {
		g.it("should increase BP by 50 each time the user is hit", func(p *ps) {
			p.battle(
				team{{Species: "Primeape", Moves: mv("ragefist")}},
				team{{Species: "Umbreon", As: "Lapras", Ability: "shellarmor", Moves: mv("tackle")}},
			)
			if p.state() == nil {
				return
			}
			foe := p.foe()
			p.turn()
			atFifty := foe.MaxHP - foe.HP
			p.atLeast(atFifty, 1, "the first Rage Fist should have connected at all")

			before := foe.HP
			p.turn()
			afterOneHit := before - foe.HP
			p.bounded(afterOneHit*100, atFifty*150, atFifty*250,
				"one hit taken should take Rage Fist from 50 BP to 100")
		})

		g.it("should not increase BP after being hit by status moves", func(p *ps) {
			p.battle(
				team{{Species: "Primeape", Moves: mv("ragefist")}},
				team{{Species: "Umbreon", As: "Lapras", Ability: "shellarmor", Moves: mv("taunt")}},
			)
			if p.state() == nil {
				return
			}
			foe := p.foe()
			p.turn()
			first := foe.MaxHP - foe.HP
			p.atLeast(first, 1, "the first Rage Fist should have connected at all")

			before := foe.HP
			p.turn()
			second := before - foe.HP
			p.bounded(second*100, first*80, first*125,
				"a status move is not a hit, so Rage Fist should still be 50 BP")
		})

		g.it("should increase BP after each hit of multi-hit moves", func(p *ps) {
			// Double Hit lands twice, so the follow-up Rage Fist should be
			// 150 BP against the 50 BP of the same fixture left unhit.
			p.battle(
				team{{Species: "Primeape", Ability: "noguard", Moves: mv("splash", "ragefist")}},
				team{{Species: "Umbreon", As: "Lapras", Ability: "shellarmor", Moves: mv("doublehit", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move doublehit")
			hit := p.foe()
			before := hit.HP
			p.makeChoices("move ragefist", "move splash")
			afterTwoHits := before - hit.HP

			p.battle(
				team{{Species: "Primeape", Ability: "noguard", Moves: mv("splash", "ragefist")}},
				team{{Species: "Umbreon", As: "Lapras", Ability: "shellarmor", Moves: mv("doublehit", "splash")}},
			)
			p.makeChoices("move splash", "move splash")
			clean := p.foe()
			beforeClean := clean.HP
			p.makeChoices("move ragefist", "move splash")
			unhit := beforeClean - clean.HP

			p.atLeast(unhit, 1, "the baseline Rage Fist should have connected at all")
			p.bounded(afterTwoHits*100, unhit*220, unhit*390,
				"both hits of Double Hit should count, trebling Rage Fist's power")
		})

		g.it("should use user's own number of times hit when called by another move", func(p *ps) {
			// Copycat is not in this dataset and is the subject here — the case
			// exists to check whose hit count the copy reads — so it stays and
			// the missing-move failure is the finding.
			p.battle(
				team{{Species: "Primeape", Moves: mv("ragefist")}},
				team{{Species: "Umbreon", As: "Lapras", Ability: "shellarmor", Moves: mv("copycat")}},
			)
			if p.state() == nil {
				return
			}
			mine := p.mine()
			p.turn()
			copied := mine.MaxHP - mine.HP

			// The same body throwing Rage Fist itself, having been hit by
			// nothing: 50 BP, against the copy's 100.
			p.battle(
				team{{Species: "Primeape", Moves: mv("splash")}},
				team{{Species: "Umbreon", As: "Lapras", Ability: "shellarmor", Moves: mv("ragefist")}},
			)
			clean := p.mine()
			p.turn()
			unhit := clean.MaxHP - clean.HP

			p.atLeast(unhit, 1, "the baseline Rage Fist should have connected at all")
			p.bounded(copied*100, unhit*150, unhit*250,
				"the copy should read the copier's own hit count, not the original user's")
		})

		g.it("should not increase BP when the user's Substitute is damaged or broken", func(p *ps) {
			// Dragon Rage's fixed 40 damage goes into the Substitute. Upstream
			// reads timesAttacked directly; here the same claim is that the
			// Rage Fist afterwards is no stronger than one from a user whose
			// Substitute was never touched.
			p.battle(
				team{{Species: "Primeape", Moves: mv("substitute", "ragefist")}},
				team{{Species: "Umbreon", As: "Lapras", Ability: "shellarmor", Moves: mv("dragonrage", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move substitute", "move dragonrage")
			hit := p.foe()
			before := hit.HP
			p.makeChoices("move ragefist", "move splash")
			behindSub := before - hit.HP

			p.battle(
				team{{Species: "Primeape", Moves: mv("substitute", "ragefist")}},
				team{{Species: "Umbreon", As: "Lapras", Ability: "shellarmor", Moves: mv("dragonrage", "splash")}},
			)
			p.makeChoices("move substitute", "move splash")
			clean := p.foe()
			beforeClean := clean.HP
			p.makeChoices("move ragefist", "move splash")
			untouched := beforeClean - clean.HP

			p.atLeast(untouched, 1, "the baseline Rage Fist should have connected at all")
			p.bounded(behindSub*100, untouched*80, untouched*125,
				"a hit absorbed by the Substitute is not a hit on the user")
		})

		g.skip("should not increase BP when healed by an ally's Pollen Puff", "doubles")
		g.skip("should increase BP when hit by Dragon Darts", "doubles")
	})
}
