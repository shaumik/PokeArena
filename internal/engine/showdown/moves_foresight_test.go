//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/foresight.js.
//
// Upstream sets the evasion stages the last two cases need with
// battle.boost(...), which has no harness counterpart. Both are re-staged
// through play: six Double Teams for +6, six Defogs for -6. The order upstream
// chose is preserved — Foresight lands first, the evasion moves after — which
// matters, because this engine zeroes positive evasion once when Foresight is
// applied as well as ignoring it live, and boosting first would let the case
// pass on the wrong half of that. Each also gains one assertion upstream does
// not have, checking the stage actually reached +6 or -6, so that a Double
// Team or Defog that did nothing cannot leave the case passing vacuously.
//
// Substitutions. Smeargle is the usual inert body and Chansey stands in for it
// through the shared table; its tiny Attack makes for small hits, but every
// assertion here is "did anything land at all". Dusknoir is a Ghost that keeps
// healing itself, and Gengar keeps the Ghost half — the Poison it adds is
// neutral to both Normal and Fighting, so the immunity the case is about is
// unchanged. Prankster comes off with it: it is not in this ability set, and
// it was only ensuring Recover moved first, which Gengar outrunning Chansey
// already does. Forretress and Zapdos come through unchanged (Magneton stands
// in for the first), and Zapdos being Electric means Zap Cannon's guaranteed
// paralysis cannot slow it and change the move order mid-case.

func TestMovesForesight(t *testing.T) {
	describe(t, "Foresight", func(g *psg) {
		g.it("should negate Normal and Fighting immunities", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("foresight", "vitalthrow", "tackle")}},
				team{{Species: "Dusknoir", As: "Gengar", Ability: "noability", Moves: mv("recover")}},
			)
			p.makeChoices("move foresight", "move recover")
			p.makeChoices("move vitalthrow", "move recover")
			p.damaged(p.foe(), "Foresight should let a Fighting move reach a Ghost")
			p.makeChoices("move tackle", "move recover")
			p.damaged(p.foe(), "Foresight should let a Normal move reach a Ghost")
		})

		g.it("should ignore the effect of positive evasion stat stages", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("avalanche", "foresight", "splash")}},
				team{{Species: "Forretress", Ability: "sturdy", Moves: mv("synthesis", "doubleteam")}},
			)
			p.makeChoices("move foresight", "move synthesis")
			for i := 0; i < 6; i++ {
				p.makeChoices("move splash", "move doubleteam")
			}
			p.statStage(p.foe(), "evasion", 6, "the six Double Teams should have maxed the target's evasion")
			for i := 0; i < 7; i++ {
				p.makeChoices("move avalanche", "move synthesis")
				p.damaged(p.foe(), "an identified target's positive evasion should not make a move miss")
			}
		})

		g.it("should not ignore the effect of negative evasion stat stages", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("zapcannon", "dynamicpunch", "foresight", "defog")}},
				team{{Species: "Zapdos", Ability: "owntempo", Moves: mv("roost")}},
			)
			p.makeChoices("move foresight", "move roost")
			for i := 0; i < 6; i++ {
				p.makeChoices("move defog", "move roost")
			}
			p.statStage(p.foe(), "evasion", -6, "the six Defogs should have bottomed out the target's evasion")
			for i := 0; i < 7; i++ {
				p.makeChoices("move zapcannon", "move roost")
				p.damaged(p.foe(), "Foresight should leave the target's negative evasion working for the attacker")
			}
		})
	})
}
