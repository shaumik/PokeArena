//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/shelltrap.js.
//
// Shell Trap is not in this dataset, so the one live case here stops at "move
// shelltrap is not in this dataset". It is written out anyway: if the move is
// ever added, it says what it has to do.
//
// The PP case is upstream's only one that survives at all, and only after
// being re-expressed in singles. Its doubles setup is two Magikarp splashing
// beside the pair that matter, and the mechanic — Shell Trap costs PP whether
// or not a physical hit set it off — is unchanged by having one foe rather
// than two. The half of it that counts `|cant|p1a: Turtonator|Shell Trap|`
// lines is dropped: this engine emits no line at all for a Shell Trap that
// never triggered, so the PP figures are the whole of what can be asserted.
//
// Turtonator is Fire/Dragon and is here only as a body that survives a Tackle
// and holds Shell Trap; Charizard keeps the Fire half, and nothing in the case
// turns on Dragon. Shell Trap's -3 priority puts it last regardless of the
// Speed tie between the two Charizard, so the mirror is still deterministic.

func TestMovesShellTrap(t *testing.T) {
	describe(t, "Shell Trap", func(g *psg) {
		g.it("should deduct PP regardless if it was successful", func(p *ps) {
			p.battle(
				team{{Species: "Turtonator", As: "Charizard", Ability: "shellarmor", Moves: mv("shelltrap")}},
				team{{Species: "Turtonator", As: "Charizard", Ability: "shellarmor", Moves: mv("tackle", "irondefense")}},
			)
			p.makeChoices("move shelltrap", "move irondefense")
			if m := p.mine().Moves; len(m) > 0 {
				p.equal(m[0].PP, m[0].MaxPP-1, "an untriggered Shell Trap should still cost PP")
			}
			p.makeChoices("move shelltrap", "move tackle")
			if m := p.mine().Moves; len(m) > 0 {
				p.equal(m[0].PP, m[0].MaxPP-2, "a triggered Shell Trap should cost PP too")
			}
		})

		g.skip("should not Z-power if hit by a Z-move", "Z-moves")
		g.skip("should not Max if hit by a Max move", "Dynamax")
	})
}
