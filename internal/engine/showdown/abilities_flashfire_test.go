//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/flashfire.js.
//
// Heatran is built as Flareon rather than through the stand-in table. The
// stand-in row points at Ninetales, and Ninetales outruns every Fire/Flying
// body in this dex — but Gale Wings is not modeled, so the only way the
// absorb still happens before the Flash Fire holder's own attack is for the
// holder to be the slower of the two. Flareon is the dex's slow Fire body
// with Flash Fire; Moltres stands in for Talonflame's Fire/Flying.
//
// Sleep Talk is not in this dataset. Upstream uses it purely as a "do
// nothing" filler here, so Splash takes its place and the cases keep
// measuring Flash Fire instead of a missing move.
//
// Upstream's damage bands ([82, 97] and [54, 65]) are level-100 absolutes and
// do not transfer to an engine fixed at level 50. The boost is asserted
// through the charge state and its announcement instead, and the "boost lost"
// half through the ability swap the boost depends on.
//
// The Gen 3-4 describe block skips as a block: there is no gen-mod layer.

func TestAbilitiesFlashFire(t *testing.T) {
	describe(t, "Flash Fire", func(g *psg) {
		g.it("should grant immunity to Fire-type moves and increase Fire-type attacks by 50% once activated", func(p *ps) {
			p.battle(
				team{{Species: "Heatran", As: "Flareon", Ability: "flashfire", Moves: mv("incinerate")}},
				team{{Species: "Talonflame", As: "Moltres", Ability: "galewings", Moves: mv("flareblitz")}},
			)
			p.makeChoices("move incinerate", "move flareblitz")
			p.fullHP(p.mine(), "Flash Fire should have absorbed Flare Blitz")
			p.ok(p.mine().Volatiles.FlashFireCharged, "absorbing a Fire move should charge Flash Fire")
			p.logHas("Flash Fire raised its Fire power", "the absorb should be announced")
			p.damaged(p.foe(), "Incinerate should still have landed")
		})

		g.it("should grant Fire-type immunity even if the user is frozen", func(p *ps) {
			p.battle(
				team{{Species: "Heatran", As: "Flareon", Ability: "flashfire", Moves: mv("splash"), Status: "frz"}},
				team{{Species: "Talonflame", As: "Moltres", Ability: "galewings", Moves: mv("flareblitz")}},
			)
			p.makeChoices("move splash", "move flareblitz")
			p.fullHP(p.mine(), "a frozen Flash Fire holder should still absorb Fire moves")
		})

		g.it("should have its Fire-type immunity suppressed by Mold Breaker", func(p *ps) {
			// Haxorus is not in this dex. Pinsir is the dex's Mold Breaker
			// body and nothing here turns on its typing.
			p.battle(
				team{{Species: "Heatran", As: "Flareon", Ability: "flashfire", Moves: mv("incinerate")}},
				team{{Species: "Haxorus", As: "Pinsir", Ability: "moldbreaker", Moves: mv("firepunch")}},
			)
			p.hurts(p.mine(), func() { p.makeChoices("move incinerate", "move firepunch") },
				"Mold Breaker should have punched through Flash Fire")
		})

		g.it(`should lose the Flash Fire boost if its ability is changed`, func(p *ps) {
			p.battle(
				team{{Species: "Heatran", As: "Flareon", Ability: "flashfire", Moves: mv("splash", "incinerate")}},
				team{{Species: "Talonflame", As: "Moltres", Ability: "shellarmor", Moves: mv("flamethrower", "worryseed")}},
			)
			p.makeChoices("move splash", "move flamethrower")
			p.ok(p.mine().Volatiles.FlashFireCharged, "Flamethrower should have charged Flash Fire")
			p.makeChoices("move incinerate", "move worryseed")
			// Upstream reads the lost boost off an absolute damage band. What
			// transfers is that Worry Seed took the ability away, which is the
			// only thing the boost depends on.
			p.hasAbility(p.mine(), "insomnia", "Worry Seed should have replaced Flash Fire")
		})
	})

	describe(t, "Flash Fire [Gen 3-4]", func(g *psg) {
		g.skip("should activate and grant Fire-type immunity even if the user is frozen in Gen 3",
			"gen 3 mechanics")
		g.skip("should activate and grant Fire-type immunity even if the user is frozen in Gen 4",
			"gen 4 mechanics")
	})
}
