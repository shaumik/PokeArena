//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/redcard.js.
//
// Both cases are doubles: each depends on a partner using Destiny Bond or
// standing by while the card drags a specific benched Pokemon in, and the second
// asserts on the identity of the second active slot. There is no second slot
// here, so both skip. (Red Card is also absent from this item set.)

func TestItemsRedCard(t *testing.T) {
	describe(t, "Red Card", func(g *psg) {
		g.skip("should not trigger if the target should be KOed from Destiny Bond and also not crash the client",
			"doubles")

		g.skip("should trigger if the target is still in battle", "doubles")
	})
}
