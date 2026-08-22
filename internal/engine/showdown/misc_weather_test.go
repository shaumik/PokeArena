//go:build showdown

package showdown

import (
	"strings"
	"testing"
)

// Ported from test/sim/misc/weather.js.
//
// Two things about this file do not survive the crossing.
//
// Upstream replaces the damage roll with `battle.randomizer = dmg => dmg` and
// then asserts an exact number of HP at level 100. This engine has no
// randomizer hook and is fixed at level 50, so the absolute figures are
// meaningless here. The first two cases therefore play the same turn twice,
// once with the weather and once without, and compare: 1.5x has to survive the
// 85-100% roll on both sides, hence the 1.2x floor, and 0.5x the 0.6x ceiling.
// The other half of what those cases assert — that the multiplier lands on the
// damage rather than on the base power — needs the `BasePower` event and is
// simply not observable from a battle here; it is not asserted.
//
// Sand Stream cannot be set through this harness: no in-dex species has it, and
// the harness renders an unknown ability id without its hyphen, so the engine's
// "sand-stream" would never be found and the fixture would silently run with no
// weather at all. The two Tyranitar cases put the same sandstorm up with the
// move instead, which starts the same five-turn clock.
//
// `sleeptalk` is not in this dataset and is pure filler upstream; `splash`
// stands in for it.

func TestMiscWeather(t *testing.T) {
	describe(t, "Weather damage calculation", func(g *psg) {
		g.it("should multiply the damage (not the basePower) in favorable weather", func(p *ps) {
			// Cryogonal is an Ice-type Levitate body; Articuno keeps both the
			// Ice typing (so Incinerate is still super effective) and the lack
			// of any interaction with sun.
			p.battle(
				team{{Species: "Ninetales", Ability: "drought", Moves: mv("incinerate")}},
				team{{Species: "Cryogonal", As: "Articuno", Ability: "levitate", Moves: mv("splash")}},
			)
			p.makeChoices("move incinerate", "move splash")
			p.equal(p.weather(), "sun", "Drought should have set the sun")
			inSun := p.foe().MaxHP - p.foe().HP

			// The same turn with no weather, as the baseline the comparison
			// needs. Not an upstream battle.
			p.battle(
				team{{Species: "Ninetales", Ability: "noability", Moves: mv("incinerate")}},
				team{{Species: "Cryogonal", As: "Articuno", Ability: "levitate", Moves: mv("splash")}},
			)
			p.makeChoices("move incinerate", "move splash")
			plain := p.foe().MaxHP - p.foe().HP

			p.atLeast(inSun, plain*12/10, "sun should have multiplied the Fire damage")
		})

		g.it("should reduce the damage (not the basePower) in unfavorable weather", func(p *ps) {
			p.battle(
				team{{Species: "Ninetales", Ability: "drizzle", Moves: mv("incinerate")}},
				team{{Species: "Cryogonal", As: "Articuno", Ability: "levitate", Moves: mv("splash")}},
			)
			p.makeChoices("move incinerate", "move splash")
			p.equal(p.weather(), "rain", "Drizzle should have set the rain")
			inRain := p.foe().MaxHP - p.foe().HP

			p.battle(
				team{{Species: "Ninetales", Ability: "noability", Moves: mv("incinerate")}},
				team{{Species: "Cryogonal", As: "Articuno", Ability: "levitate", Moves: mv("splash")}},
			)
			p.makeChoices("move incinerate", "move splash")
			plain := p.foe().MaxHP - p.foe().HP

			p.atMost(inRain, plain*6/10, "rain should have halved the Fire damage")
		})

		g.skip("should make Hail/Sandstorm damage some pokemon but not others", "gen 8 mechanics")

		g.it("should wear off on the final turn before weather effects are applied", func(p *ps) {
			p.battle(
				team{{Species: "Tyranitar", Moves: mv("sandstorm", "splash")}},
				team{{Species: "Wynaut", Moves: mv("splash")}},
			)
			p.makeChoices("move sandstorm", "move splash")
			for i := 0; i < 4; i++ {
				p.makeChoices("move splash", "move splash")
			}
			wynaut := p.foe()
			p.equal(p.weather(), "", "the sandstorm should have run out on the fifth turn")
			p.equal(wynaut.HP, wynaut.MaxHP-(wynaut.MaxHP/16)*4,
				"five turns of sandstorm should chip four times, the last turn spent wearing off")
		})

		g.it("should wear off before future attacks", func(p *ps) {
			// Doom Desire and Soak are both absent from this dataset, so this
			// fails at team construction — that absence is the finding. Soak is
			// load-bearing rather than filler: it is what makes the Rock-type
			// target start taking sandstorm damage on the turn Doom Desire
			// lands.
			//
			// Upstream reads the debug log for "the sand chip came before the
			// Doom Desire hit". Delayed-move damage has no distinct line here,
			// so the port asserts the observable half: the landing turn costs
			// more than a sandstorm tick, and the sand tick is in the log.
			p.battle(
				team{{Species: "Tyranitar", Moves: mv("sandstorm", "doomdesire", "soak")}},
				team{{Species: "Roggenrola", As: "Onix", Moves: mv("splash")}},
			)
			p.makeChoices("move sandstorm", "move splash")
			p.makeChoices("move doomdesire", "move splash")
			hpBefore := p.foe().HP
			p.makeChoices("move soak", "move splash")
			foe := p.foe()
			p.logHas("is buffeted by the sandstorm",
				"Soak should have made the target take the sandstorm chip")
			p.atLeast(hpBefore-foe.HP, foe.MaxHP/16+1,
				"the turn Doom Desire lands should cost more than the sandstorm chip alone")
		})

		g.it("should run residual weather effects in order of Speed", func(p *ps) {
			// Sunkern is only the slow half of a speed pair carrying Solar
			// Power; Snorlax is slower than Charizard by a wide margin, which
			// is the only property the ordering assertion uses.
			p.battle(
				team{{Species: "Sunkern", As: "Snorlax", Ability: "solarpower", Moves: mv("sunnyday")}},
				team{{Species: "Charizard", Ability: "dryskin", Moves: mv("splash")}},
			)
			p.turn()
			p.logHas("Dry Skin", "Charizard should have taken Dry Skin damage in the sun")
			p.logHas("Solar Power", "Sunkern should have taken Solar Power damage in the sun")
			log := p.logText()
			drySkin := strings.Index(log, "Dry Skin")
			solarPower := strings.Index(log, "Solar Power")
			p.ok(drySkin >= 0 && solarPower >= 0 && drySkin < solarPower,
				"Charizard is faster, so it should be damaged before Sunkern")
		})
	})
}
