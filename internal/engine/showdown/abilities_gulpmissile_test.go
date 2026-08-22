//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/gulpmissile.js.
//
// Nothing in this file survives the port. Cramorant is not in this
// 80-species dex and has no stand-in, Gulp Missile is not in the ability set,
// and every case is stated as an assertion about which forme is out
// (Cramorant-Gulping, Cramorant-Gorging) — forme changes are not modeled at
// all, so a substituted body would answer "the species did not change" for a
// reason that has nothing to do with the ability.
//
// The nested `Hackmons Cramorant` describe becomes its own describe block, so
// each ledger key still reads "<the describe the case was written under>:
// <the case>".

func TestAbilitiesGulpMissile(t *testing.T) {
	describe(t, "Gulp Missile", func(g *psg) {
		g.skip(`should retrieve a catch on the first turn of Dive`,
			"Cramorant is not in this 80-species dex and Gulp Missile's forme change is not modeled")
		g.skip(`should retrieve a catch only if the move was successful`,
			"Cramorant is not in this 80-species dex and Gulp Missile's forme change is not modeled")
		g.skip(`should not spit out its catch if the Cramorant is semi-invulnerable`,
			"Cramorant is not in this 80-species dex and Gulp Missile's forme change is not modeled")
		g.skip(`should change forms before damage calculation`,
			"Cramorant is not in this 80-species dex and Gulp Missile's forme change is not modeled")
	})

	describe(t, "Hackmons Cramorant", func(g *psg) {
		g.skip(`should be sent out as the hacked form`,
			"formes: Cramorant-Gulping cannot be built and forme changes are not modeled")
		g.skip(`should not force Cramorant-Gorging or -Gulping to have Gulp Missile`,
			"formes: Cramorant-Gorging cannot be built and forme changes are not modeled")
	})
}
