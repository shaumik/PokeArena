//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/pickpocket.js.
//
// Pickpocket is not in this dataset. The ability is still set on the fixtures so
// the three singles cases run: the steal never happens, which makes the first
// case fail and the two negative cases pass for the wrong reason, and that is
// the finding rather than a reason to skip.
//
// Weavile and Duraludon have no stand-in rows. Persian is a fast, frail body
// that Quick Attack hits neutrally, which is all the first case needs of
// Weavile; Dragonite is a Dragon body that can throw Dragon Tail. Sylveon
// resolves to Clefable through the stand-in table.
//
// The Eject Button is not in this item set, and it is the subject of the second
// case rather than scenery, so it is kept and the missing-item failure is the
// finding.

func TestAbilitiesPickpocket(t *testing.T) {
	describe(t, "Pickpocket", func(g *psg) {
		g.it("should steal a foe's item if hit by a contact move", func(p *ps) {
			p.battle(
				team{{Species: "Weavile", As: "Persian", Ability: "pickpocket", Moves: mv("agility")}},
				team{{Species: "Sylveon", Item: "choicescarf", Moves: mv("quickattack")}},
			)
			p.turn()
			p.holdsItem(p.mine(), "Pickpocket should have taken the Choice Scarf")
			p.noItem(p.foe(), "the contact attacker should have lost its Choice Scarf")
		})

		g.it("should not steal a foe's item if the Pickpocket user switched out through Eject Button", func(p *ps) {
			p.battle(
				team{
					{Species: "Weavile", As: "Persian", Ability: "pickpocket", Item: "ejectbutton", Moves: mv("agility")},
					{Species: "Chansey", Moves: mv("softboiled")},
				},
				team{{Species: "Sylveon", Item: "choicescarf", Moves: mv("quickattack")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.holdsItem(p.foe(), "a Pickpocket holder that left the field should not have taken anything")
		})

		g.it("should not steal a foe's item if forced to switch out", func(p *ps) {
			p.battle(
				team{
					{Species: "Weavile", As: "Persian", Ability: "pickpocket", Moves: mv("agility")},
					{Species: "Chansey", Moves: mv("softboiled")},
				},
				team{{
					Species: "Duraludon", As: "Dragonite", Ability: "compoundeyes", Item: "choicescarf",
					Moves: mv("dragontail"),
				}},
			)
			p.turn()
			p.holdsItem(p.foe(), "Dragon Tail dragging the holder out should cancel the steal")
		})

		g.skip("should steal items back and forth when hit by a Magician user",
			"Klefki is not in this 80-species dex and Magician is not modeled (upstream skips this case too)")
	})
}
