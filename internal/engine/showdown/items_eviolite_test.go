//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/eviolite.js.
//
// Eviolite is not in this item set, and the roster is 80 fully-evolved species,
// so nothing in this dex can evolve and the item could never apply to anything
// here. The two cases whose subject is a Pokemon that *can* evolve therefore
// skip — Omanyte's and Geodude's stand-ins are their evolutions, which is
// exactly the property the case needs and the stand-in does not preserve.
//
// The third case is the one this dex can state: Omastar is in the dex, cannot
// evolve, and should get nothing from the item. Sceptile is only a Grass-type
// attacker, so Venusaur stands in; both its item and Eviolite are kept, and the
// missing-item failure is the finding.

func TestItemsEviolite(t *testing.T) {
	describe(t, "Eviolite", func(g *psg) {
		g.skip("should multiply the defenses of a Pokemon that can evolve by 1.5",
			"this dex is 80 fully-evolved species, so nothing in it can evolve; "+
				"Eviolite is not in the item set either")

		g.it("should not multiply the defenses of a Pokemon that cannot evolve by 1.5", func(p *ps) {
			p.battle(
				team{
					{Species: "Omastar", Ability: "shellarmor", Item: "eviolite", Moves: mv("rest")},
					{Species: "Omastar", Ability: "shellarmor", Item: "eviolite", Moves: mv("rest")},
				},
				team{{Species: "Sceptile", As: "Venusaur", Item: "meadowplate",
					Moves: mv("leafblade", "megadrain")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.fainted(p.mine(), "")
			p.turn() // switch in the second Omastar
			p.makeChoices("", "move megadrain")
			p.fainted(p.mine(), "")
		})

		g.skip("should multiply the defenses of a National Dex Pokemon that can evolve by 1.5",
			"this dex is 80 fully-evolved species, so nothing in it can evolve; "+
				"Eviolite is not in the item set either")
	})
}
