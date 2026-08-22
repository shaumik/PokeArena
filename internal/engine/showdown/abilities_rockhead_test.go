//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/rockhead.js.
//
// Aerodactyl carries Rock Head in this dex, so it ports as itself. Rampardos
// does not exist here and has no stand-in; it is only a second Rock Head body
// upstream, so those two sets build as Aerodactyl too. Registeel has no row
// either — Magneton is the dex's Steel body and nothing in these cases turns
// on Registeel's Rock-free typing beyond surviving one Double-Edge, which it
// does. Sableye goes through its stand-in row (Gengar), which is what the
// crash-damage case needs: Jump Kick crashes because the target is
// Fighting-immune, and Gengar's Ghost half preserves that.
//
// Prankster and Mummy are not in this ability set and Mind Blown and
// Chloroblast are not in this move set. Those cases keep them: naming the gap
// is the finding.
//
// Upstream's Struggle case submits move slot 1 and relies on Showdown turning
// a Taunt-blocked slot into Struggle. This harness has no such rewrite, so the
// port submits the default action, which is whatever the engine considers
// legal — Struggle, if Taunt has done its job.

func TestAbilitiesRockHead(t *testing.T) {
	describe(t, "Rock Head", func(g *psg) {
		g.it("should block recoil from most moves", func(p *ps) {
			p.battle(
				team{{Species: "Aerodactyl", Ability: "rockhead", Moves: mv("doubleedge")}},
				team{{Species: "Registeel", As: "Magneton", Ability: "clearbody", Moves: mv("rest")}},
			)
			p.constant(func() any { return p.mine().HP },
				func() { p.makeChoices("move doubleedge", "move rest") },
				"Rock Head should have eaten the Double-Edge recoil")
		})

		g.it("should not block recoil if the ability is disabled/removed mid-attack", func(p *ps) {
			p.battle(
				team{{Species: "Aerodactyl", Ability: "rockhead", Moves: mv("doubleedge")}},
				team{{Species: "Registeel", As: "Magneton", Ability: "mummy", Moves: mv("rest")}},
			)
			p.hurts(p.mine(), func() { p.makeChoices("move doubleedge", "move rest") },
				"Mummy replaces Rock Head on contact, before the recoil is charged")
		})

		g.it("should not block recoil from Struggle", func(p *ps) {
			p.battle(
				team{{Species: "Aerodactyl", Ability: "rockhead", Moves: mv("roost")}},
				team{{Species: "Sableye", Ability: "prankster", Moves: mv("taunt")}},
			)
			p.makeChoices("move roost", "move taunt")
			p.hurts(p.mine(), func() { p.makeChoices("", "move taunt") },
				"Struggle's recoil is not recoil Rock Head can block")
		})

		g.it("should not block crash damage", func(p *ps) {
			p.battle(
				team{{Species: "Rampardos", As: "Aerodactyl", Ability: "rockhead", Moves: mv("jumpkick")}},
				team{{Species: "Sableye", Ability: "prankster", Moves: mv("taunt")}},
			)
			p.hurts(p.mine(), func() { p.makeChoices("move jumpkick", "move taunt") },
				"a Jump Kick that cannot connect crashes, and Rock Head does not cover that")
		})

		g.it(`should not block indirect damage`, func(p *ps) {
			p.battle(
				team{{Species: "Rampardos", As: "Aerodactyl", Ability: "rockhead", Moves: mv("splash")}},
				team{{Species: "Crobat", Moves: mv("toxic")}},
			)
			p.turn()
			p.damaged(p.mine(), "poison damage is not recoil")
		})

		g.it("should not block recoil from Mind Blown", func(p *ps) {
			p.battle(
				team{{Species: "Aerodactyl", Ability: "rockhead", Moves: mv("mindblown")}},
				team{{Species: "Registeel", As: "Magneton", Ability: "clearbody", Moves: mv("rest")}},
			)
			p.hurts(p.mine(), func() { p.makeChoices("move mindblown", "move rest") },
				"Mind Blown's self-damage is not recoil")
		})

		g.it("should block recoil from Chloroblast", func(p *ps) {
			p.battle(
				team{{Species: "Aerodactyl", Ability: "rockhead", Moves: mv("chloroblast")}},
				team{{Species: "Registeel", As: "Magneton", Ability: "clearbody", Moves: mv("rest")}},
			)
			p.constant(func() any { return p.mine().HP },
				func() { p.makeChoices("move chloroblast", "move rest") },
				"Chloroblast's self-damage is recoil, so Rock Head covers it")
		})
	})
}
