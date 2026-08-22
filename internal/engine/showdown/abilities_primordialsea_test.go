//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/primordialsea.js.
//
// Primordial Sea is not in this dataset and neither is its weather — this
// engine's weather is rain, sun, sandstorm, snow or clear — so `p.weather()`
// can never read "primordialsea". The cases are written anyway: the ability
// name is reported by the run, and the assertions state what heavy rain is
// supposed to do.
//
// The five "should the weather fade" cases each gain the precondition upstream
// leaves implicit, that the weather is up to begin with. Without it
// `assert.sets(..., false)` and `assert.constant(...)` are satisfied by a field
// that never had the weather, and the case goes green measuring nothing.
//
// Damage. Upstream pins the Water boost as an absolute figure at level 100
// with the randomizer disabled. Neither transfers, so the case is restated as
// the comparison the figure stood for: the same attack, same seed, once from a
// Primordial Sea holder and once from a bare one, and the first must be at
// least 1.2x the second. A 1.5x boost clears that under every damage roll and
// no boost at all cannot reach it. The second half of that case reads the move's
// base power off `battle.runEvent`, which has no counterpart here and is not
// asserted.
//
// The "treated as Rain Dance" case is ported in part. Castform's forme change
// and Kingdra's doubled Speed cannot be observed — there are no formes here and
// the harness reads no computed stats — so those two bodies stay in the team
// for shape and only Rain Dish, Dry Skin and Hydration are asserted on.
// Upstream reads the last two off Sonic Boom's flat 20; this engine models no
// fixed-damage move, so Sonic Boom lands for the one-point minimum and the
// figure carries nothing. The port chips each body with a plain special attack
// instead and reads the heal on the following turn, where nothing else moves
// its HP.
//
// Species. Kyogre's stand-in Blastoise keeps the Water typing. Ho-Oh becomes
// Moltres, Lugia becomes Articuno, Abra becomes Alakazam. Kingdra becomes
// Seadra, the same line one stage down. Ludicolo has no stand-in and only has
// to carry Rain Dish, which Tentacruel has natively; Toxicroak likewise only
// has to carry Dry Skin, which Parasect has natively. Manaphy becomes Vaporeon,
// a Water body with Hydration. Castform's body is built bare because Forecast
// is not modeled and the forme assertion is dropped.
//
// Abilities. Sand Stream and Snow Warning cannot be reached through the
// harness — it resolves a Showdown ability id by finding an in-dex species
// carrying it, and no Kanto species carries either — so those two bench
// members are bare and set their weather with the move instead.
//
// Moves. Helping Hand is not in this dataset and does nothing in singles
// anyway; Splash is the idle move in its place. Water Pledge is not in the
// dataset either and is only there upstream as a plain Water attack, so the
// damage case uses Surf. Water Sport is replaced by Bulk Up for the same
// reason. Entrainment is the subject of the last case and stays. Abra's
// Teleport is Splash here because Teleport self-switches in this generation and
// the lead has to stay on the field. Gastro Acid and Entrainment are moved to
// the second slot on the bodies that carry them, so `p.leadsEnter` does not
// spend the very move the case is about.
//
// Entry timing. Every case that reads the weather before playing a turn calls
// `p.leadsEnter` first — see the divergence documented on `battle`: this engine
// fires the leads' switch-in hooks at the top of turn 1, not at construction.

