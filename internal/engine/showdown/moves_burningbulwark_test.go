//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/burningbulwark.js.
//
// Burning Bulwark is not in this dataset, so every case reports the missing
// move rather than the burn it is supposed to apply. They are written out in
// full so they measure the right thing the day it lands.
//
// Species. Entei takes its stand-in row (Arcanine, fire). Gallade has no row
// and becomes Machamp — a Fighting physical attacker, which is all the first
// two cases need; the Psychic half is not preserved and nothing turns on it.
// Ogerpon-Wellspring becomes Vaporeon, which carries Water Absorb natively as
// upstream's set does; the Grass half is lost and no case reads it.
//
// Moves. Ivy Cudgel is not in this dataset. The third case needs it only to be
// a move that does not make contact, so Water Gun stands in — same question,
// and the case then reports Burning Bulwark alone rather than two absences at
// once.

func TestMovesBurningBulwark(t *testing.T) {
	describe(t, "Burning Bulwark", func(g *psg) {
		g.it("should burn the user of a contact move", func(p *ps) {
			p.battle(
				team{{Species: "Gallade", As: "Machamp", Ability: "justified", Moves: mv("tackle")}},
				team{{Species: "Entei", Ability: "innerfocus", Moves: mv("burningbulwark")}},
			)
			p.turn()
			p.hasStatus(p.mine(), "brn", "Gallade should be burned when using contact move")
		})

		g.it("should not burn the user of a contact move if user has protective pads", func(p *ps) {
			p.battle(
				team{{Species: "Gallade", As: "Machamp", Item: "protectivepads",
					Ability: "justified", Moves: mv("tackle")}},
				team{{Species: "Entei", Ability: "innerfocus", Moves: mv("burningbulwark")}},
			)
			p.turn()
			p.noStatus(p.mine(),
				"Gallade should not be burned when using contact move due to protective pads")
		})

		g.it("should not burn the user of a non-contact move", func(p *ps) {
			p.battle(
				team{{Species: "Ogerpon-Wellspring", As: "Vaporeon", Ability: "waterabsorb",
					Moves: mv("watergun")}},
				team{{Species: "Entei", Ability: "innerfocus", Moves: mv("burningbulwark")}},
			)
			p.turn()
			p.noStatus(p.mine(),
				"Ogerpon-Wellspring should not be burned when using non-contact move")
		})
	})
}
