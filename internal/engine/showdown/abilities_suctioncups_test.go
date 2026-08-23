//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/suctioncups.js.
//
// Suction Cups is not one of the abilities this engine models. The first case
// reports something else first: Red Card is not in this dataset at all, and a
// team naming an item the dataset does not have cannot be built, so that case
// stops at the fixture. The missing item is the finding; the assertions after
// it are left as written rather than trimmed, so the case still diffs against
// its original.
//
// Shuckle and Forretress resolve through the stand-in table (Snorlax and
// Magneton); neither is doing anything here but being switched, or refusing to
// be. Smeargle resolves to Chansey. Pangoro is not in the dex and has no row —
// Machamp is built for it, a fighting body to carry Mold Breaker and Circle
// Throw.
//
// Upstream's second case pins the RNG with forceRandomChance so Circle Throw
// cannot miss; there is no such hook here, and Circle Throw is 90% accurate,
// so the case is expected to be seed-sensitive on that account alone.

func TestAbilitiesSuctionCups(t *testing.T) {
	describe(t, "Suction Cups", func(g *psg) {
		g.it("should prevent the user from being forced out", func(p *ps) {
			p.battle(
				team{
					{Species: "Shuckle", Ability: "suctioncups", Moves: mv("rapidspin")},
					{Species: "Forretress", Ability: "sturdy", Moves: mv("rapidspin")},
				},
				team{{
					Species: "Smeargle", Ability: "noguard", Item: "redcard",
					Moves: mv("healpulse", "dragontail", "circlethrow", "roar"),
				}},
			)
			p.makeChoices("move rapidspin", "move healpulse")
			p.noItem(p.foe(), "Red Card should activate")
			p.species(p.mine(), "Shuckle", "Suction Cups should have refused the Red Card")
			// Upstream walks slots 2 through 4 by number; the moves in them
			// are Dragon Tail, Circle Throw and Roar.
			for _, phaze := range []string{"dragontail", "circlethrow", "roar"} {
				p.makeChoices("move rapidspin", "move "+phaze)
				p.species(p.mine(), "Shuckle", "Suction Cups should have refused the phazing move")
			}
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Pangoro", As: "Machamp", Ability: "moldbreaker", Moves: mv("circlethrow")}},
				team{
					{Species: "Shuckle", Ability: "suctioncups", Item: "ironball", Moves: mv("rest")},
					{Species: "Forretress", Ability: "sturdy", Moves: mv("rapidspin")},
				},
			)
			p.makeChoices("move circlethrow", "move rest")
			p.species(p.foe(), "Forretress", "Mold Breaker should have dragged the Suction Cups holder out")
		})
	})
}
