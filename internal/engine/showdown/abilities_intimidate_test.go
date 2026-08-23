//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/intimidate.js.
//
// Two cases are doubles or triples and skip; the rest are singles and come
// across.
//
// The first case is a gen-7 battle, and the gen-7 detail it leans on is that
// Own Tempo did not block Intimidate before Gen 8. Upstream reaches for Own
// Tempo as "an ability that does not interfere"; under this engine's gen-9
// data that is exactly what it stops being, so the port writes the same intent
// with `noability`. The subject — Intimidate drops the foe's Attack one stage
// on switch-in — is unchanged.
//
// The two "wait until all simultaneous switch ins have completed" cases hang
// their real assertion on a `battle.onEvent('TryBoost', ...)` counter that
// checks Intimidate resolves Arcanine-first. There is no event hook here, so
// only the outcome those two cases share is ported: after both sides come in
// together, each active is at -1 Attack. The ordering half is not expressible
// and is not asserted.
//
// Two cases assert on an entry ability with no turn played upstream, which is
// the divergence `p.leadsEnter` exists for: this engine fires the leads'
// switch-in hooks at the top of turn 1 rather than at construction. Both idle
// with Splash so that turn contributes nothing else. In the team-preview case
// that is also the closer translation — upstream's `makeChoices('default',
// 'default')` closes preview and brings both leads in without executing a
// move, so Morning Sun and Dragon Dance never run there either, and running
// them here would move the very stat being read.
//
// Moves. Sketch is not in this dataset and is only filler on the body being
// intimidated, so the first case uses Splash instead.
//
// Species. Escavalier has no stand-in; Tentacruel takes its place because the
// case needs only a body that outspeeds the pivot and resists U-turn, so its
// Substitute is up and unbroken when Intimidate would fire. Greninja's
// stand-in Poliwrath is slower than Tentacruel, which makes the turn order the
// case depends on hold whether or not Lagging Tail is modeled; its row says
// Protean is not preserved, so the pivot is built bare rather than claiming an
// ability the substitution cannot carry.

func TestAbilitiesIntimidate(t *testing.T) {
	describe(t, "Intimidate", func(g *psg) {
		g.it("should decrease Atk by 1 level", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "noability", Moves: mv("splash")}},
				team{{Species: "Gyarados", Ability: "intimidate", Moves: mv("splash")}},
			)
			p.leadsEnter()
			p.statStage(p.mine(), "atk", -1, "Intimidate should cut the foe's Attack on switch-in")
			p.logHas("Intimidate cuts", "Intimidate should have announced itself")
		})

		g.it("should be blocked by Substitute", func(p *ps) {
			// Upstream needs a second choice here for U-turn's replacement.
			// This engine resolves an untargeted self-switch inside the same
			// turn, bringing in the first live benched Pokemon — Gyarados —
			// so the two upstream calls collapse into one.
			p.battle(
				team{{
					Species: "Escavalier", As: "Tentacruel", Item: "leftovers", Ability: "shellarmor",
					Moves: mv("substitute"),
				}},
				team{
					{Species: "Greninja", Item: "laggingtail", Ability: "noability", Moves: mv("uturn")},
					{Species: "Gyarados", Item: "leftovers", Ability: "intimidate", Moves: mv("splash")},
				},
			)
			p.makeChoices("move substitute", "move uturn")
			p.species(p.foe(), "Gyarados", "U-turn should have brought Gyarados in")
			p.ok(p.mine().Volatiles.Substitute != nil, "the Substitute should have survived U-turn")
			p.statStage(p.mine(), "atk", 0, "a Substitute should block Intimidate")
		})

		g.skip("should not activate if U-turn breaks the Substitute in Gen 4", "gen 4 mechanics")
		g.skip("should affect adjacent foes only", "triples")

		g.it("should wait until all simultaneous switch ins at the beginning of a battle have completed before activating", func(p *ps) {
			p.battle(
				team{{Species: "Arcanine", Ability: "intimidate", Moves: mv("splash")}},
				team{{Species: "Gyarados", Ability: "intimidate", Moves: mv("splash")}},
			)
			p.leadsEnter()
			p.statStage(p.mine(), "atk", -1, "Arcanine should have been intimidated by Gyarados")
			p.statStage(p.foe(), "atk", -1, "Gyarados should have been intimidated by Arcanine")

			// Again with the two in the other order, as upstream does.
			p.battle(
				team{{Species: "Gyarados", Ability: "intimidate", Moves: mv("splash")}},
				team{{Species: "Arcanine", Ability: "intimidate", Moves: mv("splash")}},
			)
			p.leadsEnter()
			p.statStage(p.mine(), "atk", -1, "Gyarados should have been intimidated by Arcanine")
			p.statStage(p.foe(), "atk", -1, "Arcanine should have been intimidated by Gyarados")
		})

		g.it("should wait until all simultaneous switch ins after double-KOs have completed before activating", func(p *ps) {
			p.battle(
				team{
					{Species: "Blissey", Ability: "naturalcure", Moves: mv("healingwish")},
					{Species: "Arcanine", Ability: "intimidate", Moves: mv("healingwish")},
					{Species: "Gyarados", Ability: "intimidate", Moves: mv("healingwish")},
				},
				team{
					{Species: "Blissey", Ability: "naturalcure", Moves: mv("healingwish")},
					{Species: "Gyarados", Ability: "intimidate", Moves: mv("healingwish")},
					{Species: "Arcanine", Ability: "intimidate", Moves: mv("healingwish")},
				},
			)
			p.makeChoices("move healingwish", "move healingwish")
			p.makeChoices("switch arcanine", "switch gyarados")
			p.statStage(p.mine(), "atk", -1, "Arcanine should have been intimidated by the Gyarados that came in with it")
			p.statStage(p.foe(), "atk", -1, "Gyarados should have been intimidated by the Arcanine that came in with it")

			p.makeChoices("move healingwish", "move healingwish")
			p.makeChoices("switch gyarados", "switch arcanine")
			p.statStage(p.mine(), "atk", -1, "the second pair should intimidate each other the same way")
			p.statStage(p.foe(), "atk", -1, "the second pair should intimidate each other the same way")
		})
	})
}
