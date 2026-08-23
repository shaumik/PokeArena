//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/statuses.js.
//
// Most of the file is older generations: the whole `Toxic Poison [Gen 2]` and
// `[Gen 1]` blocks, the Gen 6 burn and paralysis figures, the three Stadium
// cases and the Gen 1 desync case all skip as generation mods. What is left is
// the modern burn, the modern paralysis cut and the modern toxic ramp.
//
// Substitutions. Sableye resolves through the stand-in table to Gengar, which
// is all these cases need from it — a body that outspeeds Machamp and lands
// Will-O-Wisp first. Prankster is not modeled and is not load-bearing for that
// (Gengar is faster than Machamp either way), so it becomes "noability".
// Talonflame is not in the dex and has no row; Charizard is the only Fire/Flying
// body here and, at Speed 120 against Machamp's 75, burns before Seismic Toss
// lands without needing Gale Wings, which is not modeled either. Machamp keeps
// No Guard, and since No Guard covers moves *to* the holder as well as from it,
// it is also what takes Will-O-Wisp's 85% accuracy out of every burn case —
// upstream got the same effect from a single unrigged seed, and this suite
// replays five.
//
// Chansey is given No Guard rather than upstream's Natural Cure / Serene Grace
// in both toxic cases, for the same reason: Toxic is 90% accurate and the ramp
// is what is being measured. Neither of upstream's abilities does anything in
// these cases except stay out of Natural Cure's way, which "noguard" also does.
// Counter is not in this dataset and was an idle move for a Pokemon with no
// attacker in front of it, so Splash stands in.
//
// Two cases could not be stated the way upstream states them:
//
//   - "should halve damage from most Physical attacks" reads an absolute damage
//     window off a level-100 Sableye. No level-50 fixture reproduces those
//     numbers, so the halving is measured by comparison — the same Bone Club
//     into a burned attacker and an unburned one — and Shell Armor keeps a
//     critical hit from being what the two runs differ by.
//   - "should reduce speed to 50% of its original value" needs a Pokemon's
//     effective Speed, which this engine computes internally and exposes to no
//     caller. It is ported as the move-order flip the cut implies instead:
//     Chansey's Speed of 70 sits between Vaporeon's 85 and the 42 that
//     paralysis leaves. Upstream's Jolteon outspeeds Vaporeon both ways and
//     would show nothing, so the Glare moves to a slower body. The port only
//     brackets the cut from above; it cannot pin the figure at exactly half.
//
// Seismic Toss deals damage equal to the user's level, so upstream's 100
// becomes 50 here.

