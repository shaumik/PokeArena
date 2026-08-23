//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/jabocaberry.js.
//
// Sleep Talk is not in this dataset and is inert filler for the berry holder, so
// Splash stands in for it. Cramorant is only a body that holds the berry and
// takes a hit; Gyarados keeps its Water/Flying typing, with its ability stripped
// so Intimidate cannot interfere with the attacker.
//
// Morpeko is only an Aura Wheel user; Raichu keeps the Electric typing and the
// speed. Aura Wheel itself is not in this dataset and is the move the case turns
// on, so it is kept and the missing-move failure is the finding.

func TestItemsJabocaBerry(t *testing.T) {
	describe(t, "Jaboca Berry", func(g *psg) {
		g.it("should activate after a physical move", func(p *ps) {
			p.battle(
				team{{
					Species: "Charizard", EVs: evs(map[string]int{"hp": 252}),
					Moves: mv("scratch", "ember"),
				}},
				team{{
					Species: "Cramorant", As: "Gyarados", Ability: "noability", Item: "jabocaberry",
					Moves: mv("splash"),
				}},
			)
			charizard := p.mine()
			p.makeChoices("move ember", "default")
			p.fullHP(charizard, "a special move should not wake the berry")
			p.hurtsBy(charizard, charizard.MaxHP/8, func() { p.turn() },
				"Jaboca Berry should take an eighth off the physical attacker")
		})

		g.it("should activate even if the holder has 0 HP", func(p *ps) {
			p.battle(
				team{{
					Species: "Morpeko", As: "Raichu", EVs: evs(map[string]int{"hp": 252}),
					Moves: mv("aurawheel"),
				}},
				team{{
					Species: "Cramorant", As: "Gyarados", Ability: "noability", Item: "jabocaberry",
					Moves: mv("splash"),
				}},
			)
			if p.state() == nil {
				return
			}
			morpeko := p.mine()
			p.hurtsBy(morpeko, morpeko.MaxHP/8, func() { p.turn() },
				"the berry should still fire from a holder the hit knocked out")
		})

		g.it("should not activate after a physical move used by a Pokemon with Magic Guard", func(p *ps) {
			p.battle(
				team{{Species: "Clefable", Ability: "magicguard", Moves: mv("pound")}},
				team{{
					Species: "Cramorant", As: "Gyarados", Ability: "noability", Item: "jabocaberry",
					Moves: mv("splash"),
				}},
			)
			p.turn()
			p.fullHP(p.mine(), "Magic Guard should refuse the berry's damage")
			p.holdsItem(p.foe(), "the berry should not have been spent")
		})
	})
}
