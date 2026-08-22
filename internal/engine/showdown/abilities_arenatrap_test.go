//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/arenatrap.js.
//
// Arena Trap is in this dataset and Dugtrio carries it, so the file's one case
// runs. It walks a six-Pokemon opposing team past the ability one exemption at a
// time — Flying, Air Balloon, Levitate, Ghost, Magnet Rise — then grounds two of
// them back with Telekinesis and Gravity.
//
// Four of those six have no stand-in row, and each substitute is chosen for the
// exemption it has to carry: Pidgeot for Tornadus is Flying, Weezing for Claydol
// has Levitate, Gengar for Dusknoir is Ghost, Magneton for Magnezone is the body
// Magnet Rise is cast from. Heatran resolves to Ninetales through the table; the
// row drops steel, but the case only ever needed Heatran for its Air Balloon.
//
// Snore is not in this dataset. It is inert filler here — Dugtrio is awake, so
// it fails every time it is used — so Splash stands in for it. Upstream's
// 'auto'/'default' choices are spelled out, so the port does not depend on which
// action the engine happens to list first, and its assert.trapped calls become
// the harness's trapped/notTrapped, which read the legal actions rather than
// submitting an illegal choice.

func TestAbilitiesArenaTrap(t *testing.T) {
	describe(t, "Arena Trap", func(g *psg) {
		g.it("should prevent grounded Pokemon that are not immune to trapping from switching out normally", func(p *ps) {
			p.battle(
				team{{Species: "Dugtrio", Ability: "arenatrap", Moves: mv("splash", "telekinesis", "gravity")}},
				team{
					{Species: "Tornadus", As: "Pidgeot", Ability: "defiant", Moves: mv("tailwind")},
					{Species: "Heatran", Ability: "flashfire", Item: "airballoon", Moves: mv("roar")},
					{Species: "Claydol", As: "Weezing", Ability: "levitate", Moves: mv("rest")},
					{Species: "Dusknoir", As: "Gengar", Ability: "frisk", Moves: mv("rest")},
					{Species: "Magnezone", As: "Magneton", Ability: "magnetpull", Moves: mv("magnetrise")},
					{Species: "Vaporeon", Ability: "waterabsorb", Moves: mv("roar")},
				},
			)
			p.makeChoices("move splash", "switch 2")
			p.species(p.foe(), "Heatran", "a Flying-type is not grounded and should be free to leave")
			p.makeChoices("move splash", "switch 3")
			p.species(p.foe(), "Weezing", "an Air Balloon holder is not grounded and should be free to leave")
			p.makeChoices("move splash", "switch 4")
			p.species(p.foe(), "Gengar", "a Levitate holder is not grounded and should be free to leave")
			p.makeChoices("move splash", "switch 5")
			p.species(p.foe(), "Magneton", "a Ghost-type is immune to trapping and should be free to leave")

			p.trapped(1, "a grounded Pokemon with no exemption should be held by Arena Trap")

			p.makeChoices("move splash", "move magnetrise")
			p.makeChoices("move splash", "switch 6")
			p.species(p.foe(), "Vaporeon", "Magnet Rise should have lifted it out of Arena Trap's reach")

			p.trapped(1, "the replacement is grounded and should be held by Arena Trap")

			p.makeChoices("move telekinesis", "move roar")
			p.makeChoices("move splash", "switch 2")
			p.species(p.foe(), "Pidgeot", "Telekinesis should have lifted it out of Arena Trap's reach")

			p.makeChoices("move gravity", "move tailwind")
			p.trapped(1, "Gravity should ground the Flying-type and hand it to Arena Trap")
			p.species(p.foe(), "Pidgeot", "the grounded Flying-type should still be out")
		})
	})
}
