//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/protectivepads.js.
//
// Protective Pads is modeled, so most of these are live behavior cases. What
// does not come across is the log half of every assertion: upstream matches
// protocol lines naming Mummy, Iron Barbs, Gooey, Perish Body and the pads
// themselves, and none of those strings exists in this engine — the three
// abilities are not among its 118, and the pads never announce themselves, they
// only suppress. So each case keeps the state assertion and drops the log one.
//
// That has a consequence worth stating plainly: the four cases whose contact
// effect is an unmodeled ability pass here whether or not the pads work, because
// nothing would have happened to the attacker either way. They are ported so the
// claim is on record, not because a green result means agreement. The two cases
// whose contact effect is modeled — Rocky Helmet and Lunge — are the ones that
// actually exercise the item.
//
// Species. Cofagrigus, Cursola, Goodra, Weavile and Miltank have no stand-in
// rows and are bodies here rather than mechanics, so the port names one in-dex
// species each: Gengar keeps the Ghost typing of the two ghosts, Dragonite
// keeps Goodra's Dragon typing, Jynx keeps Weavile's Ice half (this dex has no
// Dark type at all), and Tauros keeps Miltank's Normal typing. Ferrothorn is the
// one that cannot use its stand-in row — that row says in as many words that
// Iron Barbs is not preserved — so the port names Magneton itself and sets Iron
// Barbs explicitly, which keeps Ferrothorn's Steel typing and leaves the
// unmodeled ability to report itself. Aggron and Happiny go through their rows.
//
// Sleep Talk is not in this dataset and is inert filler on the target in every
// case, so Splash stands in for it.

func TestItemsProtectivePads(t *testing.T) {
	describe(t, "Protective Pads", func(g *psg) {
		g.it("should prevent ability-changing abilities triggered by contact from acting", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "sturdy", Item: "protectivepads", Moves: mv("bulletpunch")}},
				team{{Species: "Cofagrigus", As: "Gengar", Ability: "mummy", Moves: mv("splash")}},
			)
			p.turn()
			p.hasAbility(p.mine(), "sturdy", "the pads should have kept Mummy off the attacker")
		})

		g.it("should prevent damaging abilities triggered by contact from acting", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "protectivepads", Moves: mv("bulletpunch")}},
				team{{Species: "Ferrothorn", As: "Magneton", Ability: "ironbarbs", Moves: mv("splash")}},
			)
			p.turn()
			p.fullHP(p.mine(), "the pads should have kept Iron Barbs off the attacker")
		})

		g.it("should prevent stat stage-changing abilities triggered by contact from acting", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "protectivepads", Moves: mv("bulletpunch")}},
				team{{Species: "Goodra", As: "Dragonite", Ability: "gooey", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.mine(), "spe", 0, "the pads should have kept Gooey off the attacker")
		})

		g.it("should not stop Pickpocket", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "protectivepads", Moves: mv("bulletpunch")}},
				team{{Species: "Weavile", As: "Jynx", Ability: "pickpocket", Moves: mv("splash")}},
			)
			p.turn()
			p.noItem(p.mine(), "Pickpocket is not a contact-reactive effect the pads cover")
			p.equal(p.foe().Item, "protectivepads", "the target should be holding the stolen pads")
		})

		g.it("should prevent item effects triggered by contact from acting", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "protectivepads", Moves: mv("bulletpunch")}},
				team{{Species: "Miltank", As: "Tauros", Item: "rockyhelmet", Moves: mv("splash")}},
			)
			p.turn()
			p.fullHP(p.mine(), "the pads should have kept Rocky Helmet off the attacker")
		})

		g.it("should not activate on the opponent's moves", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "protectivepads", Moves: mv("splash")}},
				team{{Species: "Happiny", Moves: mv("lunge")}},
			)
			p.turn()
			p.statStage(p.mine(), "atk", -1, "the pads cover what the holder throws, not what is thrown at it")
		})

		g.it("should not start Perish Body on either Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "protectivepads", Moves: mv("bulletpunch")}},
				team{{Species: "Cursola", As: "Gengar", Ability: "perishbody", Moves: mv("splash")}},
			)
			p.turn()
			// The engine has no Perish Body string, so the only observable this
			// harness can reach is the perish counter the song itself announces.
			p.logLacks("perish count fell to", "no perish count should have started on either side")
		})

		g.it("should block against Protecting effects with a contact side effect", func(p *ps) {
			// Baneful Bunker, Obstruct and Spiky Shield are all missing from this
			// dataset and are the subject of the case, so they are kept and the
			// team fails to build naming them. That is the finding.
			p.battle(
				team{{Species: "Wynaut", Item: "protectivepads", Moves: mv("splash", "tackle")}},
				team{{Species: "Aggron", Moves: mv("splash", "banefulbunker", "obstruct", "spikyshield")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move tackle", "move banefulbunker")
			p.turn()
			p.makeChoices("move tackle", "move obstruct")
			p.turn()
			p.makeChoices("move tackle", "move spikyshield")
			p.turn()
			p.noStatus(p.mine(), "Baneful Bunker should not have poisoned through the pads")
			p.statStage(p.mine(), "def", 0, "Obstruct should not have dropped Defense through the pads")
			p.fullHP(p.mine(), "Spiky Shield should not have hurt through the pads")
		})

		g.skip("should not protect against Gulp Missile when using a contact move",
			"formes — Cramorant-Gorging is not in this dex and Gulp Missile's forme states are not modeled")
	})
}
