//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/deltastream.js.
//
// Delta Stream is not in this dataset and neither is its weather: this
// engine's weather is one of rain, sun, sandstorm, snow or clear, so
// `p.weather()` can never read "deltastream". Every case is written anyway —
// the ability name is reported by the run, and the weather assertions say what
// the mechanic is supposed to look like.
//
// The five "should the weather fade" cases each gain the precondition upstream
// leaves implicit: that Delta Stream's weather is up in the first place.
// Without it `assert.sets(..., false)` and `assert.constant(...)` are both
// satisfied by a field that never had the weather at all, and the case would
// go green while measuring nothing.
//
// Species. Rayquaza's stand-in Dragonite keeps dragon/flying, which cases 2
// and 3 turn on. Tornadus is pure Flying and has no stand-in; Pidgeot takes
// its place because Electric, Ice and Rock are weaknesses of its Flying half
// alone — Normal is neutral to all three — so the case still isolates exactly
// what Delta Stream is meant to cancel. Ho-Oh becomes Moltres, keeping the
// fire/flying that makes Stealth Rock a 4x hit. Abomasnow has no stand-in and
// only has to put the ice weather up, so Lapras carries the move.
//
// Abilities. Sand Stream and Snow Warning cannot be reached through the
// harness, which resolves a Showdown ability id by finding an in-dex species
// that carries it, and no Kanto species carries either. Those two bench
// members are built bare and set their weather with the move instead, so the
// "abilities" half of that case covers only Drizzle and Drought.
//
// Moves. Helping Hand is not in this dataset and does nothing in singles
// regardless; Splash is the idle move in its place. Entrainment is not in the
// dataset either, and it is the subject of the last case, so it stays. Abra's
// Teleport is Splash here because Teleport self-switches in this generation
// and the lead has to stay on the field. Gastro Acid and Entrainment are moved
// to the second slot on the bodies that carry them, so `p.leadsEnter` does not
// spend the very move the case is about.
//
// Entry timing. Every case that reads the weather before playing a turn calls
// `p.leadsEnter` first — see the divergence documented on `battle`: this engine
// fires the leads' switch-in hooks at the top of turn 1, not at construction.

