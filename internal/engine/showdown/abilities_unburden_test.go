//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/unburden.js.
//
// Every case upstream asserts on `pokemon.getStat('spe')` doubling. This
// harness cannot read an effective stat — the engine's speed calculation is
// unexported — so each case asserts the state that calculation reads instead:
// the Unburden volatile armed and the holder's hands empty, which is exactly
// the pair `abilities.go` checks before doubling. The last case is the mirror,
// and it holds an item again rather than losing the volatile, because that is
// how the boost is actually switched off.
//
// Sceptile and Drifblim are not in this dex and have no stand-in rows. They
// are only bodies carrying Unburden here, and Hitmonlee is the dex's own
// Unburden species, so all three sets build as Hitmonlee. Scizor goes through
// its stand-in row (Magneton) and Togekiss through its (Wigglytuff); neither
// is more than a body with an item.
//
// The last case swaps upstream's Fighting Gem for the White Herb / Close
// Combat pair the first case already uses: no Gem is in this item set, and
// what the case needs is only some item that gets consumed before Bestow hands
// over a new one. Follow Me is not in this dataset and is a no-op in singles
// anyway, so the turn it filled is spent on Splash.

func TestAbilitiesUnburden(t *testing.T) {
	describe(t, "Unburden", func(g *psg) {
		g.it("should trigger when an item is consumed", func(p *ps) {
			p.battle(
				team{{Species: "Hitmonlee", Ability: "unburden", Item: "whiteherb", Moves: mv("closecombat")}},
				team{{Species: "Scizor", Ability: "swarm", Item: "focussash", Moves: mv("swordsdance")}},
			)
			p.makeChoices("move closecombat", "move swordsdance")
			p.noItem(p.mine(), "the White Herb should have been eaten")
			p.ok(p.mine().Volatiles.Unburden, "Unburden should have armed, doubling Speed")
		})

		g.it("should trigger when an item is destroyed", func(p *ps) {
			p.battle(
				team{{Species: "Drifblim", As: "Hitmonlee", Ability: "unburden", Item: "airballoon", Moves: mv("endure")}},
				team{{Species: "Machamp", Ability: "noguard", Moves: mv("stoneedge")}},
			)
			p.makeChoices("move endure", "move stoneedge")
			p.noItem(p.mine(), "the Air Balloon should have popped")
			p.ok(p.mine().Volatiles.Unburden, "Unburden should have armed, doubling Speed")
		})

		g.it("should trigger when Natural Gift consumes a berry", func(p *ps) {
			p.battle(
				team{{Species: "Sceptile", As: "Hitmonlee", Ability: "unburden", Item: "oranberry", Moves: mv("naturalgift")}},
				team{{Species: "Scizor", Ability: "swarm", Item: "focussash", Moves: mv("swordsdance")}},
			)
			p.makeChoices("move naturalgift", "move swordsdance")
			p.noItem(p.mine(), "Natural Gift should have spent the Oran Berry")
			p.ok(p.mine().Volatiles.Unburden, "Unburden should have armed, doubling Speed")
		})

		g.it("should trigger when an item is flung", func(p *ps) {
			p.battle(
				team{{Species: "Sceptile", As: "Hitmonlee", Ability: "unburden", Item: "whiteherb", Moves: mv("fling")}},
				team{{Species: "Scizor", Ability: "swarm", Item: "focussash", Moves: mv("swordsdance")}},
			)
			p.makeChoices("move fling", "move swordsdance")
			p.noItem(p.mine(), "Fling should have thrown the White Herb away")
			p.ok(p.mine().Volatiles.Unburden, "Unburden should have armed, doubling Speed")
		})

		g.it("should trigger when an item is forcefully removed", func(p *ps) {
			p.battle(
				team{{Species: "Sceptile", As: "Hitmonlee", Ability: "unburden", Item: "whiteherb", Moves: mv("leechseed")}},
				team{{Species: "Scizor", Ability: "swarm", Moves: mv("knockoff")}},
			)
			p.makeChoices("move leechseed", "move knockoff")
			p.noItem(p.mine(), "Knock Off should have taken the White Herb")
			p.ok(p.mine().Volatiles.Unburden, "Unburden should have armed, doubling Speed")
		})

		g.it("should not be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Sceptile", As: "Hitmonlee", Ability: "unburden", Item: "whiteherb", Moves: mv("leechseed")}},
				team{{Species: "Scizor", Ability: "moldbreaker", Moves: mv("knockoff")}},
			)
			p.makeChoices("move leechseed", "move knockoff")
			p.noItem(p.mine(), "Knock Off should have taken the White Herb")
			p.ok(p.mine().Volatiles.Unburden, "Mold Breaker should not reach the attacker's own ability")
		})

		g.it("should lose the boost when it gains a new item", func(p *ps) {
			p.battle(
				team{{Species: "Hitmonlee", Ability: "unburden", Item: "whiteherb", Moves: mv("closecombat", "splash")}},
				team{{Species: "Togekiss", Ability: "serenegrace", Item: "laggingtail", Moves: mv("bestow", "splash")}},
			)
			p.makeChoices("move closecombat", "move splash")
			p.noItem(p.mine(), "the White Herb should have been eaten")
			p.ok(p.mine().Volatiles.Unburden, "Unburden should have armed, doubling Speed")

			p.makeChoices("move splash", "move bestow")
			p.holdsItem(p.mine(), "Bestow should have handed over the Lagging Tail")
			p.equal(p.mine().Item, "laggingtail", "a holder with an item again is no longer unburdened")
		})
	})
}
