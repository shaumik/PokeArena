//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/chloroblast.js.
//
// Chloroblast is not in this 538-move dataset, so every case here fails at team
// construction naming the move. They are written out rather than skipped
// because that absence is the finding.
//
// Species. Hisuian Electrode is Electric/Grass and has no stand-in row.
// Exeggutor is built instead: it keeps the Grass half, which is the half
// Chloroblast's STAB comes off, and unlike Electrode it is slow enough for the
// Fly case below to work without Gale Wings. Talonflame becomes Moltres —
// Fire/Flying, and fast enough to leave the ground before the Chloroblast user
// moves, which is the ordering Gale Wings supplies upstream. Gale Wings itself
// is not modeled here and is not set, since priority is not what the case
// needs.
//
// The Reckless case loses its assertion. Upstream states it as a damage band,
// which does not transfer from level 100, and the boost it is looking for
// (x1.2) is smaller than this engine's 85-100% damage roll, so no seed-
// independent comparison can separate a boosted Chloroblast from an unboosted
// one. What remains is that the move connects at all; the magnitude half of the
// case is not asserted, and is noted here rather than faked.

func TestMovesChloroblast(t *testing.T) {
	describe(t, "Chloroblast", func(g *psg) {
		g.it("should deal recoil damage to the user equal to half its max HP, rounded up", func(p *ps) {
			p.battle(
				team{{Species: "Electrode-Hisui", As: "Exeggutor", Item: "widelens", Moves: mv("chloroblast")}},
				team{{Species: "Blissey", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			user := p.mine()
			p.hurtsBy(user, (user.MaxHP+1)/2, func() { p.turn() },
				"Chloroblast should cost the user half its max HP, rounded up")
		})

		g.skip("should not crash in Gen 4 Custom Game", "gen 4 mechanics")

		g.it("should not deal recoil damage to the user if it misses or is blocked by Protect", func(p *ps) {
			p.battle(
				team{{Species: "Electrode-Hisui", As: "Exeggutor", Item: "widelens", Moves: mv("chloroblast", "protect")}},
				team{{Species: "Talonflame", As: "Moltres", Moves: mv("fly", "protect")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move chloroblast", "move fly")
			p.makeChoices("move protect", "default")
			p.makeChoices("move chloroblast", "move protect")
			p.fullHP(p.mine(), "a Chloroblast that missed and one that was Protected should both cost nothing")
		})

		g.it("should have its recoil damage negated by Rock Head", func(p *ps) {
			p.battle(
				team{{Species: "Electrode-Hisui", As: "Exeggutor", Ability: "rockhead", Item: "widelens", Moves: mv("chloroblast")}},
				team{{Species: "Blissey", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.fullHP(p.mine(), "Rock Head should cancel Chloroblast's recoil")
		})

		g.it("should not have its base power boosted by Reckless", func(p *ps) {
			p.battle(
				team{{Species: "Electrode-Hisui", As: "Exeggutor", Ability: "reckless", Item: "widelens", Moves: mv("chloroblast")}},
				team{{Species: "Blissey", Ability: "shellarmor", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.damaged(p.foe(), "Chloroblast should have landed")
		})
	})
}
