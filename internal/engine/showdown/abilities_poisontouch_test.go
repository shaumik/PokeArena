//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/poisontouch.js.
//
// Upstream builds every case with {forceRandomChance: true}, which makes the
// ability's 30% roll land every time. There is no such hook here, so the cases
// whose subject is "does it poison" are measured instead: g.itRate over 200
// seeds, expecting a rate around 30%, and the two cases that say it must
// *not* poison ask for a rate of exactly zero.
//
// Species. Wynaut resolves to Hypno and Shuckle to Snorlax. Snorlax's default
// ability is Immunity, which would block the poison and quietly turn every
// case green for the wrong reason, so it is stripped with "noability" the way
// upstream strips an interfering body. Regirock resolves to Golem.
//
// The Substitute case is restated over two turns. Upstream gives Shuckle
// Prankster so the Substitute beats False Swipe inside one turn; this engine
// has no Prankster and the stand-in is the slower of the two, so the
// Substitute goes up on its own turn and the attack comes the turn after,
// which is the same test of whether Poison Touch reaches through a
// Substitute.
//
// Mummy and Bide are not in this engine's ability and move sets, so those two
// cases report them.

func TestAbilitiesPoisonTouch(t *testing.T) {
	describe(t, "Poison Touch", func(g *psg) {
		g.itRate("should poison targets if the user damages the target with a contact move",
			0.15, 0.45, 200, func(p *ps) bool {
				p.battle(
					team{{Species: "Wynaut", Ability: "poisontouch", Moves: mv("falseswipe")}},
					team{{Species: "Shuckle", Ability: "noability", Moves: mv("falseswipe")}},
				)
				p.turn()
				p.noStatus(p.mine(), "Wynaut should not be poisoned")
				return psID(string(p.foe().Status)) == "poison"
			})

		g.itRate("should not poison targets behind a Substitute or holding Covert Cloak",
			0.0, 0.0, 100, func(p *ps) bool {
				p.battle(
					team{{Species: "Wynaut", Ability: "poisontouch", Moves: mv("falseswipe", "splash")}},
					team{
						{Species: "Shuckle", Ability: "noability", Moves: mv("substitute", "splash")},
						{Species: "Regirock", Item: "covertcloak", Moves: mv("splash")},
					},
				)
				p.makeChoices("move splash", "move substitute")
				p.makeChoices("move falseswipe", "move splash")
				poisoned := psID(string(p.foe().Status)) == "poison"
				p.makeChoices("move falseswipe", "switch 2")
				return poisoned || psID(string(p.foe().Status)) == "poison"
			})

		g.itRate("should poison independently of and after regular secondary status effects",
			0.15, 0.45, 200, func(p *ps) bool {
				p.battle(
					team{{Species: "Wynaut", Ability: "poisontouch", Moves: mv("nuzzle")}},
					team{{Species: "Shuckle", Ability: "noability", Item: "lumberry", Moves: mv("splash")}},
				)
				p.turn()
				shuckle := p.foe()
				p.noItem(shuckle, "the Lum Berry should have been spent on Nuzzle's paralysis")
				return psID(string(shuckle.Status)) == "poison"
			})

		g.itRate("should poison before Mummy takes over the user's Ability",
			0.15, 0.45, 200, func(p *ps) bool {
				p.battle(
					team{{Species: "Wynaut", Ability: "poisontouch", Moves: mv("falseswipe")}},
					team{{Species: "Shuckle", Ability: "mummy", Moves: mv("splash")}},
				)
				p.turn()
				p.hasAbility(p.mine(), "mummy", "Mummy should have taken the user's ability")
				return psID(string(p.foe().Status)) == "poison"
			})

		g.it("should not poison itself with contact moves that aren't hitting other Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "poisontouch", Moves: mv("bide")}},
				team{{Species: "Shuckle", Ability: "noability", Moves: mv("splash")}},
			)
			p.turn()
			p.turn()
			p.turn()
			p.turn()
			p.noStatus(p.mine(), "Poison Touch should never poison its own holder")
		})

		g.skip("should not have a 60% chance to poison if Pledge Rainbow is active", "doubles")
	})
}
