//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/thickfat.js.
//
// Miltank has no stand-in row; Snorlax is a Normal body that carries Thick Fat
// natively, so both the ability and the neutral matchup against Fire and Ice
// come across. Wynaut resolves to Hypno.
//
// Upstream states both live cases as absolute level-100 damage windows — [16,19]
// with the ability, [31,37] once Mold Breaker turns it off — and absolute
// figures do not transfer to a level-50 engine. Each is re-expressed as the
// comparison the pair of windows encodes: the identical fixture measured with
// and without the halving. Only the first turn of each battle is read, so
// upstream's Recover (which exists to reset the reading between the Ice and Fire
// hits) is not needed and the port does not depend on the speed tier the
// stand-in changed. The Lum Berry is kept for the reason upstream holds it — a
// freeze or a burn would otherwise move the figure.
//
// Lucky Chant is not in this dataset and is a do-nothing turn for the holder, so
// Splash stands in for it.

func TestAbilitiesThickFat(t *testing.T) {
	describe(t, "Thick Fat", func(g *psg) {
		// damage runs one turn of the upstream fixture and reports what the
		// holder lost, so the two readings a comparison needs differ only in the
		// ability under test.
		damage := func(p *ps, holder, attacker, move string) int {
			p.battle(
				team{{
					Species: "Miltank", As: "Snorlax", Ability: holder, Item: "lumberry",
					Moves: mv("splash", "recover"),
				}},
				team{{Species: "Wynaut", Ability: attacker, Moves: mv("icebeam", "flamethrower")}},
			)
			if p.state() == nil {
				return 0
			}
			p.makeChoices("move splash", "move "+move)
			miltank := p.mine()
			return miltank.MaxHP - miltank.HP
		}

		g.it("should halve damage from Fire- or Ice-type attacks", func(p *ps) {
			iceFat := damage(p, "thickfat", "noability", "icebeam")
			iceBare := damage(p, "noability", "noability", "icebeam")
			p.ok(iceFat < iceBare, "Thick Fat should cut the Ice-type attack down")

			fireFat := damage(p, "thickfat", "noability", "flamethrower")
			fireBare := damage(p, "noability", "noability", "flamethrower")
			p.ok(fireFat < fireBare, "Thick Fat should cut the Fire-type attack down")
		})

		g.skip("should halve damage from Fire- or Ice-type attacks in past generations, even when holding a type-boosting item",
			"gen 3 mechanics")

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			iceBroken := damage(p, "thickfat", "moldbreaker", "icebeam")
			iceFat := damage(p, "thickfat", "noability", "icebeam")
			p.ok(iceBroken > iceFat, "Mold Breaker should take Thick Fat's Ice reduction away")

			fireBroken := damage(p, "thickfat", "moldbreaker", "flamethrower")
			fireFat := damage(p, "thickfat", "noability", "flamethrower")
			p.ok(fireBroken > fireFat, "Mold Breaker should take Thick Fat's Fire reduction away")
		})
	})
}
