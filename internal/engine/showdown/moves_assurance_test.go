//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/assurance.js.
//
// Upstream states both live cases as absolute damage at level 100, which does
// not transfer to an engine fixed at level 50. Each is restated as a pair of
// identically built battles that differ only in whether the target lost HP
// before Assurance landed, and the assertion is the ratio between the two
// hits. The bands allow for the damage roll landing differently in each half,
// so "doubled" survives as [165%, 240%] and "unchanged" as [80%, 125%] — far
// enough apart to tell the two answers apart.
//
// Upstream also hands the first target +6 Attack through battle.boostBy, which
// has no harness counterpart. The boost is only there to make Wild Charge's
// recoil large; any recoil at all is enough for the mechanic, so the port
// leaves it out and instead backs the recoil out of the turn's HP arithmetic
// the way upstream does — recoil is a quarter of the damage Wild Charge dealt.
//
// Substitutions. This dex has no Dark type at all, so Assurance never gets
// STAB here; that scales both halves of a ratio equally and so does not touch
// the answer. Sneasel is a fast frail attacker and Persian shares its Speed
// tier exactly. Regieleki is a very fast Electric body that hurts itself with
// Wild Charge; Jolteon is the fastest Electric in the dex and still outruns
// Persian, which is all the case needs. Shell Armor stays on it, as upstream
// wrote it, so a critical hit cannot move the ratio. Greedent and Wailord are
// a Pain Split pair whose only requirements are that the Pain Split user moves
// first and starts with the larger max HP; Lickitung keeps Greedent's Normal
// typing and Lapras supplies both.

func TestMovesAssurance(t *testing.T) {
	describe(t, "Assurance", func(g *psg) {
		g.it("should double its base power if the target already took damage this turn", func(p *ps) {
			// Wild Charge's recoil is the target damaging itself this turn.
			p.battle(
				team{{Species: "Sneasel", As: "Persian", Ability: "sturdy", Moves: mv("assurance")}},
				team{{Species: "Regieleki", As: "Jolteon", Ability: "shellarmor", Moves: mv("wildcharge", "splash")}},
			)
			me, foe := p.mine(), p.foe()
			before := foe.HP
			p.makeChoices("move assurance", "move wildcharge")
			// The turn cost the target both its own recoil and the Assurance
			// hit; only the second is being measured.
			recoil := (me.MaxHP - me.HP) / 4
			boosted := (before - foe.HP) - recoil

			p.battle(
				team{{Species: "Sneasel", As: "Persian", Ability: "sturdy", Moves: mv("assurance")}},
				team{{Species: "Regieleki", As: "Jolteon", Ability: "shellarmor", Moves: mv("wildcharge", "splash")}},
			)
			foe = p.foe()
			before = foe.HP
			p.makeChoices("move assurance", "move splash")
			plain := before - foe.HP

			p.atLeast(plain, 1, "the control Assurance should have done damage")
			if plain > 0 {
				p.bounded(100*boosted/plain, 165, 240,
					"Assurance should double against a target that already took damage this turn")
			}
		})

		g.skip("should double the power against damaged Pokemon, not damaged slots", "doubles")

		g.it("should not double its base power if the target lost HP due to Pain Split", func(p *ps) {
			p.battle(
				team{{Species: "Greedent", As: "Lickitung", Moves: mv("assurance")}},
				team{{Species: "Wailord", As: "Lapras", Ability: "shellarmor", Moves: mv("painsplit", "splash")}},
			)
			foe := p.foe()
			p.makeChoices("move assurance", "move painsplit")
			// Pain Split has already leveled the two HP totals by the time
			// Assurance lands, so the hit is whatever sits below that level.
			afterSplit := (p.mine().MaxHP+foe.MaxHP)/2 - foe.HP

			p.battle(
				team{{Species: "Greedent", As: "Lickitung", Moves: mv("assurance")}},
				team{{Species: "Wailord", As: "Lapras", Ability: "shellarmor", Moves: mv("painsplit", "splash")}},
			)
			foe = p.foe()
			before := foe.HP
			p.makeChoices("move assurance", "move splash")
			plain := before - foe.HP

			p.atLeast(plain, 1, "the control Assurance should have done damage")
			if plain > 0 {
				p.bounded(100*afterSplit/plain, 80, 125,
					"Pain Split is not damage, so Assurance should not double")
			}
		})
	})
}
