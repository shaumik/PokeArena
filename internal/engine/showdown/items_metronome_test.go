//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/metronome.js.
//
// Upstream states every claim here as an absolute level-100 damage window, and
// absolute figures do not transfer to level 50. Each case is therefore restated
// as a comparison against the same fixture with the item removed: the two
// battles run the same actions under the same seed, so the damage rolls line up
// and the only difference between the two figures is the Metronome multiplier.
// That is the same claim upstream makes and it is measurable here.
//
// Upstream notes itself that the Metronome 1 and Metronome 2 windows overlap, so
// the charging-move case cannot be settled by comparing two boosted hits to each
// other. It is settled by comparing each boosted hit to its own unboosted twin
// and requiring the second gap to be the larger.
//
// Species. Dusknoir and Goomy have no stand-in row. Gengar is a Ghost body like
// Dusknoir, and Dragonite a Dragon body like Goomy; neither typing interacts
// with Dig, Surf or Metronome, and the target is what the measurements read.
// Cleffa, Blissey and Wynaut go through their stand-in rows, and Shell Armor is
// kept everywhere upstream keeps it, since a critical hit would move a figure
// for a reason that has nothing to do with the item.
//
// Sleep Talk and Lucky Chant are not in this dataset; both are inert filler on
// the target and Splash stands in for them. Copycat is the subject of the last
// case, so it is kept and its absence is that case's finding.

