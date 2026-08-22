//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/pastelveil.js.
//
// Pastel Veil is not one of this engine's 118 abilities, so the one case that
// survives the format reports it, and that report is the finding.
//
// Five of the six cases are doubles, and not incidentally so: each asserts on
// the ally as well as on the holder, which is the half of Pastel Veil a
// singles engine has no way to state. They skip.
//
// The sixth asserts only on the holder — its ally is an inert body that never
// enters an assertion — so it ports to singles unchanged. Ponyta's stand-in is
// Rapidash; Croagunk becomes Muk, which keeps the poison typing and takes Mold
// Breaker explicitly as upstream does.
//
// It is pinned to one seed because Toxic is 90% accurate here and nothing in
// this dataset poisons on every hit, so under the usual five-seed replay the
// case would be measuring the accuracy roll.

func TestAbilitiesPastelVeil(t *testing.T) {
	describe(t, "Pastel Veil", func(g *psg) {
		g.skip("should prevent itself and its allies from becoming poisoned", "doubles")
		g.skip("should remove poison on itself and allies when switched in", "doubles")
		g.skip("should remove poison on itself and allies when the ability is acquired via Skill Swap", "doubles")
		g.skip("should prevent a poison originating from an ally", "doubles")
		g.skip("should be bypassed by Mold Breaker and cured afterwards, but not for the ally", "doubles")

		g.itSeed("should only check for Pastel Veil cures after Lum/Pecha Berry", 1,
			"Toxic is 90% accurate and no move in this dataset poisons on every hit",
			func(p *ps) {
				p.battle(
					team{{Species: "ponyta", Ability: "pastelveil", Item: "lumberry", Moves: mv("splash")}},
					team{{Species: "croagunk", As: "Muk", Ability: "moldbreaker", Moves: mv("toxic")}},
				)
				p.makeChoices("move splash", "move toxic")
				p.noStatus(p.mine(), "the poison should have been cured")
				p.noItem(p.mine(), "the Lum Berry should have been the thing that cured it")
			})
	})
}
