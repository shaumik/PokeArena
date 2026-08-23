//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/terablast.js.
//
// Three of the four cases Terastallize, which this engine does not model, and
// skip. The remaining one asks the opposite question — that Tera Blast stays
// Special when the user has *not* Terastallized, even with the higher Attack
// stat — and that is expressible here.
//
// It needs one re-expression. Upstream reads `lastMove.category` off the
// Pokemon; this harness has no way to ask which category a move resolved as, so
// the category is read out of the damage instead. Cloyster's Defense is three
// times its Sp. Def, so a special Tera Blast takes about half its HP while a
// physical one — even off Dragonite's larger Attack, boosted by Dragon Dance —
// takes about a third. Shell Armor keeps a crit from muddying it, and the
// target answers the measured turn with Splash rather than a second Protect,
// which upstream can afford because it never looks at the damage.
//
// Regidrago is built as Dragonite for the same reason upstream picks Regidrago:
// the case wants the user's Attack to be the higher stat, and Dragonite's is
// (134 to 100) before Dragon Dance even lands. Regirock does have a stand-in
// row (Golem), but this case turns on the target's Def-to-Sp. Def gap and on
// taking neutral damage from a Normal-type move, and Golem gives neither;
// Cloyster gives both. Dragon's Maw is not modeled and is not set — Tera Blast
// is Normal-type here, so it would not apply anyway.

func TestMovesTeraBlast(t *testing.T) {
	describe(t, "Tera Blast", func(g *psg) {
		g.skip(`should be a special attack when base stats are tied`, "Terastallization")
		g.skip(`should be a physical attack when terastallized with higher attack stat`, "Terastallization")

		g.it(`should be a special attack when not terastallized, even if attack stat is higher`, func(p *ps) {
			p.battle(
				team{{Species: "regidrago", As: "Dragonite", Moves: mv("terablast", "dragondance")}},
				team{{Species: "regirock", As: "Cloyster", Ability: "shellarmor", Moves: mv("protect", "splash")}},
			)
			if p.state() == nil {
				return
			}
			// Dragon Dance boosts the user's Attack stat.
			p.makeChoices("move dragondance", "move protect")
			target := p.foe()
			before := target.HP
			// However, the user is not terastallized when using Tera Blast.
			p.makeChoices("move terablast", "move splash")
			p.atLeast(before-target.HP, target.MaxHP*2/5,
				"Tera Blast should read Sp. Atk, not the Dragon Dance-boosted Attack")
		})

		g.skip(`should be a special attack when terastallized even if target ignores stat changes`, "Terastallization")
	})
}
