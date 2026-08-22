//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/toxicspikes.js.
//
// Upstream reads `battle.p2.sideConditions.toxicspikes` directly. The harness
// exposes no side-condition reader, so the port watches the engine's own
// narration instead: the layer going down, and then whether each switch-in
// announces absorbing it.
//
// Substitutions. Glalie is only the lead that is already out when the spikes
// go down, and Lapras is a grounded non-Poison body that does the same job.
// Koffing comes through the shared table as Weezing and keeps Levitate, which
// is the whole point of that slot. Qwilfish is a grounded Poison type wearing
// Heavy-Duty Boots, and Tentacruel is the dex's other Water/Poison. Sleep Talk
// is not in this dataset, so the bodies use Splash for their inert turns.
//
// The second case is doubles and skips: it turns on a second switch-in
// arriving in the same replacement step as the absorber.

func TestMovesToxicSpikes(t *testing.T) {
	describe(t, "Toxic Spikes", func(g *psg) {
		g.it("should be absorbed by grounded Poison types", func(p *ps) {
			p.battle(
				team{{Species: "Muk", Moves: mv("toxicspikes", "splash")}},
				team{
					{Species: "Glalie", As: "Lapras", Moves: mv("splash")},
					{Species: "Koffing", Ability: "levitate", Moves: mv("splash")},
					{Species: "Qwilfish", As: "Tentacruel", Item: "heavydutyboots", Moves: mv("splash")},
				},
			)
			p.turn()
			p.logHas("Poison spikes were scattered", "Toxic Spikes should have gone down on the foe's side")

			p.makeChoices("move splash", "switch 2")
			p.logLacks("absorbed the Toxic Spikes", "a Levitating Poison type should not absorb Toxic Spikes")

			p.makeChoices("move splash", "switch 3")
			p.logHas("absorbed the Toxic Spikes",
				"a grounded Poison type should absorb Toxic Spikes even in Heavy-Duty Boots")
		})

		g.skip("should be disabled immediately when absorbed", "doubles")
	})
}
