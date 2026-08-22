//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/unnerve.js.
//
// Toxapex and Corviknight resolve to Tentacruel and Magneton; Wynaut to
// Hypno. None of the substitutions touch the case, which only needs two
// bodies with Unnerve and one holding a berry.
//
// Upstream inflicts the status with Toxic. This dataset's Toxic is 90%
// accurate with no poison-type exception, so over the five-seed replay the
// case would sometimes be measuring the accuracy roll; Nuzzle's guaranteed
// paralysis is the same test for a Lum Berry and never misses.
//
// The port adds one assertion upstream does not make — that the berry is
// still unspent while the first Unnerve body is out. Without it an engine
// that models no Unnerve at all would eat the berry on turn one and still
// show a cured Pokemon at the end, which is the case passing for the opposite
// of its reason.
//
// The Unnerve Desync Glitch block is `describe.skip` upstream, so it does not
// run there either; its cases are recorded here as skips.

func TestAbilitiesUnnerve(t *testing.T) {
	describe(t, "Unnerve", func(g *psg) {
		g.it("should allow Berry activation between switches of Unnerve", func(p *ps) {
			p.battle(
				team{
					{Species: "toxapex", Ability: "unnerve", Moves: mv("nuzzle")},
					{Species: "corviknight", Ability: "unnerve", Moves: mv("splash")},
				},
				team{{Species: "wynaut", Item: "lumberry", Moves: mv("splash")}},
			)
			p.turn()
			p.hasStatus(p.foe(), "par", "Unnerve should have kept the Lum Berry uneaten")
			p.makeChoices("switch 2", "auto")
			p.noStatus(p.foe(), "the berry should get its chance in the gap between the two Unnerve bodies")
		})
	})

	// Upstream nests this describe inside Unnerve, and marks it describe.skip.
	describe(t, "Unnerve Desync Glitch", func(g *psg) {
		const why = "upstream skips this block itself; the glitch needs a level-3 body, " +
			"which this engine cannot build with its fixed level of 50"

		g.skip(`should allow the undead Pokemon to switch back in after "fainting"`, why)
		g.skip("should make attacks used against the undead Pokemon fail due to lack of target", why)
		g.skip("should allow some passive abilities on the undead Pokemon to work normally", why)
		g.skip("should allow the undead Pokemon to choose to switch, but the turn will be skipped", why)
		g.skip("should allow the undead Pokemon to choose moves, but the turn will be skipped", why)
		g.skip("should allow the undead Pokemon to choose to Dynamax, but the turn will be skipped", "Dynamax")
	})
}
