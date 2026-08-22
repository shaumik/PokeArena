//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/dryskin.js.
//
// Toxicroak is not in this dex and has no stand-in row. Jynx carries Dry Skin
// natively and, like Toxicroak, outspeeds the Politoed body, which the water
// case needs — the Substitute has to be up before the Water-type move arrives.
// What Jynx does not preserve is Toxicroak's neutrality to Fire and Water:
// ice/psychic takes Fire at 2x. That only affects the magnitude cases, and
// upstream states those as absolute level-100 damage windows, which do not
// transfer to a level-50 engine anyway. Both are re-expressed as comparisons
// against the identical fixture with the ability stripped or ignored, which is
// the claim the windows encode.
//
// Politoed and Haxorus have no stand-in rows either: Poliwrath is the same line
// as Politoed's pre-evolution and keeps water and Damp, and Dragonite is a
// Dragon body with nothing that interferes. Upstream gives Haxorus Unnerve in
// the two damage cases for exactly one reason — so that it is not Mold Breaker —
// and this engine registers Unnerve without modeling it, so the port uses
// "noability", upstream's own idiom for a body that must not interfere.

func TestAbilitiesDrySkin(t *testing.T) {
	describe(t, "Dry Skin", func(g *psg) {
		g.it("should take 1/8 max HP every turn that Sunny Day is active", func(p *ps) {
			p.battle(
				team{{Species: "Toxicroak", As: "Jynx", Ability: "dryskin", Moves: mv("bulkup")}},
				team{{Species: "Ninetales", Ability: "flashfire", Moves: mv("sunnyday")}},
			)
			if p.state() == nil {
				return
			}
			dryMon := p.mine()
			p.hurtsBy(dryMon, dryMon.MaxHP/8, func() {
				p.makeChoices("move bulkup", "move sunnyday")
			}, "Dry Skin should cost 1/8 of max HP under sun")
		})

		g.it("should heal 1/8 max HP every turn that Rain Dance is active", func(p *ps) {
			p.battle(
				team{{Species: "Toxicroak", As: "Jynx", Ability: "dryskin", Moves: mv("substitute")}},
				team{{Species: "Politoed", As: "Poliwrath", Ability: "damp", Moves: mv("encore", "raindance")}},
			)
			if p.state() == nil {
				return
			}
			dryMon := p.mine()
			p.makeChoices("move substitute", "move encore")
			p.hurtsBy(dryMon, -(dryMon.MaxHP / 8), func() {
				p.makeChoices("move substitute", "move raindance")
			}, "Dry Skin should restore 1/8 of max HP under rain")
		})

		g.it("should grant immunity to Water-type moves and heal 1/4 max HP", func(p *ps) {
			p.battle(
				team{{Species: "Toxicroak", As: "Jynx", Ability: "dryskin", Moves: mv("substitute")}},
				team{{Species: "Politoed", As: "Poliwrath", Ability: "damp", Moves: mv("watergun")}},
			)
			p.makeChoices("move substitute", "move watergun")
			p.fullHP(p.mine(), "the Water-type move should have paid back what the Substitute cost")
		})

		g.it("should cause the user to take 1.25x damage from Fire-type attacks", func(p *ps) {
			// Upstream pins this as damage in [51, 61] against [41, 49] without
			// the ability. Absolute figures are level-100, so the port measures
			// the same fixture twice and compares.
			p.battle(
				team{{Species: "Toxicroak", As: "Jynx", Ability: "noability", Moves: mv("bulkup")}},
				team{{Species: "Haxorus", As: "Dragonite", Ability: "noability", Moves: mv("incinerate")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move bulkup", "move incinerate")
			plain := p.mine().MaxHP - p.mine().HP

			p.battle(
				team{{Species: "Toxicroak", As: "Jynx", Ability: "dryskin", Moves: mv("bulkup")}},
				team{{Species: "Haxorus", As: "Dragonite", Ability: "noability", Moves: mv("incinerate")}},
			)
			p.makeChoices("move bulkup", "move incinerate")
			dry := p.mine().MaxHP - p.mine().HP
			p.ok(dry > plain, "Dry Skin should make the Fire-type attack hit harder")
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Toxicroak", As: "Jynx", Ability: "dryskin", Moves: mv("bulkup")}},
				team{{Species: "Haxorus", As: "Dragonite", Ability: "noability", Moves: mv("incinerate")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move bulkup", "move incinerate")
			dry := p.mine().MaxHP - p.mine().HP

			p.battle(
				team{{Species: "Toxicroak", As: "Jynx", Ability: "dryskin", Moves: mv("bulkup")}},
				team{{Species: "Haxorus", As: "Dragonite", Ability: "moldbreaker", Moves: mv("incinerate", "surf")}},
			)
			p.makeChoices("move bulkup", "move incinerate")
			target := p.mine()
			p.ok(target.MaxHP-target.HP < dry, "Mold Breaker should take the 1.25x Fire penalty away")
			p.hurts(target, func() {
				p.makeChoices("move bulkup", "move surf")
			}, "Mold Breaker should take the Water immunity away too")
		})
	})
}
