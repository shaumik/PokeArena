//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/perishsong.js.
//
// Weavile has no stand-in row and is used upstream purely as the fastest body
// on the field — the second case turns on it fainting first — so it is built as
// Jolteon, which outspeeds Slowpoke's stand-in Slowbro by the same wide margin.
// Dark and Ice are lost, and neither case reads them.
//
// `sleeptalk` is not in this dataset; `splash` stands in for it.

func TestMovesPerishSong(t *testing.T) {
	describe(t, "Perish Song", func(g *psg) {
		g.skip(`should KO all Pokemon that heard it in 3 turns`, "doubles")

		g.it(`should cause Pokemon to faint by order of Speed`, func(p *ps) {
			p.battle(
				team{{Species: "Weavile", As: "Jolteon", Moves: mv("perishsong")}},
				team{{Species: "Slowpoke", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			for i := 0; i < 4 && !p.state().Ended(); i++ {
				p.turn()
			}
			// Upstream reads battle.winner; here the winning side index is on
			// the state, and 1 is the slower Pokemon's side.
			p.equal(p.state().Winner, 1, "the faster Pokemon should hit zero on the perish count first")
		})

		g.it(`should not affect other Pokemon with the ability Soundproof`, func(p *ps) {
			p.battle(
				team{{Species: "Weavile", As: "Jolteon", Ability: "soundproof", Moves: mv("perishsong")}},
				team{{Species: "Slowpoke", Ability: "soundproof", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.ok(p.mine().Volatiles.PerishSong != nil, "Weavile should have been affected by its own Perish Song")
			p.isFalse(p.foe().Volatiles.PerishSong != nil, "Slowpoke should not have been affected by Perish Song")
		})

		g.skip(`should not affect any Pokemon with the ability Soundproof in Gen 7`, "gen 7 mechanics")
	})
}
