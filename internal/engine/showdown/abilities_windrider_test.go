//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/windrider.js.
//
// Wind Rider is not in this ability set, which is what four of these five
// cases are here to record, so they are ported rather than skipped. None of
// the species exist in this dex and none has a stand-in row, but every one of
// them is a body:
//
//	brambleghast -> Gengar     keeps the Ghost half; only the ability matters
//	veluza       -> Pinsir     the dex's Mold Breaker body
//	flittle      -> Wigglytuff the dex's Frisk body
//	pelipper     -> Gyarados   Water/Flying, carrying Tailwind
//
// Azumarill and Magikarp go through the stand-in table. Sleep Talk is not in
// this dataset and is only filler here, so Splash takes its place.
//
// The fourth case is doubles: it is about an ally putting Tailwind up, and
// there is no ally slot. The fifth is the same mechanic reached through a
// switch-in and ports as singles unchanged.

func TestAbilitiesWindRider(t *testing.T) {
	windRiderMon := set{Species: "brambleghast", As: "Gengar", Ability: "windrider", Moves: mv("splash")}

	describe(t, "Wind Rider", func(g *psg) {
		g.it("should nullify Wind attacks and boost the target's Attack by 1", func(p *ps) {
			p.battle(
				team{{Species: "azumarill", Item: "widelens", Ability: "thickfat", Moves: mv("icywind")}},
				team{windRiderMon},
			)
			p.turn()
			brambleghast := p.foe()
			p.fullHP(brambleghast, "Icy Wind should have been nullified")
			p.statStage(brambleghast, "spe", 0, "a nullified Icy Wind drops nothing")
			p.statStage(brambleghast, "atk", 1, "Wind Rider should have raised Attack instead")
		})

		g.it("should be bypassed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "veluza", As: "Pinsir", Item: "widelens", Ability: "moldbreaker", Moves: mv("icywind")}},
				team{windRiderMon},
			)
			p.turn()
			p.damaged(p.foe(), "Mold Breaker should have punched through Wind Rider")
			p.statStage(p.foe(), "atk", 0, "and left no Attack boost behind")
		})

		g.it("should not interact with Sandstorm", func(p *ps) {
			p.battle(
				team{{Species: "flittle", As: "Wigglytuff", Ability: "frisk", Moves: mv("sandstorm")}},
				team{windRiderMon},
			)
			p.turn()
			p.equal(p.weather(), "sandstorm", "the weather move should still resolve")
			p.statStage(p.foe(), "atk", 0, "Sandstorm is not a wind move")
		})

		g.skip("should activate when Tailwind is used on the Pokemon's side", "doubles")

		g.it("should activate on switch-in if Tailwind is active on the Pokemon's side", func(p *ps) {
			p.battle(
				team{{Species: "magikarp", Ability: "swiftswim", Moves: mv("splash")}},
				team{
					{Species: "pelipper", As: "Gyarados", Ability: "keeneye", Moves: mv("tailwind")},
					windRiderMon,
				},
			)
			p.turn()
			p.makeChoices("", "switch 2")
			p.statStage(p.foe(), "atk", 1, "switching into a standing Tailwind should trigger Wind Rider")
		})
	})
}
