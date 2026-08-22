//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/fling.js.
//
// The whole file is singles and comes across. The last case is `it.skip`
// upstream, so it is a skip here with upstream's own reason.
//
// Species. Wynaut resolves to Hypno and Cleffa to Clefable through the shared
// table; neither typing matters, and the Iron Ball's Speed halving still puts
// Clefable first in the Magic Room case, which is the ordering that case needs.
//
// The damage-calculation case loses half of its assertion. Upstream's window is
// an absolute figure at level 100 that is meant to show the Life Orb boosting
// the throw; at level 50 and 30 base power the whole throw is 6-7 HP here, and
// a 1.3x boost lands inside the damage roll's own spread, so there is no honest
// way to separate the two. The port keeps the half that does travel — the
// thrower must not pay Life Orb recoil for an orb it threw away — and checks
// that the throw connected at all.

func TestMovesFling(t *testing.T) {
	describe(t, "Fling", func(g *psg) {
		g.it("should consume the user's item after being flung", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "ironball", Moves: mv("fling")}},
				team{{Species: "cleffa", Moves: mv("protect")}},
			)
			p.turn()
			p.noItem(p.mine(), "the Iron Ball should have been spent even against a Protect")
		})

		g.it("should apply custom effects when certain items are flung", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "flameorb", Moves: mv("fling")}},
				team{{Species: "cleffa", Moves: mv("splash")}},
			)
			p.turn()
			p.hasStatus(p.foe(), "brn", "a thrown Flame Orb should burn the target")
		})

		g.it("should not be usable in Magic Room", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "ironball", Moves: mv("fling")}},
				team{{Species: "cleffa", Moves: mv("magicroom")}},
			)
			p.turn()
			p.equal(p.mine().Item, "ironball", "Fling should have failed with the item suppressed")
		})

		g.it("should use its item to be flung in damage calculations", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Item: "lifeorb", Moves: mv("fling")}},
				team{{Species: "cleffa", Moves: mv("splash")}},
			)
			p.turn()
			p.damaged(p.foe(), "the throw should have connected")
			p.fullHP(p.mine(), "the thrower should not pay Life Orb recoil for an orb it threw")
		})

		g.skip("should Fling, not consume Leppa Berry when using 1 PP Leppa Berry Fling",
			"upstream skips this case: it currently depends on RNG when it should not")
	})
}
