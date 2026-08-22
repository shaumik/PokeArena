//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/dancer.js.
//
// Dancer is not in this engine's ability set, so the cases that do port are
// expected to be red on the copy — that is the finding. Most of the file is
// doubles, where Dancer's ordering and targeting rules live, and those skip.
//
// Oricorio is not in this dex. Moltres stands in for the Baile forme, which is
// an exact typing match (Fire/Flying), and Zapdos for Pom-Pom (Electric/
// Flying); Dancer is set explicitly on both, so nothing is riding on the
// species' own ability.
//
// Fiery Dance and Revelation Dance are not in this dataset. They are the Dance
// move under test in their cases, so they stay and the missing-move failure is
// recorded. Sleep Talk is likewise absent but was only ever "do nothing"
// there, so Splash replaces it.
//
// The engine emits no log line for Dancer, so the two cases that assert on
// upstream's `|-activate|...|ability: Dancer` line assert on the state a copy
// would have produced instead.

func TestAbilitiesDancer(t *testing.T) {
	describe(t, "Dancer", func(g *psg) {
		g.it("should only copy dance moves used by other Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Oricorio", As: "Moltres", Ability: "dancer", Moves: mv("swordsdance")}},
				team{{Species: "Oricorio", As: "Moltres", Ability: "dancer", Moves: mv("howl")}},
			)
			p.makeChoices("move swordsdance", "move howl")
			p.statStage(p.mine(), "atk", 2, "Howl is not a Dance move, and Dancer never copies the user's own")
			p.statStage(p.foe(), "atk", 3, "Howl plus a copy of the opposing Swords Dance")
		})

		g.skip("should activate in order of lowest to highest raw speed", "doubles")

		g.skip("should activate in order of lowest to highest raw speed inside Trick Room", "doubles")

		g.it("should not copy a move that was blocked by Protect", func(p *ps) {
			p.battle(
				team{{Species: "Oricorio", As: "Moltres", Ability: "dancer", Moves: mv("protect")}},
				team{{Species: "Wynaut", Ability: "dancer", Moves: mv("fierydance")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.fullHP(p.foe(), "a Dance move stopped by Protect should not be copied back into its user")
		})

		g.skip("should not copy Teeter Dance when all targets are confused", "doubles")

		g.skip("should not copy a Dance move that failed for other reasons", "doubles")

		g.skip("should not copy a move that missed",
			"upstream pins the miss with battle.onEvent('Accuracy'); this harness has no rigged-RNG hook, "+
				"and a directed always-miss cannot be re-expressed as a rate")

		g.skip("should copy a move that hit, but did 0 damage",
			"Shedinja is not in this 80-species dex, and nothing in it can be hit for exactly 0 damage")

		g.it("should not activate if the holder fainted", func(p *ps) {
			// Upstream makes the foes faintable with level 1 bodies; level is
			// fixed at 50 here, so they start at 1 HP instead, which is the
			// same arrangement — the move kills the Dancer holder outright.
			p.battle(
				team{{Species: "Oricoriopompom", As: "Zapdos", Ability: "dancer", Moves: mv("revelationdance")}},
				team{
					{Species: "oricorio", As: "Moltres", Ability: "dancer", Moves: mv("splash"), HP: 1},
					{Species: "oricorio", As: "Moltres", Ability: "dancer", Moves: mv("splash"), HP: 1},
				},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			// Upstream reads the absence of the Dancer activation line. This
			// engine narrates nothing for Dancer, so the state stands in: a
			// Dancer that fired would have aimed the copied Revelation Dance
			// back at the user.
			p.fullHP(p.mine(), "a Dancer that fainted to the move should not get to copy it")
		})

		g.skip("should target the user of a Dance move unless it was an ally attacking an opponent", "doubles")

		g.skip("should adopt the target selected by copycat", "doubles")
	})
}
