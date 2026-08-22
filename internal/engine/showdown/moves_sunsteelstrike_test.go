//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/sunsteelstrike.js.
//
// Sunsteel Strike is not in this dataset, so the ported case reports the
// missing move rather than the mould-breaking it is about.
//
// Metagross becomes Magneton, which keeps the Steel half; the Psychic half is
// lost and Clear Body — the ability under test — is set explicitly, as
// upstream sets it. Goodra becomes Dragonite, a Dragon body of no other
// consequence here.
//
// Gooey is not in this dataset either, and it is load-bearing: with nothing
// trying to drop the attacker's Speed, "the Speed stage is still zero" would
// be true for the wrong reason. The port keeps the ability named so the run
// reports it rather than passing silently.
//
// The Gen 7 case skips: it exists to pin the older rule that Sunsteel Strike
// ignored the user's own ability too, and this engine models one generation.

func TestMovesSunsteelStrike(t *testing.T) {
	describe(t, "Sunsteel Strike", func(g *psg) {
		g.it("should not ignore the user's own Ability", func(p *ps) {
			p.battle(
				team{{Species: "metagross", As: "Magneton", Ability: "clearbody",
					Moves: mv("sunsteelstrike")}},
				team{{Species: "goodra", As: "Dragonite", Ability: "gooey", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.mine(), "spe", 0,
				"the user's own Clear Body should still have refused Gooey's Speed drop")
		})

		g.skip("should ignore the user's own Ability (Gen 7)", "gen 7 mechanics")
	})
}
