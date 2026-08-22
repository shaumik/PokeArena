//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/receiver.js.
//
// Both upstream cases are `it.skip` — they are disabled in Showdown itself —
// and both are doubles battles built around an ally fainting so its ability
// can be inherited. There is no second active slot here, so they skip for that
// reason as well.

func TestAbilitiesReceiver(t *testing.T) {
	describe(t, "Receiver", func(g *psg) {
		g.skip("should gain a boost immediately if taking over a KO boost Ability",
			"doubles (and skipped upstream too)")
		g.skip("should do weird stuff with multiple Soul-Heart and multiple Receiver",
			"doubles (and skipped upstream too)")
	})
}
