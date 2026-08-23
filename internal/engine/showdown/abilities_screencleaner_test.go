//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/screencleaner.js.
//
// Mr. Mime-Galar is a regional forme this dex does not carry; plain Mr. Mime
// is the body instead. Nothing here reads its typing or its stats — it is the
// thing being switched in so that Screen Cleaner can fire on entry — so the
// forme difference costs the case nothing. Screen Cleaner itself is not in the
// ability set, which the harness reports.
//
// Reflect Type is not in this dataset. Upstream gives it to the second Mew
// only so that side has something to do on the switch turn; Splash serves the
// same purpose without putting a missing move between the port and its
// subject.
//
// Side conditions are read off the battle state directly: this engine emits no
// line for a screen being removed, so there is no log fragment to match.

func TestAbilitiesScreenCleaner(t *testing.T) {
	describe(t, "Screen Cleaner", func(g *psg) {
		g.it("should remove screens from both sides when sent out", func(p *ps) {
			p.battle(
				team{
					{Species: "Mew", Ability: "synchronize", Moves: mv("reflect")},
					{Species: "Mr. Mime-Galar", As: "Mr. Mime", Ability: "screencleaner", Moves: mv("psychic")},
				},
				team{{Species: "Mew", Ability: "synchronize", Moves: mv("lightscreen", "splash")}},
			)
			p.makeChoices("move reflect", "move lightscreen")
			p.makeChoices("switch 2", "move splash")
			p.ok(p.state().Sides[0].Conditions.Reflect == nil, "Reflect should have been swept off its own side")
			p.ok(p.state().Sides[1].Conditions.LightScreen == nil, "Light Screen should have been swept off the far side")
		})
	})
}
