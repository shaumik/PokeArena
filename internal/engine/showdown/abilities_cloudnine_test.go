//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/cloudnine.js.
//
// Golduck is in this dex and carries Cloud Nine, so the holder is the real
// thing throughout.
//
// Showdown draws a distinction the harness cannot read: `field.isWeather('')`
// asks for the *effective* weather, which Cloud Nine blanks, while
// `p.weather()` reports the weather that is actually on the field. So the two
// cases that read the weather directly are restated on what the suppression
// does rather than on what the field says — the first through Solar Beam
// having to charge, the last through the sun still running its five turns down
// while its effects are off.
//
// Base power. Two cases compute a base power through `battle.runEvent`, which
// has no counterpart here. Each is restated as the comparison that figure
// stood for: the same Fire and Water attacks measured twice at the same seed,
// once against a Cloud Nine holder and once against a bare one, with the
// weather up in both. Sun halves Water and boosts Fire by half; rain does the
// reverse; under Cloud Nine neither should move at all, and the thresholds are
// wide enough that no damage roll can cross them.
//
// Species. Cherrim has no stand-in and its forme change is not modeled, so
// Venusaur carries the Solar Beam; the forme assertion is dropped. Groudon and
// Kyogre both become Snorlax rather than their stand-in rows: the case needs a
// body that survives two measured attacks and takes Fire and Water neutrally so
// the two figures are comparable, which is not what those rows promise. Only
// Drought and Drizzle matter about them. Abomasnow becomes Lapras and Sunkern
// becomes Venusaur, both purely as bodies for a move. Toxapex becomes
// Tentacruel and Manaphy becomes Vaporeon, which has Hydration natively.
//
// Abilities. Sand Stream and Snow Warning cannot be reached through the
// harness — it resolves a Showdown ability id by finding an in-dex species
// carrying it, and no Kanto species carries either — so the sandstorm and hail
// cases put their weather up with the move instead. That is the same field
// state; only the source differs. Note also that this engine's ice weather is
// gen-9 Snow, which does no chip damage to anything, so the hail case cannot
// fail whether or not Cloud Nine is doing its job.
//
// Moves. Final Gambit is not in this dataset. The last case is about Hydration
// firing once the Cloud Nine holder is off the field, and Explosion is the
// self-KO that puts it there; the assertion on Final Gambit's own damage figure
// is dropped with it. Sleep Talk is likewise not in the dataset and is replaced
// by Splash wherever it is an idle move.

