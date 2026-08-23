//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/iceface.js.
//
// Ice Face is not in this ability set. Three of the four cases are stated as
// assertions about which forme is out (Eiscue vs Eiscue-Noice), and forme
// changes are not modeled at all, so those skip — a substituted body would
// answer "the species did not change" for a reason that has nothing to do with
// the ability. The first case is different: it is stated entirely in HP, so it
// ports, and it records the real gap.
//
// Eiscue has no stand-in row. What the first case needs from it is an Ice-type
// body bulky enough to sit through six turns of Mewtwo, which Lapras is;
// Eiscue's Ice typing matters only because it keeps hail from chipping the
// side under test.

func TestAbilitiesIceFace(t *testing.T) {
	describe(t, "Ice Face", func(g *psg) {
		g.it(`should block damage from one physical move per Hail`, func(p *ps) {
			p.battle(
				team{{Species: "Eiscue", As: "Lapras", Ability: "iceface", Moves: mv("splash")}},
				team{{Species: "Mewtwo", Ability: "pressure", Moves: mv("tackle", "watergun", "hail")}},
			)
			eiscue := p.mine()
			hp := func() any { return eiscue.HP }

			p.hurts(eiscue, func() { p.makeChoices("", "move watergun") },
				"Ice Face does not cover special moves")
			p.constant(hp, func() { p.turn() },
				"the first physical hit should be blocked")
			p.hurts(eiscue, func() { p.turn() },
				"the second physical hit lands, the ice is already gone")
			p.constant(hp, func() { p.makeChoices("", "move hail") },
				"hail rebuilds the ice and does not chip an Ice-type")
			p.constant(hp, func() { p.turn() },
				"the rebuilt ice should block one more physical hit")
			p.hurts(eiscue, func() { p.turn() },
				"and only one")
		})

		g.skip(`should not work while Transformed`,
			"Transform is not modeled and Eiscue-Noice is a forme")
		g.skip(`should not trigger if the Pokemon was KOed by Max Hailstorm`,
			"Dynamax")
		g.skip(`should reform Ice Face on switchin after all entrance Abilities occur`,
			"formes: the case is stated as an assertion that Eiscue-Noice is out")
	})
}
