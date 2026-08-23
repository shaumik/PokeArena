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
// not have, checking the stage really reached +6 or -6, so a Double Team or
// Defog that did nothing cannot leave the case passing vacuously.
//
// Upstream's targets spend the measuring turns healing (Synthesis, Roost) so
// that each `notEqual(hp, maxhp)` is a fresh signal rather than a leftover
// from the first hit. This engine gives both moves 5 PP — it applies no PP Ups
// — and neither would last the length of these cases. The port gets the same
// guarantee by measuring the HP change turn by turn instead, and the targets
// spend those turns on Splash. Zap Cannon's own 5 PP is why the last case
// makes five attempts where upstream makes seven; five consecutive hits of a
// 50%-accuracy move, on every seed, is still only reachable if evasion is
// being counted.
//
// Substitutions. Smeargle is the usual inert body and Chansey stands in for it
// through the shared table; its tiny Attack makes for small hits, but every
// assertion here is "did anything land at all". Dusknoir is a Ghost that keeps
// healing itself, and Gengar keeps the Ghost half — the Poison it adds is
// neutral to both Normal and Fighting, so the immunity the case is about is
// unchanged. Prankster comes off with it: it is not in this ability set, and
// it was only ensuring Recover moved first, which Gengar outrunning Chansey
// already does. Forretress builds as Magneton and Zapdos is in the dex;
// Zapdos being Electric is worth keeping, since it means Zap Cannon's
// guaranteed paralysis cannot slow it and reorder the case underneath itself.

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
				team{{Species: "Forretress", Ability: "sturdy", Moves: mv("doubleteam", "splash")}},
			)
			p.makeChoices("move foresight", "move splash")
			for i := 0; i < 6; i++ {
				p.makeChoices("move splash", "move doubleteam")
			}
			p.statStage(p.foe(), "evasion", 6, "the six Double Teams should have maxed the target's evasion")
			for i := 0; i < 7; i++ {
				p.hurts(p.foe(), func() { p.makeChoices("move avalanche", "move splash") },
					"an identified target's positive evasion should not make a move miss")
			}
		})

		g.it("should not ignore the effect of negative evasion stat stages", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo", Moves: mv("zapcannon", "dynamicpunch", "foresight", "defog")}},
				team{{Species: "Zapdos", Ability: "owntempo", Moves: mv("splash")}},
			)
			p.makeChoices("move foresight", "move splash")
			for i := 0; i < 6; i++ {
				p.makeChoices("move defog", "move splash")
			}
			p.statStage(p.foe(), "evasion", -6, "the six Defogs should have bottomed out the target's evasion")
			for i := 0; i < 5; i++ {
				p.hurts(p.foe(), func() { p.makeChoices("move zapcannon", "move splash") },
					"Foresight should leave the target's negative evasion working for the attacker")
			}
		})
	})
}
