//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/target-resolution.js.
//
// Nothing came across. Every case in the file builds a battle with more than
// one active Pokémon per side — sixteen doubles, two triples, one free-for-all
// — because the question the file asks only exists there: which slot a move
// lands on once its chosen target has fainted, vanished, swapped with an ally,
// or been redirected. This engine's Side.Active is a single index, so there is
// no slot to resolve and no `move watergun 2` / `move watergun -2` in the
// choice grammar. The mechanics the cases lean on (Ally Switch, Follow Me,
// Storm Drain and Stalwart redirection, Snipe Shot's redirection immunity) are
// all ally- or slot-shaped for the same reason.
//
// The two triples cases are additionally gen-modded (gen 5 and gen 6) and the
// last two Ally Switch cases sit on gen 8, but the active-slot count is the
// blocker that would remain in gen 9, so that is the reason recorded.
//
// The upstream file nests three describe blocks inside `Target Resolution` and
// then adds four more cases at file level; the nested blocks are flattened
// into sibling describes here, keeping their own names as the ledger keys.

func TestMiscTargetResolution(t *testing.T) {
	describe(t, "Targeted Pokémon fainted in-turn", func(g *psg) {
		g.skip("should redirect 'any' from a fainted foe to a targettable foe", "doubles")
		g.skip("should not redirect 'any' from a fainted ally to another Pokémon by default", "doubles")
		// Trailing space is upstream's, and the ledger key is byte-for-byte.
		g.skip("should support RedirectTarget event for a fainted foe and type 'any' ", "triples")
		g.skip("should not redirect non-pulse/flying moves in Triples if the Pokemon is out of range", "triples")
		g.skip("should support RedirectTarget event for a fainted ally and type 'any'", "doubles")
		g.skip("should not redirect to another random target if the intended one is fainted in FFA", "free-for-all")
	})

	describe(t, "Targeted slot is empty", func(g *psg) {
		g.skip("should redirect 'any' from a fainted foe to a targettable foe", "doubles")
		g.skip("should not redirect 'any' from a fainted ally to another Pokémon by default", "doubles")
		g.skip("should support RedirectTarget event for a fainted foe and type 'any'", "doubles")
		g.skip("should support RedirectTarget event for a fainted ally and type 'any'", "doubles")
	})

	describe(t, "Smart-tracking targeting effects", func(g *psg) {
		g.skip("should allow Stalwart to follow its target after an opposing Ally Switch", "doubles")
		g.skip("should allow Stalwart to bypass Storm Drain redirection", "doubles")
		g.skip("should allow Stalwart to bypass Follow Me redirection", "doubles")
		g.skip("should allow Stalwart to correctly target a Pokemon which switched out and back in another slot", "doubles")
		g.skip("should allow Snipe Shot to follow its target after an opposing Ally Switch", "doubles")
	})

	describe(t, "Target Resolution", func(g *psg) {
		g.skip("should not force charge moves called by another move to target an ally after Ally Switch", "doubles")
		g.skip("Ally Switch should cause single-target moves to fail if targeting an ally", "doubles")
		g.skip("charge moves like Phantom Force should target slots turn 1 and Pokemon turn 2", "doubles")
		g.skip("should cause Rollout to target the same slot after being called as a submove", "doubles")
	})
}
