//go:build showdown

package showdown

import "testing"

// Ported from test/sim/rulesets/inversebattle.js.
//
// Nothing came across. Every case builds its battle with `{inverseMod: true}`,
// which flips the type chart — resistances become weaknesses, weaknesses become
// resistances, immunities become weaknesses. This engine has no format-rule
// layer and no second type chart, so there is nothing for the flag to select.
//
// The five `should not affect ...` cases are the ones worth explaining, because
// each of them would compile and go green if translated literally: Levitate
// still blocks Earthquake, Magnet Rise still blocks Earthquake, Freeze-Dry is
// still super effective on Water, and a Flying-type is still ungrounded. But
// what those cases assert is that the inverse mod leaves those four rules
// alone, and with no inverse mod in force the assertion measures the ordinary
// rule instead — a green case answering a question nobody asked. They skip for
// the same reason as the rest.
//
// Secondary blockers, none of them the deciding one: Absol, Dusknoir, Mismagius,
// Klefki, Floatzel, Talonflame, Gourgeist, Hawlucha, Volcarona, Staraptor and
// Terapagos-Terastal have neither dex entries nor stand-in rows, Mega Rayquaza
// and Tera Shell are not modeled, and Snore, Flying Press and Wicked Blow are
// not in the 538-move set.

func TestRulesetsInverseBattle(t *testing.T) {
	describe(t, "Inverse Battle", func(g *psg) {
		g.skip("should change natural resistances into weaknesses",
			"inverse battle is not a format rule this engine has")
		g.skip("should change natural weaknesses into resistances",
			"inverse battle is not a format rule this engine has")
		g.skip("should negate natural immunities and make them weaknesses",
			"inverse battle is not a format rule this engine has")
		g.skip("should affect Stealth Rock damage",
			"inverse battle is not a format rule this engine has")
		g.skip("should affect the resistance of Delta Stream",
			"inverse battle is not a format rule this engine has")
		g.skip("should make Ghost/Grass types take neutral damage from Flying Press",
			"inverse battle is not a format rule this engine has")
		g.skip("should not affect ability-based immunities",
			"inverse battle is not a format rule this engine has")
		g.skip("should not affect move-based immunities",
			"inverse battle is not a format rule this engine has")
		g.skip("should not affect the type effectiveness of Freeze Dry on Water-type Pokemon",
			"inverse battle is not a format rule this engine has")
		g.skip("should not affect the \"ungrounded\" state of Flying-type Pokemon",
			"inverse battle is not a format rule this engine has")
		g.skip("should let Tera Shell take not very effective damage",
			"inverse battle is not a format rule this engine has")
	})
}
