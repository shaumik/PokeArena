//go:build showdown

package showdown

import (
	"strings"
	"testing"
)

// Ported from test/sim/moves/grassyterrain.js.
//
// Sleep Talk, upstream's do-nothing, is not in this dataset and is Splash
// everywhere it appears. Sky Drop is not in it either, and it is the subject of
// the semi-invulnerable case — the move has to carry the target into the air
// for both assertions to mean anything — so it stays and the missing-move
// failure is the finding. Grassy Surge is not one of this engine's 118
// abilities; where a case only needs the terrain up, the move puts it up
// instead, and the one case whose subject is the ability keeps it.
//
// Substitutions beyond the shared table, and what each preserves:
//
//   - Florges is the duration case's setter and needs nothing but to be there;
//     Clefable keeps the pure Fairy typing.
//   - Shaymin is a grounded Grass body upstream. Three cases only need
//     "grounded", and the Earthquake case needs a grounded target that is not
//     also resistant to Ground, so it takes Snorlax: the halving is then
//     measured against a full-size figure instead of one Grass has already
//     quartered. The heal case and the semi-invulnerable case take Tangela,
//     which keeps the Grass typing and the grounding. The Nature Power case
//     takes Gengar, for the reason below.
//   - Rillaboom is the slow Leftovers holder in the ordering case; Tangela is
//     Grass, holds the item and is comfortably slower than Alakazam, which is
//     the whole speed relationship the case is built on.
//
// Three cases are restated because what upstream reads is not readable here:
//
//   - The Earthquake case calls battle.runEvent('BasePower') directly. There is
//     no such hook, so the port measures Earthquake and Bulldoze damage against
//     the identical fixture with no terrain up. Only the halved direction is
//     measurable: upstream's other direction is Earthquake into a Flying
//     Aerodactyl, and anything ungrounded enough to escape the halving is also
//     immune to the move, so no damage figure can distinguish it from zero.
//     Magnitude is named in the case title and tested in neither version — its
//     base power is rolled, so no fixed figure exists to compare.
//   - Nature Power's call is named in a protocol line and in nothing this
//     engine emits, so the substitution is read off the type chart. Under
//     Grassy Terrain the call should be Energy Ball, a Grass move a Ghost-type
//     takes; with no terrain it is Tri Attack, a Normal move a Ghost-type is
//     immune to. Shaymin therefore becomes Gengar as the target — the terrain
//     comes from the move, not from typing. Reading it the other way round
//     would have let an unimplemented Nature Power pass by doing nothing.
//   - The heal-ordering case reads Showdown's debug log. This engine's
//     equivalent lines are prose, so the port finds them by line and compares
//     positions. applyTerrainResidual in residuals.go walks side 0 then side 1
//     rather than by Speed, so the first half of the claim is expected to be
//     the finding.
//
// The Grass-boost case is a Gen 7 battle and skips as a generation; with it
// goes the only coverage of the type boost, which is 1.3x here and was 1.5x
// then.

