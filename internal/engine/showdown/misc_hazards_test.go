//go:build showdown

package showdown

import (
	"strings"
	"testing"
)

// Ported from test/sim/misc/hazards.js.
//
// Three translation decisions run through the whole file.
//
// U-turn. Upstream pivots with U-turn and then answers a forced-switch request
// with `makeChoices('switch 2')`. This engine resolves a self-switch inside the
// same turn and picks the replacement itself, so there is no request to answer;
// every case that only used U-turn to get a fresh Pokemon onto hazards uses a
// plain switch on the following turn instead. The mechanic under test — what
// happens to something arriving on a hazard — is untouched.
//
// Missing moves. Sticky Web and Final Gambit are not in this dataset, so the
// two cases that use them fail at team construction naming the move. They are
// written out rather than skipped because that absence is the finding.
// `sleeptalk`, which upstream uses as a do-nothing, is also absent; `splash`
// stands in for it wherever it appears.
//
// Entrance abilities. Electric Surge is not modeled and Shedinja has no
// stand-in (Wonder Guard is its identity). What the first case needs is a
// switch-in ability with a visible effect on a body that dies to the hazard, so
// it uses Drizzle on a Golbat set to 1 HP: if the rocks land first the rain
// never comes up. The multi-switch case keeps upstream's own Drizzle and
// Intimidate for the same reason.
//
// The two Free-for-all cases skip: this engine is singles, with one active slot
// and exactly two sides.

