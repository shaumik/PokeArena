//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/trapmoves.js.
//
// Shape. Upstream generates its cases from a `for (const move of ...)` loop
// with no describe per move, so all six trapping moves register `it` blocks
// under the same string, and all eight partial trappers do the same. The ledger
// keys on "<describe>: <it>", and six rows competing for one key can never
// reconcile — some would pass and some fail against a single entry, and the
// entry would be stale and current at once. So each upstream name appears once
// here and the loop moves inside the body, which walks the same moves and
// leaves the key meaningful.
//
// Which moves exist. This dataset has Block and Mean Look but not Spider Web,
// Thousand Waves, Anchor Shot or Spirit Shackle; of the partial trappers it has
// everything but Magma Storm. A scenario stops at the first name the harness
// cannot resolve, so the partial-trapper list is reordered to put Magma Storm
// last — in upstream's order it sits fifth and would hide Sand Tomb, Whirlpool
// and Wrap behind it. The trappers need no reordering: the two that resolve are
// already first, and the case reports Spider Web on behalf of the four missing.
//
// Switch legality. ResolveTurn does not check whether a switch was legal, it
// just performs it, so "the Pokemon switched" is not evidence about trapping
// here. Every trapping claim goes through LegalActions instead — p.trapped and
// p.notTrapped — which is the gate the engine really consults. That is why the
// three "should stop trapping" cases assert freedom to switch rather than
// replaying upstream's switch and reading the active back.
//
// Species. Tangrowth, Gourgeist and Dusknoir have no stand-in rows. Tangela is
// Tangrowth's own line one stage down and is a body in every case it appears
// in; Gengar supplies the Ghost typing that is the entire subject of the two
// "immune to trapping" cases. Kyurem is only something for Roar to drag in, and
// Lapras keeps its Ice half. Blissey and Smeargle both resolve to Chansey,
// which is harmless — the partial-trapper cases never compare the two sides.
//
// Abilities and filler. Prankster is not modeled and Illuminate is registered
// inert, so the bodies upstream gives them use "noability", upstream's own
// idiom for a Pokemon that must not interfere. Reflect Type and Sleep Talk are
// not in this dataset and were idle moves in the original, so Splash stands in
// for both.
//
// Baton Pass switches inside the move here rather than raising a replacement
// request, so upstream's follow-up `makeChoices('', 'switch 2')` has no
// counterpart and the port reads the active straight after the pass.

