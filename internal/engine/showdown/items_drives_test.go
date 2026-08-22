//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/drives.js.
//
// The same shape as plates.js: four cases per drive, the inner describe (the
// drive) kept as the ledger key. Genesect is not in this 80-species dex and none
// of the four drives is in the item set, so the three cases that turn on the
// holder being Genesect skip.
//
// The fourth says a drive held by anything else comes off like any other item;
// there the Genesect is only a Knock Off user, so Mew stands in for it and Frisk
// is dropped as irrelevant. Azumarill's stand-in Clefable has its ability
// stripped so Cute Charm cannot infatuate on the contact hit. The drive is kept
// and its absence from the dataset is the finding.

func TestItemsDrives(t *testing.T) {
	genesect := "Genesect is not in this 80-species dex and the drives are not in the item set"

	describe(t, "Burn Drive", func(g *psg) {
		g.skip("should not be stolen or removed if held by a Genesect", genesect)
		g.skip("should not be removed by Fling if held by a Genesect", genesect)
		g.skip("should not be given to a Genesect", genesect)

		g.it("should be removed if not held by a Genesect", func(p *ps) {
			p.battle(
				team{{Species: "Genesect", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "burndrive", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a drive held by anything but Genesect should come off")
		})
	})

	describe(t, "Chill Drive", func(g *psg) {
		g.skip("should not be stolen or removed if held by a Genesect", genesect)
		g.skip("should not be removed by Fling if held by a Genesect", genesect)
		g.skip("should not be given to a Genesect", genesect)

		g.it("should be removed if not held by a Genesect", func(p *ps) {
			p.battle(
				team{{Species: "Genesect", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "chilldrive", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a drive held by anything but Genesect should come off")
		})
	})

	describe(t, "Douse Drive", func(g *psg) {
		g.skip("should not be stolen or removed if held by a Genesect", genesect)
		g.skip("should not be removed by Fling if held by a Genesect", genesect)
		g.skip("should not be given to a Genesect", genesect)

		g.it("should be removed if not held by a Genesect", func(p *ps) {
			p.battle(
				team{{Species: "Genesect", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "dousedrive", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a drive held by anything but Genesect should come off")
		})
	})

	describe(t, "Shock Drive", func(g *psg) {
		g.skip("should not be stolen or removed if held by a Genesect", genesect)
		g.skip("should not be removed by Fling if held by a Genesect", genesect)
		g.skip("should not be given to a Genesect", genesect)

		g.it("should be removed if not held by a Genesect", func(p *ps) {
			p.battle(
				team{{Species: "Genesect", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "shockdrive", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a drive held by anything but Genesect should come off")
		})
	})
}
