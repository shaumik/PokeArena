//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/rapidspin.js.
//
// Four of the five cases come across; the Gen 7 variant skips.
//
// Sticky Web is not in this dataset, so the first case reports the missing move
// before it can look at anything else. That absence is the finding, which is
// why it is written out rather than skipped or quietly reduced to the three
// hazards this engine does have.
//
// Sleep Talk is not in this dataset either; Splash stands in wherever upstream
// uses it to spend a turn.
//
// Species. Omastar and Mew are in the dex. Armaldo has no stand-in row;
// Kabutops is built instead, which keeps the Rock/Water body and, more to the
// point, the spinner's role. Cobalion has no row either and is only the hazard
// setter that has to act before the spinner; Alakazam is named for it because
// it is faster than Kabutops, which Magneton — the usual Steel stand-in — is
// not, and nothing here turns on Steel. Shedinja has no row by design, but it
// is used only as something that dies to the Rocky Helmet while spinning, so
// Golbat at 1 HP takes the role. Wynaut resolves to Hypno.
//
// Upstream reads `battle.p2.sideConditions`; the same state lives on
// Side.Conditions.Hazards here, so the assertions read that.

func TestMovesRapidSpin(t *testing.T) {
	describe(t, "Rapid Spin", func(g *psg) {
		g.it("should remove entry hazards", func(p *ps) {
			p.battle(
				team{{Species: "Omastar", Moves: mv("stealthrock", "spikes", "toxicspikes", "stickyweb")}},
				team{{Species: "Armaldo", As: "Kabutops", Moves: mv("splash", "rapidspin")}},
			)
			p.makeChoices("move stealthrock", "move splash")
			p.makeChoices("move spikes", "move splash")
			p.makeChoices("move toxicspikes", "move splash")
			p.makeChoices("move stickyweb", "move splash")
			p.makeChoices("move toxicspikes", "move rapidspin")
			if st := p.state(); st != nil {
				h := st.Sides[1].Conditions.Hazards
				p.isFalse(h.StealthRock, "Rapid Spin should have cleared the rocks")
				p.equal(h.Spikes, 0, "Rapid Spin should have cleared the spikes")
				p.equal(h.ToxicSpikes, 0, "Rapid Spin should have cleared the toxic spikes")
			}
		})

		g.it("should remove entry hazards past a Substitute", func(p *ps) {
			p.battle(
				team{{Species: "Cobalion", As: "Alakazam", Moves: mv("stealthrock", "substitute")}},
				team{{Species: "Armaldo", As: "Kabutops", Moves: mv("splash", "rapidspin")}},
			)
			p.makeChoices("move stealthrock", "move splash")
			p.makeChoices("move substitute", "move rapidspin")
			if st := p.state(); st != nil {
				p.isFalse(st.Sides[1].Conditions.Hazards.StealthRock,
					"a substitute should not have saved the spinner's own hazards")
			}
		})

		g.it("should not remove hazards if the user faints", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Item: "rockyhelmet", Moves: mv("stealthrock")}},
				team{
					{Species: "Shedinja", As: "Golbat", Ability: "noability", HP: 1, Moves: mv("rapidspin")},
					{Species: "Wynaut", Moves: mv("splash")},
				},
			)
			p.turn()
			p.fainted(p.slot(1, 1), "the Rocky Helmet should have killed the spinner")
			if st := p.state(); st != nil {
				p.ok(st.Sides[1].Conditions.Hazards.StealthRock,
					"a spinner that faints mid-move should not have cleared the rocks")
			}
		})

		g.it("should not remove hazards if the user has Sheer Force", func(p *ps) {
			p.battle(
				team{{Species: "Cobalion", As: "Alakazam", Moves: mv("stealthrock")}},
				team{{Species: "Armaldo", As: "Kabutops", Ability: "sheerforce", Moves: mv("rapidspin")}},
			)
			p.turn()
			if st := p.state(); st != nil {
				p.ok(st.Sides[1].Conditions.Hazards.StealthRock,
					"Sheer Force removes the spin's self-boost and its hazard clear with it")
			}
		})

		g.skip("should remove hazards if the user has Sheer Force [Gen 7]", "gen 7 mechanics")
	})
}
