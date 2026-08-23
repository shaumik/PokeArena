//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/adrenalineorb.js.
//
// Adrenaline Orb is not one of this dataset's 128 items, so every case fails at
// team construction naming it. That is the finding; the fixtures are written out
// in full so they still measure the right thing if the item is added.
//
// Timing. Upstream reads the leads' Intimidate straight off the freshly built
// battle. This engine fires the leads' switch-in hooks at the top of turn 1, so
// every case that watches a lead Intimidate takes a turn first.
//
// Machinery. Four cases drive the holder to a stat extreme before Intimidate
// arrives, and upstream gets there with Belly Drum, Topsy-Turvy and Steam
// Engine, none of which this dataset or engine has. None of them is the subject
// — the subject is only where the holder's stages are standing when Intimidate
// lands — so the port reaches the same stages with moves that are present:
// Charm for -2 Attack a turn (and, on a Contrary holder, +2), Agility for +2
// Speed, Scary Face for -2 Speed. Contrary itself is kept, because two case
// titles are about it, and it reports itself as unmodeled.
//
// Final Gambit, which upstream uses to get its lead off the field so the
// Intimidate body can come in, is also missing and is pure machinery; the port
// switches instead.
//
// Species. Mamoswine and Shedinja have no stand-in row. Sandslash keeps
// Mamoswine's Ground typing, and Shedinja is only a body that leaves the field
// here — Wonder Guard is nowhere in this file — so Chansey takes its place.
// Incineroar and Wynaut go through their stand-in rows; Dugtrio is in the dex.

func TestItemsAdrenalineOrb(t *testing.T) {
	describe(t, "Adrenaline Orb", func(g *psg) {
		g.it("should activate even if an Ability stopped Intimidate", func(p *ps) {
			p.battle(
				team{{Species: "Mamoswine", As: "Sandslash", Ability: "oblivious", Item: "adrenalineorb", Moves: mv("splash")}},
				team{{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.statStage(p.mine(), "spe", 1, "the orb answers the attempt, not the drop")
		})

		g.it("should activate even if Mist stopped Intimidate", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "adrenalineorb", Moves: mv("mist", "splash")}},
				team{
					{Species: "Shedinja", As: "Chansey", Moves: mv("splash")},
					{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mist", "move splash")
			p.makeChoices("move splash", "switch 2")
			p.statStage(p.mine(), "spe", 1, "Mist blocks the drop but not the orb")
		})

		g.it("should not activate if Substitute stopped Intimidate", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "adrenalineorb", Moves: mv("substitute", "splash")}},
				team{
					{Species: "Shedinja", As: "Chansey", Moves: mv("splash")},
					{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move substitute", "move splash")
			p.makeChoices("move splash", "switch 2")
			p.statStage(p.mine(), "spe", 0, "a Substitute stops Intimidate before the orb ever hears about it")
		})

		g.it("should not activate if the holder is at -6 Attack", func(p *ps) {
			p.battle(
				team{{Species: "Dugtrio", Item: "adrenalineorb", Moves: mv("splash")}},
				team{
					{Species: "Shedinja", As: "Chansey", Item: "stickybarb", Moves: mv("charm", "splash")},
					{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			for i := 0; i < 3; i++ {
				p.makeChoices("move splash", "move charm")
			}
			p.statStage(p.mine(), "atk", -6, "the holder should be at the floor before Intimidate arrives")
			p.makeChoices("move splash", "switch 2")
			p.statStage(p.mine(), "spe", 0, "Intimidate that cannot lower anything should not arm the orb")
			p.holdsItem(p.mine(), "the orb should still be there")
		})

		g.it("should activate if the holder is at -5 Attack", func(p *ps) {
			p.battle(
				team{{Species: "Dugtrio", Item: "adrenalineorb", Moves: mv("curse", "splash")}},
				team{
					{Species: "Shedinja", As: "Chansey", Moves: mv("charm", "splash")},
					{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			for i := 0; i < 3; i++ {
				p.makeChoices("move splash", "move charm")
			}
			p.makeChoices("move curse", "move splash")
			p.statStage(p.mine(), "atk", -5, "Curse should have bought one stage of Attack back")
			p.statStage(p.mine(), "spe", -1, "and paid a stage of Speed for it")
			p.makeChoices("move splash", "switch 2")
			p.statStage(p.mine(), "spe", 0, "the orb should put that Speed stage back")
			p.noItem(p.mine(), "the orb should be spent")
		})

		g.it("should not activate if the holder is at +6 Speed", func(p *ps) {
			p.battle(
				team{{Species: "Dugtrio", Item: "adrenalineorb", Moves: mv("agility", "splash")}},
				team{
					{Species: "Shedinja", As: "Chansey", Item: "stickybarb", Moves: mv("splash")},
					{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			for i := 0; i < 3; i++ {
				p.makeChoices("move agility", "move splash")
			}
			p.statStage(p.mine(), "spe", 6, "the holder should be at the Speed ceiling")
			p.makeChoices("move splash", "switch 2")
			p.holdsItem(p.mine(), "an orb with no Speed left to give should not be spent")
		})

		g.it("should not activate if the Contrary holder is at +6 Attack", func(p *ps) {
			// Charm is a two-stage Attack drop, so on a Contrary holder three of
			// them are the +6 upstream reaches with Belly Drum and Topsy-Turvy.
			p.battle(
				team{{Species: "Dugtrio", Ability: "contrary", Item: "adrenalineorb", Moves: mv("splash")}},
				team{
					{Species: "Shedinja", As: "Chansey", Item: "stickybarb", Moves: mv("charm", "splash")},
					{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			for i := 0; i < 3; i++ {
				p.makeChoices("move splash", "move charm")
			}
			p.statStage(p.mine(), "atk", 6, "Contrary should have turned the Charms into a climb")
			p.makeChoices("move splash", "switch 2")
			p.statStage(p.mine(), "spe", 0, "Intimidate that cannot raise anything further should not arm the orb")
			p.holdsItem(p.mine(), "the orb should still be there")
		})

		g.it("should not activate if the Contrary holder is at -6 Speed", func(p *ps) {
			// Upstream's fixture does not actually give the holder Contrary,
			// despite the title; it is ported as written, so what it checks is
			// only that an orb on a -6 Speed holder is not spent.
			p.battle(
				team{{Species: "Dugtrio", Item: "adrenalineorb", Moves: mv("splash")}},
				team{
					{Species: "Shedinja", As: "Chansey", Item: "stickybarb", Moves: mv("scaryface", "splash")},
					{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			for i := 0; i < 3; i++ {
				p.makeChoices("move splash", "move scaryface")
			}
			p.statStage(p.mine(), "spe", -6, "the holder should be at the Speed floor")
			p.makeChoices("move splash", "switch 2")
			p.holdsItem(p.mine(), "the orb should still be there")
		})
	})
}
