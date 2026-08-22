//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/gravity.js.
//
// Species. Aron has no stand-in row and is only the Earth Power user; Golem
// carries Sturdy natively and gives Earth Power its Ground STAB, and neither
// changes whether the target is grounded. Rotom likewise has no row and is
// built as Weezing, which carries Levitate natively — the ability the second
// case is entirely about. Spiritomb and Aerodactyl come through the table and
// the dex unchanged.
//
// The last case needs both Z-moves and Gen 7, and skips.

func TestMovesGravity(t *testing.T) {
	describe(t, "Gravity", func(g *psg) {
		g.it("should ground Flying-type Pokemon and remove their Ground immunity", func(p *ps) {
			p.battle(
				team{{Species: "Aerodactyl", Ability: "pressure", Moves: mv("gravity")}},
				team{{Species: "Aron", As: "Golem", Ability: "sturdy", Moves: mv("earthpower")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move gravity", "move earthpower")
			p.damaged(p.mine(), "Gravity should ground a Flying-type, so Earth Power connects")
		})

		g.it("should ground Pokemon with Levitate and remove their Ground immunity", func(p *ps) {
			p.battle(
				team{{Species: "Rotom", As: "Weezing", Ability: "levitate", Moves: mv("gravity")}},
				team{{Species: "Aron", As: "Golem", Ability: "sturdy", Moves: mv("earthpower")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move gravity", "move earthpower")
			p.damaged(p.mine(), "Gravity should ground a Levitate holder, so Earth Power connects")
		})

		g.it("should interrupt and disable the use of airborne moves", func(p *ps) {
			p.battle(
				team{{Species: "Spiritomb", Ability: "pressure", Moves: mv("gravity")}},
				team{{Species: "Aerodactyl", Ability: "pressure", Moves: mv("fly")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move gravity", "move fly")
			p.isFalse(p.foe().Volatiles.Charging != nil, "Gravity should have knocked the Fly charge out of the air")
			p.cantMove(1, "fly", "Gravity should make Fly unselectable")
		})

		g.skip("should allow the use of Z-moves of Gravity-blocked moves, but only apply their Z-effects", "Z-moves")
	})
}
