//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/trickroom.js.
//
// Substitutions. Bronzong is the slow body that sets the room and must not be
// poisoned by the Poison Jab it eats on the way there — Steel typing is what
// gives upstream that immunity, and Magneton is this dex's only Steel body but
// is not slow. Muk is: Poison-type (so equally immune to Poison Jab's
// secondary), Speed 50 against the Speed 105 of Ninjask's stand-in, which is
// the only speed relationship the first three cases read. Bronzong's Heatproof
// is inert scaffolding upstream and becomes "noability" here.
//
// Ninjask goes through the shared stand-in table to Scyther, which the table
// says keeps "bug/flying and fast" and warns that Speed Boost must be set
// explicitly. It is not set: no in-dex species carries speed-boost, and the
// harness recovers a kebab slug by searching the abilities in-dex species have,
// so an explicit "speedboost" would both be reported as unknown and silently
// leave the Pokemon bare. Speed Boost is incidental in all three cases — they
// need a fast body, not an accelerating one — so the ability is dropped and
// said so here rather than half-set.
//
// Sand Stream has the same problem and is not incidental in the third case,
// which is entirely about which of two entry weathers lands last. Drizzle is a
// single word, so it survives the harness intact and starts the same kind of
// five-turn clock; Hippowdon becomes a slow Ground body (Rhydon, Speed 40) that
// brings rain instead of sand, and Ninetales — in this dex, with Drought — is
// unchanged. Under Trick Room the slower entrant's ability must fire first, so
// the sun has to be what is left on the field.
//
// The last two cases are the Trick Room speed-rollover glitch and need a
// Pokemon at exactly 1809 Speed. This engine is fixed at level 50, where the
// fastest reachable figure with a Choice Scarf and +6 is around a thousand, so
// neither can be set up at all; the second also states its real assertion
// inside a `battle.onEvent('BasePower')` hook, which has no counterpart here.

func TestMovesTrickRoom(t *testing.T) {
	describe(t, "Trick Room", func(g *psg) {
		g.it("should cause slower Pokemon to move before faster Pokemon in a priority bracket", func(p *ps) {
			p.battle(
				team{{Species: "Bronzong", As: "Muk", Ability: "noability", Moves: mv("spore", "trickroom")}},
				team{{Species: "Ninjask", Ability: "noability", Moves: mv("poisonjab", "spore")}},
			)
			p.makeChoices("move trickroom", "move poisonjab")
			p.logHas("twisted the dimensions", "Trick Room should be up before the second turn")
			p.makeChoices("move spore", "move spore")
			p.noStatus(p.mine(), "the slower Pokemon should have moved first and never been Spored")
			p.hasStatus(p.foe(), "slp", "the faster Pokemon should have been Spored before it could act")
		})

		g.it("should not allow Pokemon using a lower priority move to act before other Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Bronzong", As: "Muk", Ability: "noability", Moves: mv("spore", "trickroom")}},
				team{{Species: "Ninjask", Ability: "noability", Moves: mv("poisonjab", "protect")}},
			)
			p.makeChoices("move trickroom", "move poisonjab")
			p.makeChoices("move spore", "move protect")
			p.noStatus(p.mine(), "the Protect user never attacked")
			p.noStatus(p.foe(), "Protect's priority should still put it ahead of Spore under Trick Room")
		})

		g.it("should also affect the activation order for abilities and other non-move actions", func(p *ps) {
			p.battle(
				team{
					{Species: "Bronzong", As: "Muk", Ability: "noability", Moves: mv("trickroom", "explosion")},
					{Species: "Hippowdon", As: "Rhydon", Ability: "drizzle", Moves: mv("protect")},
				},
				team{
					{Species: "Ninjask", Ability: "noability", Moves: mv("shellsmash")},
					{Species: "Ninetales", Ability: "drought", Moves: mv("protect")},
				},
			)
			p.makeChoices("move explosion", "move shellsmash")
			p.fainted(p.mine(), "Explosion should have knocked out its user")
			p.fainted(p.foe(), "Explosion should have knocked out the target")
			// Upstream names the replacements; here the team order does not
			// change on a switch, so both are slot 2.
			p.makeChoices("switch 2", "switch 2")
			p.species(p.mine(), "Rhydon", "")
			p.species(p.foe(), "Ninetales", "")
			p.equal(p.weather(), "sun",
				"under Trick Room the slower entrant's weather should be set first and overwritten by the faster one's")
		})

		g.skip("should roll over and cause Pokemon with 1809 or more speed to outspeed Pokemon with 1808 or less",
			"level is fixed at 50, so the 1809 Speed the Trick Room rollover turns on is unreachable")
		g.skip("should not affect damage dealt by moves whose power is reliant on speed",
			"level is fixed at 50, so the 1809 Speed the Trick Room rollover turns on is unreachable")
	})
}
