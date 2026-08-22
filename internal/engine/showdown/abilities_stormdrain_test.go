//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/stormdrain.js.
//
// Only the first case is about the half of Storm Drain a singles engine has:
// the Water immunity and the Special Attack boost. Everything else in the file
// is redirection, which needs a second slot on the field to redirect away
// from, so those skip. The last one would be half-portable — its Mold Breaker
// immunity check works in singles — but its other half is the redirect, and a
// port that quietly dropped it would report a green case for an assertion it
// never made.
//
// Gastrodon is not in this dex; Snorlax stands in as a body bulky enough to
// sit through the attack if the ability fails, with Storm Drain set explicitly
// as upstream sets it. Azumarill resolves to Clefable, and Sleep Talk (absent
// from this dataset) becomes Splash for the idle turn.

func TestAbilitiesStormDrain(t *testing.T) {
	describe(t, "Storm Drain", func(g *psg) {
		g.it("should grant immunity to Water-type moves and boost Special Attack by 1 stage", func(p *ps) {
			p.battle(
				team{{Species: "Gastrodon", As: "Snorlax", Ability: "stormdrain", Moves: mv("splash")}},
				team{{Species: "Azumarill", Ability: "thickfat", Moves: mv("aquajet")}},
			)
			p.makeChoices("move splash", "move aquajet")
			p.fullHP(p.mine(), "Storm Drain should have absorbed the Water move")
			p.statStage(p.mine(), "spa", 1, "absorbing it should have raised Special Attack")
		})

		g.skip("should redirect Max Geyser", "Dynamax")
		g.skip("should redirect single-target Water-type attacks to the user if it is a valid target", "triples")
		g.skip("should redirect to the fastest Pokemon with the ability", "doubles")
		g.skip("should not redirect if another Pokemon has used Follow Me", "doubles")
		g.skip("should have its Water-type immunity and its ability to redirect moves suppressed by Mold Breaker",
			"doubles")
	})
}
