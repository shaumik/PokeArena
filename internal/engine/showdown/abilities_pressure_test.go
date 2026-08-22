//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/pressure.js.
//
// Pressure is modeled: turn.go charges the extra PP at PP-payment time for any
// foe move that is not self-targeted. That is the whole of what a singles
// engine can express, so the cases that survive are the singles ones — the
// doubles, triples, Dynamax and Z-move cases have no second slot, no gen-mod
// layer and no gimmick layer to run in, and the [Gen 4] block skips whole.
//
// Dusknoir, Dusclops, Absol, Reuniclus and Yveltal are none of them in this
// dex. Each is standing in for one property and nothing else — a Ghost body
// (Gengar), a Pressure body (Mewtwo, which carries Pressure natively), a
// Psychic body (Alakazam) and a benched Dark Pulse (Moltres) — so the
// substitutions are named on the sets.
//
// Sleep Talk is not in this dataset; Splash replaces it wherever upstream used
// it as an inert "do nothing" move. Shadow Force, Assist and Sticky Web are
// also absent, but those are doing real work in their cases (semi-invulnerable
// vanish, a submove call, and the one hazard that does *not* pay the Pressure
// tax), so they are kept and the missing-move failure is the finding.
// Desolate Land is likewise kept: it is how upstream makes Surf fail.

func TestAbilitiesPressure(t *testing.T) {
	describe(t, "Pressure", func(g *psg) {
		g.skip("should deduct 1 extra PP from opposing Pokemon moves targeting the user", "doubles")

		g.skip("should deduct 1 extra PP if moves are redirected to the user", "doubles")

		g.it("should deduct PP even if the move fails or misses", func(p *ps) {
			// Gengar is the in-dex Ghost body for Dusknoir; the case needs it
			// only to carry Pressure and to vanish with Shadow Force. Smeargle
			// resolves to Chansey through the stand-in table, which is fine —
			// nothing here reads its typing or its stats, only its PP.
			p.battle(
				team{{
					Species: "Dusknoir", As: "Gengar", Ability: "pressure",
					Moves: mv("mistyterrain", "shadowforce"),
				}},
				team{{
					Species: "Smeargle", Ability: "desolateland",
					Moves: mv("doubleedge", "spore", "moonblast", "surf"),
				}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.foe().Moves[0].PP, p.foe().Moves[0].MaxPP-2,
				"Double-Edge should lose 1 additional PP from Pressure")

			p.makeChoices("move shadowforce", "move spore")
			p.equal(p.foe().Moves[1].PP, p.foe().Moves[1].MaxPP-2,
				"Spore should lose 1 additional PP from Pressure")

			p.makeChoices("auto", "move moonblast")
			p.equal(p.foe().Moves[2].PP, p.foe().Moves[2].MaxPP-2,
				"Moonblast should lose 1 additional PP from Pressure")

			p.makeChoices("auto", "move surf")
			p.equal(p.foe().Moves[3].PP, p.foe().Moves[3].MaxPP-2,
				"Surf should lose 1 additional PP from Pressure")
		})

		g.skip("should deduct PP for each Pressure Pokemon targeted", "triples")

		g.skip("should deduct PP for each opposing Pressure Pokemon when Snatch or Imprison are used", "triples")

		g.skip("should deduct additional PP from Max Moves", "Dynamax")

		g.skip("should deduct additional PP from Z-Moves", "Z-moves")

		g.it("should deduct additional PP from submoves that target Pressure", func(p *ps) {
			// Yveltal is only a bench slot holding a Dark Pulse for Assist to
			// find, so any body does; Moltres keeps the Flying half. Absol is
			// only a Pressure holder, and Mewtwo carries Pressure natively.
			p.battle(
				team{
					{Species: "Wynaut", Moves: mv("assist")},
					{Species: "Yveltal", As: "Moltres", Moves: mv("darkpulse")},
				},
				team{{Species: "Absol", As: "Mewtwo", Ability: "pressure", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.mine().Moves[0].PP, p.mine().Moves[0].MaxPP-2,
				"Assist should pay the Pressure tax for the move it called")
		})

		g.it("should not deduct additional PP from Sticky Web (only entry hazard to do so)", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("stickyweb", "stealthrock")}},
				team{{Species: "Absol", As: "Mewtwo", Ability: "pressure", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move stickyweb", "auto")
			p.equal(p.mine().Moves[0].PP, p.mine().Moves[0].MaxPP-1, "Sticky Web should lose only 1 PP")

			p.makeChoices("move stealthrock", "auto")
			p.equal(p.mine().Moves[1].PP, p.mine().Moves[1].MaxPP-2, "Stealth Rock should lose 2 PP")
		})

		g.skip("should deduct additional PP from Tera Blast even when not used into the Pressure target", "doubles")

		g.it("should not deduct additional PP from moves reflected by Magic Coat", func(p *ps) {
			// Alakazam is the in-dex Psychic body for Reuniclus; Gengar is the
			// Ghost body for Dusclops, with Pressure set on it.
			p.battle(
				team{{Species: "Reuniclus", As: "Alakazam", Moves: mv("magiccoat", "confuseray")}},
				team{{Species: "Dusclops", As: "Gengar", Ability: "pressure", Moves: mv("confuseray")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			// All three figures below also hold if nothing is reflected at
			// all, so the reflection is asserted as the case's premise.
			p.logHas("bounced the move back", "premise: Magic Coat should have reflected the Confuse Ray")
			p.equal(p.mine().Moves[0].PP, p.mine().Moves[0].MaxPP-1,
				"Magic Coat targets its own user, so Pressure should not charge it")
			p.equal(p.mine().Moves[1].PP, p.mine().Moves[1].MaxPP,
				"the reflected Confuse Ray should not come out of the reflector's own slot")
			p.equal(p.foe().Moves[0].PP, p.foe().Moves[0].MaxPP-1,
				"the Pressure holder does not charge itself")
		})
	})

	// The whole [Gen 4] block skips: this engine models one generation, and
	// what these cases pin is precisely the Gen 4 rule (every move targeting
	// the holder pays, self-targeted moves do not) that Gen 5 replaced.
	describe(t, "Pressure [Gen 4]", func(g *psg) {
		g.skip("should deduct 1 extra PP from any moves targeting the user", "gen 4 mechanics")
		g.skip("should deduct 1 extra PP if moves are redirected to the user", "gen 4 mechanics")
		g.skip("should deduct PP even if the move fails or misses", "gen 4 mechanics")
		g.skip("should deduct PP for each Pressure Pokemon targeted", "gen 4 mechanics")
		g.skip("should not deduct PP from self-targeting moves", "gen 4 mechanics")
	})
}
