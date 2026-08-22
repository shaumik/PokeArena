//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/magicguard.js.
//
// Magic Guard is in this dataset and Clefable carries it, so the file's shape
// survives. Magikarp resolves to Seaking, Crobat to Golbat and Ferrothorn to
// Magneton — the last drops grass and Iron Barbs, but the case only needs a
// Leech Seed user that Moonblast can damage. Haxorus has no row; Dragonite is a
// Dragon body carrying Mold Breaker, which is all the third case wants of it.
//
// Mind Blown is not in this dataset, and in the first case it is one of the six
// enumerated sources of non-attack damage rather than filler, so it is kept and
// the missing-move failure is the finding — which does mean the Spikes, Toxic,
// Life Orb, recoil and crash halves of that case go unmeasured with it. Lucky
// Chant in the same fixture *is* filler (a do-nothing turn for the opponent), so
// Splash stands in for it.

func TestAbilitiesMagicGuard(t *testing.T) {
	describe(t, "Magic Guard", func(g *psg) {
		g.it("should prevent all non-attack damage", func(p *ps) {
			p.battle(
				team{
					{Species: "Magikarp", Ability: "swiftswim", Moves: mv("splash")},
					{Species: "Clefable", Ability: "magicguard", Item: "lifeorb",
						Moves: mv("doubleedge", "mindblown", "highjumpkick")},
				},
				team{{Species: "Crobat", Ability: "roughskin", Moves: mv("splash", "spikes", "toxic", "protect")}},
			)
			p.makeChoices("move splash", "move spikes")
			p.makeChoices("switch 2", "move toxic")
			p.makeChoices("move mindblown", "move splash")
			p.makeChoices("move doubleedge", "move spikes")
			p.makeChoices("move highjumpkick", "move protect")
			p.fullHP(p.mine(), "Spikes, Toxic, Life Orb, recoil, the crash and Mind Blown's cost should all be waived")
		})

		g.it("should prevent Leech Seed's healing effect", func(p *ps) {
			p.battle(
				team{{Species: "Clefable", Ability: "magicguard", Moves: mv("moonblast")}},
				team{{Species: "Ferrothorn", Ability: "noguard", Moves: mv("leechseed")}},
			)
			p.makeChoices("move moonblast", "move leechseed")
			p.fullHP(p.mine(), "Leech Seed should not drain a Magic Guard holder")
			p.damaged(p.foe(), "the seeder should be down by Moonblast and healed by nothing")
		})

		g.it("should not be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{
					{Species: "Magikarp", Ability: "swiftswim", Moves: mv("splash")},
					{Species: "Clefable", Ability: "magicguard", Moves: mv("doubleedge")},
				},
				team{{Species: "Haxorus", As: "Dragonite", Ability: "moldbreaker", Moves: mv("stealthrock", "roar")}},
			)
			p.makeChoices("move splash", "move stealthrock")
			p.makeChoices("move splash", "move roar")
			p.fullHP(p.mine(), "Mold Breaker should not let Stealth Rock through Magic Guard")
		})
	})
}
