//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/focussash.js.
//
// Sleep Talk is not in this dataset and is inert filler for the holder, so
// Splash stands in for it. Delphox is only a Fire-type special attacker here;
// Ninetales is the in-dex one. Magician is dropped with it: the Sash is spent
// before the ability could reach it, so nothing in the case turns on it.
//
// The last three cases all need Shedinja, and they need it for its one hit
// point rather than as a body — a Pokemon at full HP that dies to confusion
// self-damage, to its own recoil, or to a single tick of poison. Nothing in this
// 80-species dex has that shape, and Wonder Guard is not modeled either, so they
// skip rather than being restated as something they are not.

func TestItemsFocusSash(t *testing.T) {
	describe(t, "Focus Sash", func(g *psg) {
		g.it("should be consumed and allow its user to survive an attack from full HP", func(p *ps) {
			p.battle(
				team{{Species: "Paras", Ability: "dryskin", Item: "focussash", Moves: mv("splash")}},
				team{{Species: "Delphox", As: "Ninetales", Moves: mv("incinerate")}},
			)
			p.makeChoices("move splash", "move incinerate")
			holder := p.mine()
			p.noItem(holder, "the Focus Sash should have been spent")
			p.notFainted(holder, "the Focus Sash should have kept the holder alive")
			p.equal(holder.HP, 1, "a Focus Sash save leaves exactly 1 HP")
		})

		g.skip("should be consumed and allow its user to survive a confusion damage hit from full HP",
			"Shedinja is not in this 80-species dex and Wonder Guard is not modeled; no in-dex "+
				"body dies to confusion self-damage from full HP")

		g.skip("should not trigger on recoil damage",
			"Shedinja is not in this 80-species dex and Wonder Guard is not modeled; no in-dex "+
				"body dies to its own recoil from full HP")

		g.skip("should not trigger on residual damage",
			"Shedinja is not in this 80-species dex and Wonder Guard is not modeled; no in-dex "+
				"body dies to one tick of residual damage from full HP")
	})
}
