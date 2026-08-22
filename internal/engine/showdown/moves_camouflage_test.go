//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/camouflage.js.
//
// Camouflage is not in this dataset, so the one live case here stops at
// "move camouflage is not in this dataset". That absence is the finding.
//
// The whole file is written as a set of generation comparisons: each case
// builds the same battle two or three times under different gen mods and reads
// the type off the user. This engine has one generation of data, so only the
// modern third of the first case can be stated at all — on a plain field with
// no terrain, Camouflage makes the user Normal. The Gen 4 and Gen 5 battles in
// the same case are dropped, and the case's parenthetical about Generation V
// therefore cannot be checked.
//
// Ralts is a body that does nothing; Mr. Mime is this dex's Psychic/Fairy, the
// same typing, and `splash` stands in for upstream's `sleeptalk`.

func TestMovesCamouflage(t *testing.T) {
	describe(t, "Camouflage", func(g *psg) {
		g.it("should change the user to Normal-type (except in Generation V, to Ground-type)", func(p *ps) {
			p.battle(
				team{{Species: "wynaut", Moves: mv("camouflage")}},
				team{{Species: "ralts", As: "Mr. Mime", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.equal(p.mine().Type1, "Normal", "Camouflage on a plain field should leave the user Normal-type")
		})

		g.skip("should fail on Multitype in Gen 4 and Arceus itself in Gen 5+",
			"Arceus is not in this 80-species dex and Multitype is not modeled")
		g.skip("should fail in Gen 3-4 if the user already has what Camouflage would change to as either of its types",
			"gen 4 mechanics")
	})
}
