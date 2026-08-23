//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/courtchange.js.
//
// Court Change is not in this 538-move dataset, and neither is Sticky Web. Both
// live cases are written out rather than skipped, because those absences are
// the finding.
//
// Species. Cinderace has no stand-in row and is only the Court Change user, so
// it is built as Arcanine (Fire, same role). Pawniard likewise has no row; it is
// built as Magneton, which is what the table uses for the dex's Steel bodies,
// with Defiant set explicitly as upstream does — Dark is lost and is not read.
// Wynaut goes to Hypno as usual.
//
// Side conditions are read off the state directly, since upstream inspects
// `battle.pN.sideConditions` and this harness has no named helper for them.
//
// `sleeptalk` is not in this dataset; `splash` stands in for it.

func TestMovesCourtChange(t *testing.T) {
	describe(t, "Court Change", func(g *psg) {
		g.it(`should swap certain side conditions to the opponent's side and vice versa`, func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Moves: mv("splash", "stealthrock", "lightscreen")}},
				team{{Species: "cinderace", As: "Arcanine", Moves: mv("courtchange", "tailwind", "safeguard")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move stealthrock", "move tailwind")
			p.makeChoices("move lightscreen", "move safeguard")
			p.makeChoices("move splash", "move courtchange")

			mine, foe := &p.state().Sides[0].Conditions, &p.state().Sides[1].Conditions
			p.ok(mine.Tailwind != nil, "Tailwind should have crossed to the other side")
			p.ok(mine.Safeguard != nil, "Safeguard should have crossed to the other side")
			p.ok(mine.Hazards.StealthRock, "Stealth Rock should have come back to the side that set it")
			p.ok(foe.LightScreen != nil, "Light Screen should have crossed to the other side")
		})

		g.it(`should allow Sticky Web to trigger Defiant when set by the Defiant user's team`, func(p *ps) {
			p.battle(
				team{
					{Species: "cinderace", As: "Arcanine", Moves: mv("courtchange", "stickyweb", "splash")},
					{Species: "pawniard", As: "Magneton", Ability: "defiant", Moves: mv("splash")},
				},
				team{{Species: "wynaut", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move stickyweb", "move splash")
			p.makeChoices("move courtchange", "move splash")
			p.makeChoices("switch 2", "move splash")

			// Upstream also asserts the web is now on p1's side. This engine's
			// Hazards bag has only Stealth Rock, Spikes and Toxic Spikes, so
			// there is no field to read; the Defiant boost below is the half of
			// the case that can be stated.
			p.statStage(p.mine(), "atk", 2,
				"the Speed drop from a Sticky Web on one's own side should still wake Defiant")
		})

		g.skip(`[Gen 8] should not allow Sticky Web to trigger Defiant when set by the Defiant user's team`,
			"gen 8 mechanics")
	})
}
