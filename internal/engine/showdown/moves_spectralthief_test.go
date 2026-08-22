//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/spectralthief.js.
//
// Spectral Thief is not in this dataset. It is the subject of every case here,
// so it stays in the fixtures and the missing-move failure is the finding —
// which does mean each case reports that one thing and stops before its
// assertions. The assertions are still written out, because they are what the
// case will measure the day the move exists.
//
// Simple, Contrary and Parental Bond are likewise absent; the engine reports
// each as an ability it has no record of, which is the second finding in the
// three cases that turn on them.
//
// Species. Smeargle is only a body, so the shared stand-in (Chansey) carries it
// everywhere except the first case, which needs the thief to move before the
// Intimidate holder — Chansey is slower than every in-dex Intimidate body, so
// Alakazam is named there instead. Litten is the Intimidate holder: Arcanine
// keeps both the Fire typing and Intimidate as a native ability. Zangoose is
// the Normal-type body whose typing is the whole point of the immunity case;
// Snorlax keeps Normal and carries Immunity natively. Serperior becomes Tangela
// (pure Grass, faster than Chansey, so Leaf Storm still resolves first) and
// Swoobat becomes Hypno (Psychic and faster than Chansey, same reason).
// Tentacruel is in the dex with Clear Body and needs no substitution.

func TestMovesSpectralThief(t *testing.T) {
	describe(t, "Spectral Thief", func(g *psg) {
		g.it("should steal the target's boosts before hitting", func(p *ps) {
			p.battle(
				team{{
					Species: "Smeargle", As: "Alakazam", Ability: "technician",
					Moves: mv("calmmind", "spectralthief"),
				}},
				team{{
					Species: "Litten", As: "Arcanine", Ability: "intimidate", Item: "focussash",
					Moves: mv("swordsdance", "roar"),
				}},
			)
			if p.state() == nil {
				return
			}
			thief, victim := p.mine(), p.foe()

			p.makeChoices("move spectralthief", "move swordsdance")
			minusOneDmg := victim.MaxHP - victim.HP

			p.makeChoices("move calmmind", "move swordsdance")
			p.makeChoices("move calmmind", "move swordsdance")

			p.statStage(thief, "atk", -1, "Intimidate should be the only thing touching the thief's Attack")
			p.statStage(victim, "atk", 6, "three Swords Dances should max the victim's Attack")

			p.makeChoices("move spectralthief", "move roar")
			p.statStage(thief, "atk", 5, "the stolen +6 lands on top of Intimidate's -1")
			p.statStage(victim, "atk", 0, "the victim should be stripped")

			p.atLeast(victim.MaxHP-victim.HP, 3*minusOneDmg,
				"the boosts should be stolen before the hit is calculated")
		})

		g.it("should double the boosts if the user has Simple", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "simple", Moves: mv("calmmind", "spectralthief")}},
				team{{Species: "Mew", Ability: "pressure", Moves: mv("swordsdance", "roar")}},
			)
			if p.state() == nil {
				return
			}
			thief, victim := p.mine(), p.foe()

			p.makeChoices("move calmmind", "move swordsdance")
			p.statStage(thief, "atk", 0, "Calm Mind does not touch Attack")
			p.statStage(victim, "atk", 2, "")

			p.makeChoices("move spectralthief", "move roar")
			p.statStage(thief, "atk", 4, "Simple should double the stolen +2")
			p.statStage(victim, "atk", 0, "")
		})

		g.it("should only steal boosts once if the user has Parental Bond", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "parentalbond", Moves: mv("calmmind", "spectralthief")}},
				team{{
					Species: "Mew", Ability: "pressure", Item: "weaknesspolicy",
					Moves: mv("swordsdance", "roar"),
				}},
			)
			if p.state() == nil {
				return
			}
			thief, victim := p.mine(), p.foe()

			p.makeChoices("move calmmind", "move swordsdance")
			p.statStage(thief, "atk", 0, "")
			p.statStage(victim, "atk", 2, "")

			p.makeChoices("move spectralthief", "move roar")
			p.noItem(victim, "the Weakness Policy should have fired on the super-effective hit")
			p.statStage(thief, "atk", 2, "the second strike should steal nothing")
			p.statStage(victim, "atk", 2, "the +2 the victim has is the Weakness Policy's, not its own")
		})

		g.it("should not steal boosts if the target is immune to the hit", func(p *ps) {
			p.battle(
				team{{
					Species: "Smeargle", Ability: "owntempo", Item: "laggingtail",
					Moves: mv("spectralthief"),
				}},
				team{{Species: "Zangoose", As: "Snorlax", Ability: "immunity", Moves: mv("swordsdance")}},
			)
			if p.state() == nil {
				return
			}
			thief, victim := p.mine(), p.foe()

			p.makeChoices("move spectralthief", "move swordsdance")
			p.statStage(thief, "atk", 0, "a Normal-type is immune, so there is nothing to steal")
			p.statStage(victim, "atk", 2, "")
		})

		g.it("should zero target's boosts if the target has Contrary", func(p *ps) {
			p.battle(
				team{{
					Species: "Smeargle", Ability: "owntempo", Item: "focussash",
					Moves: mv("spectralthief"),
				}},
				team{{Species: "Serperior", As: "Tangela", Ability: "contrary", Moves: mv("leafstorm")}},
			)
			if p.state() == nil {
				return
			}
			victim := p.foe()
			p.makeChoices("move spectralthief", "move leafstorm")
			p.statStage(victim, "spa", 0, "Contrary should not turn the theft into a boost")
			p.notFainted(victim, "")
		})

		g.it("should zero target's boosts if the target has Clear Body", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("spectralthief")}},
				team{{Species: "Tentacruel", Ability: "clearbody", Moves: mv("swordsdance")}},
			)
			if p.state() == nil {
				return
			}
			victim := p.foe()
			p.makeChoices("move spectralthief", "move swordsdance")
			p.statStage(victim, "atk", 0, "Clear Body should not save the boosts from being stolen")
			p.notFainted(victim, "")
		})

		g.it("should zero target's boosts if the target has Simple", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("spectralthief")}},
				team{{Species: "Swoobat", As: "Hypno", Ability: "simple", Moves: mv("amnesia")}},
			)
			if p.state() == nil {
				return
			}
			victim := p.foe()
			p.makeChoices("move spectralthief", "move amnesia")
			p.statStage(victim, "spd", 0, "Simple on the victim should not change what the theft leaves behind")
			p.notFainted(victim, "")
		})
	})
}
