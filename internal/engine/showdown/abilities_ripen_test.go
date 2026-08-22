//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/ripen.js.
//
// Ripen is not one of this engine's 118 abilities, so every case reports it,
// and that report is the finding. The fixtures are otherwise left as upstream
// wrote them so the port still diffs against the original: Wynaut resolves to
// Hypno through the stand-in table, Falinks becomes Machamp (a fighting body
// is all its case wants), and Super Fang — not in this dataset — is kept,
// because halving the holder's HP is how those two cases arm the berry rather
// than incidental scaffolding.
//
// The two resist-berry cases are restated. Upstream pins them with absolute
// damage windows at level 100 ("[18, 21], and 36-43 if it were only halved"),
// and no absolute figure survives a move to level 50. Each is measured twice
// instead — once with Ripen, once with the same Colbur Berry and no Ripen —
// and the quartering is asserted as the ratio between them.
//
// Lucky Chant, which upstream uses to keep a critical hit out of the first of
// those windows, is not in this dataset. The crit-free case therefore has a
// 1-in-24 exposure per measurement should Ripen ever be modeled; the
// critical-hit case has none, because Laser Focus makes both of its
// measurements crits.

func TestAbilitiesRipen(t *testing.T) {
	describe(t, "Ripen", func(g *psg) {
		g.it("should double healing from Berries", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Ability: "ripen", Item: "sitrusberry",
					IVs: ivs(map[string]int{"hp": 30}), Moves: mv("splash")}},
				team{{Species: "wynaut", Ability: "compoundeyes", Moves: mv("superfang")}},
			)
			p.turn()
			mon := p.mine()
			p.equal(mon.HP, mon.MaxHP/2+(mon.MaxHP/4)*2, "Ripen should have doubled the Sitrus Berry's healing")
		})

		g.it("should double stat boosts from Berries", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Ability: "ripen", Item: "liechiberry",
					EVs: evs(map[string]int{"hp": 4}), Moves: mv("splash")}},
				team{{Species: "wynaut", Ability: "compoundeyes", Moves: mv("superfang")}},
			)
			p.turn()
			p.turn()
			p.statStage(p.mine(), "atk", 2, "Ripen should have doubled the Liechi Berry's boost")
		})

		g.it("should double damage done from Jaboca / Rowap Berries", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Ability: "ripen", Item: "jabocaberry", Moves: mv("splash")}},
				team{{Species: "falinks", As: "Machamp", Moves: mv("tackle")}},
			)
			p.turn()
			falinks := p.foe()
			p.equal(falinks.HP, falinks.MaxHP-falinks.MaxHP/4, "Falinks should have lost 1/4 of its HP")
		})

		g.it("should allow resist Berries to quarter the damage done", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "colburberry",
					EVs: evs(map[string]int{"spe": 4}), Moves: mv("splash")}},
				team{{Species: "wynaut", Moves: mv("darkpulse")}},
			)
			p.turn()
			plain := p.mine().MaxHP - p.mine().HP

			p.battle(
				team{{Species: "wynaut", Ability: "ripen", Item: "colburberry",
					EVs: evs(map[string]int{"spe": 4}), Moves: mv("splash")}},
				team{{Species: "wynaut", Moves: mv("darkpulse")}},
			)
			p.turn()
			ripened := p.mine().MaxHP - p.mine().HP

			p.atMost(ripened, plain*3/5, "a Ripened Colbur Berry should quarter rather than halve the damage")
		})

		g.it("should allow resist Berries to quarter the damage done even on a critical hit", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "colburberry",
					EVs: evs(map[string]int{"spe": 4}), Moves: mv("splash")}},
				team{{Species: "wynaut", Moves: mv("laserfocus", "darkpulse")}},
			)
			p.turn()
			p.makeChoices("auto", "move darkpulse")
			plain := p.mine().MaxHP - p.mine().HP

			p.battle(
				team{{Species: "wynaut", Ability: "ripen", Item: "colburberry",
					EVs: evs(map[string]int{"spe": 4}), Moves: mv("splash")}},
				team{{Species: "wynaut", Moves: mv("laserfocus", "darkpulse")}},
			)
			p.turn()
			p.makeChoices("auto", "move darkpulse")
			ripened := p.mine().MaxHP - p.mine().HP

			p.atMost(ripened, plain*3/5, "the quartering should survive a critical hit")
		})

		g.it("should double the effects of Berries eaten by Fling", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Ability: "ripen", Moves: mv("splash")}},
				team{{Species: "wynaut", Item: "liechiberry", Moves: mv("fling")}},
			)
			p.turn()
			p.statStage(p.mine(), "atk", 2, "Ripen should have doubled a berry it was flung")
		})

		g.it("should double the effects of Berries eaten by Bug Bite", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Ability: "ripen", Moves: mv("bugbite")}},
				team{{Species: "wynaut", Item: "liechiberry", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.mine(), "atk", 2, "Ripen should have doubled a berry it ate with Bug Bite")
		})
	})
}
