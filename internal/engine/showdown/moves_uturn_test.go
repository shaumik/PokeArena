//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/uturn.js.
//
// Upstream reads `battle.requestState === 'switch'`, because Showdown puts the
// pivot's replacement choice to the player as a separate request. This engine
// resolves the self-switch inside the move (switching.go: applySelfSwitch),
// picking the lowest-indexed live teammate when the action names no target, so
// there is no switch request to observe. The equivalent observable is who is
// out when the turn is over, which is what the port asserts.
//
// Kakuna has no stand-in row; Venomoth is built instead, keeping the Bug/Poison
// typing. All the case wants from it is a live bench slot for the pivot to
// land in.

func TestMovesUTurn(t *testing.T) {
	describe(t, "U-turn", func(g *psg) {
		g.it("should switch the user out after a successful hit against a Substitute", func(p *ps) {
			p.battle(
				team{
					{Species: "Beedrill", Ability: "swarm", Moves: mv("uturn")},
					{Species: "Kakuna", As: "Venomoth", Ability: "shedskin", Moves: mv("harden")},
				},
				team{{Species: "Alakazam", Ability: "magicguard", Moves: mv("substitute")}},
			)
			p.makeChoices("move uturn", "move substitute")
			p.species(p.mine(), "Venomoth", "U-turn should still pivot its user out after hitting a Substitute")
		})
	})
}
