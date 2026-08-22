//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/lashout.js.
//
// Two of the five cases need an ally to do the stat-lowering and skip.
//
// How the damage assertions travel. Upstream states each one as an absolute
// window at level 100 and says in a comment what the window would be if the
// move had not doubled — the two windows are a clean factor of two apart, and
// separating them is the whole assertion. Absolute figures do not survive a
// fixed level of 50, and a ratio between two measured hits is not safe either,
// because a 1-in-24 critical hit multiplies one of them by 1.5 and there is no
// rigged-RNG hook here to stop it. So each case instead sets the target's
// starting HP to a figure that sits between the two windows *including* their
// critical-hit versions, and asks whether the target faints. That is the same
// question upstream asks, stated so that no seed can answer it differently.
//
// The windows were measured against this engine: at +0 Attack a plain Lash Out
// takes 105-124 (157-186 on a crit) and a doubled one 210-248, so 198 HP
// separates them; at -1 Attack the figures are 70-83 (105-124 on a crit) and
// 140-166, so 130 HP separates them.
//
// Species. Wynaut resolves to Hypno and Blissey to Chansey through the shared
// table. Neither typing matters — Lash Out is Dark, unresisted by both, and
// neither body is same-typed with it.
//
// The Lagging Tail in the first case is not upstream's. Upstream relies on
// Blissey outspeeding Wynaut so that Fake Tears lands before Lash Out; the two
// stand-ins are the other way round, and the item is the smallest thing that
// restores the ordering the case depends on.
//
// Shedinja has no stand-in row, and rightly — Wonder Guard is its identity. It
// is not being used for Wonder Guard here, only as something that dies to the
// first hit so the Intimidate body has to come in as a replacement, so Golbat
// at 1 HP takes the role.

func TestMovesLashOut(t *testing.T) {
	describe(t, "Lash Out", func(g *psg) {
		g.it("should double in base power if the user's stats were lowered this turn", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Item: "laggingtail", Moves: mv("lashout")}},
				team{{Species: "Blissey", HP: 198, Moves: mv("faketears")}},
			)
			p.makeChoices("move lashout", "move faketears")
			p.fainted(p.foe(),
				"a doubled Lash Out clears 198 HP and an undoubled one cannot, even on a crit")
		})

		g.skip("should double in base power if the user's stats were lowered this turn by an ally",
			"doubles")

		g.it("should double in base power if the user's stats were lowered at the start of the match", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "shellarmor", Moves: mv("lashout")}},
				team{{Species: "Blissey", Ability: "intimidate", HP: 130, Moves: mv("skillswap")}},
			)
			p.makeChoices("move lashout", "move skillswap")
			p.statStage(p.mine(), "atk", -1, "Intimidate should have fired before the move")
			p.fainted(p.foe(),
				"at -1 Attack a doubled Lash Out clears 130 HP and an undoubled one cannot, even on a crit")
		})

		g.it("should not double in base power if the user's stats were lowered at a switch after a KO", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Ability: "shellarmor", Moves: mv("lashout")}},
				team{
					{Species: "Shedinja", As: "Golbat", Ability: "noability", HP: 1, Moves: mv("splash")},
					{Species: "Blissey", Ability: "intimidate", HP: 130, Moves: mv("skillswap")},
				},
			)
			p.makeChoices("move lashout", "move splash")
			p.makeChoices("", "switch 2")
			p.makeChoices("move lashout", "move skillswap")
			p.statStage(p.mine(), "atk", -1, "the replacement's Intimidate should have fired")
			p.notFainted(p.foe(),
				"the drop happened on the replacement, not this turn, so Lash Out should not have doubled")
		})

		g.skip("should double in base power even if stat resets are reset by Haze", "doubles")
	})
}
