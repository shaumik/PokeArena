//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/emergencyexit.js.
//
// Emergency Exit is not one of this engine's 118 abilities, and the engine has
// no mid-turn forced-switch request of any kind: PhaseReplace only ever means
// "a fainted active owes a replacement". So the cases that assert a switch was
// asked for all fail, and that single gap is the finding the file is here to
// record. The mirror-image cases — the ones asserting no switch was asked for —
// pass today for the same reason, which is worth knowing before reading a green
// result as agreement.
//
// Two moves the fixtures lean on are missing from this dataset, and they are
// handled differently depending on what they are doing:
//
//   - Sleep Talk and Super Fang are plumbing. Sleep Talk means "do nothing" and
//     Splash stands in for it; Super Fang means "put the holder just under half"
//     and the port sets HP one point above half and lets a weak, roll-free
//     attack cross the line instead. Seismic Toss is the usual crosser because
//     it is fixed at 50 damage at this level and cannot crit.
//   - Photon Geyser and Mind Blown are the subject of their own cases, so they
//     are kept and the missing-move failure is the finding. Red Card and Eject
//     Button are the same story on the item side.
//
// Golisopod goes through its stand-in row (Kingler) wherever Kingler's numbers
// work. Three groups of cases need something else, and say so at the case:
// Magmar where the holder's max HP has to divide by four (two Substitutes, two
// Struggles, and Stealth Rock plus three Spikes each come to exactly half only
// then), Snorlax where Kingler is simply KO'd by the hit the case needs it to
// survive, and Seaking for the multi-hit case, which needs a Water body bulky
// enough to sit either side of the line across two turns.
//
// Not ported: the doubles case, the two Dynamax cases, and the three cases
// upstream itself has pending.

