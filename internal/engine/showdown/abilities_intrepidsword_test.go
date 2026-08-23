//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/intrepidsword.js.
//
// Intrepid Sword is not in this ability set, which is what these cases are
// here to record, so they are ported rather than skipped. Zacian is not in the
// dex and has no stand-in, but it is only the body the ability sits on;
// Wigglytuff keeps the Fairy half and the set names the ability explicitly, so
// nothing about Wigglytuff's own ability or stats reaches the assertions.
// Belly Drum and Sleep Talk are not in this move set; the second case keeps
// Belly Drum because the whole case is about switching in already at +6, and
// Sleep Talk is replaced by Splash everywhere because it is pure filler here.
//
// Two harness differences show up in the choice strings. This engine does not
// reorder a side's team when it switches, so the second switch of a pair names
// the slot it is going back to rather than repeating "switch 2". And Baton
// Pass resolves its own replacement inside the move, so upstream's separate
// `makeChoices('switch 2')` after a pass has no counterpart.

func TestAbilitiesIntrepidSword(t *testing.T) {
	describe(t, "Intrepid Sword", func(g *psg) {
		g.it(`should only increase the user's Attack stat once per game`, func(p *ps) {
			p.battle(
				team{
					{Species: "Zacian", As: "Wigglytuff", Ability: "intrepidsword", Moves: mv("splash")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
				team{{Species: "Mew", Moves: mv("splash")}},
			)
			zacian := p.slot(0, 1)
			p.statStage(zacian, "atk", 1, "Intrepid Sword should fire on the first switch-in")
			p.makeChoices("switch 2", "")
			p.makeChoices("switch 1", "")
			p.statStage(zacian, "atk", 0, "the boost is spent and should not come back")
		})

		g.it(`should use up its once-per-game boost if it switches in with +6 Attack`, func(p *ps) {
			p.battle(
				team{
					{Species: "Wynaut", Moves: mv("bellydrum", "batonpass")},
					{Species: "Zacian", As: "Wigglytuff", Ability: "intrepidsword", Moves: mv("splash")},
				},
				team{{Species: "Mew", Moves: mv("splash")}},
			)
			p.makeChoices("move bellydrum", "")
			p.makeChoices("move batonpass", "")
			p.makeChoices("switch 1", "")
			p.makeChoices("switch 2", "")
			p.statStage(p.slot(0, 2), "atk", 0,
				"an Attack that could not be raised any further still spends the boost")
		})

		g.it(`should not use up its once-per-game boost if it switches in while its Ability is suppressed`, func(p *ps) {
			p.battle(
				team{
					{Species: "Wynaut", Moves: mv("batonpass")},
					{Species: "Zacian", As: "Wigglytuff", Ability: "intrepidsword", Moves: mv("splash")},
				},
				team{{Species: "Mew", Moves: mv("splash", "gastroacid")}},
			)
			p.makeChoices("move batonpass", "move gastroacid")
			zacian := p.slot(0, 2)
			p.statStage(zacian, "atk", 0, "a suppressed Intrepid Sword should not fire")
			p.makeChoices("switch 1", "")
			p.makeChoices("switch 2", "")
			p.statStage(zacian, "atk", 1, "and should not have been spent either")
		})

		g.skip(`should be able to increase the user's Attack stat multiple times per game [Gen 8]`,
			"gen 8 mechanics")
	})
}
