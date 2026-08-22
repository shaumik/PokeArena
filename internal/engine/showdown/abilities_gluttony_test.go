//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/gluttony.js.
//
// Gluttony's entire effect is a threshold: a quarter-HP berry is eaten at half
// HP instead. Upstream can state each case as an exact figure
// (hp === floor(maxhp/2) + floor(maxhp/3)) because Super Fang lands on exactly
// half. Super Fang, Belly Drum and Sleep Talk are none of them in this
// dataset, so each case drives the holder over the half mark by a different
// route and then asserts the two things that figure stood for: the berry was
// spent, and it was spent while the holder was still above the quarter-HP mark
// a berry without Gluttony would have waited for.
//
// The three routes are kept apart because they are three different places the
// engine can notice an HP drop:
//   - Super Fang becomes Night Shade, which deals a flat 50 at this engine's
//     fixed level 50 and so crosses the mark with no damage roll in the way.
//   - Belly Drum becomes Substitute. Not the same move, but the same shape:
//     the holder pays HP out of its own move and the berry has to notice.
//   - Poison Powder is in the dataset and ports as written.
//
// Sleep Talk is Splash throughout; upstream only ever uses it as an idle move.
// Wobbuffet and Wynaut both stand in as Hypno, which is a body here and
// nothing else.

func TestAbilitiesGluttony(t *testing.T) {
	describe(t, "Gluttony", func(g *psg) {
		g.it("should activate Aguav Berry at 50% health", func(p *ps) {
			p.battle(
				team{{Species: "Wobbuffet", Ability: "gluttony", Item: "aguavberry", Moves: mv("splash")}},
				team{{Species: "Wynaut", Ability: "compoundeyes", Moves: mv("nightshade")}},
			)
			mon := p.mine()
			before := mon.HP
			// Night Shade until the berry goes, stopping short of the
			// quarter-HP mark so a berry that only fires there still leaves
			// the assertions below something to catch.
			for i := 0; i < 6 && mon.Item != "" && mon.HP > mon.MaxHP/4; i++ {
				before = mon.HP
				p.makeChoices("move splash", "move nightshade")
			}
			p.noItem(mon, "the Aguav Berry should have been eaten on the way past half HP")
			p.atLeast(mon.HP, mon.MaxHP/2, "the berry restores a third, so the holder should end above half")
			p.atLeast(before-50, mon.MaxHP/4+1,
				"Gluttony is only being measured if the berry fired above the quarter-HP mark it waits for otherwise")
		})

		g.it("should activate after Belly Drum", func(p *ps) {
			p.battle(
				team{{Species: "Snorlax", Ability: "gluttony", Item: "aguavberry", Moves: mv("substitute", "splash")}},
				team{{Species: "Wynaut", Ability: "noability", Moves: mv("seismictoss", "splash")}},
			)
			mon := p.mine()
			// Chip to within one Substitute's cost of the half mark, so the
			// HP loss the berry has to react to is the one the holder inflicts
			// on itself.
			for i := 0; i < 8 && mon.HP > mon.MaxHP/2+mon.MaxHP/4; i++ {
				p.makeChoices("move splash", "move seismictoss")
			}
			p.holdsItem(mon, "the berry should still be in hand while the holder is above half HP")
			p.makeChoices("move substitute", "move splash")
			p.noItem(mon, "the HP Substitute takes out of its own user should have set the berry off")
			p.atLeast(mon.HP, mon.MaxHP/2, "the berry restores a third, so the holder should end above half")
		})

		g.it("should activate after poison damage", func(p *ps) {
			p.battle(
				team{{Species: "Wobbuffet", Ability: "gluttony", Item: "aguavberry", Moves: mv("splash")}},
				team{{Species: "Wynaut", Ability: "noguard", Moves: mv("poisonpowder", "splash")}},
			)
			mon := p.mine()
			p.makeChoices("move splash", "move poisonpowder")
			p.hasStatus(mon, "psn", "No Guard should have landed the Poison Powder")
			for i := 0; i < 10 && mon.Item != "" && mon.HP > mon.MaxHP/4; i++ {
				p.makeChoices("move splash", "move splash")
			}
			p.noItem(mon, "end-of-turn poison damage should have set the berry off")
			p.atLeast(mon.HP, mon.MaxHP/2, "the berry restores a third, so the holder should end above half")
		})
	})
}
