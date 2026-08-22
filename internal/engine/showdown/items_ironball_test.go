//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/ironball.js.
//
// Upstream reads the effectiveness straight off the protocol line that follows
// the move (`|-supereffective|`, `|-resisted|`). This engine emits prose, so the
// ports assert on "It's super effective!" / "It's not very effective..." plus
// the state change the line is about.
//
// Species substitutions, all of them chosen for the type relationship the case
// turns on rather than for the Pokemon:
//
//   - Tropius is a Flying type whose other half resists Ground. Butterfree is
//     the in-dex body with that shape (Bug resists Ground, Flying is immune to
//     it), so Iron Ball turning the pair neutral and Gravity turning it back
//     into a resistance both read the same way.
//   - Rotom is an Electric type carrying Levitate. Raichu is pure Electric and
//     the port gives it Levitate explicitly, exactly as upstream gives Parasect
//     a Levitate it does not naturally have.
//   - Thundurus is an Electric/Flying body that is airborne by type. Zapdos is
//     the in-dex Electric/Flying. Prankster is not modeled and is dropped, so
//     the terrain gets a turn of its own — see the note on that case.
//
// The first case cannot be ported literally. Upstream reads the holder's Speed
// stat through battle.modify; nothing here exposes the item-modified speed, so
// the halving is observed through turn order instead — the two sides set
// opposing weather and the slower one, moving last, wins the field. Smeargle's
// usual stand-in Chansey is too slow for that comparison, so this one case
// names Persian, whose Speed sits between Aerodactyl's and half of it.

func TestItemsIronBall(t *testing.T) {
	describe(t, "Iron Ball", func(g *psg) {
		g.it("should reduce halve the holder's speed", func(p *ps) {
			p.battle(
				team{{
					Species: "Smeargle", As: "Persian", Ability: "owntempo", Item: "ironball",
					Moves: mv("bestow", "raindance"),
				}},
				team{{Species: "Aerodactyl", Ability: "pressure", Moves: mv("stealthrock", "sunnyday")}},
			)
			p.makeChoices("move bestow", "move stealthrock")
			p.equal(p.foe().Item, "ironball", "Bestow should have handed the Iron Ball over")
			// Aerodactyl outspeeds Persian bare and is outsped by it at half
			// Speed, so whoever sets weather last is the one under test.
			p.sets(func() any { return p.weather() }, "sun", func() {
				p.makeChoices("move raindance", "move sunnyday")
			}, "the Iron Ball holder should have moved second and had the last word on the weather")
		})

		g.it("should negate Ground immunities and deal neutral type effectiveness to Flying-type Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Item: "laggingtail", Moves: mv("earthquake")}},
				team{
					{Species: "Aerodactyl", Ability: "pressure", Item: "ironball", Moves: mv("stealthrock")},
					{
						Species: "Tropius", As: "Butterfree", Ability: "noability", Item: "ironball",
						Moves: mv("leechseed"),
					},
				},
			)
			p.makeChoices("move earthquake", "move stealthrock")
			p.logLacks("It's super effective!",
				"Iron Ball should leave Ground neutral on Rock/Flying, not super effective")
			p.damaged(p.foe(), "the Flying-type holder should not be immune to Ground")
			p.makeChoices("move earthquake", "switch 2")
			p.logLacks("It's not very effective...",
				"Iron Ball should leave Ground neutral on Bug/Flying, not resisted")
			p.damaged(p.foe(), "the Flying-type holder should not be immune to Ground")
		})

		g.it("should not deal neutral type effectiveness to Flying-type Pokemon in Gravity", func(p *ps) {
			p.battle(
				team{{
					Species: "Smeargle", Ability: "owntempo", Item: "laggingtail",
					Moves: mv("earthquake", "gravity"),
				}},
				team{
					{Species: "Aerodactyl", Ability: "shellarmor", Item: "ironball", Moves: mv("stealthrock")},
					{
						Species: "Tropius", As: "Butterfree", Ability: "shellarmor", Item: "ironball",
						Moves: mv("leechseed"),
					},
				},
			)
			p.makeChoices("move gravity", "move stealthrock")

			p.makeChoices("move earthquake", "move stealthrock")
			p.logHas("It's super effective!",
				"under Gravity the Rock half should decide the matchup")
			p.damaged(p.foe(), "")
			p.makeChoices("move earthquake", "switch 2")
			p.logHas("It's not very effective...",
				"under Gravity the Bug half should decide the matchup")
			p.damaged(p.foe(), "")
		})

		g.it("should negate artificial Ground immunities and deal normal type effectiveness", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Item: "laggingtail", Moves: mv("earthquake")}},
				team{
					{Species: "Rotom", As: "Raichu", Ability: "levitate", Item: "ironball", Moves: mv("rest")},
					{Species: "Parasect", Ability: "levitate", Item: "ironball", Moves: mv("rest")},
				},
			)
			p.makeChoices("move earthquake", "move rest")
			p.logHas("It's super effective!", "Iron Ball should override Levitate on the Electric holder")
			p.damaged(p.foe(), "")
			p.makeChoices("move earthquake", "switch 2")
			p.logHas("It's not very effective...", "Iron Ball should override Levitate on the Bug/Grass holder")
			p.damaged(p.foe(), "")
		})

		g.it("should ground Pokemon that are airborne", func(p *ps) {
			// Upstream leans on Thundurus's Prankster to get Electric Terrain
			// down before Spore in the same turn. Prankster is not modeled
			// here, and the Iron Ball under test halves Zapdos's Speed to 60,
			// below the Chansey standing in for Smeargle at 70 — so a
			// single-turn translation has the Spore land on bare ground and
			// measures nothing. The terrain gets its own turn instead; the
			// question, whether a grounded Iron Ball holder is covered by
			// Electric Terrain, is unchanged.
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("spore", "splash")}},
				team{{
					Species: "Thundurus", As: "Zapdos", Ability: "pressure", Item: "ironball",
					Moves: mv("electricterrain"),
				}},
			)
			p.makeChoices("move splash", "move electricterrain")
			p.equal(p.terrain(), "electric", "Electric Terrain should be up before the Spore")
			p.makeChoices("move spore", "move electricterrain")
			p.noStatus(p.foe(), "an Iron Ball holder is grounded, so Electric Terrain should refuse the sleep")
		})
	})
}
