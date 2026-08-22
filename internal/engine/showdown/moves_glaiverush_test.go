//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/glaiverush.js.
//
// Glaive Rush is not in this dataset, so every case here stops at "move
// glaiverush is not in this dataset". They are written out anyway: if the move
// is ever added, they say what it has to do.
//
// Upstream's damage windows are level-100 figures. Both damage cases become a
// comparison inside one battle instead: the same incoming attack measured on a
// turn the user spent on Glaive Rush and on a turn it did not, with the ratio
// asserted. The band allows for the roll landing differently in the two
// figures, so "doubled" survives as [165%, 240%].
//
// Substitutions. Baxcalibur is a physical attacker that outruns each of its
// three foes and survives two hits; Machamp does all three, and its Fighting
// typing keeps every incoming move here neutral where Baxcalibur's Dragon/Ice
// would not. Battle Armor stays on it, as upstream wrote it, so a critical hit
// cannot move a ratio. Skeledirge is a slower special attacker (Vileplume),
// Dondozo a slower bulky body (Snorlax — the shared table's Lapras is faster
// than Machamp, and the case needs Glaive Rush to resolve before Fissure), and
// Tyranitar comes through the table as Golem.
//
// Two incidentals drop out of the third case. Upstream sets Sand Stream and
// then answers it with Safety Goggles; without the ability set, there is no
// sand and nothing for the goggles to do, and the per-turn damage figures stay
// clean. Ice Punch becomes Strength for the same reason: a 10% freeze would
// cost the second measurement entirely. Shore Up is not in this dataset, and
// the user only needed a turn spent on something other than Glaive Rush, so it
// uses Splash.

func TestMovesGlaiveRush(t *testing.T) {
	describe(t, "Glaive Rush", func(g *psg) {
		g.it("should cause the user to take double damage after use", func(p *ps) {
			p.battle(
				team{{Species: "Baxcalibur", As: "Machamp", Ability: "battlearmor", Moves: mv("splash", "glaiverush")}},
				team{{Species: "Skeledirge", As: "Vileplume", Ability: "noability", Moves: mv("shadowball")}},
			)
			me := p.mine()
			before := me.HP
			p.makeChoices("move splash", "move shadowball")
			plain := before - me.HP
			before = me.HP
			p.makeChoices("move glaiverush", "move shadowball")
			doubled := before - me.HP

			p.atLeast(plain, 1, "the control Shadow Ball should have done damage")
			if plain > 0 {
				p.bounded(100*doubled/plain, 165, 240, "Glaive Rush should double what its user takes")
			}
		})

		g.it("should cause moves to never miss the user after use", func(p *ps) {
			p.battle(
				team{{Species: "Baxcalibur", As: "Machamp", Ability: "battlearmor", Moves: mv("glaiverush")}},
				team{{Species: "Dondozo", As: "Snorlax", Ability: "noability", Moves: mv("fissure")}},
			)
			p.turn()
			p.fainted(p.mine(), "even a one-hit KO's accuracy should not save a Pokemon that just used Glaive Rush")
		})

		g.it("should only apply its drawback until the user's next turn", func(p *ps) {
			p.battle(
				team{{Species: "Baxcalibur", As: "Machamp", Ability: "battlearmor", Moves: mv("glaiverush", "splash")}},
				team{{Species: "Tyranitar", Moves: mv("strength")}},
			)
			me := p.mine()
			before := me.HP
			p.makeChoices("move glaiverush", "move strength")
			doubled := before - me.HP
			before = me.HP
			p.makeChoices("move splash", "move strength")
			plain := before - me.HP

			p.atLeast(plain, 1, "the second Strength should have done damage")
			if plain > 0 {
				p.bounded(100*doubled/plain, 165, 240,
					"the drawback should be gone by the time the user next moves")
			}
		})
	})
}
