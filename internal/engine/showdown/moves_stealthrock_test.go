//go:build showdown

package showdown

import (
	"fmt"
	"testing"
)

// Ported from test/sim/moves/stealthrock.js.
//
// The effectiveness ladder in the second case is the whole point, so each body
// is replaced by one that takes rocks at exactly the same multiplier: Volcarona
// (Bug/Fire, 4x) by Charizard (Fire/Flying, 4x), Staraptor (Normal/Flying, 2x)
// by Pidgeot, and Steelix (Steel/Ground, 0.25x) by its stand-in Onix
// (Rock/Ground, also 0.25x). The stand-in row for Steelix warns that Steel is
// lost, which would matter for a Steel-typed question and does not matter here.
// Chansey and Hitmonchan are in the dex already; Ninjask and Smeargle go
// through the table to Scyther and Chansey.
//
// The Eiscue case skips: Eiscue is not in this dex, and the case is about Ice
// Face's forme change surviving the chip, which is not modeled either.

func TestMovesStealthRock(t *testing.T) {
	describe(t, "Stealth Rock", func(g *psg) {
		g.it("should succeed against Substitute", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Moves: mv("stealthrock")}},
				team{{Species: "Ninjask", Moves: mv("substitute")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move stealthrock", "move substitute")
			p.ok(p.state().Sides[1].Conditions.Hazards.StealthRock,
				"a Substitute should not keep Stealth Rock off the side")
		})

		g.it("should deal damage to Pokemon switching in based on their type effectiveness against Rock-type", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Moves: mv("splash", "stealthrock")}},
				team{
					{Species: "Ninjask", Moves: mv("protect")},
					{Species: "Volcarona", As: "Charizard", Moves: mv("roost")},
					{Species: "Staraptor", As: "Pidgeot", Moves: mv("roost")},
					{Species: "Chansey", Moves: mv("wish")},
					{Species: "Hitmonchan", Moves: mv("rest")},
					{Species: "Steelix", Moves: mv("rest")},
				},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move stealthrock", "move protect")
			// Asserted here so a failure to set the rocks past the Protect is
			// reported as itself rather than as five wrong damage figures.
			p.ok(p.state().Sides[1].Conditions.Hazards.StealthRock,
				"Protect should not keep Stealth Rock off the side behind it")
			for i := 2; i <= 6; i++ {
				p.makeChoices("move splash", fmt.Sprintf("switch %d", i))
				mon := p.foe()
				denom := 1 << uint(i-1)
				p.equal(mon.MaxHP-mon.HP, mon.MaxHP/denom,
					fmt.Sprintf("%s should take 1/%d of its max HP", mon.Name, denom))
			}
		})

		g.skip("should deal 2x damage to Eiscue",
			"Eiscue is not in this 80-species dex and Ice Face's forme change is not modeled")
	})
}
