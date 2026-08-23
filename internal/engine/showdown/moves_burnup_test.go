//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/burnup.js.
//
// Upstream reaches the state the case is about — a Burn Up user that is no
// longer a Fire type — in two steps it has tools for and this harness does
// not: a first Burn Up sheds Moltres's Fire typing, then `customRules:
// 'guaranteedsecondarymod'` plus a pinned seed makes Ice Beam freeze on
// demand. There is no guaranteed-secondary rule and no RNG hook here, and Ice
// Beam's freeze is 10%, so neither step is reproducible.
//
// The port states the precondition directly instead: Zapdos is the same Kanto
// legendary bird with Moltres's Flying half and no Fire half, it holds Burn
// Up, and it starts frozen. That is exactly "the user of Burn Up is not a Fire
// type, and it is frozen", which is what the assertion is about. What it gives
// up is the first half of upstream's setup, so this port says nothing about
// whether Burn Up removes the user's Fire typing at all — and this engine does
// not model that, or any user-side defrost.
//
// It is a rate case because the freeze cannot be held still: a frozen Pokemon
// rolls a 20% natural thaw every time it tries to move, so over the two turns
// upstream plays, correct behavior leaves it frozen about 64% of the time. A
// Burn Up that defrosted its user would read 0%.
//
// Piplup's Ice Beam is gone with the rest of the freeze setup, so the second
// side is pure filler; Blastoise stands in as a water body of the same role.

func TestMovesBurnUp(t *testing.T) {
	describe(t, "Burn Up", func(g *psg) {
		g.itRate("should not thaw the user if it is not a Fire type", 0.4, 0.9, 200, func(p *ps) bool {
			p.battle(
				team{{Species: "Moltres", As: "Zapdos", Status: "frz", Moves: mv("burnup")}},
				team{{Species: "Piplup", As: "Blastoise", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return false
			}
			p.makeChoices("move burnup", "move splash")
			p.makeChoices("move burnup", "move splash")
			return p.mine().Status == "freeze"
		})
	})
}