func TestAbilitiesEmergencyExit(t *testing.T) {
	describe(t, "Emergency Exit", func(g *psg) {
		// switchRequested is this harness's nearest reading of upstream's
		// `battle.requestState === 'switch'`, narrowed to one side so a foe's
		// fainted active cannot be mistaken for the holder being asked to leave.
		switchRequested := func(p *ps, side int) bool {
			st := p.state()
			return st != nil && st.Phase == "replace" && st.Replace[side]
		}

		g.it("should request switch-out if damaged below 50% HP", func(p *ps) {
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Moves: mv("splash"), HP: 66},
					{Species: "Clefable", Ability: "unaware", Moves: mv("splash")},
				},
				team{{Species: "Raticate", Ability: "noguard", Moves: mv("seismictoss")}},
			)
			p.makeChoices("move splash", "move seismictoss")
			p.fullHP(p.foe(), "the attacker should be untouched")
			p.atMost(p.mine().HP, p.mine().MaxHP/2, "the hit should have taken it under half")
			p.ok(switchRequested(p, 0), "Emergency Exit should have asked for a switch")
		})

		g.it("should request switch-out at the end of a multi-hit move", func(p *ps) {
			// Chansey is the Skill Link body: Cinccino's modest Attack is what
			// keeps Bullet Seed from clearing half in one turn, and Chansey is
			// the only in-dex body weak enough to reproduce that. Seaking keeps
			// Golisopod's Water half, so Bullet Seed stays super effective, and
			// its 155 max HP puts the first round above the line and the second
			// under it.
			p.battle(
				team{{Species: "Cinccino", As: "Chansey", Ability: "skilllink", Moves: mv("bulletseed")}},
				team{
					{Species: "Golisopod", As: "Seaking", Ability: "emergencyexit", Moves: mv("splash")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("splash")},
				},
			)
			p.makeChoices("move bulletseed", "move splash")
			p.makeChoices("move bulletseed", "move splash")
			p.ok(switchRequested(p, 1), "Emergency Exit should have asked once the last hit landed")
		})

		g.it("should request switch-out if brought below half HP by residual damage", func(p *ps) {
			// Toxic is 90% accurate and there is no rigged RNG here, so the
			// badly-poisoned state is set on the fixture; Crobat stays as the
			// inert body it came from. 89 is two above Mew's half of 87, which
			// is where upstream puts it, and the first toxic tick is 10.
			p.battle(
				team{{Species: "Crobat", Moves: mv("splash")}},
				team{
					{Species: "Mew", Ability: "emergencyexit", Moves: mv("splash"), HP: 89, Status: "tox"},
					{Species: "Shaymin", As: "Venusaur", Moves: mv("splash")},
				},
			)
			p.makeChoices("move splash", "move splash")
			p.ok(switchRequested(p, 1), "the residual tick should have triggered Emergency Exit")
		})

		g.it("should request switch-out if brought below half HP by Photon Geyser", func(p *ps) {
			// Photon Geyser is the subject, so it is kept and its absence from
			// this dataset is what the case reports.
			p.battle(
				team{{Species: "Mew", Moves: mv("photongeyser")}},
				team{
					{Species: "Charmeleon", Ability: "emergencyexit", Moves: mv("splash")},
					{Species: "Shaymin", As: "Venusaur", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move photongeyser", "move splash")
			p.ok(switchRequested(p, 1), "Emergency Exit should have asked for a switch")
		})

		g.it("should not request switch-out if attacked and healed by berry", func(p *ps) {
			// 70 leaves the berry room to put it back over half: Tackle takes it
			// to the high forties and Sitrus returns a quarter of 130.
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Item: "sitrusberry", Moves: mv("splash"), HP: 70},
					{Species: "Clefable", Ability: "unaware", Moves: mv("splash")},
				},
				team{{Species: "Raticate", Ability: "guts", Moves: mv("tackle")}},
			)
			p.makeChoices("move splash", "move tackle")
			p.noItem(p.mine(), "the Sitrus Berry should have been eaten")
			p.isFalse(switchRequested(p, 0), "the berry put it back over half, so nothing should be asked for")
		})

		g.skip("should not request switch-out if fainted", "doubles")

		g.it("should request switch-out before end-of-turn fainted Pokemon", func(p *ps) {
			// Black Sludge on a Water holder is 1/8 of 130, which is what crosses
			// the line; the foe is left at 1 HP so Payback finishes it in the
			// same turn. Upstream's third reading — that the side with the
			// fainted active is *not* asked to switch — has no counterpart here,
			// because a fainted active always owes a replacement in this engine;
			// what upstream is pinning is the order the two requests are raised
			// in, and this engine raises only one of them at all.
			p.battle(
				team{
					{Species: "Golisopod", Item: "blacksludge", Ability: "emergencyexit", Moves: mv("payback"), HP: 66},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{
					{Species: "Swoobat", As: "Golbat", Ability: "noguard", Moves: mv("splash"), HP: 1},
					{Species: "Stufful", As: "Snorlax", Moves: mv("splash")},
				},
			)
			p.makeChoices("move payback", "move splash")
			p.ok(switchRequested(p, 0), "Emergency Exit should have asked p1 to switch")
			p.fainted(p.foe(), "Payback should have knocked the foe out")
		})

		g.it("should request switch-out after taking hazard damage", func(p *ps) {
			// Magmar takes Stealth Rock at a quarter and its 140 max HP divides
			// evenly, so the rocks plus three layers of Spikes come to exactly
			// half — the relationship upstream gets from Golisopod's Bug/Water.
			// Magneton only has to set the hazards; Arceus-Flying's typing and
			// Multitype are not in play. Dragon Ascent is not in this dataset and
			// is only the last turn's chip, so Splash replaces it and leaves the
			// hazards as the unambiguous cause. Iron Barbs is off the setter for
			// the same reason: it is upstream's U-turn contact chip, this engine
			// does not model it, and keeping it would turn the case red for
			// something it is not asking about.
			p.battle(
				team{
					{Species: "Golisopod", As: "Magmar", Ability: "emergencyexit", Moves: mv("uturn", "splash")},
					{Species: "Magikarp", Ability: "swiftswim", Moves: mv("splash")},
				},
				team{{Species: "Arceus-Flying", As: "Magneton", Moves: mv("stealthrock", "spikes", "splash")}},
			)
			p.makeChoices("move uturn", "move stealthrock")
			p.makeChoices("move splash", "move spikes")
			p.makeChoices("move splash", "move spikes")
			p.makeChoices("move splash", "move spikes")
			p.makeChoices("switch 1", "move splash")
			p.atLeast(p.mine().HP, 1, "it should have survived the hazards")
			p.ok(switchRequested(p, 0), "the hazard chip should have triggered Emergency Exit")
		})

		g.it("should request switch-out after taking Life Orb recoil", func(p *ps) {
			// Life Orb costs a tenth of 130, so 74 is above half before the
			// attack and under it afterwards, with nothing else touching the
			// holder.
			p.battle(
				team{
					{Species: "Golisopod", Item: "lifeorb", Ability: "emergencyexit", Moves: mv("peck"), HP: 74},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "stufful", As: "Snorlax", Ability: "compoundeyes", Moves: mv("splash")}},
			)
			p.makeChoices("move peck", "move splash")
			p.ok(switchRequested(p, 0), "Life Orb recoil should have triggered Emergency Exit")
		})

		g.it("should request switch-out after taking Mind Blown self-damage", func(p *ps) {
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Moves: mv("mindblown")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "chansey", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mindblown", "move splash")
			p.ok(switchRequested(p, 0), "Mind Blown's self-damage should have triggered Emergency Exit")
		})

		g.skip("should request switch-out after taking recoil and dragging in an opponent",
			"pending upstream (it.skip)")

		g.it("should not request switch-out after taking entry hazard damage and getting healed by berry", func(p *ps) {
			// Same Magmar arithmetic as the case above, and Iron Barbs is off
			// the setter for the same reason.
			p.battle(
				team{
					{Species: "Golisopod", As: "Magmar", Ability: "emergencyexit", Item: "sitrusberry", Moves: mv("uturn", "splash")},
					{Species: "Magikarp", Ability: "swiftswim", Moves: mv("splash")},
				},
				team{{Species: "Ferrothorn", Moves: mv("stealthrock", "spikes", "protect")}},
			)
			p.makeChoices("move uturn", "move stealthrock")
			p.makeChoices("move splash", "move spikes")
			p.makeChoices("move splash", "move spikes")
			p.makeChoices("move splash", "move spikes")
			p.makeChoices("switch 1", "move protect")
			p.noItem(p.mine(), "the Sitrus Berry should have been eaten on the way in")
			p.isFalse(switchRequested(p, 0), "the berry cleared the line again, so nothing should be asked for")
		})

		g.it("should not request switch-out after taking poison damage and getting healed by berry", func(p *ps) {
			// The poison is set on the fixture rather than played out, for the
			// accuracy reason above; Gengar keeps the rest of its part. The
			// Substitute soaks the Night Shade, so the only thing moving the
			// holder's own HP is the escalating toxic tick, which reaches the
			// berry on the third turn.
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Item: "sitrusberry", Moves: mv("substitute", "splash"), Status: "tox"},
					{Species: "Magikarp", Moves: mv("splash")},
				},
				team{{Species: "Gengar", Moves: mv("nightshade", "protect")}},
			)
			p.makeChoices("move substitute", "move protect")
			p.makeChoices("move splash", "move nightshade")
			p.makeChoices("move splash", "move protect")
			p.noItem(p.mine(), "the Sitrus Berry should have been eaten")
			p.isFalse(switchRequested(p, 0), "the berry put it back over half, so nothing should be asked for")
		})

		g.it("should not request switch-out on usage of Substitute", func(p *ps) {
			// Two Substitutes at a quarter each land exactly on half only when
			// max HP divides by four, so Magmar rather than Kingler. Lagging
			// Tail keeps the attacker last, so each Substitute is up before the
			// Thunderbolt that breaks it.
			p.battle(
				team{
					{Species: "Golisopod", As: "Magmar", Ability: "emergencyexit", Moves: mv("substitute")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("splash")},
				},
				team{{Species: "Deoxys-Attack", Ability: "pressure", Item: "laggingtail", Moves: mv("thunderbolt")}},
			)
			p.makeChoices("move substitute", "move thunderbolt")
			p.isFalse(p.mine().HP <= p.mine().MaxHP/2, "one Substitute should not reach half")
			p.makeChoices("move substitute", "move thunderbolt")
			p.atMost(p.mine().HP, p.mine().MaxHP/2, "two Substitutes should land on half")
			p.isFalse(switchRequested(p, 0), "self-inflicted Substitute cost must not trigger Emergency Exit")
		})

		g.it("should prevent Volt Switch after switches", func(p *ps) {
			// Eelektrik is not in the dex and its typing is incidental — the
			// case is about whose switch happens. Snorlax's Volt Switch loses
			// the STAB that would otherwise KO Kingler outright, which leaves it
			// alive and under half, which is the state the case needs.
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Moves: mv("splash")},
					{Species: "Clefable", Moves: mv("splash")},
				},
				team{
					{Species: "Eelektrik", As: "Snorlax", Moves: mv("voltswitch")},
					{Species: "Clefable", Moves: mv("splash")},
				},
			)
			p.makeChoices("move splash", "move voltswitch")
			p.atMost(p.mine().HP, p.mine().MaxHP/2, "Volt Switch should have taken it under half")
			p.ok(switchRequested(p, 0), "Emergency Exit should have asked for a switch")

			p.makeChoices("switch 2", "")
			p.species(p.mine(), "Clefable", "")
			p.species(p.foe(), "Snorlax", "Volt Switch should not have got its own switch")
		})

		g.it("should not prevent Red Card's activation", func(p *ps) {
			// Red Card is not in this dataset, which is the finding here.
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Item: "redcard", Moves: mv("splash"), HP: 66},
					{Species: "Clefable", Ability: "unaware", Moves: mv("splash")},
				},
				team{
					{Species: "Raticate", Ability: "guts", Moves: mv("seismictoss")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move seismictoss")
			p.atMost(p.mine().HP, p.mine().MaxHP/2, "")
			p.noItem(p.mine(), "the Red Card should have been spent")
			p.ok(switchRequested(p, 0), "Emergency Exit should still have asked for a switch")

			p.makeChoices("", "")
			p.species(p.mine(), "Clefable", "")
			p.species(p.foe(), "Clefable", "the Red Card should have dragged the attacker out")
		})

		g.it("should not prevent Eject Button's activation", func(p *ps) {
			// Eject Button is not in this dataset, which is the finding here.
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Item: "ejectbutton", Moves: mv("splash"), HP: 66},
					{Species: "Clefable", Ability: "unaware", Moves: mv("splash")},
				},
				team{
					{Species: "Raticate", Ability: "guts", Moves: mv("seismictoss")},
					{Species: "Clefable", Ability: "unaware", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move splash", "move seismictoss")
			p.atMost(p.mine().HP, p.mine().MaxHP/2, "")
			p.noItem(p.mine(), "the Eject Button should have been spent")
			p.ok(switchRequested(p, 0), "Emergency Exit should still have asked for a switch")

			p.makeChoices("", "")
			p.species(p.mine(), "Clefable", "")
		})

		g.it("should be suppressed by Sheer Force", func(p *ps) {
			// A Sheer Force Thunderbolt KOs Kingler outright at this level, so
			// the holder is Snorlax, whose bulk lets the same hit land under
			// half instead. 130 is just above Snorlax's half of 117.
			p.battle(
				team{
					{Species: "Golisopod", As: "Snorlax", Ability: "emergencyexit", Moves: mv("splash"), HP: 130},
					{Species: "Clefable", Moves: mv("splash")},
				},
				team{{Species: "Nidoking", Ability: "sheerforce", Moves: mv("thunderbolt")}},
			)
			p.makeChoices("move splash", "move thunderbolt")
			p.atMost(p.mine().HP, p.mine().MaxHP/2, "the hit should have taken it under half")
			p.isFalse(switchRequested(p, 0), "Sheer Force should have suppressed Emergency Exit")
		})

		g.it("should not request switchout if its HP is already below 50%", func(p *ps) {
			// Upstream's switch out and back is forced by the first trigger;
			// here nothing forces it, so the port makes the same two switches by
			// hand and then lets the holder act.
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Moves: mv("tackle", "splash"), HP: 66},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "stufful", As: "Snorlax", Ability: "compoundeyes", Moves: mv("seismictoss", "splash")}},
			)
			p.makeChoices("move splash", "move seismictoss")
			p.makeChoices("switch 2", "move splash")
			p.makeChoices("switch 1", "move splash")
			p.makeChoices("move tackle", "move splash")
			p.isFalse(switchRequested(p, 0), "it was already under half, so nothing new should be asked for")
		})

		g.it("should request switchout if its HP was restored to above 50% and brought down again", func(p *ps) {
			// Heal Pulse returns half of the target's max, which clears the line
			// from 16 and lets the second Seismic Toss cross it again.
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Moves: mv("splash"), HP: 66},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "stufful", As: "Snorlax", Ability: "compoundeyes", Moves: mv("seismictoss", "healpulse")}},
			)
			p.makeChoices("move splash", "move seismictoss")
			p.makeChoices("move splash", "move healpulse")
			p.makeChoices("move splash", "move seismictoss")
			p.ok(switchRequested(p, 0), "crossing the line a second time should trigger Emergency Exit again")
		})

		g.it("should not request switchout if its HP is already below 50% and an effect heals it", func(p *ps) {
			// Upstream tunes this with level 65; levels are fixed at 50 here, so
			// the starting HP does the tuning instead. Crunch takes it under
			// half, False Swipe takes it into Figy Berry range without being
			// able to KO, and the berry's third of max HP is the heal the case
			// is about.
			p.battle(
				team{
					{Species: "Golisopod", Item: "figyberry", Ability: "emergencyexit", Moves: mv("splash"), HP: 90},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "ursaring", As: "Snorlax", Ability: "sheerforce", Moves: mv("falseswipe", "crunch")}},
			)
			p.makeChoices("move splash", "move crunch")
			p.makeChoices("move splash", "move falseswipe")
			p.noItem(p.mine(), "the Figy Berry should have been eaten")
			p.isFalse(switchRequested(p, 0), "a heal below the line must not raise a new request")
		})

		g.skip("should request switchout if its HP drops to below 50% while dynamaxed", "Dynamax")
		g.skip("should not request switchout if its HP is below 50% when its dynamax ends", "Dynamax")
		g.skip("should request switchout between hazards", "pending upstream (it.skip)")
		g.skip("should request switchout between residual damage", "doubles")

		g.it("should request a switchout after taking regular recoil damage", func(p *ps) {
			// Shell Armor on the target is a port device: a critical hit here
			// turns the recoil into a KO, which would end the battle instead of
			// asking the question. Chansey survives the uncritted hit and the
			// third of it that comes back takes Kingler well under half.
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Moves: mv("flareblitz")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Chansey", Ability: "shellarmor", Moves: mv("splash")}},
			)
			p.makeChoices("move flareblitz", "move splash")
			p.atMost(p.mine().HP, p.mine().MaxHP/2, "recoil should have taken it under half")
			p.ok(switchRequested(p, 0), "recoil should have triggered Emergency Exit")
		})

		g.it("should request a switchout after crash damage", func(p *ps) {
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Moves: mv("highjumpkick")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Chansey", Moves: mv("protect")}},
			)
			p.makeChoices("move highjumpkick", "move protect")
			p.atMost(p.mine().HP, p.mine().MaxHP/2, "the crash should have cost half of max HP")
			p.ok(switchRequested(p, 0), "the crash should have triggered Emergency Exit")
		})

		g.it("should request a switchout after Mind Blown recoil damage", func(p *ps) {
			p.battle(
				team{
					{Species: "Golisopod", Ability: "emergencyexit", Moves: mv("mindblown")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Chansey", Moves: mv("protect")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move mindblown", "move protect")
			p.atMost(p.mine().HP, p.mine().MaxHP/2, "")
			p.ok(switchRequested(p, 0), "Mind Blown's recoil should have triggered Emergency Exit")
		})

		g.it("should request a switchout after taking struggle recoil damage", func(p *ps) {
			// Assault Vest leaves Protect unchoosable and nothing else in the
			// slot, so Struggle is the only thing left; the port picks it
			// explicitly rather than relying on which action the harness
			// defaults to. Two Struggles cost half of Magmar's 140 under canon's
			// quarter-of-max recoil.
			p.battle(
				team{
					{Species: "Golisopod", As: "Magmar", Item: "assaultvest", Ability: "emergencyexit", Moves: mv("protect")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Wynaut", Moves: mv("splash")}},
			)
			p.makeChoices("move struggle", "move splash")
			p.makeChoices("move struggle", "move splash")
			p.ok(switchRequested(p, 0), "Struggle recoil should have triggered Emergency Exit")
		})
	})
}
