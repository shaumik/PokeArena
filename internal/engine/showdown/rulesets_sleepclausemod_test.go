//go:build showdown

package showdown

import "testing"

// Ported from test/sim/rulesets/sleepclausemod.js.
//
// Both cases came across. Sleep Clause is not optional in this engine —
// clauses.go enforces it in every battle and says why — so the upstream
// `{sleepClause: true}` format flag has nothing to select and is dropped.
//
// Feebas has no dex entry and no stand-in row. Vaporeon is built in its place:
// a plain Water body with no contact-reaction ability, which is everything
// either case asks of it — a second sleep target in the first, a Rest user in
// the second. Magikarp routes through the stand-in table to Seaking, also plain
// Water, so the two sides of the clause are distinct species.
//
// Upstream writes `switch 2` on consecutive turns because Showdown swaps party
// positions when a Pokémon switches in. This engine keeps the team array fixed
// and tracks the active as an index into it, so the second switch names the
// other slot by its absolute position instead.

func TestRulesetsSleepClauseMod(t *testing.T) {
	describe(t, "Sleep Clause Mod", func(g *psg) {
		g.it("should prevent players from putting more than one of foe's Pokemon to sleep", func(p *ps) {
			p.battle(
				team{{Species: "Paras", Moves: mv("spore")}},
				team{
					{Species: "Magikarp", Moves: mv("splash")},
					{Species: "Feebas", As: "Vaporeon", Moves: mv("splash")},
				},
			)
			p.makeChoices("move spore", "switch 2")
			p.hasStatus(p.foe(), "slp", "Spore should have put the switch-in to sleep")
			p.makeChoices("move spore", "switch 1")
			p.noStatus(p.foe(), "a second sleeper on the same side should be refused")
			p.logHas("stayed awake! (Sleep Clause)", "")
		})

		g.it("should not prevent Rest", func(p *ps) {
			p.battle(
				team{{Species: "Paras", Moves: mv("spore", "tackle")}},
				team{
					{Species: "Feebas", As: "Vaporeon", Moves: mv("rest")},
					{Species: "Magikarp", Moves: mv("splash")},
				},
			)
			p.makeChoices("move spore", "switch 2")
			p.hasStatus(p.foe(), "slp", "Spore should have put the switch-in to sleep")
			// The Tackle is upstream's, and it is load-bearing: it damages the
			// returning Feebas so that Rest has something to heal.
			p.makeChoices("move tackle", "switch 1")
			p.noStatus(p.foe(), "the Pokémon coming back in should be status-free")
			p.makeChoices("move tackle", "move rest")
			p.hasStatus(p.foe(), "slp", "Rest is self-inflicted and exempt from the Sleep Clause")
		})
	})
}
