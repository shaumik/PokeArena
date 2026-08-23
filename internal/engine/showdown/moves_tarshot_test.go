//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/tarshot.js.
//
// Tar Shot is not in this dataset, so every live case here stops at "move
// tarshot is not in this dataset". That absence is the finding, and the cases
// are written out rather than skipped so they say what the move has to do if
// it is ever added.
//
// Three translation decisions.
//
// Damage figures. Upstream bounds the raw HP lost — 82-98, 88-104 — which are
// level-100 numbers and mean nothing at the fixed level 50 here. Both cases are
// restated as a ratio against the same attack in the same battle without Tar
// Shot up, which is the claim the bound was standing in for: an extra factor of
// two, surviving the 85-100% damage roll on both measurements. The targets get
// Shell Armor so a critical hit cannot land in one measurement and not the
// other; upstream does not need it because it pins the RNG instead.
//
// Fusion Flare is not in this dataset and is only ever "a Fire-type special
// attack" here, so Flamethrower takes its place. Flame Charge, which the fourth
// case uses, is present and is kept.
//
// Ferrothorn goes through the shared stand-in table to Magneton, whose row
// warns that Grass is not preserved: the target is 2x weak to Fire here rather
// than 4x. The absolute figure was never going to transfer, and the ratio the
// case is restated as — one extra doubling on a target that is already weak —
// is unaffected.
//
// The Delta Stream case is doubles. The two Terastallization cases have no
// counterpart at all: this engine has no Tera layer, so "the Tar Shot status
// survives Terastallizing" and "a Terastallized Pokemon cannot catch it" are
// both unaskable.

func TestMovesTarShot(t *testing.T) {
	describe(t, "Tar Shot", func(g *psg) {
		g.it("should cause subsequent Fire-type attacks to deal 2x damage as a type chart multiplier", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Moves: mv("tarshot", "flamethrower", "splash")}},
				team{{Species: "cleffa", Ability: "shellarmor", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move tarshot", "move splash")
			before := p.foe().HP
			p.makeChoices("move flamethrower", "move splash")
			tarred := before - p.foe().HP

			p.battle(
				team{{Species: "wynaut", Moves: mv("tarshot", "flamethrower", "splash")}},
				team{{Species: "cleffa", Ability: "shellarmor", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move splash")
			bareBefore := p.foe().HP
			p.makeChoices("move flamethrower", "move splash")
			plain := bareBefore - p.foe().HP

			p.atLeast(plain, 1, "the baseline Fire attack should have connected at all")
			p.bounded(tarred*100, plain*165, plain*240,
				"Tar Shot should put an extra 2x on the Fire type chart multiplier")
		})

		g.it("should cause Fire-type attacks to trigger Weakness Policy", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Moves: mv("tarshot", "flamethrower")}},
				team{{Species: "cleffa", Item: "weaknesspolicy", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move tarshot", "move splash")
			p.makeChoices("move flamethrower", "move splash")
			p.noItem(p.foe(), "the Weakness Policy should have been consumed")
			p.statStage(p.foe(), "atk", 2, "")
			p.statStage(p.foe(), "spa", 2, "")
		})

		g.skip("should not interact with Delta Stream", "doubles")

		g.it("should add a Fire-type weakness, not make the target 2x weaker to Fire", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Moves: mv("tarshot", "flamecharge", "splash")}},
				team{{Species: "ferrothorn", Ability: "shellarmor", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move tarshot", "move splash")
			before := p.foe().HP
			p.makeChoices("move flamecharge", "move splash")
			tarred := before - p.foe().HP

			p.battle(
				team{{Species: "wynaut", Moves: mv("tarshot", "flamecharge", "splash")}},
				team{{Species: "ferrothorn", Ability: "shellarmor", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move splash")
			bareBefore := p.foe().HP
			p.makeChoices("move flamecharge", "move splash")
			plain := bareBefore - p.foe().HP

			p.atLeast(plain, 1, "the baseline Fire attack should have connected at all")
			p.bounded(tarred*100, plain*165, plain*240,
				"Tar Shot should add one weakness to an already Fire-weak target, not square the multiplier")
		})

		g.skip("should not remove the Tar Shot status when a Pokemon Terastallizes", "Terastallization")
		g.skip("should prevent a Terastallized Pokemon from being afflicted with the Tar Shot status",
			"Terastallization")
	})
}
