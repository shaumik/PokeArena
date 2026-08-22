//go:build showdown

package showdown

import "testing"

func TestZZProbeShaumik(t *testing.T) {
	describe(t, "probe", func(g *psg) {
		g.itRate("lashout-atk-1", 2, 2, 40, func(p *ps) bool {
			p.battle(
				team{{Species: "Wynaut", Ability: "shellarmor", Moves: mv("lashout")}},
				team{{Species: "Blissey", Ability: "intimidate", Moves: mv("splash")}},
			)
			p.makeChoices("move lashout", "move splash")
			p.note("d=%d max=%d", p.foe().MaxHP-p.foe().HP, p.foe().MaxHP)
			return false
		})
		g.itRate("lashout-atk0-laggingtail", 2, 2, 40, func(p *ps) bool {
			p.battle(
				team{{Species: "Wynaut", Item: "laggingtail", Moves: mv("lashout")}},
				team{{Species: "Blissey", Ability: "shellarmor", Moves: mv("faketears")}},
			)
			p.makeChoices("move lashout", "move faketears")
			p.note("d=%d max=%d", p.foe().MaxHP-p.foe().HP, p.foe().MaxHP)
			return false
		})
		g.itRate("explosion-plain", 2, 2, 40, func(p *ps) bool {
			p.battle(
				team{{Species: "Metagross", As: "Magneton", Ability: "noability", Nature: "adamant",
					Moves: mv("explosion", "screech")}},
				team{{Species: "Hippowdon", As: "Golem", Ability: "shellarmor", Nature: "impish",
					EVs: evs(map[string]int{"hp": 252, "def": 252}), Moves: mv("splash")}},
			)
			p.makeChoices("move explosion", "move splash")
			p.note("plain=%d max=%d", p.foe().MaxHP-p.foe().HP, p.foe().MaxHP)
			return false
		})
		g.itRate("explosion-halved", 2, 2, 40, func(p *ps) bool {
			p.battle(
				team{{Species: "Metagross", As: "Magneton", Ability: "noability", Nature: "adamant",
					Moves: mv("explosion", "screech")}},
				team{{Species: "Hippowdon", As: "Golem", Ability: "shellarmor", Nature: "impish",
					EVs: evs(map[string]int{"hp": 252, "def": 252}), Moves: mv("splash")}},
			)
			p.makeChoices("move screech", "move splash")
			p.makeChoices("move explosion", "move splash")
			p.note("halved=%d max=%d", p.foe().MaxHP-p.foe().HP, p.foe().MaxHP)
			return false
		})
		g.itRate("fling-orb", 2, 2, 30, func(p *ps) bool {
			p.battle(
				team{{Species: "wynaut", Item: "lifeorb", Moves: mv("fling")}},
				team{{Species: "cleffa", Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.makeChoices("move fling", "move splash")
			p.note("orb=%d max=%d", p.foe().MaxHP-p.foe().HP, p.foe().MaxHP)
			return false
		})
		g.itRate("fling-charcoal", 2, 2, 30, func(p *ps) bool {
			p.battle(
				team{{Species: "wynaut", Item: "charcoal", Moves: mv("fling")}},
				team{{Species: "cleffa", Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.makeChoices("move fling", "move splash")
			p.note("charcoal=%d max=%d", p.foe().MaxHP-p.foe().HP, p.foe().MaxHP)
			return false
		})
	})
}
