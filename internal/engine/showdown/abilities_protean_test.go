//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/protean.js.
//
// Protean is not in this engine's ability set, so every ported case here is
// expected to be red on the type assertion — that failure is the finding. The
// fixtures still set Ability: "protean" so the case says what it is asking
// for.
//
// Cinderace and Kecleon are not in this dex. Cinderace becomes Arcanine, which
// keeps the Fire starting type the cases measure a change *away* from;
// Kecleon becomes Snorlax, which keeps the Normal starting type the third and
// fourth cases assert the user stays at. Tsareena becomes Vileplume (Grass
// kept, Dazzling set explicitly) and Regieleki/Helioptile appear only in
// blocks that skip for their generation.
//
// Sleep Talk is not in this dataset; Splash replaces it wherever upstream used
// it as an inert "do nothing" move. Aura Wheel, Counter, Metal Burst, Mind
// Blown and Powder are absent too, but each is the vehicle whose early failure
// the case is about, so they are kept and the missing-move failure is the
// finding.
//
// Showdown renumbers a side's team as Pokemon switch, so its `switch 2` can
// mean "bring the other one back". This engine keeps team order fixed, so
// those calls are translated to the slot that actually holds the Pokemon
// upstream meant.

func TestAbilitiesProtean(t *testing.T) {
	describe(t, "Protean", func(g *psg) {
		g.it("should change the user's type when using a move", func(p *ps) {
			hasType := func(want string) bool {
				m := p.mine()
				return psID(string(m.Type1)) == want || psID(string(m.Type2)) == want
			}
			p.battle(
				team{{Species: "Cinderace", As: "Arcanine", Ability: "protean", Moves: mv("highjumpkick")}},
				team{{Species: "Gengar", Moves: mv("splash")}},
			)
			p.turn()
			p.ok(hasType("fighting"), "High Jump Kick should have made the user Fighting-type")
		})

		g.skip("should change the user's type for submoves to the type of that submove, not the move calling it",
			"gen 6 mechanics")

		g.it("should not change the user's type when using moves that fail earlier than Protean will activate", func(p *ps) {
			hasType := func(want string) bool {
				m := p.mine()
				return psID(string(m.Type1)) == want || psID(string(m.Type2)) == want
			}
			p.battle(
				team{
					{Species: "Kecleon", As: "Snorlax", Ability: "protean",
						Moves: mv("fling", "suckerpunch", "steelroller", "aurawheel")},
					{Species: "Kecleon", As: "Snorlax", Ability: "protean",
						Moves: mv("counter", "metalburst")},
					{Species: "Kecleon", As: "Snorlax", Ability: "protean",
						Moves: mv("magnetrise", "ingrain", "burnup", "auroraveil")},
				},
				team{{Species: "Wynaut", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}

			p.makeChoices("move fling", "auto")
			p.ok(hasType("normal"), "Protean changed typing when Fling was used with no item.")

			p.makeChoices("move suckerpunch", "auto")
			p.ok(hasType("normal"), "Protean changed typing when Sucker Punch was used into a status move.")

			p.makeChoices("move steelroller", "auto")
			p.ok(hasType("normal"), "Protean changed typing when Steel Roller was used with no Terrain active.")

			p.makeChoices("move aurawheel", "auto")
			p.ok(hasType("normal"), "Protean changed typing when Aura Wheel was used by non-Morpeko.")

			p.makeChoices("switch 2", "auto")

			p.makeChoices("move counter", "auto")
			p.ok(hasType("normal"), "Protean changed typing when Counter failed to return damage.")

			p.makeChoices("move metalburst", "auto")
			p.ok(hasType("normal"), "Protean changed typing when Metal Burst failed to return damage.")

			p.makeChoices("switch 3", "auto")

			p.makeChoices("move burnup", "auto")
			p.ok(hasType("normal"), "Protean changed typing when a non-Fire type used Burn Up.")

			p.makeChoices("move auroraveil", "auto")
			p.ok(hasType("normal"), "Protean changed typing when Aurora Veil used out of Hail.")

			p.makeChoices("move ingrain", "auto")
			p.makeChoices("move magnetrise", "auto")
			p.ok(hasType("grass"), "Protean changed typing when Magnet Rise was used while the effect of Ingrain was active.")
		})

		g.it("should not change the user's type when abilities that activate earlier than Protean will cause the user's moves to fail", func(p *ps) {
			hasType := func(want string) bool {
				m := p.mine()
				return psID(string(m.Type1)) == want || psID(string(m.Type2)) == want
			}
			// Vileplume keeps Tsareena's Grass typing; Dazzling is set on it
			// explicitly, which is the only thing the case reads.
			p.battle(
				team{{Species: "Kecleon", As: "Snorlax", Ability: "protean", Moves: mv("aquajet", "mindblown")}},
				team{
					{Species: "Kyogre", Ability: "primordialsea", Moves: mv("splash")},
					{Species: "Groudon", Ability: "desolateland", Moves: mv("powder")},
					{Species: "Tsareena", As: "Vileplume", Ability: "dazzling", Moves: mv("splash")},
					{Species: "Golduck", Ability: "damp", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}

			p.makeChoices("move mindblown", "move splash")
			p.ok(hasType("normal"), "Protean changed typing when a Fire-type attack was used in Primordial Sea.")

			p.makeChoices("move aquajet", "switch 2")
			p.ok(hasType("normal"), "Protean changed typing when a Water-type attack was used in Desolate Land.")

			p.makeChoices("move mindblown", "move powder")
			p.ok(hasType("normal"), "Protean changed typing when a Fire-type attack was used while the user was affected by Powder.")

			p.makeChoices("move aquajet", "switch 3")
			p.ok(hasType("normal"), "Protean changed typing when a priority move was blocked by Dazzling.")

			p.makeChoices("move mindblown", "switch 4")
			p.ok(hasType("normal"), "Protean changed typing when a exploding move was blocked by Damp.")
		})

		g.it("should not allow the user to change its typing twice", func(p *ps) {
			hasType := func(want string) bool {
				m := p.mine()
				return psID(string(m.Type1)) == want || psID(string(m.Type2)) == want
			}
			p.battle(
				team{{Species: "Cinderace", As: "Arcanine", Ability: "protean", Moves: mv("tackle", "watergun")}},
				team{{Species: "Gengar", Moves: mv("splash")}},
			)
			p.turn()
			p.ok(hasType("normal"), "Tackle should have made the user Normal-type")

			p.makeChoices("move watergun", "auto")
			p.isFalse(hasType("water"), "Protean should only fire once per switch-in")
		})

		g.it("should not allow the user to change its typing twice if the Ability was suppressed", func(p *ps) {
			hasType := func(want string) bool {
				m := p.mine()
				return psID(string(m.Type1)) == want || psID(string(m.Type2)) == want
			}
			p.battle(
				team{{Species: "Cinderace", As: "Arcanine", Ability: "protean", Moves: mv("tackle", "watergun")}},
				team{
					{Species: "Gengar", Moves: mv("splash")},
					{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("splash")},
				},
			)
			p.makeChoices("move tackle", "auto")
			p.ok(hasType("normal"), "Tackle should have made the user Normal-type")

			p.makeChoices("move watergun", "switch 2")
			// Upstream's second `switch 2` brings Gengar back; here that is
			// slot 1, because this engine does not renumber the team.
			p.makeChoices("move watergun", "switch 1")

			p.isFalse(hasType("water"), "a suppressed Protean still spends its one activation")
		})

		g.it("should allow the user to change its typing twice if it lost and regained the Ability", func(p *ps) {
			hasType := func(want string) bool {
				m := p.mine()
				return psID(string(m.Type1)) == want || psID(string(m.Type2)) == want
			}
			p.battle(
				team{{Species: "Cinderace", As: "Arcanine", Ability: "protean", Moves: mv("tackle", "watergun")}},
				team{{Species: "Gengar", Ability: "protean", Moves: mv("splash", "skillswap")}},
			)
			p.makeChoices("move tackle", "move skillswap")
			p.ok(hasType("normal"), "Tackle should have made the user Normal-type")

			p.makeChoices("move watergun", "auto")
			p.ok(hasType("water"), "Skill Swap handing Protean back should reset its activation")
		})

		g.it("should not be prevented from resetting its effectState by Ability suppression", func(p *ps) {
			hasType := func(want string) bool {
				m := p.mine()
				return psID(string(m.Type1)) == want || psID(string(m.Type2)) == want
			}
			p.battle(
				team{
					{Species: "Cinderace", As: "Arcanine", Ability: "protean", Moves: mv("tackle")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{
					{Species: "Gengar", Ability: "protean", Moves: mv("splash")},
					{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("splash")},
				},
			)
			p.makeChoices("move tackle", "auto")
			p.makeChoices("move tackle", "switch 2") // Weezing comes in
			p.makeChoices("switch 2", "auto")        // Cinderace switches out
			p.makeChoices("switch 1", "auto")        // and back in
			p.makeChoices("move tackle", "switch 1") // Weezing switches out

			p.ok(hasType("normal"), "Protean should fire again after the switch out and back in")
		})

		// Upstream nests this describe inside Protean; the ledger key keeps
		// the inner name verbatim.
		describe(t, "[Gen 6-8]", func(g *psg) {
			g.skip("should activate on both turns of a charge move", "gen 8 mechanics")
		})
	})
}
