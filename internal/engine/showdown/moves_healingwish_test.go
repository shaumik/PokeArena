//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/healingwish.js.
//
// Three of the six cases need a second active slot — two are Ally Switch
// interactions and the third is the doubles half of a case whose singles half
// is ported — and the Gen 4 case skips as a generation.
//
// The fixtures lose two things and neither is the subject of a case. Shedinja
// is upstream's way of putting Caterpie on 1 HP with Endeavor; it has no
// stand-in (its 1 HP under Wonder Guard is its identity) and Endeavor is not in
// this dataset, so the port starts Caterpie damaged through the HP field and
// leads with Tyranitar, which upstream switches in on the following turn
// anyway. Caterpie resolves to Butterfree and Jirachi to Mr. Mime through the
// shared table; neither case turns on typing beyond "Stealth Rock hurts the
// switch-in", which both satisfy.
//
// Upstream's exact HP figures are level-100 numbers and do not transfer, so the
// two hazard cases assert the shape instead: this engine consumes Healing Wish
// in doSwitch *after* the entry-hazard pass (slotconditions.go), where Showdown
// heals before it. That ordering is what both cases measure — "damaged on the
// way in" in the first, "the wish survives an already-full switch-in" in the
// second — and it is why they are worth keeping even without the numbers.

func TestMovesHealingWish(t *testing.T) {
	describe(t, "Healing Wish", func(g *psg) {
		g.it("should heal a switch-in for full before hazards at end of turn", func(p *ps) {
			p.battle(
				team{
					{Species: "Caterpie", Ability: "shielddust", HP: 100, Moves: mv("stringshot")},
					{Species: "Jirachi", Ability: "serenegrace", Moves: mv("healingwish", "protect")},
				},
				team{{Species: "Tyranitar", Ability: "sandstream", Moves: mv("seismictoss", "stealthrock")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move stringshot", "move stealthrock")
			p.makeChoices("switch jirachi", "move seismictoss")
			p.makeChoices("move healingwish", "move seismictoss")
			p.equal(string(p.state().Phase), "replace", "the Healing Wish user should have fainted and owe a replacement")
			p.makeChoices("switch Caterpie", "")
			in := p.mine()
			p.notFainted(in, "the switch-in should have been restored before the rocks reached it")
			p.damaged(in, "the heal should land first and Stealth Rock chip the restored total afterwards")
			p.equal(in.Moves[0].PP, in.Moves[0].MaxPP-1, "Healing Wish should restore HP and status, not PP")
		})

		g.it("should not be consumed if a switch-in is fully healed already", func(p *ps) {
			p.battle(
				team{
					{Species: "Jirachi", Ability: "serenegrace", Moves: mv("healingwish", "protect")},
					{Species: "Caterpie", Ability: "shielddust", Moves: mv("stringshot")},
				},
				team{{Species: "Tyranitar", Ability: "sandstream", Moves: mv("seismictoss", "stealthrock")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move protect", "move stealthrock")
			p.makeChoices("move healingwish", "move seismictoss")
			p.equal(string(p.state().Phase), "replace", "the Healing Wish user should have fainted and owe a replacement")
			p.makeChoices("switch Caterpie", "")
			p.ok(p.state().Sides[0].SlotConditions.HealingWish,
				"a switch-in that arrives at full HP should leave the wish pending for the next one")
		})

		g.skip("should heal an ally fully after Ally Switch", "doubles")

		g.it("should fail to switch the user out if no Pokemon can be switched in", func(p *ps) {
			// Only upstream's first battle, the singles one. The doubles battle
			// that follows it asks the same question with an ally present.
			p.battle(
				team{{Species: "wynaut", Moves: mv("healingwish")}},
				team{{Species: "pichu", Moves: mv("swordsdance")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.logHas("But it failed!", "Healing Wish with an empty bench should fail")
			p.notFainted(p.mine(), "a failed Healing Wish must not sacrifice its user")
		})

		g.skip("should not set up the slot condition when it fails", "doubles")
		g.skip("[Gen 4] should heal a switch-in for full after hazards mid-turn", "gen 4 mechanics")
	})
}
