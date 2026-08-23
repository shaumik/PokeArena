//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/heavydutyboots.js.
//
// Straight across. Magikarp goes through its stand-in row (Seaking), which
// keeps the water typing that leaves it grounded and so exposed to both layers
// of hazard the case sets.
//
// The Toxic Spikes half is asserted as "no status at all" rather than upstream's
// notEqual against 'psn': nothing else in the fixture can inflict a status, and
// the harness compares status names literally, so the looser form would pass
// against this engine's "poison" for the wrong reason.

func TestItemsHeavyDutyBoots(t *testing.T) {
	describe(t, "Heavy Duty Boots", func(g *psg) {
		g.it("should prevent entry hazards from affecting the holder", func(p *ps) {
			p.battle(
				team{
					{Species: "Magikarp", Ability: "swiftswim", Moves: mv("splash")},
					{Species: "Magikarp", Ability: "swiftswim", Item: "heavydutyboots", Moves: mv("splash")},
				},
				team{{Species: "Cloyster", Ability: "shellarmor", Moves: mv("spikes", "toxicspikes")}},
			)
			p.makeChoices("auto", "move spikes")
			p.makeChoices("auto", "move toxicspikes")
			p.makeChoices("switch 2", "auto")
			p.fullHP(p.mine(), "Heavy-Duty Boots should have kept Spikes off the holder")
			p.noStatus(p.mine(), "Heavy-Duty Boots should have kept Toxic Spikes off the holder")
		})
	})
}