func TestMiscStatuses(t *testing.T) {
	describe(t, "Burn", func(g *psg) {
		g.it("should inflict 1/16 of max HP at the end of the turn, rounded down", func(p *ps) {
			p.battle(
				team{{Species: "Machamp", Ability: "noguard", Moves: mv("bulkup")}},
				team{{Species: "Sableye", Ability: "noability", Moves: mv("willowisp")}},
			)
			target := p.mine()
			p.hurtsBy(target, target.MaxHP/16, func() {
				p.makeChoices("move bulkup", "move willowisp")
			}, "a burn should chip 1/16 of max HP at the end of the turn")
		})

		g.it("should halve damage from most Physical attacks", func(p *ps) {
			boneClub := func(burn bool) int {
				foeMove := "splash"
				if burn {
					foeMove = "willowisp"
				}
				p.battle(
					team{{Species: "Machamp", Ability: "noguard", Moves: mv("boneclub")}},
					team{{Species: "Sableye", Ability: "shellarmor", Moves: mv(foeMove)}},
				)
				p.makeChoices("move boneclub", "move "+foeMove)
				return p.foe().MaxHP - p.foe().HP
			}
			burned := boneClub(true)
			healthy := boneClub(false)
			p.atLeast(healthy, 1, "Bone Club should have hurt Sableye")
			// The damage roll spans 85%-100%, so halved lands inside
			// [0.425, 0.589] of the unburned figure whichever way the two rolls
			// fall. Bracketing at 0.3 and 0.7 separates a halving from both no
			// reduction and an over-reduction.
			p.atMost(10*burned, 7*healthy, "a burned attacker's Bone Club should be about half")
			p.atLeast(10*burned, 3*healthy, "a burn should halve physical damage, not more")
		})

		g.skip("should halve damage after fainting", "gen 4 mechanics")
		g.skip("should reduce atk to 50% of its original value in Stadium", "gen 1 mechanics")

		g.it("should not halve damage from moves with set damage", func(p *ps) {
			p.battle(
				team{{Species: "Machamp", Ability: "noguard", Moves: mv("seismictoss")}},
				team{{Species: "Talonflame", As: "Charizard", Ability: "noability", Moves: mv("willowisp")}},
			)
			target := p.foe()
			p.hurtsBy(target, 50, func() {
				p.makeChoices("move seismictoss", "move willowisp")
			}, "Seismic Toss deals the user's level in damage, burn or not")
		})
	})

	describe(t, "[Gen 6]", func(g *psg) {
		g.skip("should inflict 1/8 of max HP at the end of the turn, rounded down", "gen 6 mechanics")
	})

	describe(t, "Paralysis", func(g *psg) {
		g.it("should reduce speed to 50% of its original value", func(p *ps) {
			p.battle(
				team{{Species: "Vaporeon", Ability: "waterabsorb", Moves: mv("calmmind")}},
				team{{Species: "Chansey", Ability: "naturalcure", Moves: mv("glare", "taunt")}},
			)
			p.makeChoices("move calmmind", "move glare")
			p.hasStatus(p.mine(), "par", "Glare should have paralyzed Vaporeon")
			p.statStage(p.mine(), "spa", 1, "Vaporeon outsped Chansey before it was paralyzed")
			spa := func() any { return p.stage(p.mine(), "spa") }
			p.constant(spa, func() {
				p.makeChoices("move calmmind", "move taunt")
			}, "a paralyzed Vaporeon is slower than Chansey, so Taunt should land before Calm Mind")
		})

		g.skip("should apply its Speed reduction after all other Speed modifiers",
			"level is fixed at 50")
		g.skip("should reduce speed to 25% of its original value in Gen 6", "gen 6 mechanics")
		g.skip("should reduce speed to 25% of its original value in Gen 2", "gen 2 mechanics")
		g.skip("should reduce speed to 25% of its original value in Stadium", "gen 1 mechanics")
		g.skip("should reapply its speed drop when an opponent uses a stat-altering move in Gen 1",
			"gen 1 mechanics")
		g.skip("should not reapply its speed drop when an opponent uses a failed stat-altering move in Gen 1",
			"gen 1 mechanics")
	})

	describe(t, "Toxic Poison", func(g *psg) {
		g.it("should inflict 1/16 of max HP rounded down, times the number of active turns with the status, at the end of the turn", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Ability: "noguard", Moves: mv("splash")}},
				team{{Species: "Gengar", Ability: "levitate", Moves: mv("toxic")}},
			)
			target := p.mine()
			for i := 1; i <= 8; i++ {
				p.hurtsBy(target, target.MaxHP/16*i, func() {
					p.makeChoices("move splash", "move toxic")
				}, "the toxic tick should be the turn count times 1/16 of max HP, each 1/16 rounded down first")
				// Upstream keeps the target alive with Soft-Boiled, which has
				// eight PP there and five here — no PP Ups — so it runs dry
				// three ticks short. Restoring the HP directly leaves the ramp
				// as the only thing the case measures.
				target.HP = target.MaxHP
			}
		})

		g.it("should reset the damage counter when the Pokemon switches out", func(p *ps) {
			p.battle(
				team{
					{Species: "Chansey", Ability: "noguard", Moves: mv("splash")},
					{Species: "Snorlax", Ability: "immunity", Moves: mv("curse")},
				},
				team{{Species: "Crobat", Ability: "infiltrator", Moves: mv("toxic", "whirlwind")}},
			)
			target := p.slot(0, 1)
			for i := 0; i < 4; i++ {
				p.makeChoices("move splash", "move toxic")
			}
			target.HP = target.MaxHP
			// Chansey leaves, and Whirlwind drags its replacement straight back
			// out — so the same Chansey is on the field for the residual, now on
			// a counter that switching out should have reset to one.
			p.makeChoices("switch 2", "move whirlwind")
			p.equal(target.MaxHP-target.HP, target.MaxHP/16,
				"a Pokemon that switched out should tick for 1/16 again, not for where the counter had got to")
		})
	})

	describe(t, "[Gen 2]", func(g *psg) {
		g.skip("should not affect Leech Seed damage counter", "gen 2 mechanics")
		g.skip("should pass the damage counter to Pokemon with Baton Pass", "gen 2 mechanics")
		g.skip("should revert to regular poison on switch in, even for Poison types", "gen 2 mechanics")
		g.skip("should not have its damage counter affected by Heal Bell", "gen 2 mechanics")
	})

	describe(t, "[Gen 1]", func(g *psg) {
		g.skip("should affect Leech Seed damage counter", "gen 1 mechanics")
	})

	describe(t, "Freeze", func(g *psg) {
		g.skip("should cause an afflicted Shaymin-Sky to revert to its base forme",
			"Shaymin is not in this 80-species dex and formes are not modeled")
		g.skip("should not cause an afflicted Pokemon transformed into Shaymin-Sky to change to Shaymin",
			"Ditto is not in this dex, and neither Transform nor formes are modeled")
		g.skip("should not linger after fainting from switch-out", "gen 4 mechanics")

		g.it("should not be possible to burn a frozen target when using a move that thaws that target", func(p *ps) {
			// Upstream spends a turn on Meteor Assault so the target is stuck
			// recharging and cannot roll its own thaw before Sacred Fire lands.
			// Neither move is in this dataset; the freeze is set in the fixture
			// instead, which reaches the same state without the extra turn, and
			// the missing Sacred Fire is what the case now reports.
			p.battle(
				team{{Species: "wynaut", Ability: "serenegrace", Item: "widelens", Moves: mv("sacredfire")}},
				team{{Species: "shuckle", Moves: mv("splash"), Status: "frz"}},
			)
			p.makeChoices("move sacredfire", "move splash")
			p.noStatus(p.foe(), "a move that thaws its target should not then burn it")
		})
	})

	describe(t, "[Gen 1]", func(g *psg) {
		g.skip("should cause a desync if a Pokémon attacks immediately after thawing without having attacked since its last switch",
			"gen 1 mechanics")
	})
}
