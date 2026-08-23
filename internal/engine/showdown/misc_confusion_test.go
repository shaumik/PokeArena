//go:build showdown

package showdown

import (
	"strings"
	"testing"
)

// Ported from test/sim/misc/confusion.js.
//
// Upstream forces the self-hit with `forceRandomChance: true` and pins the
// damage inside [150, 177]. That figure is a level-100 Deoxys-Attack and
// transfers nowhere: this engine is fixed at level 50 and builds Mewtwo in
// Deoxys-Attack's place, a different Attack and Defense entirely. The claim
// underneath the figure does transfer — confusion damage is a fixed 40-power
// hit off the raw Attack and Defense, so neither Huge Power nor a Life Orb may
// touch it — so the case plays the same battle twice, once with both modifiers
// and once with neither, and requires the first self-hit to cost the same in
// both.
//
// Deoxys-Attack and Sableye both come from the stand-in table (Mewtwo and
// Gengar). Nothing here turns on what those rows drop, because the comparison
// is a body against itself.
//
// Two departures from the fixture. `sleeptalk` is not in this dataset and is
// pure filler upstream, so `splash` stands in for it. And Sableye is given
// `splash` as a second move so that Confuse Ray is only re-used once the
// confusion has worn off: it has 10 PP, and running it dry would drop the
// confuser into Struggle, whose damage would contaminate the HP reading the
// case takes.

func TestMiscConfusion(t *testing.T) {
	describe(t, "Confusion", func(g *psg) {
		g.it("should not be affected by modifiers like Huge Power or Life Orb", func(p *ps) {
			// firstSelfHit plays until the confused side hits itself and
			// returns what that cost it, or -1 if it never did.
			firstSelfHit := func() int {
				for i := 0; i < 20; i++ {
					if p.state() == nil || p.state().Ended() {
						break
					}
					confuse := "move confuseray"
					if p.mine().Volatiles.Confusion != nil {
						confuse = "move splash"
					}
					before := p.mine().HP
					p.makeChoices("move splash", confuse)
					if strings.Contains(p.lastTurnText(), "hurt itself in its confusion") {
						return before - p.mine().HP
					}
				}
				return -1
			}

			p.battle(
				team{{Species: "Deoxys-Attack", Ability: "hugepower", Item: "lifeorb", Moves: mv("splash")}},
				team{{Species: "Sableye", Ability: "prankster", Moves: mv("confuseray", "splash")}},
			)
			modified := firstSelfHit()

			// The same battle with the Attack modifier and the item removed, as
			// the baseline the comparison needs. Not an upstream battle.
			p.battle(
				team{{Species: "Deoxys-Attack", Ability: "noability", Moves: mv("splash")}},
				team{{Species: "Sableye", Ability: "prankster", Moves: mv("confuseray", "splash")}},
			)
			plain := firstSelfHit()

			p.atLeast(modified, 1, "the confused Pokemon never hit itself, so the case measured nothing")
			p.atLeast(plain, 1, "the confused Pokemon never hit itself, so the case measured nothing")
			p.equal(modified, plain, "Huge Power and a Life Orb must not change confusion damage")
		})
	})
}
