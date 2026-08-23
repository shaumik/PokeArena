//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/state.js.
//
// Upstream nests this under `describe('State')` > `describe('Battles')`; the
// ledger key is built from the innermost describe, matching the PRNG port.
//
// Neither case is about a battle rule. Both drive Showdown's `State.normalize`
// / `Battle.toJSON` / `Battle.fromJSON` serialization layer, which this engine
// does not have: `engine.BattleState` is a plain struct the caller persists
// however it likes, there is no restart-from-JSON path, and no equivalent of
// the "unsupported type" guard the second case asserts on. There is no weaker
// version of either claim that a ported battle could state.

func TestMiscState(t *testing.T) {
	describe(t, "Battles", func(g *psg) {
		g.skip("should be able to be serialized and deserialized without affecting functionality (slow)",
			"different subsystem: this engine has no Battle.toJSON/fromJSON round-trip to compare against a control battle")

		g.skip("should require special treatment for complex objects",
			"different subsystem: this engine has no serializer, so there is no unsupported-type guard to trip")
	})
}
