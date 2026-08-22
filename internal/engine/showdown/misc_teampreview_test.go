//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/teampreview.js.
//
// Nothing in this file crosses. All three cases read the `|poke|` protocol
// lines Showdown emits during the team-preview phase and ask whether the forme
// suffix was masked behind `-*`. This engine has no team-preview phase to emit
// those lines and no forme layer for them to hide, and every species involved
// (Pumpkaboo, Gourgeist, Dudunsparce, Silvally, Urshifu, Zacian, Zamazenta,
// Arceus) is outside the 80-species dex, so there is nothing left of the case
// to state a weaker version of.

func TestMiscTeamPreview(t *testing.T) {
	describe(t, "Team Preview", func(g *psg) {
		g.skip("should hide formes of certain Pokemon",
			"team preview is not a phase in this engine and formes are not modeled")

		g.skip("should hide Arceus formes [Gen 8]",
			"gen 8 mechanics: team preview is not a phase in this engine, and Arceus is not in this 80-species dex")

		g.skip("should not hide formes of hacked Zacian/Zamazenta formes",
			"team preview is not a phase in this engine and formes are not modeled")
	})
}
