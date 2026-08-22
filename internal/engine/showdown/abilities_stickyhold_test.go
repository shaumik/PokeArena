//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/stickyhold.js.
//
// Shuckle resolves through the stand-in table to Snorlax, a body that survives;
// the case measures whether an item moves, never how big a hit is, so the row's
// warning about Shuckle's defense extremes does not apply. Smeargle resolves to
// Chansey, the thief. Fennekin and Pangoro are not in the dex and have no rows:
// Ninetales is built for Fennekin (the same line's fully evolved form, and only
// needed to throw a Grass Knot) and Machamp for Pangoro (a fighting body to
// carry Mold Breaker and Knock Off).
//
// Razz Berry is not in this dataset. Cheri Berry stands in because the case
// needs the held item to be a *berry* — Bug Bite only eats berries, so a plain
// item would make one of the four steal moves a no-op — and Cheri has no
// trigger in a battle with no paralysis in it.

func TestAbilitiesStickyHold(t *testing.T) {
	describe(t, "Sticky Hold", func(g *psg) {
		g.it("should prevent held items from being stolen by most moves or abilities", func(p *ps) {
			p.battle(
				team{{Species: "Shuckle", Ability: "stickyhold", Item: "cheriberry", Moves: mv("recover")}},
				team{
					{Species: "Fennekin", As: "Ninetales", Ability: "magician", Moves: mv("grassknot")},
					{Species: "Smeargle", Ability: "synchronize", Moves: mv("thief", "knockoff", "switcheroo", "bugbite")},
				},
			)
			itemHolder := p.mine()
			p.makeChoices("move recover", "move grassknot")
			p.equal(itemHolder.Item, "cheriberry", "Shuckle should hold a Cheri Berry")
			p.makeChoices("move recover", "switch 2")

			for _, moveid := range []string{"thief", "knockoff", "switcheroo", "bugbite"} {
				p.makeChoices("move recover", "move "+moveid)
				p.holdsItem(itemHolder, "Shuckle should still hold its Cheri Berry")
			}
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Pangoro", As: "Machamp", Ability: "moldbreaker", Moves: mv("knockoff")}},
				team{{Species: "Shuckle", Ability: "stickyhold", Item: "ironball", Moves: mv("rest")}},
			)
			p.makeChoices("move knockoff", "move rest")
			p.noItem(p.foe(), "Mold Breaker should have knocked the Iron Ball off through Sticky Hold")
		})
	})
}
