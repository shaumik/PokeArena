//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/wonderroom.js.
//
// Three translation decisions run through the file.
//
// Damage bands. Upstream states each claim as a level-100 absolute damage
// range. None of those transfer to a level-50 engine, so each is re-expressed
// as the comparison the band encodes: a hit that reads Chansey's Sp. Def
// instead of its Defense is a small fraction of its HP rather than most of it;
// a +1 Def stage makes it smaller still; an Assault Vest must not.
//
// Speed. Both damage cases need Wonder Room up before the attack lands, and
// Wynaut's stand-in Hypno outspeeds Chansey. Wynaut is built as Slowbro
// instead — an equally inert body for this (ability stripped, Water/Psychic has
// no interaction with Fighting) that is slower than Chansey, which is the one
// thing the ordering needs.
//
// `sleeptalk`, upstream's do-nothing, is not in this dataset; `splash` stands
// in for it.

func TestMovesWonderRoom(t *testing.T) {
	describe(t, "Wonder Room", func(g *psg) {
		g.it(`should swap the raw Defense and Special Defense stats, but not stat stage changes or other defense modifiers`, func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", As: "Slowbro", Ability: "noability", Moves: mv("brickbreak")}},
				team{
					{Species: "Blissey", Ability: "shellarmor", Moves: mv("wonderroom", "defensecurl", "roost")},
					{Species: "Chansey", Ability: "shellarmor", Item: "assaultvest", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			blissey := p.foe()

			p.turn()
			first := blissey.MaxHP - blissey.HP
			p.atMost(first, blissey.MaxHP/3,
				"Wonder Room should make Brick Break read Chansey's Sp. Def, which is five times its Defense")

			// Defense Curl adds +1 to the Def stage; Roost then puts the chip
			// damage back, so the next Brick Break is measured from full HP.
			p.makeChoices("move brickbreak", "move defensecurl")
			p.makeChoices("move brickbreak", "move roost")
			curled := blissey.MaxHP - blissey.HP
			p.ok(5*curled < 4*first,
				"Wonder Room should still read Defense Curl's +1 Def stage, which is a 1/3 cut the damage roll cannot hide")

			// The second Chansey is the same body holding an Assault Vest: the
			// item raises Sp. Def, and a physical hit must not benefit from it
			// however Wonder Room has routed the stat.
			p.makeChoices("move brickbreak", "switch 2")
			vested := p.foe()
			withVest := vested.MaxHP - vested.HP
			p.ok(5*withVest >= 4*first,
				"an Assault Vest should not soften a physical hit that Wonder Room has pointed at Sp. Def")
		})

		g.it(`should cause Body Press to use Sp. Def stat stage changes`, func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", As: "Slowbro", Ability: "noability", Moves: mv("amnesia", "bodypress")}},
				team{{Species: "Blissey", Ability: "shellarmor", Moves: mv("wonderroom", "splash")}},
			)
			if p.state() == nil {
				return
			}
			blissey := p.foe()

			// Upstream's [100, 118] band is the same claim as "Amnesia doubled
			// it": with Wonder Room up, Body Press reads the user's Sp. Def
			// stage, and +2 stages is a 2x that no damage roll can imitate.
			before := blissey.HP
			p.makeChoices("move bodypress", "move wonderroom")
			unboosted := before - blissey.HP

			p.makeChoices("move amnesia", "move splash")
			before = blissey.HP
			p.makeChoices("move bodypress", "move splash")
			boosted := before - blissey.HP

			p.atLeast(boosted, unboosted*3/2,
				"under Wonder Room, Body Press should read the user's Sp. Def stage")
		})

		g.it(`should be ignored by Download when determining raw stats, but not stat stage changes`, func(p *ps) {
			// No leadsEnter here on purpose. Showdown fires Download at battle
			// start and this engine fires it at the top of turn 1, but in this
			// case Porygon arrives mid-battle both times, so the two agree.
			p.battle(
				team{
					{Species: "Wynaut", Moves: mv("wonderroom")},
					{Species: "Porygon", Ability: "download", Moves: mv("splash")},
				},
				team{{Species: "Venusaur", Moves: mv("splash", "amnesia")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move wonderroom", "move splash")
			p.makeChoices("switch porygon", "move splash")
			p.statStage(p.mine(), "atk", 1,
				"Download should compare Venusaur's raw Def and Sp. Def, ignoring Wonder Room")

			p.makeChoices("switch wynaut", "move amnesia")
			p.makeChoices("switch porygon", "move splash")
			p.statStage(p.mine(), "spa", 1,
				"Wonder Room should put Venusaur's +2 Sp. Def stage on the Defense side of Download's comparison")
		})
	})
}
