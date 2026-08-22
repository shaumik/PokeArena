//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/relicsong.js.
//
// Relic Song is not in this dataset and Meloetta is not in this dex, so the two
// forme cases skip on the species: a forme change is the whole subject and
// there is nothing to stand in for it.
//
// The substitute case survives, because what it measures — a sound move
// reaching the Pokemon behind the doll rather than the doll — does not depend
// on Meloetta at all. It is written out with the move name intact so the run
// reports the missing move, which is the finding.
//
// Two things that case cannot state exactly. Upstream builds a level-2 Caterpie
// so a single Relic Song is lethal and the Focus Sash has something to save it
// from; level is fixed at 50 here, so the sash's trigger cannot be arranged and
// the port asserts the target was reached through the substitute instead.
// Victory Star is stripped from the thrower: it is incidental (Relic Song never
// misses in this fixture — the Lagging Tail is what the case actually needs),
// and leaving an unmodeled ability in the fixture would report a gap that has
// nothing to do with Relic Song.
//
// The Gen 5 block skips whole: this engine has one generation.

func TestMovesRelicSong(t *testing.T) {
	describe(t, "Relic Song", func(g *psg) {
		g.skip("should transform Meloetta into its Pirouette forme",
			"formes: Meloetta is not in this 80-species dex and forme changes are not modeled")

		g.skip("should transform Meloetta-Pirouette into its Aria forme",
			"formes: Meloetta is not in this 80-species dex and forme changes are not modeled")

		g.it("should pierce through substitutes", func(p *ps) {
			// Deoxys-Attack resolves through the shared table to Mewtwo — a
			// fast special attacker, which is all the thrower has to be, and
			// the Lagging Tail makes it move second regardless. Caterpie
			// resolves to Butterfree.
			p.battle(
				team{{
					Species: "Deoxys-Attack", Ability: "noability", Item: "laggingtail",
					Moves: mv("splash", "relicsong"),
				}},
				team{{
					Species: "Caterpie", Ability: "naturalcure", Item: "focussash",
					Moves: mv("substitute", "rest"),
				}},
			)
			p.makeChoices("move splash", "move substitute")
			p.makeChoices("move relicsong", "move rest")
			p.damaged(p.foe(), "a sound move should have reached the Pokemon behind the substitute")
		})
	})

	describe(t, "Relic Song [Gen 5]", func(g *psg) {
		g.skip("should not pierce through substitutes", "gen 5 mechanics")

		g.skip("should transform Meloetta into its Pirouette forme even if it hits a substitute",
			"gen 5 mechanics")
	})
}
