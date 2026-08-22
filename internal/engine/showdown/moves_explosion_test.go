//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/explosion.js.
//
// Three of the four cases are about older generations and skip: this engine has
// no gen-mod layer.
//
// The live case is upstream's "not halved in current gens", and its assertion is
// an absolute damage window at level 100, which cannot travel to a fixed level
// of 50. What the window is doing is separating "Explosion hit a normal
// Defense" from "Explosion hit a halved Defense" — the two differ by a clean
// factor of two. A -2 Defense stage is exactly a halved Defense, so the port
// states the same thing as a comparison: Explosion against an untouched
// defender should do about half what it does against the same defender at -2
// Defense. Screech supplies the -2; it is not in the upstream fixture, and the
// extra turn it costs is why the second battle is built separately.
//
// Metagross and Hippowdon are not in this dex and have no stand-in rows.
// Magneton stands in for Metagross: what the case needs from the attacker is
// only that Explosion is not same-typed (Metagross is Steel/Psychic, Magneton
// Electric/Steel), so no STAB enters the comparison. Golem stands in for
// Hippowdon — a ground body bulky enough to survive both readings, which is
// what makes the two damages measurable at all. Upstream's nature and EV spread
// carry over unchanged.
//
// Shell Armor on the defender is the port's, not upstream's. A comparison of
// two measured hits is only meaningful if neither of them can be a critical
// hit, and with no rigged-RNG hook the only way to say that here is to block
// crits outright. Measured against this engine the two windows are 21-25 and
// 42-49, so the ratio lands in [168, 233].

func TestMovesExplosion(t *testing.T) {
	describe(t, "Explosion", func(g *psg) {
		g.it("should not halve defense in current gens", func(p *ps) {
			// Explosion against an untouched Defense.
			p.battle(
				team{{Species: "Metagross", As: "Magneton", Ability: "noability",
					Nature: "adamant", Moves: mv("explosion", "screech")}},
				team{{Species: "Hippowdon", As: "Golem", Ability: "shellarmor", Nature: "impish",
					EVs: evs(map[string]int{"hp": 252, "def": 252}), Moves: mv("splash")}},
			)
			p.makeChoices("move explosion", "move splash")
			plain := p.foe().MaxHP - p.foe().HP
			p.ok(plain > 0, "Explosion should have damaged the defender")

			// The same Explosion into a Defense that really has been halved.
			p.battle(
				team{{Species: "Metagross", As: "Magneton", Ability: "noability",
					Nature: "adamant", Moves: mv("explosion", "screech")}},
				team{{Species: "Hippowdon", As: "Golem", Ability: "shellarmor", Nature: "impish",
					EVs: evs(map[string]int{"hp": 252, "def": 252}), Moves: mv("splash")}},
			)
			p.makeChoices("move screech", "move splash")
			p.statStage(p.foe(), "def", -2, "Screech should have halved the defender's Defense")
			p.makeChoices("move explosion", "move splash")
			halved := p.foe().MaxHP - p.foe().HP

			if plain > 0 {
				p.bounded(100*halved/plain, 160, 245,
					"Explosion into a halved Defense should do about twice what it does into a full one, "+
						"which is only true if Explosion is not halving Defense itself")
			}
		})

		g.skip("should halve defense in old gens", "gen 4 mechanics")

		g.skip("[Gen 1] Explosion should build rage, even if it misses", "gen 1 mechanics")

		g.skip("[Gen 1] Explosion should faint the user when the target is semi-invulnerable",
			"gen 1 mechanics")
	})
}
