//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/truant.js.
//
// Truant is not one of this engine's 118 abilities, so every case reports it,
// and that report is the finding.
//
// Species. Slaking is not in this dex; Snorlax stands in as the closest
// Normal-type body, with Truant set explicitly as upstream sets it. Steelix
// resolves to Onix through the stand-in table and Registeel becomes Magneton,
// the dex's only steel body. None of the cases turn on the typing — every
// foe is there to hold still and be hit or not hit.
//
// Sleep length. Upstream's Rest is a fixed two turns and Spore's sleep is
// random in both engines, which is why the fourth case loops until its sleeper
// wakes rather than counting turns; that loop is kept.
//
// Entrainment is not in this dataset, but the case using it is a Mega
// Evolution case and skips for that first.

func TestAbilitiesTruant(t *testing.T) {
	describe(t, "Truant", func(g *psg) {
		g.it("should prevent the user from acting the turn after using a move", func(p *ps) {
			p.battle(
				team{{Species: "Slaking", As: "Snorlax", Ability: "truant", Moves: mv("scratch")}},
				team{{Species: "Steelix", Ability: "sturdy", Moves: mv("endure")}},
			)
			pokemon := p.foe()
			p.hurts(pokemon, func() { p.turn() }, "the first turn's Scratch should land")
			p.constant(func() any { return pokemon.HP }, func() { p.turn() },
				"the turn after, Truant should have it loafing around")
		})

		g.it("should allow the user to act after a recharge turn", func(p *ps) {
			p.battle(
				team{{Species: "Slaking", As: "Snorlax", Ability: "truant", Moves: mv("hyperbeam")}},
				team{{Species: "Registeel", As: "Magneton", Ability: "noguard", Moves: mv("endure")}},
			)
			pokemon := p.foe()
			p.hurts(pokemon, func() { p.turn() }, "Hyper Beam should land")
			p.constant(func() any { return pokemon.HP }, func() { p.turn() }, "the recharge turn does nothing")
			p.hurts(pokemon, func() { p.turn() }, "a recharge turn should not also be a loafing turn")
		})

		g.it("should not allow the user to act the turn it wakes up, if it moved the turn it fell asleep", func(p *ps) {
			p.battle(
				team{{Species: "Slaking", As: "Snorlax", Ability: "truant", Moves: mv("scratch", "rest")}},
				team{{Species: "Steelix", Ability: "sturdy", Moves: mv("endure", "quickattack")}},
			)
			pokemon := p.foe()
			p.makeChoices("move rest", "move quickattack")
			p.constant(func() any { return pokemon.HP }, func() {
				p.makeChoices("move scratch", "move endure")
			}, "Attacked on turn 1 of sleep")
			p.constant(func() any { return pokemon.HP }, func() {
				p.makeChoices("move scratch", "move endure")
			}, "Attacked on turn 2 of sleep")
			p.constant(func() any { return pokemon.HP }, func() {
				p.makeChoices("move scratch", "move endure")
			}, "Attacked after waking up")
		})

		g.it("should allow the user to act the turn it wakes up, if it was loafing the turn it fell asleep", func(p *ps) {
			p.battle(
				team{{Species: "Slaking", As: "Snorlax", Ability: "truant", Moves: mv("scratch", "irondefense")}},
				team{{Species: "Steelix", Ability: "sturdy", Moves: mv("endure", "spore")}},
			)
			user := p.mine()
			pokemon := p.foe()
			p.makeChoices("move irondefense", "move endure")
			p.makeChoices("move irondefense", "move spore")
			for i := 0; i < 5; i++ {
				if psID(string(user.Status)) != "sleep" {
					break
				}
				p.fullHP(pokemon, "a sleeping Pokemon should not be landing Scratches")
				p.makeChoices("move scratch", "move endure")
			}
			p.damaged(pokemon, "the turn it wakes up should not also be a loafing turn")
		})

		g.it("should cause two-turn moves to fail", func(p *ps) {
			p.battle(
				team{{Species: "Slaking", As: "Snorlax", Ability: "truant", Moves: mv("razorwind")}},
				team{{Species: "Steelix", Ability: "sturdy", Moves: mv("endure")}},
			)
			pokemon := p.foe()
			p.constant(func() any { return pokemon.HP }, func() { p.turn() }, "the charging turn does no damage")
			p.constant(func() any { return pokemon.HP }, func() { p.turn() },
				"Truant should loaf away the turn the move would have fired")
		})

		g.skip("should prevent a newly-Mega Evolved Pokemon from acting if given the ability", "mega evolution")
	})
}