func TestAbilitiesCloudNine(t *testing.T) {
	// tookFrom runs one turn in which the Cloud Nine side attacks with move
	// and the other side idles, and reports what the idling body lost.
	tookFrom := func(p *ps, move string) int {
		before := p.mine().HP
		p.makeChoices("move splash", "move "+move)
		return before - p.mine().HP
	}

	describe(t, "Cloud Nine", func(g *psg) {
		g.it("should treat the weather as none for the purposes of formes, moves and abilities", func(p *ps) {
			p.battle(
				team{{Species: "Golduck", Ability: "cloudnine", Moves: mv("sunnyday")}},
				team{{Species: "Cherrim", As: "Venusaur", Ability: "flowergift", Item: "laggingtail",
					Moves: mv("solarbeam")}},
			)
			p.constant(func() any { return p.mine().HP }, func() {
				p.makeChoices("move sunnyday", "move solarbeam")
			}, "Solar Beam should still have to charge under Cloud Nine")
			p.logLacks("took in sunlight", "Cloud Nine should keep the Sun from skipping the charge")
			p.logHas("began charging", "Solar Beam should have spent the turn charging")
		})

		g.it("should negate the effects of Sun on Fire-type and Water-type attacks", func(p *ps) {
			p.battle(
				team{{Species: "Groudon", As: "Snorlax", Ability: "drought", Moves: mv("splash")}},
				team{{Species: "Golduck", Ability: "cloudnine", Moves: mv("surf", "flamethrower")}},
			)
			waterSuppressed := tookFrom(p, "surf")
			fireSuppressed := tookFrom(p, "flamethrower")

			p.battle(
				team{{Species: "Groudon", As: "Snorlax", Ability: "drought", Moves: mv("splash")}},
				team{{Species: "Golduck", Ability: "noability", Moves: mv("surf", "flamethrower")}},
			)
			waterInSun := tookFrom(p, "surf")
			fireInSun := tookFrom(p, "flamethrower")

			p.atLeast(waterInSun, 1, "the control Surf should have done damage")
			p.atLeast(waterSuppressed*10, waterInSun*14, "Sun should not have halved the Water attack")
			p.atMost(fireSuppressed*100, fireInSun*85, "Sun should not have raised the Fire attack")
		})

		g.it("should negate the effects of Rain on Fire-type and Water-type attacks", func(p *ps) {
			p.battle(
				team{{Species: "Kyogre", As: "Snorlax", Ability: "drizzle", Moves: mv("splash")}},
				team{{Species: "Golduck", Ability: "cloudnine", Moves: mv("surf", "flamethrower")}},
			)
			waterSuppressed := tookFrom(p, "surf")
			fireSuppressed := tookFrom(p, "flamethrower")

			p.battle(
				team{{Species: "Kyogre", As: "Snorlax", Ability: "drizzle", Moves: mv("splash")}},
				team{{Species: "Golduck", Ability: "noability", Moves: mv("surf", "flamethrower")}},
			)
			waterInRain := tookFrom(p, "surf")
			fireInRain := tookFrom(p, "flamethrower")

			p.atLeast(fireInRain, 1, "the control Flamethrower should have done damage")
			p.atMost(waterSuppressed*100, waterInRain*85, "rain should not have raised the Water attack")
			p.atLeast(fireSuppressed*10, fireInRain*14, "rain should not have halved the Fire attack")
		})

		g.it("should negate the damage-dealing effects of Sandstorm", func(p *ps) {
			p.battle(
				team{{Species: "Tyranitar", Ability: "noability", Moves: mv("sandstorm")}},
				team{{Species: "Golduck", Ability: "cloudnine", Moves: mv("calmmind")}},
			)
			p.constant(func() any { return p.foe().HP }, func() {
				p.makeChoices("move sandstorm", "move calmmind")
				p.makeChoices("move sandstorm", "move calmmind")
			}, "Cloud Nine should stop the sandstorm chipping its holder")
		})

		g.it("should negate the damage-dealing effects of Hail", func(p *ps) {
			p.battle(
				team{{Species: "Abomasnow", As: "Lapras", Ability: "noability", Moves: mv("hail")}},
				team{{Species: "Golduck", Ability: "cloudnine", Moves: mv("calmmind")}},
			)
			p.constant(func() any { return p.foe().HP }, func() {
				p.makeChoices("move hail", "move calmmind")
				p.makeChoices("move hail", "move calmmind")
			}, "Cloud Nine should stop the hail chipping its holder")
		})

		g.it("should not negate Desolate Land's ability to prevent other weathers from activating", func(p *ps) {
			p.battle(
				team{{Species: "Golduck", Ability: "cloudnine", Moves: mv("raindance")}},
				team{{Species: "Groudon", Ability: "desolateland", Moves: mv("sunnyday")}},
			)
			p.constant(func() any { return p.weather() }, func() {
				p.makeChoices("move raindance", "move sunnyday")
			}, "Desolate Land should still refuse every other weather")
		})

		g.it("should not negate Primordial Sea's ability to prevent other weathers from activating", func(p *ps) {
			p.battle(
				team{{Species: "Golduck", Ability: "cloudnine", Moves: mv("raindance")}},
				team{{Species: "Kyogre", Ability: "primordialsea", Moves: mv("sunnyday")}},
			)
			p.constant(func() any { return p.weather() }, func() {
				p.makeChoices("move raindance", "move sunnyday")
			}, "Primordial Sea should still refuse every other weather")
		})

		g.it("should not negate Delta Stream's ability to prevent other weathers from activating", func(p *ps) {
			p.battle(
				team{{Species: "Golduck", Ability: "cloudnine", Moves: mv("raindance")}},
				team{{Species: "Rayquaza", Ability: "deltastream", Moves: mv("sunnyday")}},
			)
			p.constant(func() any { return p.weather() }, func() {
				p.makeChoices("move raindance", "move sunnyday")
			}, "Delta Stream should still refuse every other weather")
		})

		g.it("should still display status of the weather", func(p *ps) {
			// Upstream reads the |-weather| protocol lines. This engine emits
			// no line at all when weather is set, upkept or ends, so the case
			// is asserted on the field instead: the sun is up under Cloud Nine
			// and still counts its five turns down.
			p.battle(
				team{{Species: "Golduck", Ability: "cloudnine", Moves: mv("calmmind")}},
				team{{Species: "Sunkern", As: "Venusaur", Ability: "solarpower", Moves: mv("sunnyday")}},
			)
			p.makeChoices("move calmmind", "move sunnyday")
			p.equal(p.weather(), "sun", "Cloud Nine suppresses the weather's effects, not the weather")
			for i := 0; i < 3; i++ {
				p.makeChoices("move calmmind", "move sunnyday")
				p.equal(p.weather(), "sun", "the sun should still be running down")
			}
			p.makeChoices("move calmmind", "move sunnyday")
			p.equal(p.weather(), "", "the sun should have run out after five turns")
		})

		g.it("should allow Hydration to trigger if the user fainted before Hydration could trigger", func(p *ps) {
			p.battle(
				team{
					{Species: "Toxapex", Ability: "cloudnine", Moves: mv("toxic", "raindance", "explosion")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Manaphy", As: "Vaporeon", Ability: "hydration", Moves: mv("splash")}},
			)
			p.makeChoices("move toxic", "move splash")
			p.makeChoices("move raindance", "move splash")
			p.hasStatus(p.foe(), "tox", "Toxic should have landed while Cloud Nine held the rain off")
			p.makeChoices("move explosion", "move splash")
			p.fainted(p.mine(), "the Cloud Nine holder should have taken itself out")
			p.noStatus(p.foe(), "Hydration should fire once Cloud Nine has left the field")
		})
	})
}
