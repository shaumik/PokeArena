//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/healbell.js.
//
// Only the first case is singles. Dunsparce is a Normal-type body there whose
// job is to catch a paralysis and then sit on the bench while the cure is cast
// from the other slot; Raticate keeps that. Chansey and Nidoking are both in
// the dex, and Sleep Talk — Dunsparce's inert filler — becomes Splash.
//
// Showdown moves the active Pokemon to the front of `side.pokemon`, so its
// `pokemon[0]` after the switch is Chansey and `pokemon[1]` is Dunsparce. This
// engine leaves the team in build order, so the two slots are read the other
// way round. The assertion is the same either way: nobody on the team is left
// with a status.
//
// The Soundproof pair is doubles, and the two remaining cases are multi and
// free-for-all formats. All four turn on Heal Bell reaching Pokemon on a side
// this engine does not have, so none is re-expressible in singles.

func TestMovesHealBell(t *testing.T) {
	describe(t, "Heal Bell", func(g *psg) {
		g.it("should heal the major status conditions of the user's team", func(p *ps) {
			p.battle(
				team{
					{Species: "Dunsparce", As: "Raticate", Moves: mv("splash")},
					{Species: "Chansey", Moves: mv("healbell")},
				},
				team{{Species: "Nidoking", Moves: mv("toxic", "glare")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("", "move glare")
			p.makeChoices("switch chansey", "")
			p.turn()
			p.noStatus(p.slot(0, 1), "the benched Pokemon's paralysis should have been rung off")
			p.noStatus(p.slot(0, 2), "the caster should cure itself too")
		})

		g.skip("should not heal the major status conditions of a Pokemon with Soundproof", "doubles")
		g.skip("with Mold Breaker should heal the major status conditions of a Pokemon with Soundproof", "doubles")
		g.skip("in a Multi Battle, should heal the major status conditions of the ally's team", "multi battles")
		g.skip("in a Free-For-All, should heal the major status conditions of the user's team, and not any opposing teams",
			"free-for-all")
	})
}
