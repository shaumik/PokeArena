//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/terastal.js.
//
// Nothing came across. Terastallization is not modeled: `set` has no teraType,
// there is no `terastallize` suffix in the choice grammar, and nothing in the
// engine overrides a Pokémon's types for the rest of a battle. Every case here
// measures a consequence of that override — the type swap and its persistence
// across a switch, the double STAB when the Tera type matches an original type,
// the way Adaptability stacks with it, and the floor that lifts a sub-60 BP
// same-Tera-type move to 60.
//
// The low-BP block is the one place a reader might expect a salvageable half:
// several of its cases measure a non-Tera baseline first and the Tera figure
// second. The baseline alone pins nothing about this file's subject, and every
// upstream figure is a level-100 damage range that does not transfer to a
// level-50 engine anyway, so the cases skip whole rather than being ported down
// to their first assertion.
//
// The file is `common.gen(9).createBattle` throughout, which is this engine's
// own data generation, so the generation is not the obstacle — Terastallization
// is. The nested `Buffing low BP move behavior` describe is flattened into a
// sibling block, keeping its own name as the ledger key.

func TestMiscTerastal(t *testing.T) {
	describe(t, "Terastallization", func(g *psg) {
		g.skip("should change the user's type to its Tera type after terastallizing", "Terastallization")
		g.skip("should persist the user's changed type after switching", "Terastallization")
		g.skip("should give STAB correctly to the user's old types", "Terastallization")
		g.skip("should give STAB correctly to the user's underlying types after changing forme", "Terastallization")
		g.skip("should combine with Adaptability for an overall STAB of x2.25", "Terastallization")
		g.skip("should not give the Adaptability boost on the user's old types", "Terastallization")
		g.skip("should allow hacked Megas to Terastallize in Hackmons play", "Terastallization")
	})

	describe(t, "Buffing low BP move behavior", func(g *psg) {
		g.skip("should boost the base power of weaker moves with the same Tera Type to 60 BP", "Terastallization")
		g.skip("should only boost base power 60 BP after all other base power modifiers are applied", "Terastallization")
		g.skip("should not boost priority moves with <60 BP", "Terastallization")
		g.skip("should not boost the base power of moves with variable base power under 60 BP", "Terastallization")
		g.skip("should boost STAB moves that weren't STAB moves prior to terastallizing", "Terastallization")
		g.skip("shouldn't boost non-STAB moves with <60 Base Power", "Terastallization")
		g.skip("shouldn't boost <60 Base Power priority moves forced via Encore", "Terastallization")
	})
}
