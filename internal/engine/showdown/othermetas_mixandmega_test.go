//go:build showdown

package showdown

import "testing"

// Ported from test/sim/othermetas/mixandmega.js.
//
// Nothing came across. The single case hands a Mimikyu a Metagrossite, plays
// `move shadowclaw mega`, and reads Tough Claws off the resulting forme. This
// engine has no `mega` suffix in the choice grammar, no mega stone in the
// 128-item set, and no forme layer for Disguise or Zero to Hero to be
// overwritten on — the case is entirely about which of those two forme-change
// abilities survives the swap.

func TestOtherMetasMixAndMega(t *testing.T) {
	describe(t, "Mix and Mega", func(g *psg) {
		g.skip("should overwrite forme-change abilities on Mega Evolution",
			"mega evolution")
	})
}
