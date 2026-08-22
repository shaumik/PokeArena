//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/protosynthesis.js.
//
// Protosynthesis is not in this dataset, and neither is Booster Energy, so
// every case here is a live question rather than a skip; the run names both.
//
// Upstream reads the answer straight out of `pokemon.volatiles['protosynthesis']
// .bestStat`. There is no volatile to read here, and Protosynthesis is a hidden
// 1.3x on a stat rather than a stat stage, so each case is restated as the
// measurement that multiplier would show: the same attack under the condition
// being tested and under a control where the boost is definitely off, with the
// boosted figure required to be at least 15% apart from the control.
//
// That threshold is a judgment, and worth stating plainly. The damage roll
// spans 85-100%, so two independent measurements of the *same* damage can
// differ by up to 17.6%, and a 1.3x effect can shrink to 10.5% — the two ranges
// overlap and no threshold separates them for certain. 15% sits between the
// two expectations (1.0 and 1.3) and is where a false red is far likelier than
// a false green, which is the right way round: a green case measuring nothing
// is the one outcome this port must not produce. Nothing here can go green
// today in any event, because the ability itself is missing.
//
// Species. Scream Tail has no stand-in. Chansey takes its place because its
// best stat other than HP is Sp. Def, exactly as Scream Tail's is, so the boost
// shows up as damage the holder does not take; its bulk also lets a case take
// three or four measured hits without the numbers running out. Roaring Moon
// becomes Dragonite: a Dragon body whose best stat other than HP is Attack, as
// Roaring Moon's is, so there the boost is measured on damage dealt instead.
// Salamence's stand-in Dragonite needs Intimidate set explicitly, which its row
// says. Torkoal has no stand-in and is only ever a Drought body; Snorlax
// carries Drought where the case wants a slow attacker, and Golduck carries it
// where the case needs the attacker's stats to stay identical across a switch
// into Psyduck — whose own stand-in is Golduck. Lotad is the same: a body that
// is simply not weather-suppressing, so Golduck again. Groudon-Primal becomes
// Snorlax; only Desolate Land matters about it.
//
// Moves. Sleep Talk and Lucky Chant are not in this dataset; Splash is the idle
// move in their place, and where upstream idles with Recover the port uses
// Splash too so a heal cannot land inside a measured turn. Several cases
// upstream take no turns at all, reading the volatile the moment the battle is
// built; the port has to play the turns that make the boost visible.
//
// What is not asserted: which stat Protosynthesis picked (case 2), and whether
// the activation came from the Sun or from Booster Energy (case 4). Both are
// facts about the volatile, and the volatile is what the harness cannot see.
// The Booster Energy cases die at setup, since the item is not in the dataset.

