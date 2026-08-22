//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/mirrorarmor.js.
//
// Mirror Armor is not in this dataset. The ability is set anyway so the cases
// run; the first will report the drop landing on the holder instead of bouncing
// back, which is the finding.
//
// Corviknight resolves to Magneton through the stand-in table (steel kept,
// flying lost) — neither Rock Tomb nor Leer nor the case's arithmetic depends on
// the flying half. Machop resolves to Machamp. Drapion, Pangoro and Gossifleur
// have no rows: Arbok keeps Drapion's poison, Machamp keeps Pangoro's fighting,
// and Tangela keeps Gossifleur's grass.
//
// Sleep Talk is not in this dataset and is inert filler wherever it appears here
// (its users are awake, so it fails), so Splash stands in for it.
//
// Parting Shot is not in this dataset and is the subject of the second case, so
// it is kept and the missing-move failure is the finding. That case also asserts
// requestState === 'switch'; there is no request-state accessor here, so the
// port asks for the replacement instead and checks it arrived.

func TestAbilitiesMirrorArmor(t *testing.T) {
	describe(t, "Mirror Armor", func(g *psg) {
		g.it("should bounce boosts back to the source", func(p *ps) {
			p.battle(
				team{{Species: "Corviknight", Ability: "mirrorarmor", Moves: mv("endure")}},
				team{{Species: "Machop", Ability: "noguard", Moves: mv("rocktomb", "leer")}},
			)
			p.makeChoices("move endure", "move rocktomb")
			p.statStage(p.mine(), "spe", 0, "Rock Tomb's Speed drop should not stick to the Mirror Armor holder")
			p.statStage(p.foe(), "spe", -1, "Rock Tomb's Speed drop should land on its own user")
			p.makeChoices("move endure", "move leer")
			p.statStage(p.mine(), "def", 0, "Leer should not stick to the Mirror Armor holder")
			p.statStage(p.foe(), "def", -1, "Leer should land on its own user")
		})

		g.it("should reflect Parting Shot's stat drops, then the Parting Shot user should switch", func(p *ps) {
			p.battle(
				team{{Species: "Corviknight", Ability: "mirrorarmor", Moves: mv("splash")}},
				team{
					{Species: "Drapion", As: "Arbok", Moves: mv("partingshot")},
					{Species: "Pangoro", As: "Machamp", Moves: mv("splash")},
				},
			)
			p.turn()
			p.statStage(p.mine(), "atk", 0, "Parting Shot's Attack drop should have been bounced")
			p.statStage(p.mine(), "spa", 0, "Parting Shot's Sp. Atk drop should have been bounced")
			p.statStage(p.foe(), "atk", -1, "the bounced Attack drop should land on the Parting Shot user")
			p.statStage(p.foe(), "spa", -1, "the bounced Sp. Atk drop should land on the Parting Shot user")
			p.makeChoices("", "switch 2")
			p.species(p.foe(), "Machamp", "the Parting Shot user should still get its switch")
		})

		// Upstream one-shots Gossifleur so that Cotton Down's Speed drop has no
		// source left to bounce to, then checks the log never names Mirror Armor.
		// Tangela is far bulkier than Gossifleur, so its HP is set to 1 to
		// preserve "the source faints from this hit".
		//
		// Cotton Down is not modeled either, so nothing drops the holder's Speed
		// in the first place: this case cannot tell "activated silently" from
		// "never activated" and will pass whatever the engine does. It is kept as
		// a real case so the pair of missing abilities is recorded against it.
		g.it("should activate, but silently, if the source has fainted", func(p *ps) {
			p.battle(
				team{{Species: "Corviknight", Ability: "mirrorarmor", Moves: mv("bravebird")}},
				team{
					{Species: "Gossifleur", As: "Tangela", Ability: "cottondown", Moves: mv("splash"), HP: 1},
					{Species: "Wynaut", Ability: "shadowtag", Moves: mv("splash")},
				},
			)
			p.turn()
			p.fainted(p.slot(1, 1), "the Cotton Down source should have fainted to Brave Bird")
			p.statStage(p.mine(), "spe", 0, "no Speed drop should be left on the Mirror Armor holder")
		})
	})
}
