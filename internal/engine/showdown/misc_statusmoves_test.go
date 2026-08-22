//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/statusmoves.js.
//
// Both live cases are a single Smeargle walking one probe move into each of
// several targets, so the substitutions are chosen entirely for the typing the
// probe is aimed at:
//
//	Klefki    -> Magneton   Steel, so Gastro Acid's Poison typing is still the
//	                        immunity being ignored. Magician is not modeled, so
//	                        upstream's "the item was not stolen" reading is
//	                        replaced by the engine's own suppression line.
//	Dusknoir  -> Gengar     Ghost, the immunity Glare's Normal typing faces.
//	Slaking   -> Snorlax    Normal, the immunity Confuse Ray's Ghost typing faces.
//	Tornadus  -> Pidgeot    Flying, the immunity Sand Attack's Ground typing faces.
//	Unown     -> Weezing    Levitate natively, the other Ground immunity.
//	Emboar    -> Charizard  Fire, which is all the burn probe needs.
//	Aron      -> Magneton   Steel, which is all the poison probe needs.
//
// Prankster and Truant are not modeled, and the harness refuses an unmodeled
// ability rather than letting a fixture run silently without it. Neither is
// load-bearing: Truant is on a body being confused, and the move order
// Prankster buys upstream changes nothing here, since a switch resolves before
// the incoming move either way. Both become "noability", upstream's own idiom
// for a body that must not interfere. Snorlax's own Immunity is stripped for
// the same reason. `sleeptalk` and `shadowsneak` are not in this dataset and
// are pure filler in the original, so `splash` stands in for both.
//
// Upstream's `|-immune|` protocol assertions are dropped: this engine's status
// sink refuses an immune target silently, so there is no line to match. The
// state assertion — the status did not land — carries the case.
//
// The Gen 2 block skips whole.

func TestMiscStatusMoves(t *testing.T) {
	describe(t, "Most status moves", func(g *psg) {
		g.it("should ignore natural type immunities", func(p *ps) {
			p.battle(
				team{{
					Species: "Smeargle", Ability: "noability", Item: "leftovers",
					Moves: mv("gastroacid", "glare", "confuseray", "sandattack"),
				}},
				team{
					{Species: "Klefki", As: "Magneton", Ability: "sturdy", Moves: mv("return")},
					{Species: "Dusknoir", As: "Gengar", Ability: "frisk", Moves: mv("shadowpunch")},
					{Species: "Slaking", As: "Snorlax", Ability: "noability", Moves: mv("shadowclaw")},
					{Species: "Tornadus", As: "Pidgeot", Ability: "noability", Moves: mv("tailwind")},
					{Species: "Unown", As: "Weezing", Ability: "levitate", Moves: mv("hiddenpower")},
				},
			)
			p.makeChoices("move gastroacid", "move return")
			p.logHas("ability was suppressed", "Gastro Acid should land on a Steel-type")

			p.makeChoices("move glare", "switch 2")
			p.hasStatus(p.foe(), "par", "Glare should paralyze a Ghost-type")

			p.makeChoices("move confuseray", "switch 3")
			p.ok(p.foe().Volatiles.Confusion != nil, "Confuse Ray should confuse a Normal-type")

			p.makeChoices("move sandattack", "switch 4")
			p.statStage(p.foe(), "accuracy", -1, "Sand Attack should land on a Flying-type")

			p.makeChoices("move sandattack", "switch 5")
			p.statStage(p.foe(), "accuracy", -1, "Sand Attack should land on a Levitate holder")
		})

		g.it("should fail when the opposing Pokemon is immune to the status effect it sets", func(p *ps) {
			p.battle(
				team{{
					Species: "Smeargle", Ability: "noguard", Item: "laggingtail",
					Moves: mv("thunderwave", "willowisp", "poisongas", "toxic"),
				}},
				team{
					{Species: "Zapdos", Moves: mv("charge")},
					{Species: "Emboar", As: "Charizard", Moves: mv("splash")},
					{Species: "Muk", Moves: mv("splash")},
					{Species: "Aron", As: "Magneton", Moves: mv("magnetrise")},
				},
			)
			p.makeChoices("move thunderwave", "move charge")
			p.noStatus(p.foe(), "an Electric-type cannot be paralyzed")

			p.makeChoices("move willowisp", "switch 2")
			p.noStatus(p.foe(), "a Fire-type cannot be burned")

			p.makeChoices("move poisongas", "switch 3")
			p.noStatus(p.foe(), "a Poison-type cannot be poisoned")

			p.makeChoices("move toxic", "move splash")
			p.noStatus(p.foe(), "a Poison-type cannot be badly poisoned")

			p.makeChoices("move poisongas", "switch 4")
			p.noStatus(p.foe(), "a Steel-type cannot be poisoned")

			p.makeChoices("move toxic", "move magnetrise")
			p.noStatus(p.foe(), "a Steel-type cannot be badly poisoned")
		})
	})

	describe(t, "Poison-inflicting status moves [Gen 2]", func(g *psg) {
		g.skip("should not ignore type immunities", "gen 2 mechanics")
	})
}
