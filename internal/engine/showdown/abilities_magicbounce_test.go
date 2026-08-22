//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/magicbounce.js.
//
// Magic Bounce is not in this dataset, so every case here is a real question
// rather than a skip; the ability name is reported by the run and the
// assertions say what should have followed from it.
//
// Species. Espeon and Xatu are not in this dex and have no stand-in row.
// Alakazam takes Espeon's place — a fast, frail Psychic body, which is all
// either side of these cases asks of it — and Mr. Mime takes Xatu's, losing
// the Flying half that nothing here turns on. Spoink becomes Hypno, another
// Psychic body with no bearing on the result.
//
// Moves. Future Sight is not in this dataset and is only filler for the
// bouncer's turn, so it is Recover instead.
//
// The choice-lock case gains one assertion upstream leaves implicit: that the
// Growl really was bounced. Without it the case passes for free while Magic
// Bounce is missing — nothing is bounced, so nothing can choice-lock — which
// is the one outcome a port must not produce.
//
// The two free-for-all cases and the two semi-invulnerable doubles cases have
// no singles form.

func TestAbilitiesMagicBounce(t *testing.T) {
	describe(t, "Magic Bounce", func(g *psg) {
		g.it("should bounce Growl", func(p *ps) {
			p.battle(
				team{{Species: "Bulbasaur", Ability: "overgrow", Moves: mv("growl")}},
				team{{Species: "Espeon", As: "Alakazam", Ability: "magicbounce", Moves: mv("recover")}},
			)
			p.makeChoices("move growl", "move recover")
			p.statStage(p.mine(), "atk", -1, "Growl should have been bounced back at its user")
			p.statStage(p.foe(), "atk", 0, "the Magic Bounce holder should be untouched")
		})

		g.it("should bounce once when target and source share the ability", func(p *ps) {
			p.battle(
				team{{Species: "Xatu", As: "Mr. Mime", Ability: "magicbounce", Moves: mv("roost")}},
				team{{Species: "Espeon", As: "Alakazam", Ability: "magicbounce", Moves: mv("growl")}},
			)
			p.makeChoices("move roost", "move growl")
			p.statStage(p.mine(), "atk", 0, "the bouncer should be untouched")
			p.statStage(p.foe(), "atk", -1, "a bounced move should not be bounced a second time")
		})

		g.it("should not cause a choice-lock", func(p *ps) {
			p.battle(
				team{
					{Species: "Spoink", As: "Hypno", Ability: "thickfat", Moves: mv("bounce")},
					{
						Species: "Xatu", As: "Mr. Mime", Item: "choicescarf", Ability: "magicbounce",
						Moves: mv("roost", "growl"),
					},
				},
				team{{Species: "Espeon", As: "Alakazam", Ability: "magicbounce", Moves: mv("growl", "recover")}},
			)
			p.makeChoices("switch 2", "move growl")
			p.statStage(p.foe(), "atk", -1, "the Growl should have been bounced back before anything can lock")
			p.canMove(0, "roost", "bouncing a move must not lock the Choice item")
			p.canMove(0, "growl", "bouncing a move must not lock the Choice item")
			p.makeChoices("move roost", "move recover")
			p.notEqual(p.mine().Volatiles.LastMoveID, "growl", "the bounced Growl is not the bouncer's last move")
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Bulbasaur", Ability: "moldbreaker", Moves: mv("growl")}},
				team{{Species: "Espeon", As: "Alakazam", Ability: "magicbounce", Moves: mv("recover")}},
			)
			p.makeChoices("move growl", "move recover")
			p.statStage(p.mine(), "atk", 0, "Mold Breaker should stop the bounce")
			p.statStage(p.foe(), "atk", -1, "Mold Breaker should stop the bounce")
		})

		g.skip("should not bounce moves while semi-invulnerable", "doubles")
		g.skip("should only activate for the fastest Pokemon in a Free-for-all battle", "free-for-all")
		g.skip("should activate from fastest to slowest based on unmodified speed", "free-for-all")
		g.skip("[Gen 5] should bounce moves that target the foe's side while semi-invulnerable", "gen 5 mechanics")
	})
}
