//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/mindblown.js.
//
// Mind Blown is not in this dataset, so all three cases report the missing
// move rather than the recoil they are about. They are written out in full so
// they measure the right thing the day it lands.
//
// Species. Blacephalon has no stand-in row; Moltres is built instead, which
// keeps the Fire typing and nothing else — no case here turns on the Ghost
// half or on Beast Boost. Talonflame likewise becomes Charizard, Fire/Flying
// and fast enough to be airborne before Mind Blown resolves, which is all the
// second case asks of it. Blissey takes its stand-in row and Dugtrio is in the
// dex already.
//
// Moves. Sleep Talk is not in this dataset and is idle here, so it is Splash.
// Memento is absent too and is load-bearing in the third case — it is how
// upstream removes the target before the attacker moves — so that case reports
// two missing moves.
//
// Parental Bond is kept even though it is not modeled: the first case exists
// precisely to say the recoil is charged once per *use* and not once per hit,
// so the doubling ability is the subject rather than filler.

func TestMovesMindBlown(t *testing.T) {
	describe(t, "Mind Blown", func(g *psg) {
		g.it("should deal damage to the user once per use equal to half its max HP, rounded up", func(p *ps) {
			p.battle(
				team{{Species: "Blacephalon", As: "Moltres", Ability: "parentalbond",
					Moves: mv("mindblown")}},
				team{{Species: "Blissey", Ability: "healer", Moves: mv("splash")}},
			)
			mon := p.mine()
			p.hurtsBy(mon, (mon.MaxHP+1)/2, func() { p.turn() },
				"Mind Blown should cost half the user's max HP, rounded up, once")
		})

		g.it("should deal damage to the user even if it misses", func(p *ps) {
			p.battle(
				team{{Species: "Blacephalon", As: "Moltres", Moves: mv("mindblown")}},
				team{{Species: "Talonflame", As: "Charizard", Moves: mv("fly")}},
			)
			mon := p.mine()
			p.hurtsBy(mon, (mon.MaxHP+1)/2, func() { p.turn() },
				"a Mind Blown that cannot reach its target still costs the user half its max HP")
		})

		g.it("should not deal damage to the user if there is no target", func(p *ps) {
			// Singles upstream too: the second Dugtrio is only the replacement
			// for the one Memento removes.
			p.battle(
				team{
					{Species: "Dugtrio", Ability: "sandveil", Moves: mv("memento")},
					{Species: "Dugtrio", Ability: "sandveil", Moves: mv("memento")},
				},
				team{{Species: "Blacephalon", As: "Moltres", Ability: "limber",
					Moves: mv("mindblown")}},
			)
			p.turn()
			p.fullHP(p.foe(), "with the target already gone the move should not resolve at all")
		})
	})
}