func TestAbilitiesPrimordialSea(t *testing.T) {
	describe(t, "Primordial Sea", func(g *psg) {
		g.it("should activate the Primordial Sea weather upon switch-in", func(p *ps) {
			p.battle(
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("splash")}},
				team{{Species: "Abra", Ability: "magicguard", Moves: mv("splash")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "primordialsea", "Primordial Sea should set its weather on switch-in")
		})

		g.it("should increase the damage (not the basePower) of Water-type attacks", func(p *ps) {
			p.battle(
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("surf")}},
				team{{Species: "Blastoise", Ability: "torrent", Moves: mv("splash")}},
			)
			before := p.foe().HP
			p.makeChoices("move surf", "move splash")
			heavyRain := before - p.foe().HP

			p.battle(
				team{{Species: "Kyogre", Ability: "noability", Moves: mv("surf")}},
				team{{Species: "Blastoise", Ability: "torrent", Moves: mv("splash")}},
			)
			before = p.foe().HP
			p.makeChoices("move surf", "move splash")
			plain := before - p.foe().HP

			p.atLeast(plain, 1, "the control Surf should have done damage")
			p.atLeast(heavyRain*10, plain*12, "heavy rain should have raised the Water damage")
		})

		g.it("should cause Fire-type attacks to fail", func(p *ps) {
			p.battle(
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("splash")}},
				team{{Species: "Charizard", Ability: "blaze", Moves: mv("flamethrower")}},
			)
			p.makeChoices("move splash", "move flamethrower")
			p.fullHP(p.mine(), "a Fire attack should fail outright in heavy rain")
		})

		g.it("should not cause Fire-type Status moves to fail", func(p *ps) {
			p.battle(
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("splash")}},
				team{{Species: "Charizard", Ability: "noguard", Moves: mv("willowisp")}},
			)
			p.makeChoices("move splash", "move willowisp")
			p.hasStatus(p.mine(), "brn", "heavy rain should not stop a Fire status move")
		})

		g.it("should prevent moves and abilities from setting the weather to Sunny Day, Rain Dance, Sandstorm, or Hail", func(p *ps) {
			p.battle(
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("splash")}},
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
				p.equal(p.weather(), "primordialsea", "a switch-in should not displace Primordial Sea")
				p.makeChoices("move splash", "move 1")
				p.equal(p.weather(), "primordialsea", "a weather move should not displace Primordial Sea")
			}
		})

		g.it("should be treated as Rain Dance for any forme, move or ability that requires it", func(p *ps) {
			p.battle(
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("psychic", "splash")}},
				team{
					{Species: "Castform", As: "Chansey", Ability: "noability", Moves: mv("weatherball")},
					{Species: "Kingdra", As: "Seadra", Ability: "swiftswim", Moves: mv("focusenergy")},
					{Species: "Ludicolo", As: "Tentacruel", Ability: "raindish", Moves: mv("bulkup")},
					{Species: "Toxicroak", As: "Parasect", Ability: "dryskin", Moves: mv("bulkup")},
					{Species: "Manaphy", As: "Vaporeon", Ability: "hydration", Item: "laggingtail", Moves: mv("rest")},
				},
			)
			p.makeChoices("move psychic", "move weatherball")
			p.makeChoices("move psychic", "switch 2")

			p.makeChoices("move psychic", "switch 3")
			before := p.foe().HP
			p.makeChoices("move splash", "move bulkup")
			p.atLeast(p.foe().HP, before+1, "Rain Dish should heal in Primordial Sea's rain")

			p.makeChoices("move psychic", "switch 4")
			before = p.foe().HP
			p.makeChoices("move splash", "move bulkup")
			p.atLeast(p.foe().HP, before+1, "Dry Skin should heal in Primordial Sea's rain")

			p.makeChoices("move psychic", "switch 5")
			p.makeChoices("move psychic", "move rest")
			p.noStatus(p.foe(), "Hydration should have cured the Rest sleep at end of turn")
		})

		g.it("should cause the Primordial Sea weather to fade if it switches out and no other Primordial Sea Pokemon are active", func(p *ps) {
			p.battle(
				team{
					{Species: "Kyogre", Ability: "primordialsea", Moves: mv("splash")},
					{Species: "Ho-Oh", Ability: "pressure", Moves: mv("roost")},
				},
				team{{Species: "Lugia", Ability: "pressure", Moves: mv("roost")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "primordialsea", "the weather should be up before the holder leaves")
			p.sets(func() any { return p.weather() == "primordialsea" }, false, func() {
				p.makeChoices("switch 2", "move roost")
			}, "Primordial Sea should fade when its only holder leaves")
		})

		g.it("should not cause the Primordial Sea weather to fade if it switches out and another Primordial Sea Pokemon is active", func(p *ps) {
			p.battle(
				team{
					{Species: "Kyogre", Ability: "primordialsea", Moves: mv("splash")},
					{Species: "Ho-Oh", Ability: "pressure", Moves: mv("roost")},
				},
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("bulkup")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "primordialsea", "the weather should be up before the holder leaves")
			p.constant(func() any { return p.weather() }, func() {
				p.makeChoices("switch 2", "move bulkup")
			}, "the second holder should keep Primordial Sea up")
		})

		g.it("should cause the Primordial Sea weather to fade if its ability is suppressed and no other Primordial Sea Pokemon are active", func(p *ps) {
			p.battle(
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("splash")}},
				team{{Species: "Lugia", Ability: "pressure", Moves: mv("roost", "gastroacid")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "primordialsea", "the weather should be up before the ability is suppressed")
			p.sets(func() any { return p.weather() == "primordialsea" }, false, func() {
				p.makeChoices("move splash", "move gastroacid")
			}, "Primordial Sea should fade when its only holder is suppressed")
		})

		g.it("should not cause the Primordial Sea weather to fade if its ability is suppressed and another Primordial Sea Pokemon is active", func(p *ps) {
			p.battle(
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("splash")}},
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("roost", "gastroacid")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "primordialsea", "the weather should be up before the ability is suppressed")
			p.constant(func() any { return p.weather() }, func() {
				p.makeChoices("move splash", "move gastroacid")
			}, "the unsuppressed holder should keep Primordial Sea up")
		})

		g.it("should cause the Primordial Sea weather to fade if its ability is changed and no other Primordial Sea Pokemon are active", func(p *ps) {
			p.battle(
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("splash")}},
				team{{Species: "Lugia", Ability: "pressure", Moves: mv("roost", "entrainment")}},
			)
			p.leadsEnter()
			p.equal(p.weather(), "primordialsea", "the weather should be up before the ability is replaced")
			p.sets(func() any { return p.weather() == "primordialsea" }, false, func() {
				p.makeChoices("move splash", "move entrainment")
			}, "Primordial Sea should fade when its holder loses the ability")
		})
	})
}
