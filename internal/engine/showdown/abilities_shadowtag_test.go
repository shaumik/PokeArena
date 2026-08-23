//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/shadowtag.js.
//
// Shadow Tag is not in this dataset. Setting it on the fixture is still the
// right translation: the first case then fails because nothing is trapped, which
// is the finding, and the three negative cases pass without measuring anything,
// since there is no trapping for the exemptions to escape. The Wobbuffet
// stand-in row says outright that it does not preserve Shadow Tag, which is why
// the ability is written on the set rather than left to the species.
//
// Tornadus and Gothitelle have no rows: Pidgeot is a Flying body — Shadow Tag
// does not care about grounding, so nothing is lost — and Alakazam keeps
// Gothitelle's psychic typing and only has to carry a second copy of the
// ability. Heatran resolves to Ninetales, used here purely as a benched body.
//
// Counter is not in this dataset and is inert filler on the trapper, which never
// gets attacked in any of these cases, so Splash stands in for it. Upstream's
// assert.trapped/assert.doesNotThrow become the harness's trapped/notTrapped,
// which read the legal actions instead of submitting a choice and catching it.

func TestAbilitiesShadowTag(t *testing.T) {
	describe(t, "Shadow Tag", func(g *psg) {
		g.it("should prevent most Pokemon from switching out normally", func(p *ps) {
			p.battle(
				team{{Species: "Wobbuffet", Ability: "shadowtag", Moves: mv("splash")}},
				team{
					{Species: "Tornadus", As: "Pidgeot", Ability: "defiant", Moves: mv("tailwind")},
					{Species: "Heatran", Ability: "flashfire", Moves: mv("roar")},
				},
			)
			p.trapped(1, "Shadow Tag should hold an ordinary Pokemon in")
		})

		g.it("should not prevent Pokemon from switching out using moves", func(p *ps) {
			p.battle(
				team{{Species: "Wobbuffet", Ability: "shadowtag", Moves: mv("splash")}},
				team{
					{Species: "Tornadus", As: "Pidgeot", Ability: "defiant", Moves: mv("uturn")},
					{Species: "Heatran", Ability: "flashfire", Moves: mv("roar")},
				},
			)
			p.makeChoices("move splash", "move uturn")
			p.makeChoices("", "switch 2")
			p.species(p.foe(), "Heatran", "U-turn should get a trapped Pokemon out anyway")
		})

		g.it("should not prevent other Pokemon with Shadow Tag from switching out", func(p *ps) {
			p.battle(
				team{{Species: "Wobbuffet", Ability: "shadowtag", Moves: mv("splash")}},
				team{
					{Species: "Gothitelle", As: "Alakazam", Ability: "shadowtag", Moves: mv("psychic")},
					{Species: "Heatran", Ability: "flashfire", Moves: mv("roar")},
				},
			)
			p.notTrapped(1, "Shadow Tag should not hold another Shadow Tag holder")
		})

		g.it("should not prevent Pokemon immune to trapping from switching out", func(p *ps) {
			p.battle(
				team{{Species: "Wobbuffet", Ability: "shadowtag", Moves: mv("splash")}},
				team{
					{Species: "Gengar", Ability: "levitate", Moves: mv("curse")},
					{Species: "Heatran", Ability: "flashfire", Moves: mv("roar")},
				},
			)
			p.notTrapped(1, "a Ghost-type is immune to trapping")
		})
	})
}
