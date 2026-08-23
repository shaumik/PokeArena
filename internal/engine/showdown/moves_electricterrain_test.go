//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/electricterrain.js.
//
// Substitutions beyond the shared table, and what each preserves:
//
//   - Florges is the duration case's body on both sides and needs nothing but
//     to be there; Clefable keeps the pure Fairy typing. Upstream's Symbiosis
//     is not one of this engine's abilities and upstream picked it only so
//     nothing would interfere, so the port strips the ability rather than name
//     one that reports itself as unmodeled in a case it has no part in.
//   - Prankster is likewise absent here. Upstream uses it twice, and only the
//     Yawn case depends on what it buys — Sableye's Yawn landing before the
//     terrain goes up. Electrode outspeeds Jolteon outright and is grounded, so
//     it gets the Yawn in first and still needs the terrain's protection for
//     the second half of the case. In the semi-invulnerable case the priority
//     buys nothing and Sableye takes the shared table's Gengar with no ability.
//   - The Nature Power case reads the call off the type chart, because the
//     called move is named in a protocol line upstream and in nothing this
//     engine emits. Under Electric Terrain the call should be Thunderbolt,
//     which a Ghost-type takes; with no terrain it is Tri Attack, a Normal move
//     a Ghost-type is immune to. So Jolteon becomes Gengar for that case only —
//     the terrain comes from the move, not from the setter's typing. Reading it
//     the other way round (a Ground-type immune to Thunderbolt) would have let
//     an unimplemented Nature Power pass by doing nothing at all.
//
// Sleep Talk, upstream's do-nothing, is Splash. Sky Drop is not in this dataset
// and is the subject of the semi-invulnerable case — the move has to carry the
// target into the air for the second assertion to mean anything — so it stays
// and the missing-move failure is the finding.
//
// The base-power case is a Gen 7 battle and skips as a generation; with it goes
// the only coverage of the type boost, which is 1.3x here and was 1.5x then.
//
// Two things worth knowing before reading a red result. isGrounded in
// terrain.go says in as many words that Telekinesis does not reach it, so the
// sleep case's airborne target is still grounded here. And Rest is lifted by
// move ID in effects.go and sets sleep directly rather than through
// inflictStatus, so the terrain's sleep block never sees it.

func TestMovesElectricTerrain(t *testing.T) {
	describe(t, "Electric Terrain", func(g *psg) {
		g.it("should change the current terrain to Electric Terrain for five turns", func(p *ps) {
			p.battle(
				team{{Species: "Florges", As: "Clefable", Ability: "noability", Moves: mv("mist", "electricterrain")}},
				team{{Species: "Florges", As: "Clefable", Ability: "noability", Moves: mv("mist")}},
			)
			if p.state() == nil {
				return
			}
			for turn := 1; turn <= 4; turn++ {
				p.makeChoices("move electricterrain", "move mist")
				p.equal(p.terrain(), "electric", "the terrain should still be up")
			}
			p.makeChoices("move electricterrain", "move mist")
			p.equal(p.terrain(), "", "the terrain should have run out after five turns")
		})

		g.skip("should increase the base power of Electric-type attacks used by grounded Pokemon",
			"gen 7 mechanics")

		g.it("should prevent moves from putting grounded Pokemon to sleep", func(p *ps) {
			p.battle(
				team{{Species: "Jolteon", Ability: "voltabsorb", Moves: mv("electricterrain", "spore")}},
				team{{Species: "Abra", Ability: "magicguard", Moves: mv("telekinesis", "spore")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move electricterrain", "move telekinesis")
			p.makeChoices("move spore", "move spore")
			p.hasStatus(p.mine(), "slp", "Telekinesis lifts the terrain setter off the ground, so its own terrain no longer covers it")
			p.noStatus(p.foe(), "the grounded Pokemon should be protected from Spore by the terrain")
		})

		g.it("should not remove active non-volatile statuses from grounded Pokemon", func(p *ps) {
			// Upstream never sets the terrain in this case — Electric Terrain is
			// in the move list and never chosen — so what it actually pins is
			// that a Spore landed before any terrain existed. Ported as written.
			// Whimsicott is only the sleeper here; Vileplume keeps the Grass
			// typing and Prankster is not modeled, so the ability is stripped.
			p.battle(
				team{{Species: "Jolteon", Ability: "voltabsorb", Moves: mv("splash", "electricterrain")}},
				team{{Species: "Whimsicott", As: "Vileplume", Ability: "noability", Moves: mv("spore")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move spore")
			p.hasStatus(p.mine(), "slp", "the sleep should have landed and stayed")
		})

		g.it("should prevent Yawn from putting grounded Pokemon to sleep, and cause Yawn to fail", func(p *ps) {
			p.battle(
				team{{Species: "Jolteon", Ability: "voltabsorb", Moves: mv("electricterrain", "yawn")}},
				team{{Species: "Sableye", As: "Electrode", Ability: "noability", Moves: mv("yawn")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move electricterrain", "move yawn")
			p.makeChoices("move yawn", "move yawn")
			p.noStatus(p.mine(), "the terrain should keep the drowsy setter awake")
			p.ok(p.foe().Volatiles.Yawn == nil,
				"a Yawn cast at a grounded target under Electric Terrain should fail outright")
		})

		g.it("should cause Rest to fail on grounded Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Jolteon", Ability: "shellarmor", Moves: mv("electricterrain", "rest")}},
				team{{Species: "Pidgeot", Ability: "keeneye", Moves: mv("doubleedge", "rest")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move electricterrain", "move doubleedge")
			p.damaged(p.mine(), "Double-Edge should have taken a chunk off before Rest is tried")
			p.makeChoices("move rest", "move rest")
			p.damaged(p.mine(), "Rest should have failed on the grounded Pokemon")
			p.fullHP(p.foe(), "Rest should still work for a Flying-type, which the terrain does not reach")
		})

		g.it("should not affect Pokemon in a semi-invulnerable state", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("yawn", "skydrop")}},
				team{{Species: "Sableye", Ability: "noability", Moves: mv("yawn", "electricterrain")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move yawn", "move yawn")
			p.makeChoices("move skydrop", "move electricterrain")
			p.hasStatus(p.mine(), "slp", "the Sky Drop user is airborne, so the terrain does not reach it")
			p.hasStatus(p.foe(), "slp", "Sky Drop carries the target up with it, so the terrain misses it too")
		})

		g.it("should cause Nature Power to become Thunderbolt", func(p *ps) {
			p.battle(
				team{{Species: "Jolteon", As: "Gengar", Ability: "noability", Moves: mv("electricterrain")}},
				team{{Species: "Shuckle", Ability: "sturdy", Moves: mv("naturepower")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move electricterrain", "move naturepower")
			p.equal(p.terrain(), "electric", "the terrain should be up before Nature Power resolves")
			p.damaged(p.mine(),
				"Nature Power should have become Thunderbolt, which a Ghost-type takes; "+
					"the no-terrain default, Tri Attack, would not have touched it")
		})

		g.skip("should block Sleep before the move would have missed",
			"pending upstream (it.skip); it also needs an accuracy hook this harness does not have")
	})
}
