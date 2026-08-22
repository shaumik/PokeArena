//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/batonpass.js.
//
// Showdown asks for the replacement as a separate choice after Baton Pass
// resolves; this engine's applySelfSwitch brings the teammate in inside the
// move itself, with no replacement phase, so the extra
// `battle.makeChoices('switch wingull')` has no counterpart and is dropped.
// The default target is the lowest-indexed live teammate, which on a team of
// two is the one upstream names.
//
// The second case is two battles upstream, the second of them doubles. Only
// the singles half is ported; the doubles half — Baton Pass with a live but
// already-active ally — has no singles reading.
//
// Wingull is a body to receive the pass and Pidgeot is one; Wynaut and Pichu
// come through the shared table as Hypno and Raichu. No Guard stays on the
// seeder so Leech Seed cannot miss.

func TestMovesBatonPass(t *testing.T) {
	describe(t, "Baton Pass", func(g *psg) {
		g.it("should switch the user out, passing with it a variety of effects", func(p *ps) {
			p.battle(
				team{
					{Species: "wynaut", Moves: mv("focusenergy", "substitute", "swordsdance", "batonpass")},
					{Species: "wingull", As: "Pidgeot", Moves: mv("splash")},
				},
				team{{Species: "pichu", Ability: "noguard", Moves: mv("leechseed")}},
			)
			p.makeChoices("move focusenergy", "")
			p.makeChoices("move substitute", "")
			p.makeChoices("move swordsdance", "")
			p.makeChoices("move batonpass", "")

			p.species(p.mine(), "Pidgeot", "Baton Pass should have brought the benched Pokemon in")
			p.statStage(p.mine(), "atk", 2, "Swords Dance's boost should have come across")
			p.ok(p.mine().Volatiles.FocusEnergy, "Focus Energy should have come across")
			p.ok(p.mine().Volatiles.Substitute != nil, "the Substitute should have come across")
			p.ok(p.mine().Volatiles.LeechSeed != nil, "Leech Seed should have come across")
		})

		g.it("should fail to switch the user out if no Pokemon can be switched in", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Moves: mv("batonpass")}},
				team{{Species: "pichu", Moves: mv("swordsdance")}},
			)
			p.turn()
			p.logHas("But it failed!", "Baton Pass with nobody on the bench should fail")
		})
	})
}
