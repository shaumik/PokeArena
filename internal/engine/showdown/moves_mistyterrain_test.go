//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/mistyterrain.js.
//
// Florges is the terrain setter in most of these and is only a body; Clefable
// keeps the pure Fairy typing. Upstream gives it Symbiosis, which is not an
// ability this engine models and which upstream picked only so Flower Veil
// would not interfere — so the port strips the ability instead of naming one
// that would report itself as unmodeled in every case in the file. Sableye
// resolves to Gengar through the shared table, and its Prankster likewise goes
// away: Gengar already outspeeds Clefable, which is all the priority was
// buying. Crobat (Golbat), Machamp, Pidgeot and Shuckle (Snorlax) need no
// comment beyond the table.
//
// Three cases are restated because what upstream reads is not readable here:
//
//   - The Dragon-halving case calls battle.runEvent('BasePower') directly.
//     There is no such hook, so the port measures Dragon Pulse damage against
//     the identical fixture with no terrain up, and checks both halves of the
//     upstream claim at once — halved into a grounded defender, untouched into
//     a Flying one. Shell Armor is added to both defenders so a crit cannot
//     move the figure.
//   - The Yawn case reads the protocol line that says Yawn started. Here that
//     is the count of "grew drowsy" lines: two, one per Yawn, the second of
//     them cast under the terrain.
//   - Nature Power's call is named in a protocol line and nowhere in this
//     engine's strings, so the substitution is read off the type chart
//     instead. Under Misty Terrain the call should be Moonblast, a Fairy move
//     a Ghost-type takes; with no terrain it is Tri Attack, a Normal move a
//     Ghost-type is immune to. Gengar therefore replaces Whimsicott as the
//     setter and target — the terrain comes from the move, not from typing,
//     so nothing else about the case depends on the swap.
//
// Sky Drop is not in this dataset. It is the subject of the semi-invulnerable
// case — the move has to carry the target up with it for the second assertion
// to mean anything, which no in-dex two-turn move does — so it stays and the
// missing-move failure is the finding.