func TestAbilitiesProtosynthesis(t *testing.T) {
	// damageTaken / damageDealt run one turn and report the HP swing on the
	// side the case is measuring.
	damageTaken := func(p *ps, c1, c2 string) int {
		before := p.mine().HP
		p.makeChoices(c1, c2)
		return before - p.mine().HP
	}
	damageDealt := func(p *ps, c1, c2 string) int {
		before := p.foe().HP
		p.makeChoices(c1, c2)
		return before - p.foe().HP
	}

	describe(t, "Protosynthesis", func(g *psg) {
		g.it("should boost the user's highest stat except HP while Sunny Day is active", func(p *ps) {
			p.battle(
				team{{Species: "Scream Tail", As: "Chansey", Ability: "protosynthesis",
					Moves: mv("splash", "raindance")}},
				team{{Species: "Torkoal", As: "Snorlax", Ability: "drought", Moves: mv("psychic")}},
			)
			inSun := damageTaken(p, "move splash", "move psychic")
			inRain := damageTaken(p, "move raindance", "move psychic")
			p.atLeast(inSun, 1, "the measured attack should have done damage")
			p.atLeast(inRain*100, inSun*115, "the Sp. Def boost should be on in Sun and off once Rain replaces it")
		})

		g.it("should take stat stages and no other modifiers into account when determining the best stat", func(p *ps) {
			p.battle(
				team{{Species: "Roaring Moon", As: "Dragonite", Ability: "protosynthesis",
					EVs: evs(map[string]int{"atk": 252, "spd": 252}), Moves: mv("tailwind")}},
				team{{Species: "Salamence", Ability: "intimidate", Moves: mv("sunnyday")}},
			)
			p.turn()
			p.statStage(p.mine(), "atk", -1, "Intimidate should have dropped the Attack Protosynthesis reads")
		})

		g.it("should not activate while Desolate Land is active", func(p *ps) {
			p.battle(
				team{{Species: "Roaring Moon", As: "Dragonite", Ability: "protosynthesis", Moves: mv("strength")}},
				team{{Species: "Groudon-Primal", As: "Snorlax", Ability: "desolateland", Moves: mv("splash")}},
			)
			underDesolateLand := damageDealt(p, "move strength", "move splash")

			p.battle(
				team{{Species: "Roaring Moon", As: "Dragonite", Ability: "protosynthesis", Moves: mv("strength")}},
				team{{Species: "Groudon-Primal", As: "Snorlax", Ability: "drought", Moves: mv("splash")}},
			)
			inSun := damageDealt(p, "move strength", "move splash")

			p.atLeast(underDesolateLand, 1, "the measured attack should have done damage")
			p.atLeast(inSun*100, underDesolateLand*115, "only real Sun should turn Protosynthesis on")
		})

		g.it("should be activated by Booster Energy when Sunny Day is not active", func(p *ps) {
			p.battle(
				team{{Species: "Scream Tail", As: "Chansey", Ability: "protosynthesis", Item: "boosterenergy",
					Moves: mv("raindance", "splash")}},
				team{{Species: "Torkoal", As: "Snorlax", Ability: "drought", Moves: mv("psychic")}},
			)
			p.makeChoices("move raindance", "move psychic")
			withBooster := damageTaken(p, "move splash", "move psychic")

			p.battle(
				team{{Species: "Scream Tail", As: "Chansey", Ability: "protosynthesis",
					Moves: mv("raindance", "splash")}},
				team{{Species: "Torkoal", As: "Snorlax", Ability: "drought", Moves: mv("psychic")}},
			)
			p.makeChoices("move raindance", "move psychic")
			bare := damageTaken(p, "move splash", "move psychic")

			p.atLeast(bare, 1, "the measured attack should have done damage")
			p.atLeast(bare*100, withBooster*115, "Booster Energy should hold the boost up once the Sun is gone")
		})

		g.it("should not be prevented from activating if the user holds Utility Umbrella", func(p *ps) {
			p.battle(
				team{{Species: "Scream Tail", As: "Chansey", Ability: "protosynthesis", Item: "utilityumbrella",
					Moves: mv("splash", "raindance")}},
				team{{Species: "Torkoal", As: "Snorlax", Ability: "drought", Moves: mv("psychic")}},
			)
			inSun := damageTaken(p, "move splash", "move psychic")
			inRain := damageTaken(p, "move raindance", "move psychic")
			p.atLeast(inSun, 1, "the measured attack should have done damage")
			p.atLeast(inRain*100, inSun*115, "Utility Umbrella should not keep Protosynthesis from reading the Sun")
		})

		g.it("should be deactiviated by weather suppressing abilities", func(p *ps) {
			p.battle(
				team{{Species: "Scream Tail", As: "Chansey", Ability: "protosynthesis", Moves: mv("splash")}},
				team{
					{Species: "Torkoal", As: "Golduck", Ability: "drought", Moves: mv("psychic")},
					{Species: "Psyduck", Ability: "cloudnine", Moves: mv("psychic")},
				},
			)
			inSun := damageTaken(p, "move splash", "move psychic")
			p.makeChoices("move splash", "switch 2")
			suppressed := damageTaken(p, "move splash", "move psychic")
			p.atLeast(inSun, 1, "the measured attack should have done damage")
			p.atLeast(suppressed*100, inSun*115, "Cloud Nine arriving should end the boost even though the Sun is still up")
		})

		g.it("should not activate if weather is suppressed", func(p *ps) {
			p.battle(
				team{{Species: "Scream Tail", As: "Chansey", Ability: "protosynthesis", Moves: mv("splash")}},
				team{{Species: "Psyduck", Ability: "cloudnine", Moves: mv("sunnyday", "psychic")}},
			)
			p.makeChoices("move splash", "move sunnyday")
			suppressed := damageTaken(p, "move splash", "move psychic")

			p.battle(
				team{{Species: "Scream Tail", As: "Chansey", Ability: "protosynthesis", Moves: mv("splash")}},
				team{{Species: "Psyduck", Ability: "noability", Moves: mv("sunnyday", "psychic")}},
			)
			p.makeChoices("move splash", "move sunnyday")
			inSun := damageTaken(p, "move splash", "move psychic")

			p.atLeast(suppressed, 1, "the measured attack should have done damage")
			p.atLeast(suppressed*100, inSun*115, "Sun that starts under Cloud Nine should not turn Protosynthesis on")
		})

		g.it("should activate when weather supression ends", func(p *ps) {
			p.battle(
				team{{Species: "Scream Tail", As: "Chansey", Ability: "protosynthesis", Moves: mv("splash")}},
				team{
					{Species: "Psyduck", Ability: "cloudnine", Moves: mv("sunnyday", "psychic")},
					{Species: "Lotad", As: "Golduck", Ability: "swiftswim", Moves: mv("psychic")},
				},
			)
			p.makeChoices("move splash", "move sunnyday")
			suppressed := damageTaken(p, "move splash", "move psychic")
			p.makeChoices("move splash", "switch 2")
			unsuppressed := damageTaken(p, "move splash", "move psychic")
			p.atLeast(suppressed, 1, "the measured attack should have done damage")
			p.atLeast(suppressed*100, unsuppressed*115, "the boost should start once the suppressor leaves")
		})

		g.it("should have its boost nullified by Neutralizing Gas", func(p *ps) {
			p.battle(
				team{{Species: "Scream Tail", As: "Chansey", Ability: "protosynthesis", Item: "boosterenergy",
					Moves: mv("splash")}},
				team{
					{Species: "Weezing", Ability: "levitate", Moves: mv("venoshock")},
					{Species: "Weezing", Ability: "neutralizinggas", Moves: mv("venoshock")},
				},
			)
			boosted := damageTaken(p, "move splash", "move venoshock")
			p.makeChoices("move splash", "switch 2")
			nullified := damageTaken(p, "move splash", "move venoshock")
			p.makeChoices("move splash", "switch 1")
			restored := damageTaken(p, "move splash", "move venoshock")

			p.atLeast(boosted, 1, "the measured attack should have done damage")
			p.atLeast(nullified*100, boosted*115, "Neutralizing Gas should switch the boost off")
			p.atLeast(nullified*100, restored*115, "and the boost should come back when it leaves")
		})

		g.skip("should not activate while the user is Transformed",
			"Transform and Ditto are not modeled, and there is deliberately no stand-in for Ditto")
	})
}
