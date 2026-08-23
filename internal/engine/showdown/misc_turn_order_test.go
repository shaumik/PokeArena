//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/turn-order.js.
//
// Two of the six blocks come across.
//
// Both Mega Evolution blocks skip whole: every case turns on the `mega` /
// `ultra` choice suffix, and neither the forme change nor the mid-turn ability
// swap it causes exists here. The second block is additionally gen 6.
//
// `Pokemon Speed` is a doubles pair — the mechanic is an ally switching in
// mid-turn and changing the active's Speed before it moves, which has no
// singles shape at all. `Switching in` is gen 7 and turns on primal reversion,
// a forme change.
//
// `Switching out` ports in full. Its subject is that the faster side's switch
// completes — replacement on the field, entry ability resolved — before the
// slower side's switch begins, so the incoming Pokemon never sees the entry
// ability that fired while its predecessor was still out. That is a singles
// mechanic and this engine has both halves of it. Accelgor, Durant and
// Barraskewda are substituted for speed order alone (Electrode clearly outruns
// Scyther at level 50; Seaking is an inert body that either is or is not
// intimidated), Shuckle takes its stand-in and carries the hacked Intimidate
// upstream gives it, and Sleep Talk — filler on all four sets — becomes Splash,
// which this dataset has. Run Away, upstream's marker for "an ability that
// does nothing", becomes noability: this engine registers Run Away but leaves
// it deliberately inert, and the harness reports an inert ability as a finding,
// which would bury the one this case is asking about.
//
// That case also carries one assertion upstream does not: that Intimidate fired
// at all. Without it, an engine that simply never runs entry abilities on a
// mid-turn switch would pass by doing nothing, which is the one outcome a port
// must not report as agreement.
//
// `Speed ties` becomes an itRate. Upstream loops 20 rigged seeds and demands
// each side win at least once; the band here is wide enough to say the same
// thing without turning into a test of how fair the coin is.

func TestMiscTurnOrder(t *testing.T) {
	describe(t, "Mega Evolution", func(g *psg) {
		g.skip("should cause mega ability to affect the order of the turn in which it happens",
			"mega evolution")
		g.skip("should cause an ability copied with Trace by a mega to affect the order of the turn in which it happens",
			"mega evolution")
		g.skip("should cause base ability to not affect the order of the turn in which it happens",
			"mega evolution")
		g.skip("should cause mega forme speed to decide turn order",
			"mega evolution")
		g.skip("should cause ultra forme speed to decide turn order",
			"formes — Ultra Burst is not modeled and Necrozma is not in this 80-species dex")
	})

	describe(t, "Mega Evolution [Gen 6]", func(g *psg) {
		g.skip("should not cause mega ability to affect the order of the turn in which it happens",
			"gen 6 mechanics")
		g.skip("should not cause an ability copied with Trace by a mega to affect the order of the turn in which it happens",
			"gen 6 mechanics")
		g.skip("should cause base ability to affect the order of the turn in which it happens",
			"gen 6 mechanics")
		g.skip("should cause base forme speed to decide turn order",
			"gen 6 mechanics")
	})

	describe(t, "Pokemon Speed", func(g *psg) {
		g.skip("should update dynamically in Gen 8", "doubles")
		g.skip("should NOT update dynamically in Gen 7", "gen 7 mechanics")
	})

	describe(t, "Switching out", func(g *psg) {
		g.it("should happen in order of switch-out's Speed stat", func(p *ps) {
			p.battle(
				team{
					{Species: "Accelgor", As: "Electrode", Ability: "noability", Moves: mv("splash")},
					{Species: "Shuckle", Ability: "intimidate", Moves: mv("splash")},
				},
				team{
					{Species: "Durant", As: "Scyther", Ability: "noability", Moves: mv("splash")},
					{Species: "Barraskewda", As: "Seaking", Ability: "noability", Moves: mv("splash")},
				},
			)
			p.makeChoices("switch 2", "switch 2")
			p.logHas("Intimidate cuts",
				"the switch-in ability has to have fired for this case to measure anything")
			p.statStage(p.foe(), "atk", 0,
				"the slower side's replacement arrives after Intimidate has already resolved")
		})
	})

	describe(t, "Switching in", func(g *psg) {
		g.skip("should trigger events in an order determined by what each Pokemon's speed was when they switched in",
			"gen 7 mechanics — the case turns on primal reversion, which is a forme change")
	})

	describe(t, "Speed ties", func(g *psg) {
		g.itRate("(slow) Perish Song faint order should be random", 0.2, 0.8, 50, func(p *ps) bool {
			// Politoed is not in this dex and has no stand-in row; Poliwrath is
			// the in-dex water body, and the case needs only that both sides are
			// the same species so their Speed ties.
			p.battle(
				team{{Species: "Politoed", As: "Poliwrath", Moves: mv("perishsong")}},
				team{{Species: "Politoed", As: "Poliwrath", Moves: mv("perishsong")}},
			)
			for i := 0; i < 4; i++ {
				p.turn()
			}
			return p.state().Winner == 0
		})
	})
}
