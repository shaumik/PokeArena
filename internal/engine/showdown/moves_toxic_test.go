//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/toxic.js.
//
// Naganadel becomes Arbok, which is what the case actually needs: a Poison
// type, since that is the condition on Toxic never missing. The Dragon half is
// not preserved and nothing reads it; the ability is stripped so Intimidate
// does not act where upstream's set does nothing.
//
// Sigilyph becomes Hypno carrying Wonder Skin. The only in-dex species with
// Wonder Skin natively is Venomoth, which is Poison and therefore immune to
// Toxic outright — that would answer the case for the wrong reason — so the
// ability is set on a Psychic body instead. Sigilyph's Flying half is lost and
// nothing here reads it.
//
// Sleep Talk is not in this dataset and is idle here, so it is Splash.
//
// The Gen 7 block skips: it is a doubles battle in an older generation, and
// this engine has neither.

func TestMovesToxic(t *testing.T) {
	describe(t, "Toxic", func(g *psg) {
		g.it("should always hit when used by a Poison-type", func(p *ps) {
			p.battle(
				team{{Species: "Naganadel", As: "Arbok", Ability: "noability", Moves: mv("toxic")}},
				team{{Species: "Sigilyph", As: "Hypno", Ability: "wonderskin", Moves: mv("splash")}},
			)
			p.makeChoices("move toxic", "move splash")
			p.hasStatus(p.foe(), "tox",
				"Toxic from a Poison-type should bypass accuracy, Wonder Skin included")
		})
	})

	describe(t, "Toxic [Gen 7]", func(g *psg) {
		g.skip("should set all moves to sure-hit until the end of the turn", "gen 7 mechanics")
	})
}
