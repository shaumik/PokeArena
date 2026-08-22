//go:build showdown

package showdown

import (
	"strings"
	"testing"
)

// Ported from test/sim/misc/critical.js.
//
// Upstream pins the coin flips with `forceRandomChance: true` so the confused
// Chansey always hits itself, then asserts two things about that turn: the
// self-hit happened, and no `|-crit` line was emitted. There is no rigging hook
// here, so the case becomes a sweep. The rate it measures is upstream's first
// assertion — how often a self-hit landed at all — which has to be nearly every
// seed or the case is watching nothing. The second assertion is made inside the
// body, so it holds on all 200 seeds and a single critical self-hit anywhere in
// the sweep fails the case.
//
// No attacking move is ever chosen, so "A critical hit!" has exactly one
// possible source: the self-hit. That is what makes the log assertion mean what
// upstream's `|-crit` scan meant.
//
// Zubat keeps its Golbat stand-in, and Chansey is in the dex already. Lucky
// Punch and Super Luck are kept because they are the point of the fixture —
// upstream's way of making a crit as likely as it can be — and Super Luck is
// not modeled here, which the run reports. That report currently ends the case
// on its first seed, so the sweep below only starts measuring once the ability
// exists; dropping it to get a green sweep would be measuring a self-hit at the
// base crit rate, which is not what upstream asked.
//
// Two departures from the fixture, both to keep PP from deciding the case. Zubat
// is given `splash` so Confuse Ray is only re-used once the confusion has worn
// off (10 PP), and Chansey's Soft-Boiled is replaced by `splash` (5 PP would not
// cover the turns this sweep needs). Running either move dry would drop that
// side into Struggle, which deals damage and can crit for reasons that have
// nothing to do with a self-hit. Chansey never needs the healing: its own
// confusion damage is a few percent of its HP a turn.

func TestMiscCritical(t *testing.T) {
	describe(t, "Critical hits", func(g *psg) {
		g.itRate("should not happen on self-hits", 0.9, 1.0, 200, func(p *ps) bool {
			p.battle(
				team{{Species: "Zubat", Moves: mv("confuseray", "splash")}},
				team{{Species: "Chansey", Item: "luckypunch", Ability: "superluck", Moves: mv("splash")}},
			)
			selfHit := false
			for i := 0; i < 12; i++ {
				if p.state() == nil || p.state().Ended() {
					break
				}
				confuse := "move confuseray"
				if p.foe().Volatiles.Confusion != nil {
					confuse = "move splash"
				}
				p.makeChoices(confuse, "move splash")
				if strings.Contains(p.lastTurnText(), "hurt itself in its confusion") {
					selfHit = true
				}
			}
			p.logLacks("A critical hit!", "a confusion self-hit must never be a critical hit")
			return selfHit
		})
	})
}