func TestMiscTrapMoves(t *testing.T) {
	trappers := []string{"block", "meanlook", "spiderweb", "thousandwaves", "anchorshot", "spiritshackle"}
	partialTrappers := []string{"bind", "clamp", "firespin", "infestation", "sandtomb", "whirlpool", "wrap", "magmastorm"}

	describe(t, "Trapping Moves", func(g *psg) {
		g.it("should prevent Pokemon from switching out normally", func(p *ps) {
			for _, move := range trappers {
				p.battle(
					team{{Species: "Smeargle", Ability: "noability", Moves: mv(move)}},
					team{
						{Species: "Tangrowth", As: "Tangela", Ability: "leafguard", Moves: mv("swordsdance")},
						{Species: "Starmie", Ability: "noability", Moves: mv("splash")},
					},
				)
				p.makeChoices("move "+move, "move swordsdance")
				p.trapped(1, move+" should have trapped Tangrowth")
				p.species(p.foe(), "Tangela", "the trapped Pokemon should still be the active one")
			}
		})

		g.it("should not prevent Pokemon from switching out using moves", func(p *ps) {
			for _, move := range trappers {
				p.battle(
					team{{Species: "Smeargle", Ability: "noability", Moves: mv(move)}},
					team{
						{Species: "Tangrowth", As: "Tangela", Ability: "leafguard", Moves: mv("batonpass")},
						{Species: "Starmie", Ability: "noability", Moves: mv("splash")},
					},
				)
				p.makeChoices("move "+move, "move batonpass")
				p.species(p.foe(), "Starmie", "Baton Pass should carry a Pokemon out of "+move)
			}
		})

		g.it("should not prevent Pokemon immune to trapping from switching out", func(p *ps) {
			for _, move := range trappers {
				p.battle(
					team{{Species: "Smeargle", Ability: "noability", Moves: mv(move)}},
					team{
						{Species: "Gourgeist", As: "Gengar", Ability: "noability", Moves: mv("synthesis")},
						{Species: "Starmie", Ability: "noability", Moves: mv("splash")},
					},
				)
				p.makeChoices("move "+move, "move synthesis")
				p.notTrapped(1, "a Ghost-type should walk out of "+move)
			}
		})

		g.it("should stop trapping the Pokemon if the user is no longer active", func(p *ps) {
			for _, move := range trappers {
				p.battle(
					team{
						{Species: "Smeargle", Ability: "noability", Moves: mv(move)},
						{Species: "Kyurem", As: "Lapras", Ability: "pressure", Moves: mv("rest")},
					},
					team{
						{Species: "Tangrowth", As: "Tangela", Ability: "leafguard", Moves: mv("roar")},
						{Species: "Starmie", Ability: "noability", Moves: mv("splash")},
					},
				)
				// Roar is priority -6, so the trap lands first and then blows its
				// own trapper off the field.
				p.makeChoices("move "+move, "move roar")
				p.notTrapped(1, move+" should stop holding anything once its user has left")
			}
		})

		g.skip("should free all trapped Pokemon if the user is no longer active", "doubles")
		g.skip("should be passed when the user uses Baton Pass in Gen 4", "gen 4 mechanics")
	})

	describe(t, "Partial Trapping Moves", func(g *psg) {
		g.it("should deal 1/8 HP per turn", func(p *ps) {
			for _, move := range partialTrappers {
				p.battle(
					team{{Species: "Smeargle", Ability: "noguard", Moves: mv(move, "rest")}},
					team{{Species: "Blissey", Ability: "naturalcure", Moves: mv("healbell")}},
				)
				p.makeChoices("move "+move, "move healbell")
				target := p.foe()
				// Upstream heals the target back to full so the move's own damage
				// is not counted against the chip.
				target.HP = target.MaxHP
				p.hurtsBy(target, target.MaxHP/8, func() {
					p.makeChoices("move rest", "move healbell")
				}, move+" should chip 1/8 of max HP at the end of the turn")
			}
		})

		g.it("should prevent Pokemon from switching out normally", func(p *ps) {
			for _, move := range partialTrappers {
				p.battle(
					team{{Species: "Smeargle", Ability: "noguard", Moves: mv(move)}},
					team{
						{Species: "Blissey", Ability: "naturalcure", Moves: mv("healbell")},
						{Species: "Starmie", Ability: "noability", Moves: mv("splash")},
					},
				)
				p.makeChoices("move "+move, "move healbell")
				p.trapped(1, move+" should have trapped Blissey")
				p.species(p.foe(), "Blissey", "the trapped Pokemon should still be the active one")
			}
		})

		g.it("should not prevent Pokemon from switching out using moves", func(p *ps) {
			for _, move := range partialTrappers {
				p.battle(
					team{{Species: "Smeargle", Ability: "noguard", Moves: mv(move)}},
					team{
						{Species: "Blissey", Ability: "naturalcure", Moves: mv("batonpass")},
						{Species: "Starmie", Ability: "noability", Moves: mv("splash")},
					},
				)
				p.makeChoices("move "+move, "move batonpass")
				p.species(p.foe(), "Starmie", "Baton Pass should carry a Pokemon out of "+move)
			}
		})

		g.it("should not prevent Pokemon immune to trapping from switching out", func(p *ps) {
			for _, move := range partialTrappers {
				p.battle(
					team{{Species: "Smeargle", Ability: "noguard", Moves: mv(move)}},
					team{
						{Species: "Dusknoir", As: "Gengar", Ability: "frisk", Moves: mv("splash")},
						{Species: "Starmie", Ability: "noability", Moves: mv("splash")},
					},
				)
				p.makeChoices("move "+move, "move splash")
				p.notTrapped(1, "a Ghost-type should walk out of "+move)
			}
		})

		g.it("should stop trapping the Pokemon if the user is no longer active", func(p *ps) {
			for _, move := range partialTrappers {
				p.battle(
					team{
						{Species: "Smeargle", Ability: "noguard", Moves: mv(move)},
						{Species: "Kyurem", As: "Lapras", Ability: "pressure", Moves: mv("rest")},
					},
					team{
						{Species: "Blissey", Ability: "naturalcure", Moves: mv("roar")},
						{Species: "Starmie", Ability: "noability", Moves: mv("splash")},
					},
				)
				p.makeChoices("move "+move, "move roar")
				p.notTrapped(1, move+" should stop holding anything once its user has left")
			}
		})

		g.it("should stop trapping the Pokemon if the target uses Rapid Spin", func(p *ps) {
			for _, move := range partialTrappers {
				p.battle(
					team{
						{Species: "Smeargle", Ability: "noguard", Moves: mv(move)},
						{Species: "Kyurem", As: "Lapras", Ability: "pressure", Moves: mv("rest")},
					},
					team{
						{Species: "Blissey", Ability: "naturalcure", Moves: mv("rapidspin")},
						{Species: "Starmie", Ability: "noability", Moves: mv("splash")},
					},
				)
				p.makeChoices("move "+move, "move rapidspin")
				p.notTrapped(1, "Rapid Spin should have shaken off "+move)
			}
		})
	})

	describe(t, "Partial Trapping Moves [Gen 1]", func(g *psg) {
		g.skip("Wrap ends when wrapped Pokemon dies of residual damage", "gen 1 mechanics")
		g.skip("Wrap ends when wrapped Pokemon switches to a Pokemon that dies of residual damage",
			"gen 1 mechanics")
		g.skip("Wrap ends when wrapper dies to residual damage", "gen 1 mechanics")
		g.skip("Wrap ends when wrapper switches to a Pokemon that dies of residual damage",
			"gen 1 mechanics")
		g.skip("Wrap should damage the target's substitute", "gen 1 mechanics")
		g.skip("Wrap should never miss if the target is already trapped", "gen 1 mechanics")
		g.skip("should stay asleep if it switched in after a Pokemon spent a turn trapped",
			"gen 1 mechanics")
		g.skip("should continue if copied by Mirror Move", "gen 1 mechanics")
		g.skip("should restart the trapping move if copied by Mirror Move and the target switches",
			"gen 1 mechanics")
		g.skip("should use Metronome if copied by Metronome even if the target switches",
			"gen 1 mechanics")
	})
}
