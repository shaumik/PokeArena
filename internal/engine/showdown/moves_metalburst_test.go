//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/metalburst.js.
//
// Two of the four cases are doubles — one about which of two attackers Metal
// Burst answers, one about redirection — and skip. The two singles cases are
// written out even though Metal Burst is not in this dataset: that absence is
// the finding.
//
// Sleep Talk is not in this dataset either. The first case keeps it, because
// "Metal Burst called by a submove still runs its conditions" is what the case
// is about and dropping the caller would drop the case. The third only uses it
// as a way to spend a turn, so Splash stands in there.
//
// Species. Golem, Snorlax and Munchlax (which resolves to Snorlax) are all in
// the dex. Breloom has no stand-in row; Vileplume is built instead, which keeps
// a Grass body and, more to the point, keeps the two moves the case needs to
// come from the same Pokemon.
//
// Upstream compares against `dex.moves.get('sonicboom').damage * 1.5`; Sonic
// Boom is 20 fixed damage, so the figure is written out as 30 here.

func TestMovesMetalBurst(t *testing.T) {
	describe(t, "Metal Burst", func(g *psg) {
		g.it("should run conditions for submove", func(p *ps) {
			p.battle(
				team{
					{Species: "golem", Moves: mv("splash")},
					{Species: "snorlax", Moves: mv("sleeptalk", "metalburst")},
				},
				team{{Species: "breloom", As: "Vileplume", Moves: mv("spore", "sonicboom")}},
			)
			p.makeChoices("switch 2", "move spore")
			p.makeChoices("move sleeptalk", "move sonicboom")
			p.equal(p.foe().MaxHP-p.foe().HP, 30,
				"Metal Burst should return 1.5x the 20 damage Sonic Boom dealt")
		})

		g.skip("should target the opposing Pokemon that hit the user with an attack most recently that turn",
			"doubles")

		g.it("should deal 1 damage if the user was hit by a 0-damage attack", func(p *ps) {
			p.battle(
				team{{Species: "munchlax", Ability: "sturdy", Moves: mv("splash", "metalburst")}},
				team{{Species: "breloom", As: "Vileplume", Moves: mv("closecombat", "falseswipe")}},
			)
			p.makeChoices("move splash", "move closecombat")
			p.makeChoices("move metalburst", "move falseswipe")
			p.equal(p.foe().MaxHP-p.foe().HP, 1,
				"a 0-damage hit should still be answered, for 1 HP")
		})

		g.skip("should be subject to redirection", "doubles")
	})
}
