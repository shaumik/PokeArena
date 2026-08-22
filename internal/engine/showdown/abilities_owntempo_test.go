//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/owntempo.js.
//
// Adrenaline Orb is not in this item set, and it is load-bearing: the +1 Speed
// the case checks is the Orb reacting to an Intimidate that was refused. So it
// stays in the fixture and the harness reports it, which stops the battle
// before either assertion can be read. That report is the finding.
//
// Smeargle's stand-in Chansey carries Own Tempo only because the port sets it,
// which is what the case wants — the ability is the subject, not the species.
//
// Upstream reads the stat stages straight after createBattle. This engine
// fires a lead's switch-in ability at the top of turn 1 rather than at
// construction, so the port spends one turn of Splash on both sides to get
// Intimidate to happen at all.

func TestAbilitiesOwnTempo(t *testing.T) {
	describe(t, "Own Tempo", func(g *psg) {
		g.it("should block Intimidate", func(p *ps) {
			p.battle(
				team{{Species: "Gyarados", Ability: "intimidate", Moves: mv("splash")}},
				team{{Species: "Smeargle", Ability: "own tempo", Item: "adrenaline orb", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.foe(), "atk", 0, "Own Tempo should have refused the Attack drop")
			p.statStage(p.foe(), "spe", 1, "the refused Intimidate should have set off the Adrenaline Orb")
		})
	})
}
