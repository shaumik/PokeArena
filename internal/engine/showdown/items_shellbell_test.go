//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/shellbell.js.
//
// The first case is doubles — it spreads Earthquake over two targets and adds up
// the healing — so it skips.
//
// The second is singles, but its scaffolding does not survive. Upstream uses
// Final Gambit (not in this dataset) to put the Shell Bell holder on exactly
// 1 HP, then switches a fresh target in and measures the heal off a five-hit
// Icicle Spear. The harness's HP field arranges the same starting state without
// the scaffolding, so the port sets the holder to 1 HP directly and keeps the
// measurement, which is what the case is actually about. Landorus is only the
// target whose max HP the heal is a fraction of; Dragonite is in the dex and is
// 4x weak to Ice, so the five hits take it out and the whole of its HP bar is
// the damage the bell sees. Sleep Talk is inert filler on both sides and Splash
// stands in for it.

func TestItemsShellBell(t *testing.T) {
	describe(t, "Shell Bell", func(g *psg) {
		g.skip("should heal from the damage against all targets of the move", "doubles")

		g.it("should heal from the damage from all hits of multi-hit moves", func(p *ps) {
			p.battle(
				team{{Species: "landorus", As: "Dragonite", Moves: mv("splash")}},
				team{{Species: "cloyster", Ability: "skilllink", Item: "shellbell", HP: 1,
					Moves: mv("splash", "iciclespear")}},
			)
			p.makeChoices("move splash", "move iciclespear")
			landorus := p.mine()
			cloyster := p.foe()
			p.equal(cloyster.HP, 1+landorus.MaxHP/8,
				"Shell Bell should heal an eighth of every hit, not just the last one")
		})
	})
}