func TestMovesGrassyTerrain(t *testing.T) {
	describe(t, "Grassy Terrain", func(g *psg) {
		g.it("should change the current terrain to Grassy Terrain for five turns", func(p *ps) {
			p.battle(
				team{{Species: "Florges", As: "Clefable", Ability: "noability", Moves: mv("grassyterrain", "splash")}},
				team{{Species: "Wynaut", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			for turn := 1; turn <= 4; turn++ {
				p.makeChoices("move grassyterrain", "move splash")
				p.equal(p.terrain(), "grassy", "the terrain should still be up")
			}
			p.makeChoices("move splash", "move splash")
			p.equal(p.terrain(), "", "the terrain should have run out after five turns")
		})

		g.it("should halve the base power of Earthquake, Bulldoze, and Magnitude against grounded targets", func(p *ps) {
			// Shell Armor on the target so a crit cannot move either figure.
			p.battle(
				team{{Species: "Shaymin", As: "Snorlax", Ability: "shellarmor",
					Moves: mv("grassyterrain", "splash")}},
				team{{Species: "Aerodactyl",
					Moves: mv("earthquake", "bulldoze", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move grassyterrain", "move splash")
			p.equal(p.terrain(), "grassy", "the terrain the case needs should be up")
			def := p.mine()
			// The terrain's own 1/16 end-of-turn heal lands in the same call as
			// the hit, so it is added back to recover the damage figure.
			heal := def.MaxHP / 16
			before := def.HP
			p.makeChoices("move splash", "move earthquake")
			quakeUnder := before - def.HP + heal
			before = def.HP
			p.makeChoices("move splash", "move bulldoze")
			bulldozeUnder := before - def.HP + heal

			p.battle(
				team{{Species: "Shaymin", As: "Snorlax", Ability: "shellarmor",
					Moves: mv("grassyterrain", "splash")}},
				team{{Species: "Aerodactyl",
					Moves: mv("earthquake", "bulldoze", "splash")}},
			)
			p.makeChoices("move splash", "move splash")
			bare := p.mine()
			bareBefore := bare.HP
			p.makeChoices("move splash", "move earthquake")
			quakeBare := bareBefore - bare.HP
			bareBefore = bare.HP
			p.makeChoices("move splash", "move bulldoze")
			bulldozeBare := bareBefore - bare.HP

			p.atLeast(quakeBare, 1, "the Earthquake baseline should have connected at all")
			p.atLeast(bulldozeBare, 1, "the Bulldoze baseline should have connected at all")
			p.bounded(quakeUnder*100, quakeBare*40, quakeBare*62,
				"Earthquake into a grounded target should be halved")
			p.bounded(bulldozeUnder*100, bulldozeBare*40, bulldozeBare*62,
				"Bulldoze into a grounded target should be halved")
		})

		g.skip("should increase the base power of Grass-type attacks used by grounded Pokemon",
			"gen 7 mechanics")

		g.it("should heal grounded Pokemon by 1/16 of their max HP", func(p *ps) {
			p.battle(
				team{{Species: "Shaymin", As: "Tangela", Moves: mv("grassyterrain", "seismictoss")}},
				team{{Species: "Wynaut", Moves: mv("magnetrise", "seismictoss")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move grassyterrain", "move magnetrise")
			p.makeChoices("move seismictoss", "move seismictoss")
			// Seismic Toss deals damage equal to the level, fixed at 50 here,
			// where upstream's figure of 100 is its level-100 equivalent.
			mine, foe := p.mine(), p.foe()
			p.equal(mine.HP, mine.MaxHP-50+mine.MaxHP/16,
				"the grounded Pokemon should have taken Seismic Toss and healed a sixteenth back")
			p.equal(foe.HP, foe.MaxHP-50,
				"Magnet Rise lifts its user off the ground, so the terrain should not heal it")
		})

		g.it("should not affect Pokemon in a semi-invulnerable state", func(p *ps) {
			p.battle(
				team{{Species: "Shaymin", As: "Tangela", Moves: mv("grassyterrain", "seismictoss")}},
				team{{Species: "Wynaut", Moves: mv("skydrop", "seismictoss")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move seismictoss", "move seismictoss")
			p.makeChoices("move grassyterrain", "move skydrop")
			mine, foe := p.mine(), p.foe()
			p.equal(mine.HP, mine.MaxHP-50,
				"Sky Drop carries the setter up with it, so the fresh terrain should not heal it")
			p.equal(foe.HP, foe.MaxHP-50,
				"the Sky Drop user is airborne and should not be healed either")
		})

		g.it("should cause Nature Power to become Energy Ball", func(p *ps) {
			p.battle(
				team{{Species: "Shaymin", As: "Gengar", Ability: "noability", Moves: mv("grassyterrain")}},
				team{{Species: "Wynaut", Moves: mv("naturepower")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move grassyterrain", "move naturepower")
			p.equal(p.terrain(), "grassy", "the terrain should be up before Nature Power resolves")
			p.damaged(p.mine(),
				"Nature Power should have become Energy Ball, which a Ghost-type takes; "+
					"the no-terrain default, Tri Attack, would not have touched it")
		})

		g.it("should heal by Speed order in the same block as Leftovers", func(p *ps) {
			// Grassy Surge is not modeled, so the slow Leftovers holder puts the
			// terrain up with the move on turn one and both sides trade Seismic
			// Toss on turn two, which is the turn the ordering is read from.
			p.battle(
				team{{Species: "rillaboom", As: "Tangela", Ability: "noability", Item: "leftovers",
					Moves: mv("grassyterrain", "seismictoss")}},
				team{{Species: "alakazam", Item: "focussash", Moves: mv("seismictoss")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move grassyterrain", "move seismictoss")
			p.makeChoices("move seismictoss", "move seismictoss")

			// Checked through logHas first so a mistyped fragment is reported as
			// a typo rather than silently making the ordering unmeasurable.
			p.logHas("is healed by the Grassy Terrain!", "both actives are damaged and grounded, so both should heal")
			p.logHas("restored a little HP (", "the Leftovers holder should heal from its item too")

			slow, fast := p.mine(), p.foe()
			lines := strings.Split(p.lastTurnText(), "\n")
			lineOf := func(name, fragment string) int {
				for i, l := range lines {
					if strings.Contains(l, name) && strings.Contains(l, fragment) {
						return i
					}
				}
				return -1
			}
			fastGrassy := lineOf(fast.Name, "is healed by the Grassy Terrain!")
			slowGrassy := lineOf(slow.Name, "is healed by the Grassy Terrain!")
			slowLeftovers := lineOf(slow.Name, "restored a little HP (")
			p.atLeast(fastGrassy, 0, "the faster Pokemon should have a Grassy Terrain heal line")
			p.atLeast(slowGrassy, 0, "the slower Pokemon should have a Grassy Terrain heal line")
			p.atLeast(slowLeftovers, 0, "the Leftovers holder should have an item heal line")
			p.ok(fastGrassy >= 0 && slowGrassy > fastGrassy,
				"Grassy Terrain should heal in Speed order, the faster Pokemon first")
			p.ok(slowGrassy >= 0 && slowLeftovers > slowGrassy,
				"a Pokemon's Grassy Terrain heal should come before its Leftovers heal")
		})

		g.it("should only decrement turn count when being set before it would decrement in the end-of-turn effects", func(p *ps) {
			// Shedinja is here only as a body that kills itself with its own
			// Sticky Barb so Neutralizing Gas lifts mid-battle; HP: 1 on an
			// in-dex body reproduces that exactly. Grassy Surge is kept because
			// it is the case's subject — the terrain has to arrive from the
			// ability, part-way through a turn, for there to be anything to ask.
			p.battle(
				team{{Species: "grookey", As: "Tangela", Ability: "grassysurge", Moves: mv("splash")}},
				team{
					{Species: "shedinja", As: "Butterfree", Ability: "neutralizinggas", Item: "stickybarb",
						HP: 1, Moves: mv("splash")},
					{Species: "wynaut", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			p.turn() // KO Neutralizing Gas
			p.turn() // Switch
			for i := 0; i < 4; i++ {
				p.turn()
			}
			p.equal(p.terrain(), "grassy", "Grassy Terrain should still be active turn 5, ending turn 6.")
		})

		g.skip("should not skip healing Pokemon if it was set during the block it would heal Pokemon",
			"Dynamax (pending upstream: it.skip)")
	})
}