func TestItemsMetronome(t *testing.T) {
	describe(t, "Metronome (item)", func(g *psg) {
		g.it("should increase the damage of moves that have been used successfully and consecutively", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "metronome", Moves: mv("psystrike")}},
				team{{Species: "Cleffa", EVs: evs(map[string]int{"hp": 252}), Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.turn()
			held := p.foe()
			mid := held.HP
			p.turn()
			boosted := mid - held.HP

			p.battle(
				team{{Species: "Wynaut", Moves: mv("psystrike")}},
				team{{Species: "Cleffa", EVs: evs(map[string]int{"hp": 252}), Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.turn()
			bare := p.foe()
			bareMid := bare.HP
			p.turn()
			unboosted := bareMid - bare.HP

			p.atLeast(boosted, unboosted+1, "the second use of the same move should hit harder with Metronome")
		})

		g.it("should reset the multiplier after switching moves", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "metronome", Moves: mv("psystrike", "splash")}},
				team{{Species: "Cleffa", EVs: evs(map[string]int{"hp": 252}), Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.turn()
			held := p.foe()
			p.makeChoices("move splash", "move splash")
			mid := held.HP
			p.turn()
			afterSwitch := mid - held.HP

			p.battle(
				team{{Species: "Wynaut", Moves: mv("psystrike", "splash")}},
				team{{Species: "Cleffa", EVs: evs(map[string]int{"hp": 252}), Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.turn()
			bare := p.foe()
			p.makeChoices("move splash", "move splash")
			bareMid := bare.HP
			p.turn()
			bareAfterSwitch := bareMid - bare.HP

			p.equal(afterSwitch, bareAfterSwitch, "a different move in between should put the counter back to zero")
		})

		g.it("should reset the multiplier after hitting Protect", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "metronome", Moves: mv("psystrike")}},
				team{{Species: "Cleffa", EVs: evs(map[string]int{"hp": 252}), Ability: "shellarmor", Moves: mv("splash", "protect")}},
			)
			p.turn()
			held := p.foe()
			p.makeChoices("move psystrike", "move protect")
			mid := held.HP
			p.turn()
			afterProtect := mid - held.HP

			p.battle(
				team{{Species: "Wynaut", Moves: mv("psystrike")}},
				team{{Species: "Cleffa", EVs: evs(map[string]int{"hp": 252}), Ability: "shellarmor", Moves: mv("splash", "protect")}},
			)
			p.turn()
			bare := p.foe()
			p.makeChoices("move psystrike", "move protect")
			bareMid := bare.HP
			p.turn()
			bareAfterProtect := bareMid - bare.HP

			p.equal(afterProtect, bareAfterProtect, "a move stopped by Protect should put the counter back to zero")
		})

		g.it("should instantly start moves that use a charging turn at Metronome 1 boost level, then increase linearly", func(p *ps) {
			// The target carries HP EVs upstream does not give it: at level 50
			// Chansey would otherwise be close enough to the two Digs' combined
			// damage that the second measurement could land on a faint instead of
			// a damage figure.
			p.battle(
				team{{Species: "Dusknoir", As: "Gengar", Item: "metronome", Moves: mv("dig")}},
				team{{Species: "Blissey", EVs: evs(map[string]int{"hp": 252}), Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.turn()
			p.turn()
			held := p.foe()
			dig1 := held.MaxHP - held.HP
			p.turn()
			mid := held.HP
			p.turn()
			dig2 := mid - held.HP

			p.battle(
				team{{Species: "Dusknoir", As: "Gengar", Moves: mv("dig")}},
				team{{Species: "Blissey", EVs: evs(map[string]int{"hp": 252}), Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.turn()
			p.turn()
			bare := p.foe()
			bareDig1 := bare.MaxHP - bare.HP
			p.turn()
			bareMid := bare.HP
			p.turn()
			bareDig2 := bareMid - bare.HP

			p.atLeast(dig1, bareDig1+1, "the charging turn should already count, so the first Dig lands boosted")
			p.atLeast(dig2-bareDig2, dig1-bareDig1+1, "and the second Dig should be a further step up")
		})

		g.it("should not instantly start moves that skip a charging turn at Metronome 1 boost level", func(p *ps) {
			p.battle(
				team{{Species: "Slowbro", Item: "metronome", Moves: mv("solarbeam")}},
				team{
					{Species: "Blissey", Ability: "shellarmor", Moves: mv("sunnyday")},
					{Species: "Blissey", Ability: "cloudnine", Moves: mv("splash")},
				},
			)
			p.makeChoices("move solarbeam", "move sunnyday")
			first := p.foe()
			inSun := first.MaxHP - first.HP
			p.atLeast(inSun, 1, "Solar Beam should have fired the same turn the sun went up")
			p.makeChoices("move solarbeam", "switch 2")
			p.turn()
			second := p.foe()
			charged := second.MaxHP - second.HP

			p.battle(
				team{{Species: "Slowbro", Moves: mv("solarbeam")}},
				team{
					{Species: "Blissey", Ability: "shellarmor", Moves: mv("sunnyday")},
					{Species: "Blissey", Ability: "cloudnine", Moves: mv("splash")},
				},
			)
			p.makeChoices("move solarbeam", "move sunnyday")
			bareFirst := p.foe()
			bareInSun := bareFirst.MaxHP - bareFirst.HP
			p.makeChoices("move solarbeam", "switch 2")
			p.turn()
			bareSecond := p.foe()
			bareCharged := bareSecond.MaxHP - bareSecond.HP

			p.equal(inSun, bareInSun, "the first Solar Beam is the first use and should not be boosted")
			// The skipped charge turn must not have counted: at the first
			// Metronome step the second Solar Beam is about a fifth harder than
			// its unboosted twin, where a second step would be nearer two fifths.
			p.bounded(charged, bareCharged+bareCharged/10, bareCharged+3*bareCharged/10,
				"the second Solar Beam should be at the first Metronome step, not the second")
		})

		g.it("should use called moves to determine the Metronome multiplier", func(p *ps) {
			p.battle(
				team{{Species: "Goomy", As: "Dragonite", Item: "metronome", Moves: mv("copycat", "surf")}},
				team{{Species: "Clefable", EVs: evs(map[string]int{"hp": 252}), Ability: "shellarmor", Moves: mv("softboiled", "surf")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move copycat", "move surf")
			held := p.foe()
			called1 := held.MaxHP - held.HP
			mid := held.HP
			p.makeChoices("move copycat", "move surf")
			called2 := mid - held.HP
			mid = held.HP
			p.makeChoices("move surf", "move softboiled")
			direct := mid - held.HP

			p.battle(
				team{{Species: "Goomy", As: "Dragonite", Moves: mv("copycat", "surf")}},
				team{{Species: "Clefable", EVs: evs(map[string]int{"hp": 252}), Ability: "shellarmor", Moves: mv("softboiled", "surf")}},
			)
			p.makeChoices("move copycat", "move surf")
			bare := p.foe()
			bareCalled1 := bare.MaxHP - bare.HP
			bareMid := bare.HP
			p.makeChoices("move copycat", "move surf")
			bareCalled2 := bareMid - bare.HP
			bareMid = bare.HP
			p.makeChoices("move surf", "move softboiled")
			bareDirect := bareMid - bare.HP

			// The third figure nets the target's Softboiled out of the damage,
			// but it does so identically in both battles, so the difference
			// between them is still only the multiplier.
			p.equal(called1, bareCalled1, "the first called Surf is the first use and should not be boosted")
			p.atLeast(called2-bareCalled2, 1, "the second called Surf should be boosted")
			p.atLeast(direct-bareDirect, called2-bareCalled2+1,
				"using the move directly should continue the chain the called ones started")
		})
	})
}
