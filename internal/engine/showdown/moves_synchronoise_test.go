//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/synchronoise.js.
//
// Gardevoir becomes Mr. Mime, which is Psychic/Fairy exactly as Gardevoir is —
// the typing is the whole subject of both cases, so the substitution has to
// carry it. Its ability is stripped so Soundproof cannot come into a case
// about a sound move. Granbull becomes Clefable, a pure Fairy body that shares
// the Fairy half and not the Psychic one; Caterpie takes its stand-in row
// (Butterfree), which shares neither.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.

func TestMovesSynchronoise(t *testing.T) {
	describe(t, "Synchronoise", func(g *psg) {
		g.it("should damage Pokemon that share a type with the user", func(p *ps) {
			p.battle(
				team{{Species: "Gardevoir", As: "Mr. Mime", Ability: "noability",
					Moves: mv("synchronoise")}},
				team{{Species: "Granbull", As: "Clefable", Moves: mv("splash")}},
			)
			p.turn()
			p.damaged(p.foe(), "the two share the Fairy type, so Synchronoise should connect")
		})

		g.it("should not damage Pokemon that do not share a type with the user", func(p *ps) {
			p.battle(
				team{{Species: "Gardevoir", As: "Mr. Mime", Ability: "noability",
					Moves: mv("synchronoise")}},
				team{{Species: "Caterpie", Moves: mv("splash")}},
			)
			p.turn()
			p.fullHP(p.foe(), "the two share no type, so Synchronoise should do nothing")
		})
	})
}
