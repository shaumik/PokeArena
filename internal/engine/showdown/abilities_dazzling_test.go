//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/dazzling.js.
//
// Dazzling is not one of the abilities this engine models, and neither is
// Prankster, which is what gives the moves in both cases the priority Dazzling
// is supposed to refuse. Both are reported at the fixture.
//
// Bruxish is not in the dex and has no stand-in row; Slowbro is built instead
// and keeps Bruxish's water/psychic typing exactly. Sableye resolves through
// the stand-in table to Gengar, which the row says keeps ghost and not
// Prankster — the ability is set on the set rather than inherited from the
// species, so that is fine here.
//
// The second case reads a volatile off the Pokemon (`volatiles['perishsong']`);
// this engine has no such accessor in the harness, so the equivalent assertion
// is that Perish Song never announced itself.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.

func TestAbilitiesDazzling(t *testing.T) {
	describe(t, "Dazzling", func(g *psg) {
		g.it("should block moves with positive priority", func(p *ps) {
			p.battle(
				team{{Species: "Sableye", Ability: "prankster", Moves: mv("taunt")}},
				team{{Species: "Bruxish", As: "Slowbro", Ability: "dazzling", Moves: mv("swordsdance")}},
			)
			p.makeChoices("move taunt", "move swordsdance")
			p.statStage(p.foe(), "atk", 2, "Dazzling should have refused the Prankster Taunt, leaving Swords Dance free")
		})

		g.it("should not block moves that target all Pokemon, except Perish Song, Rototiller, and Flower Shield", func(p *ps) {
			p.battle(
				team{{Species: "Bruxish", As: "Slowbro", Ability: "dazzling", Moves: mv("swordsdance", "splash")}},
				team{{Species: "Mew", Ability: "prankster", Moves: mv("perishsong", "haze")}},
			)
			p.makeChoices("move swordsdance", "move perishsong")
			p.makeChoices("move splash", "move haze")
			p.statStage(p.mine(), "atk", 0, "Haze targets everything and should not have been blocked")
			p.logLacks("will faint in three turns", "Perish Song is the exception Dazzling does block")
		})
	})
}
