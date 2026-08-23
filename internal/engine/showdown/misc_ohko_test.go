//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/ohko.js.
//
// Upstream pins the 30% accuracy roll with `forceRandomChance: true` so Horn
// Drill always connects. There is no such hook here, so both live cases become
// g.itRate sweeps, which states the same thing without picking a lucky seed:
//
//   - "should faint the target" measures the connect rate against Horn Drill's
//     30% accuracy and, on every seed, asserts the all-or-nothing shape — the
//     target is either untouched (missed) or fainted. A partial-damage result
//     would fail the case whatever the rate came out at.
//   - "should faint the target's substitute" asserts the target *never* faints
//     over the sweep, which is the whole claim: the one-hit KO is spent on the
//     substitute.
//
// Breloom is a body with nothing to do with the mechanic; Hypno stands in for
// it because it is neither Ghost (Horn Drill is Normal) nor Sturdy, which are
// the only two things that would change the answer. Upstream's `sleeptalk`
// filler is not in this dataset, so `splash` is the do-nothing move.
//
// The three older-generation describe blocks skip whole. Two of their cases
// are `it.skip` upstream as well and are noted as such.

func TestMiscOHKO(t *testing.T) {
	describe(t, "OHKO moves", func(g *psg) {
		g.itRate("should faint the target", 0.2, 0.4, 300, func(p *ps) bool {
			p.battle(
				team{{Species: "Rhydon", Moves: mv("horndrill")}},
				team{{Species: "Breloom", As: "Hypno", Moves: mv("splash")}},
			)
			p.turn()
			foe := p.foe()
			p.ok(foe.Fainted || foe.HP == foe.MaxHP,
				"an OHKO move either faints the target outright or does nothing")
			return foe.Fainted
		})

		g.itRate("should faint the target's substitute", 0.0, 0.0, 300, func(p *ps) bool {
			p.battle(
				team{{Species: "Rhydon", Moves: mv("horndrill", "splash")}},
				team{{Species: "Breloom", As: "Hypno", Moves: mv("substitute", "splash")}},
			)
			p.makeChoices("move splash", "move substitute")
			p.makeChoices("move horndrill", "move splash")
			foe := p.foe()
			if p.logCount("substitute faded") > 0 {
				p.ok(!foe.Fainted, "the one-hit KO should have been spent on the substitute")
			}
			return foe.Fainted
		})
	})

	describe(t, "[Gen 3]", func(g *psg) {
		g.skip("should deal damage equal to the target's HP", "gen 3 mechanics")
	})

	describe(t, "[Gen 2]", func(g *psg) {
		g.skip("should faint the target's substitute", "gen 2 mechanics")
		// Already `it.skip` upstream.
		g.skip("should produce a super-effective message", "gen 2 mechanics")
	})

	describe(t, "[Gen 1]", func(g *psg) {
		g.skip("should faint the target's substitute", "gen 1 mechanics")
		// Already `it.skip` upstream.
		g.skip("should produce a super-effective message", "gen 1 mechanics")
		g.skip("should fail if the target has a higher speed", "gen 1 mechanics")
	})
}