func TestMovesMistyTerrain(t *testing.T) {
	describe(t, "Misty Terrain", func(g *psg) {
		g.it("should change the current terrain to Misty Terrain for five turns", func(p *ps) {
			p.battle(
				team{{Species: "Florges", As: "Clefable", Ability: "noability", Moves: mv("mist", "mistyterrain")}},
				team{{Species: "Florges", As: "Clefable", Ability: "noability", Moves: mv("mist")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mistyterrain", "move mist")
			p.equal(p.terrain(), "misty", "the terrain should be up on the turn it was set")
			for turn := 2; turn <= 4; turn++ {
				p.makeChoices("move mist", "move mist")
				p.equal(p.terrain(), "misty", "the terrain should still be up")
			}
			p.makeChoices("move mist", "move mist")
			p.equal(p.terrain(), "", "the terrain should have run out after five turns")
		})

		g.it("should halve the base power of Dragon-type attacks on grounded Pokemon", func(p *ps) {
			// Shaymin is the grounded defender and Shaymin-Sky the Flying one;
			// Venusaur and Charizard keep only that distinction, which is the
			// one the case turns on. Both throw Dragon Pulse at each other on
			// the same turn, so one battle measures both directions.
			p.battle(
				team{{Species: "Shaymin", As: "Venusaur", Ability: "shellarmor",
					Moves: mv("mistyterrain", "dragonpulse", "splash")}},
				team{{Species: "Shaymin-Sky", As: "Charizard", Ability: "shellarmor",
					Moves: mv("dragonpulse", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mistyterrain", "move splash")
			p.equal(p.terrain(), "misty", "the terrain the case needs should be up")
			grounded, flying := p.mine(), p.foe()
			groundedBefore, flyingBefore := grounded.HP, flying.HP
			p.makeChoices("move dragonpulse", "move dragonpulse")
			groundedUnder := groundedBefore - grounded.HP
			flyingUnder := flyingBefore - flying.HP

			p.battle(
				team{{Species: "Shaymin", As: "Venusaur", Ability: "shellarmor",
					Moves: mv("mistyterrain", "dragonpulse", "splash")}},
				team{{Species: "Shaymin-Sky", As: "Charizard", Ability: "shellarmor",
					Moves: mv("dragonpulse", "splash")}},
			)
			p.makeChoices("move splash", "move splash")
			bareGrounded, bareFlying := p.mine(), p.foe()
			bareGroundedBefore, bareFlyingBefore := bareGrounded.HP, bareFlying.HP
			p.makeChoices("move dragonpulse", "move dragonpulse")
			groundedBare := bareGroundedBefore - bareGrounded.HP
			flyingBare := bareFlyingBefore - bareFlying.HP

			p.atLeast(groundedBare, 1, "the baseline hit should have connected at all")
			p.bounded(groundedUnder*100, groundedBare*40, groundedBare*62,
				"a Dragon move into a grounded target should be halved")
			p.bounded(flyingUnder*100, flyingBare*80, flyingBare*125,
				"a Dragon move into a Flying target should be untouched")
		})

		g.it("should prevent moves from setting non-volatile status on grounded Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Florges", As: "Clefable", Ability: "noability", Moves: mv("mistyterrain", "toxic")}},
				team{{Species: "Machamp", Ability: "noguard", Item: "airballoon", Moves: mv("bulkup", "toxic")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mistyterrain", "move bulkup")
			p.makeChoices("move toxic", "move toxic")
			p.noStatus(p.mine(), "the grounded Pokemon should be protected by its own terrain")
			p.hasStatus(p.foe(), "tox", "an Air Balloon holder is not grounded, so the terrain does not reach it")
		})

		g.it("should not remove active non-volatile statuses from grounded Pokemon", func(p *ps) {
			// Toxic is 90% accurate and there is no rigged RNG here, so the
			// status the case starts from is set on the fixture rather than
			// played out. Crobat keeps its Toxic anyway, as the body upstream
			// wrote; landing it again on an already-poisoned target is a no-op.
			p.battle(
				team{{Species: "Florges", As: "Clefable", Ability: "noability", Status: "tox",
					Moves: mv("mistyterrain")}},
				team{{Species: "Crobat", Ability: "infiltrator", Moves: mv("toxic")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mistyterrain", "move toxic")
			p.hasStatus(p.mine(), "tox", "setting the terrain should not cure a status already in place")
		})

		g.it("should prevent Yawn from putting grounded Pokemon to sleep, but not cause Yawn to fail", func(p *ps) {
			p.battle(
				team{{Species: "Florges", As: "Clefable", Ability: "noability", Moves: mv("mistyterrain", "yawn")}},
				team{{Species: "Sableye", Ability: "noability", Moves: mv("yawn")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mistyterrain", "move yawn")
			p.makeChoices("move yawn", "move yawn")
			p.noStatus(p.mine(), "the terrain should keep the drowsy Pokemon awake")
			p.equal(p.logCount("grew drowsy"), 2,
				"Yawn cast under the terrain should still take hold, it just cannot land the sleep")
		})

		g.it("should cause Rest to fail on grounded Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Florges", As: "Clefable", Ability: "noability", Moves: mv("mistyterrain", "rest")}},
				team{{Species: "Pidgeot", Ability: "keeneye", Moves: mv("doubleedge", "rest")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mistyterrain", "move doubleedge")
			p.makeChoices("move rest", "move rest")
			p.damaged(p.mine(), "Rest should have failed on the grounded Pokemon")
			p.fullHP(p.foe(), "Rest should still work for a Flying-type, which the terrain does not reach")
		})

		g.it("should not affect Pokemon in a semi-invulnerable state", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("yawn", "skydrop")}},
				team{{Species: "Sableye", Ability: "noability", Moves: mv("yawn", "mistyterrain")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move yawn", "move yawn")
			p.makeChoices("move skydrop", "move mistyterrain")
			p.hasStatus(p.mine(), "slp", "the Sky Drop user is airborne, so the terrain does not reach it")
			p.hasStatus(p.foe(), "slp", "Sky Drop carries the target up with it, so the terrain misses it too")
		})

		g.it("should cause Nature Power to become Moonblast", func(p *ps) {
			p.battle(
				team{{Species: "Whimsicott", As: "Gengar", Ability: "noability", Moves: mv("mistyterrain")}},
				team{{Species: "Shuckle", Ability: "sturdy", Moves: mv("naturepower")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mistyterrain", "move naturepower")
			p.equal(p.terrain(), "misty", "the terrain should be up before Nature Power resolves")
			p.damaged(p.mine(),
				"Nature Power should have become Moonblast, which a Ghost-type takes; "+
					"the no-terrain default, Tri Attack, would not have touched it")
		})
	})
}
