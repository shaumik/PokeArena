//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/desolateland.js.
//
// Desolate Land is not modeled, and neither is the weather it sets: weather.go
// knows rain, sun, sandstorm and snow and nothing else. Groudon is not in this
// dex either, so the fixtures build Sandslash through the stand-in table with
// the ability named on the set. Every ported case here is therefore expected
// to be red, and the shape of the redness is the finding.
//
// Five of the cases ask whether the weather *fades* after some event. On an
// engine that never puts it up those would all pass while measuring nothing,
// so each one first states the premise upstream leaves implicit — that the
// Desolate Land weather is up to begin with — as its own assertion. That read
// happens where upstream reads it, straight after the teams are built; note
// that this engine fires lead switch-in abilities at the top of turn 1 rather
// than at construction, so the premise would need a turn even if the ability
// existed.
//
// Substitutions beyond the stand-in table: Abomasnow becomes Jynx, which keeps
// the Ice half and takes Snow Warning explicitly, since all the case wants is
// a body that tries to set hail. Ho-Oh, Lugia, Kyogre, Tyranitar and Abra all
// have stand-in rows.
//
// Helping Hand and Sleep Talk are not in this dataset; both were inert in a
// singles battle with no ally, so Splash replaces them. Soak, Entrainment and
// the Red Orb are absent and are the subject of their cases, so they stay and
// the missing-name failure is recorded.

