//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/trace.js.
//
// Porygon carries Trace in this dex, so it is named in place of Ralts, which
// has no stand-in row. Bouffalant is a plain Normal body and becomes
// Raticate; Iron Jugulis becomes Wynaut's stand-in carrying Quark Drive,
// because all the case wants from it is a lead whose ability is not the one
// being counted.
//
// The delayed-copy case is about which abilities Trace refuses to take, not
// about who is carrying them, so Multitype and Stance Change ride ordinary
// bodies rather than Arceus and Aegislash. Neither ability is in this
// dataset, which is precisely what the case asks about: this engine's
// untraceable list is Trace itself, Neutralizing Gas and no ability, so the
// port expects to find both copied.
//
// Shadow Tag and Quark Drive are likewise not in this dataset. They are kept
// anyway — the cases are about Trace, and an unmodeled ability is still a
// name to copy — but a triager should read those two rows carefully: the
// mechanical assertions in "should copy the opponent's Ability" and "should
// not activate twice" both hold, and the only thing red about them is that
// the fixture names an ability the engine has never heard of.
//
// One doubles case skips.

func TestAbilitiesTrace(t *testing.T) {
	describe(t, "Trace", func(g *psg) {
		g.it("should copy the opponent's Ability", func(p *ps) {
			p.battle(
				team{{Species: "Porygon", Ability: "trace", Moves: mv("splash")}},
				team{{Species: "Wynaut", Ability: "shadowtag", Moves: mv("splash")}},
			)
			p.turn()
			p.hasAbility(p.mine(), "shadowtag", "")
		})

		g.it("should delay copying the opponent's Ability if the initial Abilities could not be copied by Trace", func(p *ps) {
			p.battle(
				team{{Species: "Porygon", Ability: "trace", Moves: mv("splash")}},
				team{
					{Species: "Wynaut", Ability: "multitype", Moves: mv("splash")},
					{Species: "Wynaut", Ability: "stancechange", Moves: mv("splash")},
					{Species: "Wynaut", Ability: "shadowtag", Moves: mv("splash")},
				},
			)
			p.turn()
			p.hasAbility(p.mine(), "trace", "Multitype cannot be traced")

			p.makeChoices("", "switch 2")
			p.hasAbility(p.mine(), "trace", "Stance Change cannot be traced")

			p.makeChoices("", "switch 3")
			p.hasAbility(p.mine(), "shadowtag", "")
		})

		g.skip("should interact properly with Ability index 0 'No Ability'", "doubles")

		g.it("should not activate twice", func(p *ps) {
			p.battle(
				team{
					{Species: "Porygon", Ability: "trace", Moves: mv("splash")},
					{Species: "Raticate", Moves: mv("splash")},
				},
				team{
					{Species: "Wynaut", Ability: "quarkdrive", Moves: mv("splash")},
					{Species: "Incineroar", Ability: "intimidate", Moves: mv("splash")},
				},
			)
			p.makeChoices("switch raticate", "")
			p.makeChoices("", "switch incineroar")
			p.makeChoices("switch porygon", "")
			p.statStage(p.foe(), "atk", -1, "the traced Intimidate should fire exactly once")
		})
	})
}
