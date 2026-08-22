//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/kingsshield.js.
//
// King's Shield is not in this dataset, so the two live cases here stop at
// "move kingsshield is not in this dataset". They are written out anyway: if
// the move is ever added, they say what it has to do.
//
// The Gen 7 case and the whole "King's Shield [Gen 6]" block skip — this
// engine models one generation, and both are about how large the Attack drop
// used to be and whether a Fighting move immune to the shield's holder still
// paid it.
//
// Substitutions. Gallade is here only as a Fighting body with a contact move,
// which Machamp supplies. Aegislash is harder: its Ghost typing is load-
// bearing, because the third case is precisely "a Fighting move that cannot
// touch a Ghost still loses Attack", so Gengar builds instead and keeps that
// immunity. Stance Change is dropped rather than named: it is not in this
// ability set, and leaving it on the fixture would report an unmodeled ability
// and bury the King's Shield finding underneath it. Nothing in these cases
// depends on the forme change.

func TestMovesKingsShield(t *testing.T) {
	describe(t, "King's Shield", func(g *psg) {
		g.skip("should lower the Atk of a contactor by 2 in Gen 7", "gen 7 mechanics")

		g.it("should lower the Atk of a contactor by 1 in Gen 8", func(p *ps) {
			p.battle(
				team{{Species: "Gallade", As: "Machamp", Ability: "justified", Moves: mv("zenheadbutt")}},
				team{{Species: "Aegislash", As: "Gengar", Ability: "noability", Moves: mv("kingsshield")}},
			)
			p.makeChoices("move zenheadbutt", "move kingsshield")
			p.statStage(p.mine(), "atk", -1, "King's Shield should cost a contact attacker one Attack stage")
		})

		g.it("should lower the Atk of a contact-move attacker in 2 levels even if immune", func(p *ps) {
			p.battle(
				team{{Species: "Gallade", As: "Machamp", Ability: "justified", Moves: mv("drainpunch")}},
				team{{Species: "Aegislash", As: "Gengar", Ability: "noability", Moves: mv("kingsshield")}},
			)
			p.makeChoices("move drainpunch", "move kingsshield")
			p.statStage(p.mine(), "atk", -1,
				"a Fighting move a Ghost is immune to should still pay King's Shield's Attack drop")
		})
	})

	describe(t, "King's Shield [Gen 6]", func(g *psg) {
		g.skip("should not lower the Atk of a contact-move attacker if immune", "gen 6 mechanics")
	})
}
