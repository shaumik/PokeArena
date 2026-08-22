//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/magnetpull.js.
//
// Magnet Pull is in this dataset (Magneton), but the roster fights the file
// hard. Magneton is the only Steel body in the dex, so both the trapper and the
// Steel-type being trapped have to be built from it — the standIns row for
// Heatran gives Ninetales and says outright that steel is not preserved, which
// is the one thing every case here turns on, so it is not used.
//
// Soak and Reflect Type are not in this dataset. They are not filler in the
// first case: they are how it turns the target's Steel typing off and then back
// on again, which is three of the case's four claims, so they are kept and the
// missing-move failure is the finding. That does mean Magnet Pull's basic
// trapping goes unmeasured in this file.
//
// Upstream gives Starmie Illuminate as a deliberately inert ability; this engine
// registers Illuminate but models nothing for it (it changes wild-encounter
// rates), so the port writes "noability" rather than report an inertness the
// case does not care about.
//
// The nested '[Gen 3]' describe is named here by its Mocha full title so the
// ledger key says which file it came from.

func TestAbilitiesMagnetPull(t *testing.T) {
	describe(t, "Magnet Pull", func(g *psg) {
		g.it("should prevent Steel-type Pokemon from switching out normally", func(p *ps) {
			// Both Magnezone and Heatran are built as Magneton: the case needs a
			// Magnet Pull holder and a grounded Steel-type to hold in, and this
			// dex has exactly one Steel body for either job.
			p.battle(
				team{{Species: "Magnezone", As: "Magneton", Ability: "magnetpull", Moves: mv("soak", "charge")}},
				team{
					{Species: "Heatran", As: "Magneton", Ability: "flashfire", Moves: mv("curse")},
					{Species: "Starmie", Ability: "noability", Moves: mv("reflecttype")},
				},
			)
			p.trapped(1, "Magnet Pull should hold a Steel-type in")
			p.makeChoices("move soak", "move curse")
			p.species(p.foe(), "Magneton", "the trapped Steel-type should still be out")
			p.makeChoices("move soak", "switch 2")
			p.species(p.foe(), "Starmie", "Soak should have washed the Steel typing off and freed it")
			p.makeChoices("move charge", "move reflecttype")
			p.trapped(1, "Reflect Type should have made Starmie part Steel, and Steel is trapped")
			p.makeChoices("move charge", "move reflecttype")
			p.species(p.foe(), "Starmie", "the newly Steel Starmie should still be out")
		})

		g.it("should not prevent Steel-type Pokemon from switching out using moves", func(p *ps) {
			p.battle(
				team{{Species: "Magnezone", As: "Magneton", Ability: "magnetpull", Moves: mv("toxic")}},
				team{
					{Species: "Heatran", As: "Magneton", Ability: "flashfire", Moves: mv("batonpass")},
					{Species: "Tentacruel", Ability: "clearbody", Moves: mv("rapidspin")},
				},
			)
			p.makeChoices("move toxic", "move batonpass")
			p.makeChoices("", "switch 2")
			p.species(p.foe(), "Tentacruel", "Baton Pass should get a trapped Steel-type out anyway")
		})

		// Aegislash is Steel/Ghost, and Ghost is the half that matters: the case
		// is a Steel-type Magnet Pull may not hold. Nothing in this dex is both
		// Steel and Ghost, and substituting a plain Ghost would make the case
		// vacuous — Magnet Pull would have no claim on it in the first place.
		g.skip("should not prevent Pokemon immune to trapping from switching out",
			"Aegislash is not in this 80-species dex and no in-dex species is both Steel and immune to trapping")
	})

	describe(t, "Magnet Pull [Gen 3]", func(g *psg) {
		g.skip("should prevent Steel-type allies from switching out normally", "gen 3 mechanics")
	})
}
