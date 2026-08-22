//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/round.js.
//
// Species. Deoxys-Attack takes its stand-in row (Mewtwo, psychic and
// offensive) and Caterpie takes its own (Butterfree, the same line evolved).
// Upstream's `level: 2` on the target has no counterpart — this engine is fixed
// at level 50 — and that costs the case its original marker: at level 2 the
// sound move one-shots the target through its Substitute, so upstream can read
// "the Focus Sash was spent" as proof the hit reached the holder. At level 50
// nothing in this dex is frail enough for that, so the port reads the piercing
// directly instead: the target itself loses HP over the turn, and no log line
// says the doll absorbed anything. The Focus Sash stays in the fixture, inert.
//
// Lagging Tail is doing the same job as upstream — it holds the attacker to
// last, so the target's Rest restores it to full before the measured hit and
// "damaged" cannot be confused with the quarter the Substitute cost.
//
// Victory Star is not in this dataset. Upstream picks it only to keep
// Deoxys-Attack's own ability out of the way, so the port strips the ability
// instead of reporting an ability the case does not care about.
//
// The Gen 5 block skips as a block: it exists to pin the older rule that sound
// moves did not pierce a Substitute, and this engine models one generation.

func TestMovesRound(t *testing.T) {
	describe(t, "Round", func(g *psg) {
		g.it("should pierce through substitutes", func(p *ps) {
			p.battle(
				team{{Species: "Deoxys-Attack", Ability: "noability", Item: "laggingtail",
					Moves: mv("splash", "round")}},
				team{{Species: "Caterpie", Ability: "naturalcure", Item: "focussash",
					Moves: mv("substitute", "rest")}},
			)
			p.makeChoices("move splash", "move substitute")
			p.ok(p.foe().Volatiles.Substitute != nil, "the Substitute should be up before the sound move")
			p.makeChoices("move round", "move rest")
			p.damaged(p.foe(), "Round is a sound move and should have reached Caterpie itself")
			p.logLacks("substitute took the damage", "the Substitute should not have absorbed a sound move")
		})
	})

	describe(t, "Round [Gen 5]", func(g *psg) {
		g.skip("should not pierce through substitutes", "gen 5 mechanics")
	})
}
