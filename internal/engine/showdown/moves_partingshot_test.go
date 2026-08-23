//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/partingshot.js.
//
// Parting Shot is not in this 538-move dataset. The first case is written out
// rather than skipped, because that absence is the finding.
//
// Species. None of Silvally, Type: Null, Registeel, Solgaleo, Shaymin or Spinda
// has a stand-in row, and each is only a body carrying one drop-blocking
// ability, so each is built as an in-dex body of roughly its type: Kangaskhan
// and Snorlax for the two Normal bodies, Magneton for the two Steel ones,
// Magmar for Torkoal, Tangela for Shaymin. Kingler is in the dex already.
// Prankster is dropped — it is not modeled, and every target here answers with
// Splash, so nothing depends on the user moving first.
//
// Two legs of the case are dropped and said so here rather than faked.
// Upstream reaches into the battle and calls boostBy to put Kingler at -6 Sp.
// Atk and Spinda at +6/+6 before the relevant Parting Shot; there is no
// counterpart to that here, and no move in this dataset arranges either state
// inside the turn budget the case has. What survives is the four
// ability-blocked legs, which is the bulk of the original.
//
// `sleeptalk` is not in this dataset; `splash` stands in for it.

func TestMovesPartingShot(t *testing.T) {
	describe(t, "Parting Shot", func(g *psg) {
		g.it(`should not switch the user out if the target's stats are not changed`, func(p *ps) {
			p.battle(
				team{
					{Species: "Silvally", As: "Kangaskhan", Ability: "noability", Moves: mv("partingshot", "splash")},
					{Species: "Type: Null", As: "Snorlax", Ability: "battlearmor", Moves: mv("return")},
				},
				team{
					{Species: "Registeel", As: "Magneton", Ability: "clearbody", Moves: mv("splash")},
					{Species: "Solgaleo", As: "Magneton", Ability: "fullmetalbody", Moves: mv("splash")},
					{Species: "Torkoal", As: "Magmar", Ability: "whitesmoke", Moves: mv("splash")},
					{Species: "Shaymin", As: "Tangela", Ability: "flowerveil", Moves: mv("splash")},
				},
			)
			if p.state() == nil {
				return
			}
			// The user staying put is what upstream reads as requestState
			// 'move'; here it is simply that the same Pokemon is still out.
			p.makeChoices("move partingshot", "move splash")
			p.species(p.mine(), "Kangaskhan", "Clear Body should refuse the drops, so Parting Shot does not pivot")

			p.makeChoices("move partingshot", "switch 2") // Solgaleo
			p.species(p.mine(), "Kangaskhan", "Full Metal Body should refuse the drops, so Parting Shot does not pivot")

			p.makeChoices("move partingshot", "switch 3") // Torkoal
			p.species(p.mine(), "Kangaskhan", "White Smoke should refuse the drops, so Parting Shot does not pivot")

			p.makeChoices("move partingshot", "switch 4") // Shaymin
			p.species(p.mine(), "Kangaskhan", "Flower Veil should refuse the drops, so Parting Shot does not pivot")
		})

		g.skip(`should set the Z-Parting Shot healing flag even if the Parting Shot itself was not successful`, "Z-moves")
	})
}
