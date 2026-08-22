//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/sitrusberry.js.
//
// Sitrus Berry is modeled, so the three live cases are real behavior tests. The
// two `it.skip` cases and the Gen 3 block are recorded so the case count matches
// the original.
//
// Levels and lethality. The first two cases want a hit that would knock the
// holder out so Sturdy leaves it on 1 HP and the berry heals from there; both
// arrange that at level 50 with an in-dex pair. The first swaps upstream's Aura
// Sphere for Close Combat, because Machamp's physical Attack against Magneton's
// Defense is what makes the hit lethal at this level; Adaptability, which is
// upstream's way of getting the same lethality and is not one of this engine's
// 118 abilities, is dropped rather than left in to report an unrelated gap.
//
// The third case cannot be arranged that way at all: no Knock Off in this dex
// knocks out a Sturdy body from full HP at level 50, so the case is restated
// without Sturdy. Two Knock Offs take the holder well under half with the berry
// already gone, and the claim — a knocked-off berry does not heal — is measured
// as the absence of the heal rather than as a specific HP figure.
//
// Species. Aggron, Magnemite, Deoxys-Attack and Garchomp go through their
// stand-in rows. Lucario and Krookodile have none: Machamp is the Fighting body
// the first case needs, and Tauros is an Intimidate body carrying Knock Off,
// this dex having no Dark type at all. Rough Skin, which upstream leaves on
// Garchomp, is not modeled and does not bear on Earthquake, so the port lets
// Dragonite keep its own ability.
//
// Sleep Talk is not in this dataset and is inert filler on the holder, so Splash
// stands in for it.

func TestItemsSitrusBerry(t *testing.T) {
	describe(t, "Sitrus Berry", func(g *psg) {
		g.it("should heal 25% hp when consumed", func(p *ps) {
			p.battle(
				team{{Species: "Aggron", Ability: "sturdy", Item: "sitrusberry", Moves: mv("splash")}},
				team{{Species: "Lucario", As: "Machamp", Ability: "noability", Moves: mv("closecombat")}},
			)
			p.makeChoices("move splash", "move closecombat")
			holder := p.mine()
			p.noItem(holder, "the berry should have been eaten")
			p.equal(holder.HP, holder.MaxHP/4+1, "Sturdy leaves it on 1, and the berry adds a quarter of its maximum")
		})

		g.it("should be eaten immediately if (re)gained on low hp", func(p *ps) {
			p.battle(
				team{{Species: "Magnemite", Ability: "sturdy", Item: "sitrusberry", Moves: mv("recycle")}},
				team{{Species: "Garchomp", Moves: mv("earthquake")}},
			)
			p.makeChoices("move recycle", "move earthquake")
			holder := p.mine()
			p.noItem(holder, "the recycled berry should have been eaten again straight away")
			p.equal(holder.HP, 2*(holder.MaxHP/4)+1, "one quarter from each of the two berries, on top of Sturdy's 1 HP")
		})

		g.it("should not heal if Knocked Off", func(p *ps) {
			p.battle(
				team{{Species: "Deoxys-Attack", Ability: "sturdy", Item: "sitrusberry", Moves: mv("splash")}},
				team{{Species: "Krookodile", As: "Tauros", Ability: "intimidate", Moves: mv("knockoff")}},
			)
			p.makeChoices("move splash", "move knockoff")
			p.makeChoices("move splash", "move knockoff")
			holder := p.mine()
			p.noItem(holder, "Knock Off should have taken the berry")
			p.logLacks("restored a little HP", "a berry that was knocked off should never heal")
			p.atMost(holder.HP, holder.MaxHP/2, "the holder should be left under the trigger line, unhealed")
		})

		g.skip("should not heal 25% HP if a confusion self-hit would bring the user into Berry trigger range",
			"pending upstream (it.skip)")
		g.skip("should heal 25% HP immediately after any end-of-turn effect",
			"pending upstream (it.skip); Zen Mode is a forme change and is not modeled")
	})

	describe(t, "[Gen 3]", func(g *psg) {
		g.skip("should not activate in the same turn that was put below 50% HP by a status condition",
			"gen 3 mechanics")
	})
}
