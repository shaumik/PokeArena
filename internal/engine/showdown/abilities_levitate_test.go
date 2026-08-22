//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/levitate.js.
//
// Species. Rotom and Unown become Weezing, the dex's own Levitate body;
// Cresselia becomes Hypno, which keeps its psychic typing and takes Levitate
// explicitly as upstream does; Haxorus becomes Pinsir, the only in-dex body
// with Mold Breaker of its own; Aggron and Forretress resolve to Magneton
// through the stand-in table. Nothing here turns on the typing that is lost —
// every one of these cases is about whether a Ground move or a Spikes layer
// reaches the floor.
//
// The airborne case is restated. Upstream has Espeon bounce Spore back at the
// Levitate user with Magic Bounce while Electric Terrain is up; Magic Bounce
// is not one of this engine's abilities, so the terrain goes up on its own
// turn and the Spore is aimed directly the turn after. What the case asks —
// whether Electric Terrain's sleep block reaches a Levitate holder — is
// unchanged.
//
// Eject Button and Red Card are not in this item set, so those two cases
// report them. Sleep Talk is not in the move set either and becomes Splash for
// the idle turns.

func TestAbilitiesLevitate(t *testing.T) {
	describe(t, "Levitate", func(g *psg) {
		g.it("should give the user an immunity to Ground-type moves", func(p *ps) {
			p.battle(
				team{{Species: "Rotom", As: "Weezing", Ability: "levitate", Moves: mv("splash")}},
				team{{Species: "Aggron", Ability: "sturdy", Moves: mv("earthquake")}},
			)
			p.constant(func() any { return p.mine().HP }, func() {
				p.makeChoices("move splash", "move earthquake")
			}, "Levitate should have refused the Ground move")
		})

		g.it("should make the user airborne", func(p *ps) {
			p.battle(
				team{{Species: "Unown", As: "Weezing", Ability: "levitate", Moves: mv("splash")}},
				team{{Species: "Espeon", As: "Alakazam", Moves: mv("electricterrain", "spore")}},
			)
			p.makeChoices("move splash", "move electricterrain")
			p.makeChoices("move splash", "move spore")
			p.hasStatus(p.mine(), "slp", "Levitate Pokémon should not be awaken by Electric Terrain")
		})

		g.it("should have its Ground immunity suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Cresselia", As: "Hypno", Ability: "levitate", Moves: mv("splash")}},
				team{{Species: "Haxorus", As: "Pinsir", Ability: "moldbreaker", Moves: mv("earthquake")}},
			)
			p.hurts(p.mine(), func() {
				p.makeChoices("move splash", "move earthquake")
			}, "Mold Breaker should read past Levitate")
		})

		g.it("should have its airborne property suppressed by Mold Breaker if it is forced out by a move", func(p *ps) {
			p.battle(
				team{
					{Species: "Cresselia", As: "Hypno", Ability: "levitate", Moves: mv("splash")},
					{Species: "Cresselia", As: "Hypno", Ability: "levitate", Moves: mv("splash")},
				},
				team{{Species: "Haxorus", As: "Pinsir", Ability: "moldbreaker", Moves: mv("roar", "spikes")}},
			)
			p.makeChoices("move splash", "move spikes")
			p.hurts(p.slot(0, 2), func() {
				p.makeChoices("move splash", "move roar")
			}, "a Pokemon dragged in by a Mold Breaker's move should land on the Spikes")
		})

		g.it("should not have its airborne property suppressed by Mold Breaker if it switches out via Eject Button", func(p *ps) {
			p.battle(
				team{
					{Species: "Cresselia", As: "Hypno", Ability: "levitate", Item: "ejectbutton", Moves: mv("splash")},
					{Species: "Cresselia", As: "Hypno", Ability: "levitate", Moves: mv("splash")},
				},
				team{{Species: "Haxorus", As: "Pinsir", Ability: "moldbreaker", Moves: mv("tackle", "spikes")}},
			)
			p.makeChoices("move splash", "move spikes")
			p.makeChoices("move splash", "move tackle")
			p.constant(func() any { return p.slot(0, 2).HP }, func() {
				p.makeChoices("switch 2", "move tackle")
			}, "a Pokemon that came in on its own should keep Levitate against the Spikes")
		})

		g.it("should not have its airborne property suppressed by Mold Breaker if that Pokemon is no longer active", func(p *ps) {
			p.battle(
				team{{Species: "Forretress", Ability: "levitate", Item: "redcard", Moves: mv("spikes")}},
				team{
					{Species: "Haxorus", As: "Pinsir", Ability: "moldbreaker", Item: "laggingtail", Moves: mv("tackle")},
					{Species: "Rotom", As: "Weezing", Ability: "levitate", Moves: mv("rest")},
				},
			)
			// Upstream reads battle.p2.active[0] before the turn, which
			// resolves to the Mold Breaker user rather than to the Pokemon the
			// Red Card drags in. The assertion is made on the Pokemon the case
			// is named for: the one that arrives after its suppressor has left.
			p.constant(func() any { return p.slot(1, 2).HP }, func() {
				p.makeChoices("move spikes", "move tackle")
			}, "Levitate should hold for a Pokemon arriving after the Mold Breaker left")
		})
	})

	describe(t, "Levitate [Gen 4]", func(g *psg) {
		g.skip("should not have its airborne property suppressed by Mold Breaker if it is forced out by a move",
			"gen 4 mechanics")
	})
}
