//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/taunt.js.
//
// Sableye builds as Gengar through the shared table. Prankster is not in this
// ability set and comes off: it was only ensuring Taunt landed before the
// status move it was blocking, which Gengar outrunning both foes already does.
//
// The first case gains one assertion upstream states only implicitly — it
// follows the boost check with `battle.makeChoices('move taunt', 'move
// struggle')`, which is a claim that Calm Mind is no longer a legal choice.
// p.cantMove says that directly; the Struggle turn is kept as well, since it
// is what upstream actually ran.
//
// The Z-move case skips. The Hackmons case does not: it reaches Extreme
// Evoboost with no crystal, so it stops at "move extremeevoboost is not in
// this dataset", which is the finding. Eevee builds as Vaporeon through the
// shared table.

func TestMovesTaunt(t *testing.T) {
	describe(t, "Taunt", func(g *psg) {
		g.it("should prevent the target from using Status moves and disable them", func(p *ps) {
			p.battle(
				team{{Species: "Sableye", Ability: "noability", Moves: mv("taunt")}},
				team{{Species: "Chansey", Ability: "naturalcure", Moves: mv("calmmind")}},
			)
			p.makeChoices("move taunt", "move calmmind")
			p.statStage(p.foe(), "spa", 0, "Taunt should have stopped Calm Mind")
			p.statStage(p.foe(), "spd", 0, "Taunt should have stopped Calm Mind")
			p.cantMove(1, "calmmind", "a taunted Pokemon's status moves should be off the menu")
			p.makeChoices("move taunt", "move struggle")
		})

		g.skip("should not prevent the target from using Z-Powered Status moves", "Z-moves")

		g.it("[Hackmons] should prevent the target from using Z-Powered Status moves if not boosted by a Z-crystal", func(p *ps) {
			p.battle(
				team{{Species: "Sableye", Ability: "noability", Moves: mv("taunt")}},
				team{{Species: "Eevee", Ability: "runaway", Moves: mv("extremeevoboost")}},
			)
			p.turn()
			p.statStage(p.foe(), "spe", 0, "an unboosted status move should still be refused under Taunt")
		})
	})
}
