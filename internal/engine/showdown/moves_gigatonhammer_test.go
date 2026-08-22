//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/gigatonhammer.js.
//
// Gigaton Hammer is not in this dataset, so both cases report the missing move
// rather than the once-every-other-turn lock they are about. They are written
// out in full so they measure the right thing the day it lands.
//
// Species. Tinkaton has no stand-in row and becomes Scyther: neither Fairy nor
// Steel survives, and nothing here reads either. What the cases do need is a
// physical attacker faster than both opponents — the Spore has to land after
// the hammer in the first case, and Instruct has to resolve after it in the
// second — and hard enough that two hammers in one turn knock the Instruct
// user out where one does not. Brute Bonnet becomes Parasect, a slower Grass
// body that carries Spore itself; Oranguru becomes Hypno, a Psychic body slow
// enough to Instruct after the attacker has moved.
//
// Moves. Helping Hand is not in this dataset and is only there to be a second,
// selectable move, so it is Splash. Instruct is absent too, which the second
// case reports alongside Gigaton Hammer.
//
// The first case's second half rests on the attacker staying asleep through
// turn two, so that it never gets to use a different move; this engine rolls
// two or three turns of sleep, so that holds under every seed.

func TestMovesGigatonHammer(t *testing.T) {
	describe(t, "Gigaton Hammer", func(g *psg) {
		g.it("should not be able to be selected if it was the last move used", func(p *ps) {
			p.battle(
				team{{
					Species: "Tinkaton", As: "Scyther", Ability: "noability",
					Moves: mv("splash", "gigatonhammer"),
				}},
				team{{
					Species: "Brute Bonnet", As: "Parasect", Ability: "noability",
					Moves: mv("spore"),
				}},
			)
			p.makeChoices("move gigatonhammer", "")
			p.cantMove(0, "gigatonhammer", "Gigaton Hammer should be locked out the turn after it is used")
			p.turn()
			p.cantMove(0, "gigatonhammer",
				"the lock should hold while the user is asleep and so has used nothing else")
		})

		g.it("should be able to be used twice in one turn", func(p *ps) {
			p.battle(
				team{{
					Species: "Tinkaton", As: "Scyther", Ability: "noability",
					Moves: mv("gigatonhammer"),
				}},
				team{{Species: "Oranguru", As: "Hypno", Ability: "noability", Moves: mv("instruct")}},
			)
			p.turn()
			p.fainted(p.foe(), "an instructed second Gigaton Hammer should have finished Oranguru")
		})
	})
}
