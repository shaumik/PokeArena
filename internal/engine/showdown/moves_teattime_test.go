//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/teattime.js.
//
// Teatime is not in this dataset, so every live case here stops at "move
// teatime is not in this dataset". That absence is the finding, and the cases
// are written out in full rather than skipped.
//
// Wynaut goes through the shared stand-in table to Hypno on both sides, so the
// two bodies are identical and the Speed EVs upstream sprinkles on one side or
// the other are still the only thing deciding who moves first — which is what
// three of these cases turn on.
//
// A Sitrus Berry is not eaten at full HP, so in every case the Berry going
// missing is Teatime's doing and nothing else's. `splash` stands in for
// upstream's `sleeptalk`, which is not in this dataset and does nothing here.
//
// The first case is the only one that cannot be re-expressed: its subject is
// that Teatime reaches all four Pokemon on the field at once, and this engine
// has two.

func TestMovesTeatTime(t *testing.T) {
	describe(t, "Teatime", func(g *psg) {
		g.skip("should force all Pokemon to eat their Berries immediately", "doubles")

		g.it("should force Pokemon to eat Berries while affected by Unnerve", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "sitrusberry", Moves: mv("splash")}},
				team{{Species: "wynaut", Ability: "unnerve", Moves: mv("teatime")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.noItem(p.mine(), "Unnerve should not stop Teatime from forcing the Berry down")
		})

		g.it("should force Pokemon to eat Berries while Magic Room is active", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "sitrusberry", EVs: evs(map[string]int{"spe": 252}),
					Moves: mv("magicroom")}},
				team{{Species: "wynaut", Moves: mv("teatime")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.noItem(p.mine(), "Magic Room should not stop Teatime from forcing the Berry down")
		})

		g.it("should force Pokemon with Klutz to eat Berries", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "sitrusberry", Ability: "klutz", Moves: mv("splash")}},
				team{{Species: "wynaut", Moves: mv("teatime")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.noItem(p.mine(), "Klutz should not stop Teatime from forcing the Berry down")
		})

		g.it("should force Pokemon with Substitute to eat Berries", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "sitrusberry", EVs: evs(map[string]int{"spe": 252}),
					Moves: mv("substitute")}},
				team{{Species: "wynaut", Moves: mv("teatime")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.noItem(p.mine(), "a Substitute should not stop Teatime from forcing the Berry down")
		})

		g.it("should not cause Pokemon in the semi-invulnerable state to eat their Berries", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "sitrusberry", EVs: evs(map[string]int{"spe": 252}),
					Moves: mv("fly")}},
				team{{Species: "wynaut", Moves: mv("teatime")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.mine().Item, "sitrusberry", "Teatime should not reach a Pokemon in the air")
		})

		g.it("should not cause Recycle to fail to restore the Berry", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "sitrusberry", Moves: mv("recycle")}},
				team{{Species: "wynaut", EVs: evs(map[string]int{"spe": 252}), Moves: mv("teatime")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.mine().Item, "sitrusberry",
				"a Berry eaten by Teatime should still be the one Recycle brings back")
		})
	})
}
