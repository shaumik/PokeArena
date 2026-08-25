//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/plates.js.
//
// Upstream generates the same four cases for each of seventeen plates; the port
// keeps all of them, and keeps the inner describe name (the plate) as the ledger
// key, matching the nesting upstream uses.
//
// Three of the four cases are about what a plate does *in Arceus's hands* —
// that it cannot be stolen, flung away or handed over. Arceus is not in this
// 80-species dex and Multitype is not modeled, so those skip.
//
// The plates themselves now ship. They are 1.2x type boosters here and nothing
// else (see registerPlates): upstream's onTakeItem refusal keys on either
// party's species being Arceus, and its forcedForme needs Multitype, so both
// guard a case that cannot arise in this dex. That is exactly why the three
// Arceus cases stay skipped while the fourth passes.
//
// The fourth case is not about Arceus at all: it says a plate held by anything
// else comes off like any other item, and the Arceus in it is only a Knock Off
// user. That one is ported, with Mew as the attacker and Multitype dropped (it
// is irrelevant when the plate is on the other side). Azumarill's stand-in
// Clefable has its ability stripped because Cute Charm could infatuate on the
// contact hit and confuse the result.

func TestItemsPlates(t *testing.T) {
	// The plates ship as type boosters; what is missing for these three cases
	// is Arceus itself, without which "held by an Arceus" has no subject.
	arceus := "Arceus is not in this 80-species dex and Multitype is not modeled, " +
		"so a plate's hold-and-forme behavior has no holder to attach to"

	describe(t, "Draco Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "dracoplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Dread Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "dreadplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Earth Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "earthplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Fist Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "fistplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Flame Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "flameplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Icicle Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "icicleplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Insect Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "insectplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Iron Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "ironplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Meadow Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "meadowplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Mind Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "mindplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Pixie Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "pixieplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Sky Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "skyplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Splash Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "splashplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Spooky Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "spookyplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Stone Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "stoneplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Toxic Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "toxicplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})

	describe(t, "Zap Plate", func(g *psg) {
		g.skip("should not be stolen or removed if held by an Arceus", arceus)
		g.skip("should not be removed by Fling if held by an Arceus", arceus)
		g.skip("should not be given to an Arceus", arceus)

		g.it("should be removed if not held by an Arceus", func(p *ps) {
			p.battle(
				team{{Species: "Arceus", As: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Azumarill", Ability: "noability", Item: "zapplate", Moves: mv("bulkup")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.foe(), "a plate held by anything but Arceus should come off")
		})
	})
}
