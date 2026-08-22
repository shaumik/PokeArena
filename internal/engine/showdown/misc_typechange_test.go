//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/typechange.js.
//
// Upstream generates the cases from a loop over two adder moves inside an outer
// `describe('Type addition')`; the two inner describes are what the ledger keys
// on here, since they are what tells a reader which move a case is about.
//
// Neither Trick-or-Treat nor Forest's Curse is in this dataset, so every live
// case below fails at team construction naming the missing move. They are
// written out in full rather than skipped because that absence is the finding,
// and because the fixtures are then already correct if the moves ever land.
//
// Substitutions. Gourgeist is only a body carrying the move (Gengar). The
// targets are chosen for the typing each assertion turns on: Machamp is in the
// dex and Fighting; Trevenant is Ghost/Grass and is replaced per case by a body
// that already has the type being added (Gengar for Ghost, Venusaur for Grass),
// which is the only property the "should not add" cases use; Deoxys-Speed goes
// through the shared stand-in to Alakazam, keeping the Psychic typing the
// override assertions name. The Arceus cases skip — see misc_arceus_test.go.
//
// The engine stores typing as two fields rather than a list, so a port reads
// Type1/Type2 where upstream reads getTypes().join('/').

func TestMiscTypeChange(t *testing.T) {
	describe(t, "Trick-or-Treat", func(g *psg) {
		g.it("should add Ghost type to its target", func(p *ps) {
			p.battle(
				team{{Species: "Gourgeist", As: "Gengar", Ability: "frisk", Moves: mv("trickortreat")}},
				team{{Species: "Machamp", Ability: "guts", Moves: mv("crosschop")}},
			)
			target := p.foe()
			types := func() any { return string(target.Type1) + "/" + string(target.Type2) }
			p.sets(types, "fighting/ghost", func() {
				p.makeChoices("move trickortreat", "move crosschop")
			}, "Trick-or-Treat should have added Ghost")
		})

		g.it("should not add Ghost type to Ghost targets", func(p *ps) {
			p.battle(
				team{{Species: "Gourgeist", As: "Gengar", Ability: "frisk", Moves: mv("trickortreat")}},
				team{{Species: "Trevenant", As: "Gengar", Ability: "harvest", Moves: mv("ingrain")}},
			)
			target := p.foe()
			types := func() any { return string(target.Type1) + "/" + string(target.Type2) }
			p.constant(types, func() {
				p.makeChoices("move trickortreat", "move ingrain")
			}, "a target that is already Ghost should be unchanged")
		})

		g.skip("should be able to add Ghost type to Arceus",
			"Arceus is not in this 80-species dex and Multitype is not modeled")

		g.it("should fail on repeated use", func(p *ps) {
			p.battle(
				team{{Species: "Gourgeist", As: "Gengar", Ability: "frisk", Moves: mv("trickortreat")}},
				team{{Species: "Deoxys-Speed", Ability: "pressure", Moves: mv("spikes")}},
			)
			target := p.foe()
			types := func() any { return string(target.Type1) + "/" + string(target.Type2) }
			p.makeChoices("move trickortreat", "move spikes")
			p.constant(types, func() {
				p.makeChoices("move trickortreat", "move spikes")
			}, "the second Trick-or-Treat should change nothing")
			p.logHas("But it failed!", "the second use should have failed")
		})

		g.it("should override Forest's Curse", func(p *ps) {
			p.battle(
				team{{Species: "Gourgeist", As: "Gengar", Ability: "frisk", Moves: mv("trickortreat", "forestscurse")}},
				team{{Species: "Deoxys-Speed", Ability: "pressure", Moves: mv("spikes")}},
			)
			target := p.foe()
			types := func() any { return string(target.Type1) + "/" + string(target.Type2) }
			p.makeChoices("move forestscurse", "move spikes")
			p.sets(types, "psychic/ghost", func() {
				p.makeChoices("move trickortreat", "move spikes")
			}, "Trick-or-Treat should have replaced the added Grass type")
		})
	})

	describe(t, "Forest's Curse", func(g *psg) {
		g.it("should add Grass type to its target", func(p *ps) {
			p.battle(
				team{{Species: "Gourgeist", As: "Gengar", Ability: "frisk", Moves: mv("forestscurse")}},
				team{{Species: "Machamp", Ability: "guts", Moves: mv("crosschop")}},
			)
			target := p.foe()
			types := func() any { return string(target.Type1) + "/" + string(target.Type2) }
			p.sets(types, "fighting/grass", func() {
				p.makeChoices("move forestscurse", "move crosschop")
			}, "Forest's Curse should have added Grass")
		})

		g.it("should not add Grass type to Grass targets", func(p *ps) {
			p.battle(
				team{{Species: "Gourgeist", As: "Gengar", Ability: "frisk", Moves: mv("forestscurse")}},
				team{{Species: "Trevenant", As: "Venusaur", Ability: "harvest", Moves: mv("ingrain")}},
			)
			target := p.foe()
			types := func() any { return string(target.Type1) + "/" + string(target.Type2) }
			p.constant(types, func() {
				p.makeChoices("move forestscurse", "move ingrain")
			}, "a target that is already Grass should be unchanged")
		})

		g.skip("should be able to add Grass type to Arceus",
			"Arceus is not in this 80-species dex and Multitype is not modeled")

		g.it("should override Trick-or-Treat", func(p *ps) {
			p.battle(
				team{{Species: "Gourgeist", As: "Gengar", Ability: "frisk", Moves: mv("forestscurse", "trickortreat")}},
				team{{Species: "Deoxys-Speed", Ability: "pressure", Moves: mv("spikes")}},
			)
			target := p.foe()
			types := func() any { return string(target.Type1) + "/" + string(target.Type2) }
			p.makeChoices("move trickortreat", "move spikes")
			p.sets(types, "psychic/grass", func() {
				p.makeChoices("move forestscurse", "move spikes")
			}, "Forest's Curse should have replaced the added Ghost type")
		})

		g.it("should fail on repeated use", func(p *ps) {
			p.battle(
				team{{Species: "Gourgeist", As: "Gengar", Ability: "frisk", Moves: mv("forestscurse")}},
				team{{Species: "Deoxys-Speed", Ability: "pressure", Moves: mv("spikes")}},
			)
			target := p.foe()
			types := func() any { return string(target.Type1) + "/" + string(target.Type2) }
			p.makeChoices("move forestscurse", "move spikes")
			p.constant(types, func() {
				p.makeChoices("move forestscurse", "move spikes")
			}, "the second Forest's Curse should change nothing")
			p.logHas("But it failed!", "the second use should have failed")
		})
	})
}
