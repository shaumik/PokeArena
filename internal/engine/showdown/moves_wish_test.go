//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/wish.js.
//
// The first case is doubles — it is entirely about which slot a Wish belongs to
// when there are two — and skips. The third needs the battle's turn counter set
// to 255 by hand, which the harness cannot do and which no amount of play can
// reach here (Splash runs out of PP after 40 turns), so it skips too.
//
// The two that remain are both about a Wish whose slot is empty or dead when it
// comes due, and both are written out even though a move each of them needs is
// missing from this dataset — Thousand Arrows in the first, Stone Axe in the
// second. Those absences are the finding.
//
// Species. Pichu resolves to Raichu, Wynaut to Hypno and Happiny to Chansey
// through the shared table; Parasect is in the dex. Zygarde has no row and is
// only the attacker that knocks the Wish's caster out, so Rhydon takes the role
// (Ground is kept, Dragon is not). Shedinja has no row either, and rightly —
// Wonder Guard is its identity — but it is used here only as something that
// dies the moment it arrives, so Golbat at 1 HP takes that role.
//
// Prankster and Aura Break are dropped for "noability". Neither is modeled, and
// neither is load-bearing with these stand-ins: Raichu already outspeeds Rhydon,
// so the Wish is cast before the knockout either way.
//
// Upstream's second `switch 2` in the last case relies on Showdown reordering a
// side's list so the active is always first. This engine keeps team slots fixed,
// so the same step is written as `switch 1`.

func TestMovesWish(t *testing.T) {
	describe(t, "Wish", func(g *psg) {
		g.skip("should heal the Pokemon in the user's slot by 1/2 of the user's max HP 1 turn after use",
			"doubles")

		g.it("should progress its duration whether or not the Pokemon in its slot is fainted", func(p *ps) {
			p.battle(
				team{
					{Species: "Pichu", Ability: "noability", Moves: mv("wish")},
					{Species: "Parasect", Ability: "effectspore", Moves: mv("splash")},
				},
				team{{Species: "Zygarde", As: "Rhydon", Ability: "noability",
					Moves: mv("thousandarrows")}},
			)
			p.makeChoices("move wish", "move thousandarrows")
			p.makeChoices("switch 2", "")
			p.makeChoices("move splash", "move thousandarrows")
			p.fullHP(p.mine(), "the Wish should have come due on the replacement")
		})

		g.skip("should never resolve when used on a turn that is a multiple of 256n - 1",
			"the harness cannot set the battle's turn counter, and 255 turns of play are not "+
				"reachable — Splash runs out of PP first")

		g.it("should do nothing if no Pokemon is present to heal from Wish", func(p *ps) {
			p.battle(
				team{
					{Species: "Wynaut", Moves: mv("splash", "wish")},
					{Species: "Shedinja", As: "Golbat", Ability: "noability", HP: 1, Moves: mv("splash")},
				},
				team{{Species: "Happiny", Ability: "noguard", Moves: mv("splash", "stoneaxe")}},
			)
			wynaut := p.slot(0, 1)
			p.makeChoices("move wish", "move stoneaxe")
			p.makeChoices("switch 2", "move splash")
			p.makeChoices("switch 1", "")
			p.damaged(wynaut, "Wish should not have healed Wynaut even after it was KOed.")
			p.turn()
			p.damaged(wynaut, "Wish should not have healed Wynaut later either.")
		})
	})
}
