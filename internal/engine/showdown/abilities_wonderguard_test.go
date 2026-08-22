//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/wonderguard.js.
//
// Wonder Guard is not one of this engine's 118 abilities, so every case here
// reports it and that report is the finding. The ports are otherwise kept as
// close to the original as the roster allows, so they still read against it.
//
// Species. Aerodactyl and Alakazam (for Abra) are in the dex. Zekrom becomes
// Dragonite, which keeps the property the case needs — a body Fire is not
// super effective against. Smeargle's stand-in is Chansey.
//
// The two Shedinja cases are not ported: Shedinja's 1 HP and Wonder Guard are
// the same fact, so no stand-in can carry it, and this port does not invent
// one.
//
// Substitutions inside the Mold Breaker case: Turbo Blaze is not in this
// engine's ability set and Fusion Flare is not in its move set, so Reshiram
// becomes a Charizard with Mold Breaker itself — which is what the case name
// asks about — throwing Flamethrower, a plain Fire special move with the same
// not-super-effective matchup. Sleep Talk is not in the dataset either and is
// replaced by Splash wherever it was only an idle turn.

func TestAbilitiesWonderGuard(t *testing.T) {
	describe(t, "Wonder Guard", func(g *psg) {
		g.it("should make the user immune to damaging attacks that are not super effective", func(p *ps) {
			p.battle(
				team{{Species: "Aerodactyl", Ability: "wonderguard", Moves: mv("splash")}},
				team{{
					Species: "Smeargle", Ability: "owntempo",
					Moves: mv("knockoff", "flamethrower", "thousandarrows", "moonblast"),
				}},
			)
			for _, move := range []string{"knockoff", "flamethrower", "thousandarrows", "moonblast"} {
				p.makeChoices("move splash", "move "+move)
				p.fullHP(p.mine(), "Wonder Guard should have refused "+move)
			}
			// Thousand Arrows should not leave the Smack Down volatile behind
			// when Wonder Guard blocked it, so the second one is refused too.
			p.constant(func() any { return p.mine().HP }, func() {
				p.makeChoices("move splash", "move thousandarrows")
			}, "a blocked Thousand Arrows should not have grounded the target")
		})

		g.it("should not make the user immune to status moves", func(p *ps) {
			p.battle(
				team{{Species: "Abra", Ability: "wonderguard", Moves: mv("teleport")}},
				team{{
					Species: "Smeargle", Ability: "noguard",
					Moves: mv("poisongas", "screech", "healpulse", "gastroacid"),
				}},
			)
			target := p.mine()
			p.makeChoices("move teleport", "move poisongas")
			p.hasStatus(target, "psn", "a status move should reach a Wonder Guard holder")
			p.makeChoices("move teleport", "move screech")
			p.statStage(target, "def", -2, "a status move should reach a Wonder Guard holder")
			// Heal Pulse restores half and the poison then takes an eighth, so
			// the turn is a net gain of one eighth of max HP.
			p.hurtsBy(target, -(target.MaxHP / 8), func() {
				p.makeChoices("move teleport", "move healpulse")
			}, "Heal Pulse should reach a Wonder Guard holder")
			p.makeChoices("move teleport", "move gastroacid")
			// This engine suppresses an ability with a volatile flag rather
			// than by clearing the ability field, so upstream's two
			// assertions — the volatile is present, the ability no longer
			// reads as Wonder Guard — are the same assertion here.
			p.ok(target.Volatiles.GastroAcid, "Gastro Acid should have suppressed Wonder Guard")
		})

		g.it("should be suppressed by Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Zekrom", As: "Dragonite", Ability: "wonderguard", Moves: mv("splash")}},
				team{{Species: "Reshiram", As: "Charizard", Ability: "moldbreaker", Moves: mv("flamethrower")}},
			)
			p.hurts(p.mine(), func() {
				p.makeChoices("move splash", "move flamethrower")
			}, "Mold Breaker should read past Wonder Guard")
		})

		g.skip("should activate if the target is immune to the attack",
			"no dark-type body is in this 80-species dex to be immune to Psychic, and the "+
				"engine's immunity line carries no ability attribution, so the case would measure nothing")
		g.skip("should not make the user immune to Struggle",
			"Shedinja is not in this 80-species dex and Wonder Guard is not modeled")
		g.skip("should make the user immune to typeless moves",
			"Shedinja is not in this 80-species dex and Wonder Guard is not modeled")

		// Upstream nests this describe inside Wonder Guard; the ledger key
		// keeps the inner name verbatim.
		describe(t, "[Gen 4]", func(g *psg) {
			g.skip("should not make the user immune to typeless moves", "gen 4 mechanics")
		})
	})
}
