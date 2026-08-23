//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/magician.js.
//
// Magician is not in this dataset, so the seven singles cases are real
// questions: the four "should steal" ones are expected to be red and the
// three "should not steal" ones pass vacuously.
//
// Klefki is not in the dex and has no stand-in row; Magneton takes its place,
// which keeps the Steel typing Flash Cannon is used off. Hatterene likewise
// has no row: the Weakness Policy case uses Articuno, chosen because Flash
// Cannon is super effective on Ice and Articuno survives the hit with room to
// spare even on a critical, and the Throat Spray case uses Mr. Mime, which
// preserves Hatterene's psychic/fairy exactly and only has to stand there.
//
// TR69 is a TR — an item with no battle effect, picked upstream so nothing
// but the theft is observable. Shed Shell plays that part here.
//
// Level 1 bodies become explicit HP: the two cases that need someone to faint
// set the victim to 1 HP rather than dropping a level the engine does not
// have. The Dragon Tail case gives the target No Guard so the 90%-accurate
// move cannot whiff — upstream pins that with a seed, which this harness does
// not offer.

func TestAbilitiesMagician(t *testing.T) {
	describe(t, "Magician", func(g *psg) {
		g.it("should steal the opponents item", func(p *ps) {
			p.battle(
				team{{Species: "Magneton", Ability: "magician", Moves: mv("flashcannon")}},
				team{{Species: "Wynaut", Item: "shedshell", Moves: mv("splash")}},
			)
			p.turn()
			p.equal(p.mine().Item, "shedshell", "")
		})

		g.it("should steal the opponents item if the target faints", func(p *ps) {
			p.battle(
				team{{Species: "Magneton", Ability: "magician", Moves: mv("flashcannon")}},
				team{
					{Species: "Wynaut", Item: "shedshell", Moves: mv("splash"), HP: 1},
					{Species: "Wynaut", Moves: mv("splash")},
				},
			)
			p.makeChoices("move flashcannon", "move splash")
			p.fainted(p.foe(), "the 1 HP body should have gone down")
			p.equal(p.mine().Item, "shedshell", "")
		})

		g.it("should not steal the opponents item if the user faints", func(p *ps) {
			p.battle(
				team{
					{Species: "Magneton", Ability: "magician", Moves: mv("tackle"), HP: 1},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Wynaut", Item: "rockyhelmet", Moves: mv("falseswipe")}},
			)
			p.makeChoices("move tackle", "move falseswipe")
			p.fainted(p.mine(), "Rocky Helmet should have killed the attacker mid-move")
			p.noItem(p.mine(), "")
			p.holdsItem(p.foe(), "")
		})

		g.it("should steal the opponents item if the user uses U-turn", func(p *ps) {
			// U-turn's switch resolves inside the same turn here, so the
			// stolen item is read off the pivot on the bench rather than off
			// the active slot upstream still has it in.
			p.battle(
				team{
					{Species: "Magneton", Ability: "magician", Moves: mv("uturn")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Wynaut", Item: "shedshell", Moves: mv("splash")}},
			)
			p.turn()
			p.equal(p.slot(0, 1).Item, "shedshell", "")
		})

		g.it("should steal the opponents item if the user uses Dragon Tail", func(p *ps) {
			p.battle(
				team{{Species: "Magneton", Ability: "magician", Moves: mv("dragontail")}},
				team{
					{Species: "Wynaut", Ability: "noguard", Item: "shedshell", Moves: mv("splash")},
					{Species: "Wynaut", Ability: "noguard", Moves: mv("splash")},
				},
			)
			p.turn()
			p.equal(p.mine().Item, "shedshell", "")
		})

		g.it("should not steal Weakness Policy on super-effective hits", func(p *ps) {
			p.battle(
				team{{Species: "Magneton", Ability: "magician", Moves: mv("flashcannon")}},
				team{{Species: "Articuno", Item: "weaknesspolicy", Moves: mv("splash")}},
			)
			p.turn()
			p.logHas("super effective", "the hit has to be super effective for the Policy to fire")
			p.noItem(p.foe(), "Weakness Policy should have been consumed")
			p.noItem(p.mine(), "Klefki should not have stolen Weakness Policy.")
		})

		g.it("should not steal an item on the turn Throat Spray activates", func(p *ps) {
			p.battle(
				team{{Species: "Magneton", Ability: "magician", Item: "throatspray", Moves: mv("psychicnoise")}},
				team{{Species: "Mr. Mime", Item: "shedshell", Moves: mv("splash")}},
			)
			p.turn()
			p.noItem(p.mine(), "Klefki should not have stolen an item.")
		})

		g.skip("should steal the item from the faster opponent hit", "doubles")
	})
}
