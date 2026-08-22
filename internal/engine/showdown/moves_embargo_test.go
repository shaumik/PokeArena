//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/embargo.js.
//
// Species. Lopunny goes to Kangaskhan through the shared table (Normal, and
// nothing about it interacts with Embargo). Giratina has no stand-in row and is
// only the Embargo user here, so it is built as Hypno. Sableye's row gives
// Gengar; the row says Prankster is not preserved and this engine does not
// model it either, but Gengar outspeeds Kangaskhan on its own, so Embargo still
// lands before the Fling it is supposed to stop — see the case.
//
// Items. Sea Incense is not in this 128-item set and is incidental upstream —
// any held item makes Fling a legal attempt — so Leftovers is thrown instead.
// Belly Drum is not in this dataset at all, and that absence is the finding the
// first case reports.

func TestMovesEmbargo(t *testing.T) {
	describe(t, "Embargo", func(g *psg) {
		g.it("should negate residual healing events", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Ability: "limber", Item: "leftovers", Moves: mv("bellydrum")}},
				team{{Species: "Giratina", As: "Hypno", Ability: "pressure", Moves: mv("embargo")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move bellydrum", "move embargo")
			lopunny := p.mine()
			p.equal(lopunny.HP, (lopunny.MaxHP+1)/2,
				"Leftovers should not have healed the half-HP Belly Drum user under Embargo")
		})

		g.it("should prevent items from being consumed", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Ability: "limber", Item: "chopleberry", Moves: mv("bulkup")}},
				team{{Species: "Golem", Ability: "noguard", Moves: mv("embargo", "lowkick")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move bulkup", "move embargo")
			p.makeChoices("move bulkup", "move lowkick")
			p.equal(p.mine().Item, "chopleberry",
				"a Chople Berry under Embargo should not be eaten by a super-effective Fighting hit")
		})

		g.it("should ignore the effects of items that disable moves", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Ability: "limber", Item: "assaultvest", Moves: mv("protect")}},
				team{{Species: "Golem", Ability: "noguard", Moves: mv("embargo")}},
			)
			if p.state() == nil {
				return
			}
			// Upstream reads lastMove; this harness can ask which moves are
			// choosable, which is the same question one step earlier.
			p.cantMove(0, "protect", "an Assault Vest should lock out the holder's only status move")
			p.makeChoices("default", "move embargo")
			// Upstream reads lastMove.id. This engine's Struggle is a synthetic
			// move with no id, and the "last move" volatile is only written for
			// moves that have one, so the narration is where a Struggle shows.
			p.logHas("Struggle", "with Protect locked out, the only remaining action is Struggle")

			p.canMove(0, "protect", "Embargo should switch the Assault Vest off")
			p.makeChoices("default", "move embargo")
			p.equal(p.mine().Volatiles.LastMoveID, "protect",
				"Protect should be usable once the Assault Vest is under Embargo")
		})

		g.it("should cause Fling to fail", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Ability: "limber", Item: "leftovers", Moves: mv("fling")}},
				team{{Species: "Sableye", As: "Gengar", Ability: "noability", Moves: mv("embargo")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move fling", "move embargo")
			p.holdsItem(p.mine(), "Fling should fail under Embargo, leaving the item in place")
			p.equal(p.mine().Item, "leftovers", "the item Fling failed to throw should still be held")
		})

		g.skip("should not prevent Pokemon from Mega Evolving", "mega evolution")
	})
}
