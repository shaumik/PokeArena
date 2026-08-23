//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/download.js.
//
// Porygon carries Download in this dex, so the ability side of the file ports
// straight across. Two substitutions on the other side: Stonjourner is not
// here, and Onix takes its place because the case needs a body whose Defense
// is far above its Sp. Def (180 to 65) and nothing else; Furret is a plain
// Normal body, so Raticate takes it, named directly because neither species
// has a stand-in row.
//
// One timing difference. This engine fires the leads' switch-in hooks on the
// first turn rather than at construction, so where upstream reads the boost
// straight off a freshly built battle, the port takes a turn first. Both
// sides use Splash, so nothing else happens in it.
//
// The nested [Gen 4] block skips as a block.

func TestAbilitiesDownload(t *testing.T) {
	describe(t, "Download", func(g *psg) {
		g.it("should boost based on which defensive stat is higher", func(p *ps) {
			p.battle(
				team{
					{Species: "Porygon", Ability: "download", Moves: mv("splash")},
					{Species: "Raticate", Moves: mv("splash")},
				},
				team{
					{Species: "Onix", Moves: mv("splash")},
					{Species: "Chansey", Moves: mv("splash")},
				},
			)
			p.turn()
			p.statStage(p.mine(), "spa", 1, "Onix's Defense is the higher of the two")
			p.makeChoices("switch raticate", "switch chansey")
			p.makeChoices("switch porygon", "")
			p.statStage(p.mine(), "atk", 1, "Chansey's Sp. Def is the higher of the two")
		})

		g.it("should boost Special Attack if both stats are tied", func(p *ps) {
			p.battle(
				team{{Species: "Porygon", Ability: "download", Moves: mv("splash")}},
				team{{Species: "Mew", Moves: mv("splash")}},
			)
			p.turn()
			p.statStage(p.mine(), "spa", 1, "")
			p.statStage(p.mine(), "atk", 0, "")
		})

		g.skip("should boost based on the total of both foes in a Double Battle", "doubles")

		g.it("should trigger even if the foe is behind a Substitute", func(p *ps) {
			p.battle(
				team{
					{Species: "Raticate", Moves: mv("splash")},
					{Species: "Porygon", Ability: "download", Moves: mv("splash")},
				},
				team{{Species: "Blissey", Moves: mv("substitute")}},
			)
			p.turn()
			p.makeChoices("switch porygon", "")
			p.statStage(p.mine(), "atk", 1, "a Substitute should not hide the foe's stats from Download")
		})
	})

	describe(t, "[Gen 4]", func(g *psg) {
		g.skip("should not trigger if the foe is behind a Substitute", "gen 4 mechanics")
		g.skip("in Double Battles, should only account for foes not behind a Substitute", "gen 4 mechanics")
		g.skip("should ignore the effect of the Simple ability", "gen 4 mechanics")
	})
}
