//go:build showdown

package showdown

import "testing"

func TestZZProbeShaumik(t *testing.T) {
	describe(t, "probe", func(g *psg) {
		g.it("lowkick-chople", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Item: "chopleberry", Moves: mv("splash")}},
				team{{Species: "Golem", Moves: mv("lowkick")}},
			)
			p.turn()
			p.note("no magic room: item=%q hp=%d/%d log:\n%s", p.mine().Item, p.mine().HP, p.mine().MaxHP, p.lastTurnText())
			p.fail("dump")
		})
		g.it("clearsmog-crit", func(p *ps) {
			p.battle(
				team{{Species: "Amoonguss", As: "Venusaur", Ability: "regenerator",
					Item: "scopelens", Moves: mv("focusenergy", "clearsmog")}},
				team{{Species: "Primeape", Ability: "angerpoint", Moves: mv("bulkup")}},
			)
			p.makeChoices("move focusenergy", "move bulkup")
			p.makeChoices("move clearsmog", "move bulkup")
			p.note("log:\n%s", p.logText())
			p.fail("dump")
		})
		g.it("fakeout-focuspunch", func(p *ps) {
			p.battle(
				team{{Species: "Hitmontop", As: "Hitmonlee", Ability: "steadfast", Moves: mv("fakeout")}},
				team{{Species: "Gallade", As: "Machamp", Ability: "steadfast", Moves: mv("focuspunch")}},
			)
			p.turn()
			p.note("log:\n%s", p.lastTurnText())
			p.fail("dump")
		})
		g.it("magicroom-assaultvest", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Item: "assaultvest", Moves: mv("protect")}},
				team{{Species: "Golem", Moves: mv("magicroom")}},
			)
			p.makeChoices("", "move magicroom")
			p.note("t1:\n%s", p.lastTurnText())
			p.makeChoices("move protect", "move magicroom")
			p.note("t2 last=%q:\n%s", p.mine().Volatiles.LastMoveID, p.lastTurnText())
			p.fail("dump")
		})
		g.it("naturepower", func(p *ps) {
			p.battle(
				team{{Species: "Kilowattrel", As: "Zapdos", Ability: "noability",
					Moves: mv("charge", "naturepower")}},
				team{{Species: "Baxcalibur", As: "Chansey", Ability: "noability", Moves: mv("splash", "electricterrain")}},
			)
			p.makeChoices("move charge", "move electricterrain")
			p.makeChoices("move naturepower", "move splash")
			p.note("terrain=%q charge=%v log:\n%s", p.terrain(), p.mine().Volatiles.Charge, p.lastTurnText())
			p.fail("dump")
		})
	})
}
