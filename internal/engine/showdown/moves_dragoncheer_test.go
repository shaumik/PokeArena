//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/dragoncheer.js.
//
// Dragon Cheer targets an ally, so seven of the eight cases are doubles and
// skip. Only "should fail in singles" survives, and it is the one case whose
// answer this engine could ever have: there is never an ally to cheer.
//
// Dragon Cheer is not in this dataset at all, so that surviving case reports
// the missing move rather than the failure message. That is the finding.

func TestMovesDragonCheer(t *testing.T) {
	describe(t, "Dragon Cheer", func(g *psg) {
		g.skip("should raise critical hit ratio by 2 stages for dragon types", "doubles")
		g.skip("should raise critical hit ratio by 1 stage for non-dragon types", "doubles")
		g.skip("should fail if used twice on the same ally", "doubles")
		g.skip("should not increase ratio if affected Pokemon turns into a Dragon Type after Dragon Cheer",
			"doubles")

		g.it("should fail in singles or if no ally exists", func(p *ps) {
			// Dragapult goes through its stand-in row (Gengar); it is only a
			// body holding Splash while the cheer has nobody to land on.
			//
			// Upstream's assertion is vacuous as written — `some(line =>
			// !line.startsWith('|-fail'))` is true of any log with a line in
			// it — so the port asserts what the case is named for instead.
			p.battle(
				team{{Species: "gyarados", Moves: mv("dragoncheer")}},
				team{{Species: "dragapult", Moves: mv("splash")}},
			)
			p.turn()
			p.logHas("But it failed!", "Dragon Cheer has no ally to cheer in singles and should fail")
		})

		g.skip("should be copied by Psych Up, using the target's Dragon Cheer level", "doubles")
		g.skip("should be copied by Psych Up, using the target's Dragon Cheer level and replacing the user's current critical hit stage",
			"doubles")
		g.skip("should be copied by Transform, using the target's Dragon Cheer level", "doubles")
	})
}
