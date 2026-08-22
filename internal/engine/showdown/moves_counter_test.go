//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/counter.js.
//
// Neither Counter nor Mirror Coat is in this dataset, so every live case here
// is expected to stop at "move ... is not in this dataset". That is the
// finding the port exists to record, so the cases are written out in full
// rather than skipped: if the moves are ever added, these say what they have
// to do. The third describe block is entirely Gen 1 and Gen 1 Stadium and
// skips whole.
//
// Two upstream cases read a single hit out of a `battle.onEvent('Damage')`
// hook, which has no counterpart here. Both are re-stated as a comparison
// between the damage the reflector took and the damage it dealt back, which is
// the claim without the hook: reflecting only the last hit of a multi-hit move
// costs the attacker about one move's worth, where reflecting the whole move
// would cost about twice that.
//
// Substitutions the stand-in table does not cover. Sawk and Throh are plain
// Fighting bodies, so Machamp and Primeape stand in — neither is Ghost, which
// is the only typing that would change a Fighting-type fixed-damage exchange.
// Espeon and Umbreon are eeveelutions used for their Psychic/Dark split;
// Jolteon and Vaporeon stand in, and what has to survive is that neither is
// Dark (Mirror Coat is Psychic and Dark is immune to it) and neither is Ghost
// (Sonic Boom and Tackle are Normal).
//
// Two figures move with the level: upstream's Seismic Toss deals 100 at level
// 100, so Counter answers with 200; here it deals 50 and Counter answers with
// 100. Sonic Boom's 20 is level-independent, so Mirror Coat's 40 carries over
// unchanged — though this dataset gives Sonic Boom 0 power and no
// fixed-damage flag, so its 20 is not modeled either.

func TestMovesCounter(t *testing.T) {
	describe(t, "Counter", func(g *psg) {
		g.it("should deal damage equal to twice the damage taken from the last Physical attack", func(p *ps) {
			p.battle(
				team{{Species: "Sawk", As: "Machamp", Ability: "sturdy", Moves: mv("seismictoss")}},
				team{{Species: "Throh", As: "Primeape", Ability: "guts", Moves: mv("counter")}},
			)
			p.hurtsBy(p.mine(), 100, func() { p.turn() },
				"Counter should return twice Seismic Toss's level-fixed damage")
		})

		g.it("should deal damage based on the last hit from the last Physical attack", func(p *ps) {
			p.battle(
				team{{Species: "Sawk", As: "Machamp", Ability: "sturdy", Moves: mv("doublekick")}},
				team{{Species: "Throh", As: "Primeape", Ability: "guts", Moves: mv("counter")}},
			)
			p.turn()
			taken := p.foe().MaxHP - p.foe().HP
			dealt := p.mine().MaxHP - p.mine().HP
			p.atLeast(taken, 1, "Double Kick should have damaged the Counter user")
			// Double Kick hits exactly twice, so twice the last hit is about
			// what the pair cost; twice the total would be about double this.
			p.bounded(dealt, taken*4/5, taken*6/5,
				"Counter should reflect only the last of Double Kick's two hits")
		})

		g.it("should fail if user is not damaged by Physical attacks this turn", func(p *ps) {
			p.battle(
				team{{Species: "Sawk", As: "Machamp", Ability: "sturdy", Moves: mv("aurasphere")}},
				team{{Species: "Throh", As: "Primeape", Ability: "guts", Moves: mv("counter")}},
			)
			p.turn()
			p.fullHP(p.mine(), "Counter should fail against a special attack")
		})

		g.skip("should target the opposing Pokemon that hit the user with a Physical attack most recently that turn",
			"triples")
		g.skip("should respect Follow Me", "doubles")
		g.skip("should not have its target changed by Stalwart", "doubles")
	})

	describe(t, "Mirror Coat", func(g *psg) {
		g.it("should deal damage equal to twice the damage taken from the last Special attack", func(p *ps) {
			p.battle(
				team{{Species: "Espeon", As: "Jolteon", Ability: "noguard", Moves: mv("sonicboom")}},
				team{{Species: "Umbreon", As: "Vaporeon", Moves: mv("mirrorcoat")}},
			)
			p.hurtsBy(p.mine(), 40, func() { p.turn() },
				"Mirror Coat should return twice Sonic Boom's fixed 20")
		})

		g.it("should deal damage based on the last hit from the last Special attack", func(p *ps) {
			p.battle(
				team{{Species: "Espeon", As: "Jolteon", Ability: "synchronize", Moves: mv("watershuriken")}},
				team{{Species: "Umbreon", As: "Vaporeon", Ability: "synchronize", Moves: mv("mirrorcoat")}},
			)
			p.turn()
			taken := p.foe().MaxHP - p.foe().HP
			dealt := p.mine().MaxHP - p.mine().HP
			p.atLeast(taken, 1, "Water Shuriken should have damaged the Mirror Coat user")
			p.atLeast(dealt, 1, "Mirror Coat should have answered the barrage")
			// Water Shuriken hits two to five times, so the only figure that
			// holds for every roll is the inequality: twice the last hit is at
			// most the whole barrage.
			p.atMost(dealt, taken*6/5,
				"Mirror Coat should reflect only the last hit of the barrage, not its total")
		})

		g.it("should fail if user is not damaged by Special attacks this turn", func(p *ps) {
			p.battle(
				team{{Species: "Espeon", As: "Jolteon", Ability: "synchronize", Moves: mv("tackle")}},
				team{{Species: "Umbreon", As: "Vaporeon", Ability: "synchronize", Moves: mv("mirrorcoat")}},
			)
			p.turn()
			p.fullHP(p.mine(), "Mirror Coat should fail against a physical attack")
		})

		g.skip("should target the opposing Pokemon that hit the user with a Special attack most recently that turn",
			"doubles")
		g.skip("should respect Follow Me", "doubles")
		g.skip("should not have its target changed by Stalwart", "doubles")
	})

	// Upstream opens a second describe under the same name for the older
	// generations; the name is kept as written so the ledger key still points
	// at the original.
	describe(t, "Counter", func(g *psg) {
		g.skip("[Gen 1] Counter Desync Clause", "gen 1 mechanics")
		g.skip("[Gen 1] should counter attacks made against substitutes", "gen 1 mechanics")
		g.skip("[Gen 1] simultaneous counters should both fail", "gen 1 mechanics")
		g.skip("[Gen 1 Stadium] should counter Normal/Fighting moves only", "gen 1 mechanics")
		g.skip("[Gen 1 Stadium] should counter attacks made against substitutes", "gen 1 mechanics")
		g.skip("[Gen 1] (High) Jump Kick recoil can be countered", "gen 1 mechanics")
		g.skip("[Gen 1] confusion damage can be countered", "gen 1 mechanics")
		g.skip("[Gen 1] draining can be countered", "gen 1 mechanics")
		g.skip("[Gen 1] Mirror Move can be countered when it calls a counterable move", "gen 1 mechanics")
		g.skip("[Gen 1] Moves with unique damage calculation don't overdamage a target with less HP",
			"gen 1 mechanics")
		g.skip("[Gen 1] Metronome calling Counter fails", "gen 1 mechanics")
	})
}
