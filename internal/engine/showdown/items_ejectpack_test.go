//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/ejectpack.js.
//
// Eject Pack is not one of this dataset's 128 items, so every case that holds
// one fails at team construction naming the item — that is the finding. The
// fixtures are written out in full anyway so they still measure the right thing
// if the item is ever added.
//
// Upstream reads `battle.p1.requestState === 'switch'`. This engine has no
// mid-turn forced-switch request: PhaseReplace only ever means "a fainted
// active owes a replacement". The closest honest reading is the replace phase
// narrowed to the one side, which is what the switchRequested closure does, and
// it is the same reading the Emergency Exit port uses.
//
// Moves. Sleep Talk is not in this dataset and is pure filler upstream, so
// Splash stands in for it. Parting Shot is the subject of its own case, so it is
// kept and its absence is part of that case's finding.
//
// Species. Glalie, Klefki and Grimmsnarl have no stand-in row and are bodies
// rather than mechanics here, so the port names an in-dex species for each:
// Lapras keeps Glalie's Ice typing, Magneton keeps Klefki's Steel half, and
// Wigglytuff keeps Grimmsnarl's Fairy half. Magikarp, Machop, Charmeleon and
// Wynaut go through their stand-in rows.
//
// Not ported: five doubles cases, one of which upstream skips itself, and the
// Power Construct case, whose assertion is that Zygarde-Complete is on the field.

func TestItemsEjectPack(t *testing.T) {
	describe(t, "Eject Pack", func(g *psg) {
		// switchRequested is this harness's nearest reading of upstream's
		// `battle.pN.requestState === 'switch'`, narrowed to one side so a
		// fainted foe cannot be mistaken for the holder being ejected.
		switchRequested := func(p *ps, side int) bool {
			st := p.state()
			return st != nil && st.Phase == "replace" && st.Replace[side]
		}

		g.it("should switch out the holder when its stats are lowered", func(p *ps) {
			p.battle(
				team{
					{Species: "Magikarp", Item: "ejectpack", Moves: mv("splash")},
					{Species: "Mew", Moves: mv("splash")},
				},
				team{{Species: "Machop", Moves: mv("leer")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.ok(switchRequested(p, 0), "Leer's Defense drop should have ejected the holder")
		})

		g.it("should switch out the holder after Moody's stat drop", func(p *ps) {
			p.battle(
				team{
					{Species: "Glalie", As: "Lapras", Ability: "moody", Item: "ejectpack", Moves: mv("protect")},
					{Species: "Mew", Moves: mv("protect")},
				},
				team{{Species: "Mew", Moves: mv("protect")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.ok(switchRequested(p, 0), "Moody's end-of-turn drop should have ejected the holder")
		})

		g.it("should not switch the holder out if the move was Parting Shot and the opponent could switch", func(p *ps) {
			p.battle(
				team{
					{Species: "Wynaut", Item: "ejectpack", Moves: mv("splash")},
					{Species: "Mew", Moves: mv("splash")},
				},
				team{
					{Species: "Mew", Moves: mv("partingshot")},
					{Species: "Muk", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.isFalse(switchRequested(p, 0), "the pivot's own switch should take precedence over the pack")
			// Upstream checks p2 is the side holding a switch request. This
			// engine resolves an untargeted self-switch inside the same turn and
			// picks the replacement itself, so the equivalent observation is that
			// the pivot has already left.
			p.species(p.foe(), "Muk", "Parting Shot should have switched its user out")
		})

		g.it("should switch out the holder if its stats are lowered during the semi-invulnerable state", func(p *ps) {
			p.battle(
				team{
					{Species: "Charmeleon", Item: "ejectpack", Moves: mv("phantomforce")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Wynaut", Ability: "noguard", Moves: mv("growl")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.ok(switchRequested(p, 0), "a drop landed through No Guard should still eject the holder")
		})

		g.it("should switch out the holder if its stats are lowered after using Swallow", func(p *ps) {
			p.battle(
				team{
					{Species: "Charmeleon", Item: "ejectpack", Moves: mv("stockpile", "swallow")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Wynaut", Moves: mv("tackle")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.makeChoices("move swallow", "")
			p.ok(switchRequested(p, 0), "losing the Stockpile boosts to Swallow should eject the holder")
		})

		g.it("should not switch out the user if the user acquired the Eject Pack after the stat drop occurred", func(p *ps) {
			// Upstream's fixture has no Eject Pack in it at all; it is ported as
			// written, so what it actually checks is that nobody is asked to
			// switch after Magician and Pickpocket trade items around a stat drop.
			p.battle(
				team{
					{Species: "Klefki", As: "Magneton", Ability: "magician", Moves: mv("lowsweep")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{
					{Species: "Grimmsnarl", As: "Wigglytuff", Ability: "pickpocket", Item: "cheriberry", Moves: mv("splash")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.isFalse(switchRequested(p, 0), "the attacker should not be ejected")
			p.isFalse(switchRequested(p, 1), "the target should not be ejected")
		})

		g.skip("should wait until after all other end-turn effects have resolved before switching out the holder",
			"formes — the case asserts Zygarde-Complete is on the field and Power Construct is not modeled")
		g.skip("should not activate when another switching effect was triggered as part of the move", "doubles")
		g.skip("should only trigger the fastest Eject Pack when multiple targets with Eject Pack have stats lowered", "doubles")
		g.skip("should not prevent entrance Abilities from resolving during simultaneous switches", "doubles")
		g.skip("should not prohibit switchins if a switch has already resolved to a slot replaced by Eject Pack",
			"doubles (upstream skips this case too)")
		g.skip("Should be able to switch back in if ejected", "doubles")
	})
}
