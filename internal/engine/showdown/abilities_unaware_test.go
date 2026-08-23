//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/unaware.js.
//
// Upstream states every case as an absolute damage window at level 100
// (`assert.bounded(damage, [19, 22])`). None of those numbers transfer to a
// level-50 engine, so each case is restated as the comparison the window was
// standing in for: the same attack measured once without the boost and once
// with it, and a ratio wide enough that a damage roll cannot cross it. The
// idle turns are there for the same reason — the side that is not being
// measured uses Splash while the other one boosts, so exactly two hits land
// per case and neither measurement is taken off a chipped Pokémon.
//
// Species. Wynaut's stand-in Hypno is the attacker; it is faster than
// Clefable here where upstream's Wynaut was slower, so Clefable's measuring
// turns use an inert move rather than Soft-Boiled. Registeel is not in the
// dex; Lapras takes its place — it carries the Shell Armor upstream picked
// (no critical hits to widen the window) and is bulky enough to sit through
// both measured hits.
//
// Moves. Belly Drum, Wicked Blow, Tail Glow and Lucky Chant are not in this
// dataset. Belly Drum's +6 is built from three Swords Dances; Wicked Blow is
// replaced by Strength, a plain physical move with no secondary — its
// guaranteed critical hit only existed upstream to narrow the damage window,
// and critical hits in this engine do not ignore defense stages anyway, which
// is also why Lucky Chant is not needed. Pay Day is replaced by Strength for
// headroom: at level 50 Pay Day lands for about 20, and a 4x defense drop
// puts the measurement into single digits where rounding dominates. Super
// Luck is likewise dropped from the Foul Play case.

func TestAbilitiesUnaware(t *testing.T) {
	describe(t, "Unaware", func(g *psg) {
		g.it("should ignore attack stage changes when Pokemon with it are attacked", func(p *ps) {
			p.battle(
				team{{Species: "Clefable", Ability: "unaware", Moves: mv("splash", "softboiled")}},
				team{{Species: "Wynaut", Ability: "noability", Moves: mv("strength", "swordsdance")}},
			)
			before := p.mine().HP
			p.makeChoices("move splash", "move strength")
			plain := before - p.mine().HP

			for i := 0; i < 3; i++ {
				p.makeChoices("move softboiled", "move swordsdance")
			}
			p.statStage(p.foe(), "atk", 6, "three Swords Dances should have maxed the Attack")

			before = p.mine().HP
			p.makeChoices("move splash", "move strength")
			boosted := before - p.mine().HP

			p.atMost(boosted, plain*2, "Unaware should read the attacker's Attack unboosted")
		})

		g.it("should not ignore attack stage changes when Pokemon with it attack", func(p *ps) {
			p.battle(
				team{{Species: "Clefable", Ability: "unaware", Moves: mv("moonblast", "nastyplot")}},
				team{{Species: "Lapras", Ability: "shellarmor", Moves: mv("splash")}},
			)
			before := p.foe().HP
			p.makeChoices("move moonblast", "move splash")
			plain := before - p.foe().HP

			p.makeChoices("move nastyplot", "move splash")

			before = p.foe().HP
			p.makeChoices("move moonblast", "move splash")
			boosted := before - p.foe().HP

			p.atLeast(boosted*2, plain*3, "Unaware should not blind its holder to its own Sp. Atk boost")
		})

		g.it("should ignore defense stage changes when Pokemon with it attack", func(p *ps) {
			// Lagging Tail keeps Clefable last, as upstream: the two are on the
			// same speed tier here, so without it the Amnesia might land after
			// the Moonblast it is supposed to be resisting.
			p.battle(
				team{{Species: "Clefable", Ability: "unaware", Item: "laggingtail", Moves: mv("moonblast")}},
				team{{Species: "Lapras", Ability: "shellarmor", Moves: mv("splash", "amnesia")}},
			)
			before := p.foe().HP
			p.makeChoices("move moonblast", "move splash")
			plain := before - p.foe().HP

			before = p.foe().HP
			p.makeChoices("move moonblast", "move amnesia")
			boosted := before - p.foe().HP
			p.statStage(p.foe(), "spd", 2, "Amnesia should have landed first")

			p.atLeast(boosted*4, plain*3, "Unaware should ignore the target's Sp. Def boost")
		})

		g.it("should not ignore defense stage changes when Pokemon with it are attacked", func(p *ps) {
			p.battle(
				team{{Species: "Clefable", Ability: "unaware", Moves: mv("splash", "irondefense")}},
				team{{Species: "Lapras", Ability: "shellarmor", Moves: mv("strength", "splash")}},
			)
			before := p.mine().HP
			p.makeChoices("move splash", "move strength")
			plain := before - p.mine().HP

			for i := 0; i < 3; i++ {
				p.makeChoices("move irondefense", "move splash")
			}
			p.statStage(p.mine(), "def", 6, "three Iron Defenses should have maxed the Defense")

			before = p.mine().HP
			p.makeChoices("move splash", "move strength")
			boosted := before - p.mine().HP

			p.atMost(boosted*2, plain, "Unaware should not blind its holder to its own Defense boost")
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Clefable", Ability: "unaware", Moves: mv("splash", "softboiled")}},
				team{{Species: "Wynaut", Ability: "moldbreaker", Moves: mv("strength", "swordsdance")}},
			)
			before := p.mine().HP
			p.makeChoices("move splash", "move strength")
			plain := before - p.mine().HP

			for i := 0; i < 3; i++ {
				p.makeChoices("move softboiled", "move swordsdance")
			}

			before = p.mine().HP
			p.makeChoices("move splash", "move strength")
			boosted := before - p.mine().HP

			p.atLeast(boosted, plain*2, "Mold Breaker should switch Unaware off, so the boost counts")
		})

		g.skip("should only apply to targets with Unaware in battles with multiple Pokemon", "doubles")

		g.it("should ignore attack stage changes when Pokemon with it are attacked with Foul Play", func(p *ps) {
			p.battle(
				team{{Species: "Clefable", Ability: "unaware", Moves: mv("splash", "swordsdance")}},
				team{{Species: "Wynaut", Ability: "noability", Moves: mv("foulplay", "splash")}},
			)
			before := p.mine().HP
			p.makeChoices("move splash", "move foulplay")
			plain := before - p.mine().HP

			for i := 0; i < 3; i++ {
				p.makeChoices("move swordsdance", "move splash")
			}
			p.statStage(p.mine(), "atk", 6, "three Swords Dances should have maxed the Attack")

			before = p.mine().HP
			p.makeChoices("move splash", "move foulplay")
			boosted := before - p.mine().HP

			p.atMost(boosted, plain*2, "Foul Play swings the target's own Attack, which Unaware should ignore")
		})
	})
}