func TestAbilitiesDesolateLand(t *testing.T) {
	describe(t, "Desolate Land", func(g *psg) {
		g.it("should activate the Desolate Land weather upon switch-in", func(p *ps) {
			p.battle(
				team{{Species: "Groudon", Ability: "desolateland", Moves: mv("splash")}},
				team{{Species: "Abra", Ability: "magicguard", Moves: mv("teleport")}},
			)
			p.equal(p.weather(), "desolateland", "the lead's ability should have set its weather")
		})

		g.skip("should increase the damage (not the basePower) of Fire-type attacks",
			"upstream pins the damage roll with battle.randomizer and asserts an absolute "+
				"level-100 figure; level is fixed at 50 here and there is no roll hook")

		g.it("should cause Water-type attacks to fail", func(p *ps) {
			p.battle(
				team{{Species: "Groudon", Ability: "desolateland", Moves: mv("splash")}},
				team{{Species: "Blastoise", Ability: "torrent", Moves: mv("surf")}},
			)
			p.makeChoices("move splash", "move surf")
			p.fullHP(p.mine(), "a Water-type attack should fail outright in Desolate Land")
		})

		g.it("should not cause Water-type Status moves to fail", func(p *ps) {
			p.battle(
				team{{Species: "Groudon", Ability: "desolateland", Moves: mv("splash")}},
				team{{Species: "Blastoise", Ability: "torrent", Moves: mv("soak")}},
			)
			if p.state() == nil {
				return
			}
			p.sets(func() any { return string(p.mine().Type1) + "/" + string(p.mine().Type2) }, "Water",
				func() { p.makeChoices("move splash", "move soak") },
				"Soak is a status move and should still land in Desolate Land")
		})

		g.it("should prevent moves and abilities from setting the weather to Sunny Day, Rain Dance, Sandstorm, or Hail", func(p *ps) {
			// Jynx keeps Abomasnow's Ice half and carries Snow Warning here;
			// only "a body that tries to put hail up" is being asked for.
			p.battle(
				team{{Species: "Groudon", Ability: "desolateland", Moves: mv("splash")}},
				team{
					{Species: "Abra", Ability: "magicguard", Moves: mv("teleport")},
					{Species: "Kyogre", Ability: "drizzle", Moves: mv("raindance")},
					{Species: "Groudon", Ability: "drought", Moves: mv("sunnyday")},
					{Species: "Tyranitar", Ability: "sandstream", Moves: mv("sandstorm")},
					{Species: "Abomasnow", As: "Jynx", Ability: "snowwarning", Moves: mv("hail")},
				},
			)
			for _, sw := range []string{"switch 2", "switch 3", "switch 4", "switch 5"} {
				p.makeChoices("move splash", sw)
				p.equal(p.weather(), "desolateland", "a weather-setting ability should not displace Desolate Land")
				p.makeChoices("move splash", "move 1")
				p.equal(p.weather(), "desolateland", "a weather-setting move should not displace Desolate Land")
			}
		})

		g.skip("should be treated as Sunny Day for any forme, move or ability that requires it",
			"formes — Castform-Sunny and Cherrim-Sunshine are the first two assertions")

		g.it("should cause the Desolate Land weather to fade if it switches out and no other Desolate Land Pokemon are active", func(p *ps) {
			p.battle(
				team{
					{Species: "Groudon", Ability: "desolateland", Moves: mv("splash")},
					{Species: "Ho-Oh", Ability: "pressure", Moves: mv("roost")},
				},
				team{{Species: "Lugia", Ability: "pressure", Moves: mv("roost")}},
			)
			p.equal(p.weather(), "desolateland", "premise: the weather is up while the holder is in")
			p.sets(func() any { return p.weather() }, "",
				func() { p.makeChoices("switch 2", "move roost") },
				"the weather should fade when the only Desolate Land Pokemon leaves")
		})

		g.it("should not cause the Desolate Land weather to fade if it switches out and another Desolate Land Pokemon is active", func(p *ps) {
			p.battle(
				team{
					{Species: "Groudon", Ability: "desolateland", Moves: mv("splash")},
					{Species: "Ho-Oh", Ability: "pressure", Moves: mv("roost")},
				},
				team{{Species: "Groudon", Ability: "desolateland", Moves: mv("bulkup")}},
			)
			p.equal(p.weather(), "desolateland", "premise: the weather is up while both holders are in")
			p.constant(func() any { return p.weather() },
				func() { p.makeChoices("switch 2", "move bulkup") },
				"a second Desolate Land Pokemon should hold the weather up")
		})

		g.it("should cause the Desolate Land weather to fade if its ability is suppressed and no other Desolate Land Pokemon are active", func(p *ps) {
			p.battle(
				team{{Species: "Groudon", Ability: "desolateland", Moves: mv("splash")}},
				team{{Species: "Lugia", Ability: "pressure", Moves: mv("gastroacid")}},
			)
			p.equal(p.weather(), "desolateland", "premise: the weather is up before the ability is suppressed")
			p.sets(func() any { return p.weather() }, "",
				func() { p.makeChoices("move splash", "move gastroacid") },
				"suppressing the only Desolate Land ability should drop the weather")
		})

		g.it("should not cause the Desolate Land weather to fade if its ability is suppressed and another Desolate Land Pokemon is active", func(p *ps) {
			p.battle(
				team{{Species: "Groudon", Ability: "desolateland", Moves: mv("splash")}},
				team{{Species: "Groudon", Ability: "desolateland", Moves: mv("gastroacid")}},
			)
			p.equal(p.weather(), "desolateland", "premise: the weather is up while both holders are in")
			p.constant(func() any { return p.weather() },
				func() { p.makeChoices("move splash", "move gastroacid") },
				"the unsuppressed holder should hold the weather up")
		})

		g.it("should cause the Desolate Land weather to fade if its ability is changed and no other Desolate Land Pokemon are active", func(p *ps) {
			p.battle(
				team{{Species: "Groudon", Ability: "desolateland", Moves: mv("splash")}},
				team{{Species: "Lugia", Ability: "pressure", Moves: mv("entrainment")}},
			)
			if p.state() == nil {
				return
			}
			p.equal(p.weather(), "desolateland", "premise: the weather is up before the ability is replaced")
			p.sets(func() any { return p.weather() }, "",
				func() { p.makeChoices("move splash", "move entrainment") },
				"overwriting the only Desolate Land ability should drop the weather")
		})

		g.it("should fade after being forced out via Roar", func(p *ps) {
			p.battle(
				team{
					{Species: "Groudon", Item: "Red Orb", Moves: mv("splash")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Wynaut", Moves: mv("roar")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.notEqual(p.weather(), "desolateland", "Roar dragging the holder out should drop its weather")
		})

		g.it("should cause Water-type Natural Gift to fail", func(p *ps) {
			p.battle(
				team{{Species: "Groudon", Item: "Red Orb", Moves: mv("splash")}},
				team{{Species: "Wynaut", Moves: mv("naturalgift")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.fullHP(p.mine(), "a Water-type Natural Gift should fail in Desolate Land")
		})
	})
}
