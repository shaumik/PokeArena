//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/commander.js.
//
// Nothing in this file crosses over. Commander is an ability whose entire
// mechanic is an ally slot: Tatsugiri climbs into an adjacent Dondozo's mouth,
// stops being targetable, and hands its ally the boosts. Every upstream case
// builds a doubles battle (one builds a multi battle) because there is no way
// to state the ability at all in singles — with one active per side there is
// no ally to command.
//
// So all fifteen cases are recorded as skips rather than translated. The
// secondary obstacles are real too and would each be enough on their own —
// Commander is not in this engine's 118 abilities, Tatsugiri and Dondozo are
// not in the 80-species dex, and the Transform half of five cases has no
// counterpart either — but the binding reason is the format, so that is the
// reason each skip carries.

func TestAbilitiesCommander(t *testing.T) {
	describe(t, "Commander", func(g *psg) {
		g.skip("should skip Tatsugiri's action while commanding", "doubles")
		g.skip("should not work if another Pokemon is Transformed into Dondozo", "doubles")
		g.skip("should not work if another Pokemon is Transformed into Tatsugiri", "doubles")
		g.skip("should work if Tatsugiri is Transformed into another Pokemon with Commander", "doubles")
		g.skip("should work if Dondozo is Transformed", "doubles")
		g.skip("should cause Tatsugiri to dodge all moves, including moves which normally bypass semi-invulnerability", "doubles")
		g.skip("should prevent all kinds of switchouts", "doubles")
		g.skip("should prevent Eject Pack switchouts", "doubles")
		g.skip("should cause Dondozo to stay commanded even if Tatsugiri faints", "doubles")
		g.skip("should allow one Tatsugiri to occupy multiple Dondozo", "doubles")
		g.skip("should not work in Multi Battles", "multi battles")
		g.skip("should prevent Dondozo and Tatsugiri from combining if Commander is suppressed", "doubles")
		g.skip("should not split apart Dondozo and Tatsugiri if Neutralizing Gas switches in", "doubles")
		g.skip("should allow Tatsugiri to move again if Dondozo faints while Neutralizing Gas is active", "doubles")
		g.skip("should activate after hazards run", "doubles")
	})
}
