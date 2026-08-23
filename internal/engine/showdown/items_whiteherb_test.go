//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/whiteherb.js.
//
// Three of the five cases are doubles — Costar, two simultaneous Intimidates,
// and Opportunist on a switch-in all need a second active slot — and skip. The
// two singles cases come across.
//
// Parting Shot is not in this dataset. In the first case it is the subject, so
// it is kept and the team failing to build naming it is the finding; in the
// third case it sits on a bench Pokemon that never moves, so Splash stands in
// for it there. Sleep Talk, also missing and inert everywhere it appears here,
// is likewise Splash.
//
// Species. Litten and Torracat have no stand-in row and are Fire bodies with no
// bearing on the herb, so Flareon and Arcanine take their places. Wynaut goes
// through its row.
//
// Levels. The third case gives Litten `level: 1` so the Draco Meteor that
// triggers Grim Neigh is a guaranteed KO. Level is fixed at 50 here, so the port
// sets that Pokemon's starting HP to 1 instead, which is the same guarantee.
// Grim Neigh itself is not one of this engine's 118 abilities and reports
// itself; note that without it the White Herb still has a Draco Meteor drop to
// answer, so a green result here does not mean Grim Neigh was involved.

func TestItemsWhiteHerb(t *testing.T) {
	describe(t, "White Herb", func(g *psg) {
		g.it("should activate after Parting Shot drops both stats, but before the switch is resolved", func(p *ps) {
			p.battle(
				team{
					{Species: "Torracat", As: "Arcanine", Moves: mv("partingshot")},
					{Species: "Litten", As: "Flareon", Moves: mv("splash")},
				},
				team{{Species: "Wynaut", Item: "whiteherb", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			holder := p.foe()
			p.noItem(holder, "the herb should have been spent")
			p.statStage(holder, "atk", 0, "")
			p.statStage(holder, "spa", 0, "")
		})

		g.skip("should activate after Costar", "doubles")

		g.it("should activate after Abilities that boost stats on KOs", func(p *ps) {
			p.battle(
				team{
					{Species: "Litten", As: "Flareon", Ability: "noguard", Moves: mv("splash"), HP: 1},
					{Species: "Torracat", As: "Arcanine", Moves: mv("splash")},
				},
				team{{Species: "Wynaut", Item: "whiteherb", Ability: "grimneigh", Moves: mv("dracometeor")}},
			)
			p.turn()
			holder := p.foe()
			p.noItem(holder, "the herb should have been spent")
			p.statStage(holder, "spa", 0, "the herb should settle up after the KO boost, not before it")
		})

		g.skip("should activate after two Intimidate switch in at the same time", "doubles")
		g.skip("should activate before Opportunist during switch-ins", "doubles")
	})
}
