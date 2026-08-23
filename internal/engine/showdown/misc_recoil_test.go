//go:build showdown

package showdown

import (
	"math"
	"testing"
)

// Ported from test/sim/misc/recoil.js.
//
// Upstream drives three hits through one battle: Happiny survives each on
// Sturdy at 1 HP, and Strength Sap puts it back to full so the next hit lands
// on a full-HP target too, which makes the damage dealt exactly maxhp-1 every
// time. Neither half of that survives here — Strength Sap is not in this
// dataset and nothing else heals a target back to full — so the case plays one
// battle per move. That costs nothing, because the assertion is a relation
// between two figures the battle reports (damage dealt, HP the user lost) and
// never needed the damage itself to be predictable.
//
// Head Charge is not in this dataset at all. Rather than dropping upstream's
// third move, the case ends with a fixture naming it, so the absence is
// reported; it is built last because a team the harness cannot resolve ends the
// case.
//
// Kartana has no dex entry and no stand-in row. Snorlax is built in its place:
// all the user has to do here is survive its own recoil, and Snorlax has the HP
// for it. Compound Eyes is kept from upstream and is load-bearing — it is what
// stops Head Smash's 80% accuracy from reading as zero recoil on some seeds.
// Happiny keeps its Chansey stand-in and its Sturdy, which is what makes the
// damage dealt something other than "the target's whole HP bar".

func TestMiscRecoil(t *testing.T) {
	describe(t, "Recoil", func(g *psg) {
		g.it("should deal damage to the user after an attack depending on the damage dealt", func(p *ps) {
			// factor is Showdown's recoil fraction for the move; the rounding
			// mirrors the engine's own, which rounds the damage dealt.
			check := func(move string, factor float64) {
				p.battle(
					team{{Species: "Kartana", As: "Snorlax", Ability: "compoundeyes", Moves: mv(move)}},
					team{{Species: "Happiny", Ability: "sturdy", Moves: mv("splash")}},
				)
				user, target := p.mine(), p.foe()
				userBefore, targetBefore := user.HP, target.HP
				p.makeChoices("move "+move, "move splash")

				dealt := targetBefore - target.HP
				p.atLeast(dealt, 1, move+" should have connected")
				p.equal(userBefore-user.HP, int(math.Round(float64(dealt)*factor)),
					move+" recoil should be its fraction of the damage dealt")
			}
			check("doubleedge", 0.33)
			check("headsmash", 0.5)

			// Head Charge is upstream's third recoil move and is not in this
			// dataset; this fixture exists to say so by name.
			p.battle(
				team{{Species: "Kartana", As: "Snorlax", Ability: "compoundeyes", Moves: mv("headcharge")}},
				team{{Species: "Happiny", Ability: "sturdy", Moves: mv("splash")}},
			)
		})

		g.skip("[Gen 1] should deal recoil damage based on the damage dealt",
			"gen 1 mechanics: the recoil fraction and the damage it is taken from both differ, and there is no gen-mod layer")
	})
}