func TestAbilitiesDeltaStream(t *testing.T) {
	describe(t, "Delta Stream", func(g *psg) {
		g.it("should activate the Delta Stream weather upon switch-in", func(p *ps) {
			p.battle(
				team{{Species: "Rayquaza", Ability: "deltastream", Moves: mv("roost")}},
				team{{Species: "Abra", Ability: "magicguard", Moves: mv("splash")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "deltastream", "Delta Stream should set its weather on switch-in")
		})

		g.it("should negate the type weaknesses of the Flying-type", func(p *ps) {
			p.battle(
				team{{Species: "Tornadus", As: "Pidgeot", Ability: "deltastream", Item: "weaknesspolicy",
					Moves: mv("recover")}},
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("thundershock", "powdersnow", "powergem")}},
			)
			for _, move := range []string{"thundershock", "powdersnow", "powergem"} {
				p.makeChoices("move recover", "move "+move)
				p.statStage(p.mine(), "atk", 0, move+" should not have been super effective")
				p.statStage(p.mine(), "spa", 0, move+" should not have been super effective")
				p.holdsItem(p.mine(), "Weakness Policy should not have fired for "+move)
			}
		})

		g.it("should not negate the type weaknesses of any other type, even if the Pokemon is Flying-type", func(p *ps) {
			p.battle(
				team{{Species: "Rayquaza", Ability: "deltastream", Item: "weaknesspolicy", Moves: mv("recover")}},
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("dragonpulse")}},
			)
			p.makeChoices("move recover", "move dragonpulse")
			p.statStage(p.mine(), "atk", 2, "Dragon is still super effective on a Dragon/Flying body")
			p.statStage(p.mine(), "spa", 2, "Dragon is still super effective on a Dragon/Flying body")
			p.noItem(p.mine(), "Weakness Policy should have been used up")
		})

		g.it("should not reduce damage from Stealth Rock", func(p *ps) {
			p.battle(
				team{
					{Species: "Rayquaza", Ability: "pressure", Moves: mv("roost")},
					{Species: "Ho-Oh", Ability: "pressure", Moves: mv("roost")},
				},
				team{{Species: "Lugia", Ability: "deltastream", Moves: mv("stealthrock")}},
			)
			p.makeChoices("move roost", "move stealthrock")
			hooh := p.slot(0, 2)
			p.hurtsBy(hooh, hooh.MaxHP/2, func() {
				p.makeChoices("switch 2", "move stealthrock")
			}, "Stealth Rock should still hit a Fire/Flying body for 4x")
		})

		g.it("should prevent moves and abilities from setting the weather to Sunny Day, Rain Dance, Sandstorm, or Hail", func(p *ps) {
			p.battle(
				team{{Species: "Rayquaza", Ability: "deltastream", Moves: mv("splash")}},
				team{
					{Species: "Abra", Ability: "magicguard", Moves: mv("teleport")},
					{Species: "Kyogre", Ability: "drizzle", Moves: mv("raindance")},
					{Species: "Groudon", Ability: "drought", Moves: mv("sunnyday")},
					{Species: "Tyranitar", Ability: "noability", Moves: mv("sandstorm")},
					{Species: "Abomasnow", As: "Lapras", Ability: "noability", Moves: mv("hail")},
				},
			)
			for _, slot := range []string{"2", "3", "4", "5"} {
				p.makeChoices("move splash", "switch "+slot)
				p.equal(p.weather(), "deltastream", "a switch-in should not displace Delta Stream")
				p.makeChoices("move splash", "move 1")
				p.equal(p.weather(), "deltastream", "a weather move should not displace Delta Stream")
			}
		})

		g.it("should cause the Delta Stream weather to fade if it switches out and no other Delta Stream Pokemon are active", func(p *ps) {
			p.battle(
				team{
					{Species: "Rayquaza", Ability: "deltastream", Moves: mv("splash")},
					{Species: "Ho-Oh", Ability: "pressure", Moves: mv("roost")},
				},
				team{{Species: "Lugia", Ability: "pressure", Moves: mv("roost")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "deltastream", "the weather should be up before the holder leaves")
			p.sets(func() any { return p.weather() == "deltastream" }, false, func() {
				p.makeChoices("switch 2", "move roost")
			}, "Delta Stream should fade when its only holder leaves")
		})

		g.it("should not cause the Delta Stream weather to fade if it switches out and another Delta Stream Pokemon is active", func(p *ps) {
			p.battle(
				team{
					{Species: "Rayquaza", Ability: "deltastream", Moves: mv("splash")},
					{Species: "Ho-Oh", Ability: "pressure", Moves: mv("roost")},
				},
				team{{Species: "Rayquaza", Ability: "deltastream", Moves: mv("bulkup")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "deltastream", "the weather should be up before the holder leaves")
			p.constant(func() any { return p.weather() }, func() {
				p.makeChoices("switch 2", "move bulkup")
			}, "the second holder should keep Delta Stream up")
		})

		g.it("should cause the Delta Stream weather to fade if its ability is suppressed and no other Delta Stream Pokemon are active", func(p *ps) {
			p.battle(
				team{{Species: "Rayquaza", Ability: "deltastream", Moves: mv("splash")}},
				team{{Species: "Lugia", Ability: "pressure", Moves: mv("roost", "gastroacid")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "deltastream", "the weather should be up before the ability is suppressed")
			p.sets(func() any { return p.weather() == "deltastream" }, false, func() {
				p.makeChoices("move splash", "move gastroacid")
			}, "Delta Stream should fade when its only holder is suppressed")
		})

		g.it("should not cause the Delta Stream weather to fade if its ability is suppressed and another Delta Stream Pokemon is active", func(p *ps) {
			p.battle(
				team{{Species: "Rayquaza", Ability: "deltastream", Moves: mv("splash")}},
				team{{Species: "Rayquaza", Ability: "deltastream", Moves: mv("roost", "gastroacid")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "deltastream", "the weather should be up before the ability is suppressed")
			p.constant(func() any { return p.weather() }, func() {
				p.makeChoices("move splash", "move gastroacid")
			}, "the unsuppressed holder should keep Delta Stream up")
		})

		g.it("should cause the Delta Stream weather to fade if its ability is changed and no other Delta Stream Pokemon are active", func(p *ps) {
			p.battle(
				team{{Species: "Rayquaza", Ability: "deltastream", Moves: mv("splash")}},
				team{{Species: "Lugia", Ability: "pressure", Moves: mv("roost", "entrainment")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "deltastream", "the weather should be up before the ability is replaced")
			p.sets(func() any { return p.weather() == "deltastream" }, false, func() {
				p.makeChoices("move splash", "move entrainment")
			}, "Delta Stream should fade when its holder loses the ability")
		})
	})
}
