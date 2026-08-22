//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/burningjealousy.js.
//
// Every case turns on ordering — the boost has to happen before, after, or on a
// different turn from the attack — so each substitution below is chosen for its
// speed tier as much as its typing.
//
// Torkoal is built as Magmar (Fire, so Burning Jealousy keeps its STAB, and
// slower than Mew exactly as Torkoal is). Magearna is built as Magneton, which
// is what the shared table uses for a Steel body; the case needs a target that
// takes a super-effective Fire hit so its Weakness Policy goes off after the
// attack, and Steel supplies that. Cobalion and Darmanitan become Primeape and
// Flareon: Cobalion's Steel half is lost, but all the case needs is a target
// that raises a stat, survives the hit, and moves first — Primeape outspeeds
// Flareon the way Cobalion outspeeds Darmanitan.
//
// The Download case deliberately does not call leadsEnter. Upstream has
// Download fire at battle start and Burning Jealousy land on turn 1; this engine
// fires lead switch-in abilities at the top of turn 1, so playing the turn
// straight reproduces upstream's "boosted, then attacked" ordering. Adding a
// leadsEnter turn would move the boost into a *previous* turn and quietly test
// the opposite case.
//
// `memento` and `sleeptalk` are not in this dataset; splash stands in for
// sleeptalk, and Memento's absence is reported as the finding it is.

func TestMovesBurningJealousy(t *testing.T) {
	describe(t, "Burning Jealousy", func(g *psg) {
		g.it(`should burn a target whose stats were raised this turn`, func(p *ps) {
			p.battle(
				team{{Species: "Mew", Moves: mv("dragondance")}},
				team{{Species: "Torkoal", As: "Magmar", Moves: mv("burningjealousy")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.hasStatus(p.mine(), "brn", "Dragon Dance this turn should make Burning Jealousy burn")
		})

		g.it(`should not burn a target whose stats were raised after the attack`, func(p *ps) {
			p.battle(
				team{{Species: "Ninetales", Moves: mv("burningjealousy")}},
				team{{Species: "Magearna", As: "Magneton", Item: "weaknesspolicy", Moves: mv("imprison")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.noStatus(p.foe(), "a Weakness Policy that goes off on the hit itself is too late to be burned for")
		})

		g.it(`should burn a target whose stats were boosted at the start of the match`, func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("burningjealousy")}},
				team{{Species: "Porygon", Ability: "download", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.hasStatus(p.foe(), "brn", "a boost taken on entry should still count as raised this turn")
		})

		g.it(`should not burn a target whose stats were boosted at a switch after a KO`, func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("burningjealousy")}},
				team{
					{Species: "Porygon", Ability: "download", Moves: mv("memento")},
					{Species: "Porygon2", As: "Porygon", Ability: "download", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.makeChoices("", "switch 2")
			p.turn()
			p.noStatus(p.foe(), "a Download boost taken on a replacement switch is not a boost taken this turn")
		})

		g.it(`should be affected by Sheer Force`, func(p *ps) {
			p.battle(
				team{{Species: "Cobalion", As: "Primeape", Moves: mv("swordsdance")}},
				team{{Species: "Darmanitan", As: "Flareon", Ability: "sheerforce", Item: "kingsrock", Moves: mv("burningjealousy")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.noStatus(p.mine(), "Sheer Force should strip Burning Jealousy's burn along with every other secondary")
		})
	})
}
