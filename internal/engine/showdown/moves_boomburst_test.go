//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/boomburst.js.
//
// Boomburst is not in this dataset, so the case reports that and stops. The
// fixture is still built to be correct the day the move arrives, because the
// assertion is indirect: what proves Boomburst went through the Substitute is
// that the Focus Sash behind it was spent, and a Sash only fires from full HP
// against a hit that would otherwise KO. So the sequence matters — the target
// puts the Substitute up, then Rests back to full, and Lagging Tail is what
// makes the attacker move after the Rest.
//
// Upstream gets the guaranteed KO from a level 2 Caterpie. Level is fixed at
// 50 here, so the frailty has to come from the fixture instead: Dugtrio is the
// thinnest body in this dex and its HP and Sp. Def IVs are floored, which
// takes it to 95 HP against a Boomburst that cannot roll below 122. Caterpie's
// stand-in row (Butterfree) is not used, because Butterfree survives — and a
// target that survives never fires the Sash, so the case would pass while
// measuring nothing.
//
// Victory Star is upstream's inert filler on a Deoxys that never had it;
// stripping the ability says the same thing without reporting a missing
// ability that the case does not care about. Deoxys-Attack's stand-in is
// Mewtwo, the dex's strongest special attacker, which is what the KO needs.

func TestMovesBoomburst(t *testing.T) {
	describe(t, "Boomburst", func(g *psg) {
		g.it("should pierce through substitutes", func(p *ps) {
			p.battle(
				team{{
					Species: "Deoxys-Attack", Ability: "noability", Item: "laggingtail",
					Moves: mv("splash", "boomburst"),
				}},
				team{{
					Species: "Caterpie", As: "Dugtrio", Ability: "naturalcure", Item: "focussash",
					Moves: mv("substitute", "rest"),
					IVs:   ivs(map[string]int{"hp": 0, "spd": 0}),
				}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move substitute")
			p.makeChoices("move boomburst", "move rest")
			p.noItem(p.foe(), "the Focus Sash should have been spent, which only happens if Boomburst reached past the Substitute")
		})
	})
}
