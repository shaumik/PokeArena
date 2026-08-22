//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/imprison.js.
//
// The second describe block, `Maybe locked and maybe disabled`, is mostly
// about Showdown's request protocol — `maybeDisabled`, `maybeLocked`,
// `choice.cantUndo`, `undoChoice`, the `testfight` probe. None of that exists
// here: this engine answers "what may I pick" with LegalActions and has no
// choice preview and no undo, so those cases skip and say so. Its two cases
// with a mechanical core underneath the protocol are ported to that core —
// which moves are choosable after Imprison — and say what was dropped. Its
// nested describes are nested here the way they are upstream, so the ledger
// keys stay the strings upstream wrote.
//
// Prankster is not among the abilities this engine implements, so the run
// records it. It is left on the fixtures as upstream wrote it, and the two
// cases whose turn order actually depended on it get the order from Speed
// instead: the imprisoner is Alakazam (base 120) and the foes it must outrun
// are built as Hypno (base 67) rather than through Abra's stand-in row, which
// would have made both sides the same Alakazam and left the order to a coin
// flip. Where the order does not matter, the stand-in row is used unchanged.
//
// Two engine differences shape the first case. A self-switch resolves its
// replacement inside the move, so upstream's separate `switch 2` request after
// Baton Pass has no counterpart; and the engine emits no line when a Pokemon
// is left with Struggle, so "all its moves are sealed" is asserted as "none of
// them is choosable". Upstream's `sleeptalk` filler is not in this dataset,
// so `splash` is the do-nothing move.

func TestMovesImprison(t *testing.T) {
	describe(t, "Imprison", func(g *psg) {
		g.it("should prevent foes from using moves that the user knows", func(p *ps) {
			p.battle(
				team{
					{Species: "Abra", Ability: "prankster", Moves: mv("imprison", "calmmind", "batonpass")},
					{Species: "Kadabra", Ability: "prankster", Moves: mv("imprison", "calmmind")},
				},
				team{
					{Species: "Abra", As: "Hypno", Ability: "synchronize", Moves: mv("calmmind", "gravity")},
					{Species: "Kadabra", As: "Hypno", Ability: "prankster", Moves: mv("imprison", "calmmind")},
				},
			)
			p.makeChoices("move imprison", "move calmmind")
			p.logHas("sealed any moves its opponent shares with it", "")
			p.statStage(p.foe(), "spa", 0, "Calm Mind should have been sealed")
			p.statStage(p.foe(), "spd", 0, "Calm Mind should have been sealed")
			p.cantMove(1, "calmmind", "Calm Mind is one of the imprisoner's moves")

			// Imprison does not end when the foe switches, and a replacement
			// whose whole moveset is sealed is left with Struggle.
			p.makeChoices("", "switch 2")
			p.cantMove(1, "calmmind", "Imprison should survive the foe switching out")
			p.cantMove(1, "imprison", "every move the replacement knows is sealed")

			// Imprison is not passed by Baton Pass. Upstream's `auto` resolves
			// to Struggle here, because the foe's whole moveset is sealed at
			// the moment it chooses; naming Struggle outright keeps that true
			// even on an engine that seals less than it should, where `auto`
			// would pick Imprison and the foe imprisoning back would make the
			// rest of the case unreadable.
			p.makeChoices("move batonpass", "move struggle")
			p.canMove(1, "calmmind", "Baton Pass should not have carried Imprison to the replacement")
			p.makeChoices("move calmmind", "move calmmind")
			p.statStage(p.foe(), "spa", 1, "the foe should be free to boost again")

			// Imprison also ends when its user leaves the field.
			p.makeChoices("switch 1", "move calmmind")
			p.statStage(p.foe(), "spa", 2, "")
			p.makeChoices("move calmmind", "move calmmind")
			p.statStage(p.foe(), "spa", 3, "")
		})

		g.skip("should not prevent foes from using Z-Powered Status moves", "Z-moves")

		g.it("should not prevent the user from using moves that a foe knows", func(p *ps) {
			p.battle(
				team{{Species: "Abra", Ability: "prankster", Moves: mv("imprison", "calmmind", "batonpass")}},
				team{{Species: "Abra", Ability: "synchronize", Moves: mv("calmmind", "gravity")}},
			)
			imprisonUser := p.mine()

			p.makeChoices("move imprison", "")
			p.makeChoices("move calmmind", "")
			p.statStage(imprisonUser, "spa", 1, "Imprison should not seal its own user's moves")
			p.statStage(imprisonUser, "spd", 1, "Imprison should not seal its own user's moves")
		})
	})

	describe(t, "Maybe locked and maybe disabled", func(g *psg) {
		describe(t, "Singles", func(g *psg) {
			g.skip("should not show Imprisoned moves as disabled",
				"the request protocol's maybeDisabled / maybeLocked flags have no counterpart here")

			g.it("should disable moves as the user uses them", func(p *ps) {
				// Upstream's subject is the reveal: a move is only reported
				// disabled once the client has tried it. There is no request
				// object here, so what ports is the state underneath — after
				// Imprison the shared moves are unchoosable and the unshared
				// one still is.
				p.battle(
					team{{Species: "Abra", Moves: mv("imprison", "tackle", "growl")}},
					team{{Species: "Abra", Moves: mv("splash", "tackle", "growl")}},
				)
				p.makeChoices("move imprison", "move splash")
				p.cantMove(1, "tackle", "Tackle is shared with the imprisoner")
				p.cantMove(1, "growl", "Growl is shared with the imprisoner")
				p.canMove(1, "splash", "Splash is not one of the imprisoner's moves")
			})

			g.it("should lock the user into Struggle if all moves are Imprisoned", func(p *ps) {
				p.battle(
					team{{Species: "Abra", Moves: mv("imprison", "splash", "tackle")}},
					team{{Species: "Abra", Moves: mv("splash", "tackle")}},
				)
				p.makeChoices("move imprison", "move splash")
				p.cantMove(1, "splash", "")
				p.cantMove(1, "tackle", "")
				// No Struggle line is emitted, so the lock is read off the
				// consequence: the foe's default action is Struggle, which
				// damages the imprisoner.
				p.makeChoices("", "")
				p.damaged(p.mine(), "a foe with every move sealed should have been left with Struggle")
			})

			g.skip("should not allow the user to cancel a move",
				"choice cancellation is not a concept in this engine")
		})

		describe(t, "Doubles' left position", func(g *psg) {
			g.skip("should show disabled moves and should allow to cancel them", "doubles")
		})

		describe(t, "Doubles' right position", func(g *psg) {
			g.skip("should not show Imprisoned moves as disabled", "doubles")
			g.skip("should not allow the user to cancel if it gets locked into Struggle", "doubles")
			g.skip("should allow the user to cancel a non-disabled move", "doubles")
			g.skip("Test Fight should force Struggle if all moves are Imprisoned", "doubles")
			g.skip("Test Fight should reveal disabled moves", "doubles")
		})
	})
}