func TestMiscHazards(t *testing.T) {
	describe(t, "Hazards", func(g *psg) {
		g.it("should damage Pokemon before regular entrance Abilities", func(p *ps) {
			p.battle(
				team{
					{Species: "wynaut", Moves: mv("splash")},
					{Species: "shedinja", As: "Golbat", Ability: "drizzle", HP: 1, Moves: mv("splash")},
				},
				team{{Species: "landorus", As: "Golem", Moves: mv("stealthrock", "splash")}},
			)
			p.makeChoices("move splash", "move stealthrock")
			p.makeChoices("switch 2", "move splash")
			p.fainted(p.slot(0, 2), "a 1 HP body should have fainted to Stealth Rock")
			p.equal(p.weather(), "", "the entrance ability should not have got to fire")
			p.logLacks("ability set the weather", "the entrance ability should not have got to fire")
		})

		g.it("should damage multiple Pokemon switching in simultaneously by Speed order", func(p *ps) {
			// Re-expressed as two voluntary switches on the same turn. Upstream
			// gets its simultaneous entry by killing one side with Final
			// Gambit, which is not in this dataset; two sides switching at once
			// puts the same two Pokemon on hazards in the same instant, which
			// is all the ordering assertions read.
			//
			// Landorus-Therian becomes Sandslash, the same body the shared
			// stand-in table picks for it, with Intimidate set explicitly.
			// Miltank is only the other side's rock setter, and its stand-in
			// Tauros has Intimidate natively — stripped, so the one Intimidate
			// line in the log is the one the case is about.
			p.battle(
				team{
					{Species: "wynaut", Moves: mv("stealthrock", "splash")},
					{Species: "kyogre", Ability: "drizzle", Item: "choicescarf", Moves: mv("splash")},
				},
				team{
					{Species: "miltank", As: "Tauros", Ability: "noability", Moves: mv("stealthrock", "splash")},
					{Species: "landorus-therian", As: "Sandslash", Ability: "intimidate", Moves: mv("splash")},
				},
			)
			p.makeChoices("move stealthrock", "move stealthrock")
			p.makeChoices("switch 2", "switch 2")

			kyogre, landorus := p.mine(), p.foe()
			p.logHas("Pointed stones dug into ", "both switch-ins should have taken rocks")
			p.logHas("ability set the weather", "Drizzle should have fired")
			p.logHas("Intimidate cuts", "Intimidate should have fired")

			log := p.logText()
			rocksKyogre := strings.Index(log, "Pointed stones dug into "+kyogre.Name)
			abilityKyogre := strings.Index(log, kyogre.Name+"'s ability set the weather")
			rocksLandorus := strings.Index(log, "Pointed stones dug into "+landorus.Name)
			abilityLandorus := strings.Index(log, "Intimidate cuts")

			p.ok(rocksKyogre >= 0 && abilityKyogre > rocksKyogre,
				"Stealth Rock should damage Kyogre before Drizzle activates.")
			p.ok(abilityKyogre >= 0 && rocksLandorus > abilityKyogre,
				"Kyogre should activate Drizzle before Landorus takes rocks damage.")
			p.ok(rocksLandorus >= 0 && abilityLandorus > rocksLandorus,
				"Stealth Rock should damage Landorus before Intimidate activates.")
		})

		g.it("should set up hazards even if there is no target", func(p *ps) {
			// Upstream's Digletts are level 1 so that Final Gambit costs the
			// hazard setter almost nothing and the switch-in survives a layer.
			// Level is fixed at 50 here, so the same relationship is arranged
			// with starting HP: 40 of Dugtrio's 110 is enough to survive one
			// layer of hazards and small enough that three Final Gambits do not
			// kill the setter.
			p.battle(
				team{
					{Species: "diglett", HP: 40, Moves: mv("splash", "finalgambit")},
					{Species: "diglett", HP: 40, Moves: mv("splash", "finalgambit")},
					{Species: "diglett", HP: 40, Moves: mv("splash", "finalgambit")},
					{Species: "diglett", HP: 40, Moves: mv("splash", "finalgambit")},
				},
				team{{Species: "wynaut", Item: "laggingtail",
					Moves: mv("stealthrock", "spikes", "stickyweb", "defog")}},
			)

			p.makeChoices("move finalgambit", "move stealthrock")
			p.makeChoices("switch 2", "")
			p.damaged(p.mine(), "Stealth Rock should have gone up with nothing to point at")
			p.makeChoices("move splash", "move defog")
			p.makeChoices("move finalgambit", "move spikes")
			p.makeChoices("switch 3", "")
			p.damaged(p.mine(), "Spikes should have gone up with nothing to point at")
			p.makeChoices("move splash", "move defog")
			p.makeChoices("move finalgambit", "move stickyweb")
			p.makeChoices("switch 4", "")
			p.statStage(p.mine(), "spe", -1, "Sticky Web should have gone up with nothing to point at")
		})

		g.it("should apply hazards in the order they were set up", func(p *ps) {
			// Snorlax stands in for Whismur as a grounded Normal body, with
			// Immunity stripped so Toxic Spikes can land on it.
			//
			// The Sticky Web leg of the ordering assertion has no counterpart:
			// the hazard is not in this dataset and the engine emits no line
			// for it. The other three legs are asserted, and the fixture still
			// asks for Sticky Web so the gap is recorded.
			p.battle(
				team{
					{Species: "wynaut", Moves: mv("splash")},
					{Species: "whismur", As: "Snorlax", Ability: "noability", Moves: mv("splash")},
				},
				team{{Species: "landorus", As: "Golem",
					Moves: mv("stealthrock", "spikes", "stickyweb", "toxicspikes")}},
			)
			p.makeChoices("move splash", "move toxicspikes")
			p.makeChoices("move splash", "move stickyweb")
			p.makeChoices("move splash", "move spikes")
			p.makeChoices("move splash", "move toxicspikes")
			p.makeChoices("move splash", "move stealthrock")
			p.makeChoices("switch 2", "")

			whismur := p.mine()
			p.logHas("badly poisoned", "two layers of Toxic Spikes should badly poison")
			p.logHas("was hurt by the spikes!", "Spikes should have damaged the switch-in")
			p.logHas("Pointed stones dug into ", "Stealth Rock should have damaged the switch-in")

			log := p.logText()
			tSpike := strings.Index(log, whismur.Name+" was badly poisoned")
			spikes := strings.Index(log, whismur.Name+" was hurt by the spikes!")
			rocks := strings.Index(log, "Pointed stones dug into "+whismur.Name)

			p.ok(tSpike >= 0 && spikes > tSpike,
				"Toxic Spikes should have poisoned before Spikes damage.")
			p.ok(spikes >= 0 && rocks > spikes,
				"Spikes should have damaged before Stealth Rock.")
		})

		g.it("should allow Berries to trigger between hazards", func(p *ps) {
			// Shedinja is a 1 HP body that has to be poisoned by Toxic Spikes
			// before Stealth Rock kills it; Snorlax at HP 1 is grounded and
			// neither Poison nor Steel, which is what Toxic Spikes needs. Its
			// own Immunity is stripped — it would refuse the poison and the
			// case would measure nothing.
			p.battle(
				team{
					{Species: "wynaut", Moves: mv("splash")},
					{Species: "shedinja", As: "Snorlax", Ability: "noability", Item: "lumberry", HP: 1, Moves: mv("splash")},
				},
				team{{Species: "landorus", As: "Golem", Moves: mv("toxicspikes", "stealthrock")}},
			)
			p.makeChoices("move splash", "move toxicspikes")
			p.makeChoices("move splash", "move stealthrock")
			p.makeChoices("switch 2", "")
			p.noItem(p.slot(0, 2), "Shedinja should have lost Lum Berry before fainting to rocks.")
		})

		g.skip("should set up hazards to every opponents' side in a Free-for-all battle",
			"free-for-all: this engine is singles, with one active slot and two sides")
		g.skip("should set up hazards even if there is no target in a Free-for-all battle",
			"free-for-all: this engine is singles, with one active slot and two sides")
	})
}
