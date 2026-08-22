//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/photongeyser.js.
//
// Photon Geyser is not in this dataset, so every live case here stops at "move
// photongeyser is not in this dataset". That absence is the finding, and the
// cases are written out in full rather than skipped.
//
// Sleep Talk, Assist, Copycat, Counter and Electrify are not in the dataset
// either. Unlike most files, none of them is filler here: the two submove cases
// are *about* being called by Assist, Copycat and Sleep Talk, Counter is how
// the first case reads the damage category, and Electrify is what turns the
// move Electric so Volt Absorb has something to refuse. All five stay as
// themselves, and each is a separate finding of its own.
//
// Species. Photon Geyser picks its category by comparing the user's Attack to
// its Special Attack, and no Psychic-type in these 80 has Attack the higher of
// the two — so the first case, which needs exactly that, uses Machamp. The
// move's own type is irrelevant to the comparison and to both assertions, which
// key on the category rather than on the damage. The second case needs the
// opposite and needs one Special Attack drop to flip it, so Latias becomes
// Starmie rather than taking the shared table's Alakazam: Starmie's 95 Attack
// against 120 Special Attack is special until Struggle Bug lands and physical
// afterwards, which is the whole case, where Alakazam's 50 against 135 would
// never flip. Necrozma becomes Mewtwo (Psychic, the same role), Zeraora becomes
// Jolteon (Electric, and Volt Absorb is one of its own abilities), Bruxish
// becomes Starmie (Water/Psychic, exactly Bruxish's typing), and Liepard becomes
// Persian, a Normal body that only ever calls other moves — Dark is not
// preserved and nothing here reads it.
//
// Shedinja has no stand-in because Wonder Guard is its identity, but this case
// does not use Wonder Guard: it gives Shedinja Disguise and uses it as
// something that dies to one hit. A body started at 1 HP reproduces that.
//
// Huge Power, Prankster, Dazzling and Disguise are not modeled here. All four
// are load-bearing — the case is that Photon Geyser must not be swayed by Huge
// Power, and must not punch through Dazzling or Disguise unless the user has
// Mold Breaker of its own — so they stay and the run reports them.

func TestMovesPhotonGeyser(t *testing.T) {
	describe(t, "Photon Geyser", func(g *psg) {
		g.it("should become physical when Attack stat is higher than Special Attack stat", func(p *ps) {
			p.battle(
				team{{Species: "Necrozma-Dusk-Mane", As: "Machamp", Ability: "noability",
					Moves: mv("photongeyser")}},
				team{{Species: "Mew", Item: "keeberry", Moves: mv("counter")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.statStage(p.foe(), "def", 1, "physical Photon Geyser should trigger Kee Berry")
			p.damaged(p.mine(), "physical Photon Geyser should be susceptible to Counter")
		})

		g.it("should determine which attack stat is higher after factoring in stat stages, but no other kind of modifier", func(p *ps) {
			p.battle(
				team{{Species: "Latias", As: "Starmie", Ability: "hugepower", Item: "choiceband",
					Moves: mv("photongeyser")}},
				team{{Species: "Scizor-Mega", As: "Magneton", Item: "keeberry",
					Moves: mv("strugglebug", "splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn() // should be special this turn
			p.statStage(p.foe(), "def", 0, "incorrectly swayed by Choice Band and/or Huge Power")
			p.turn()
			p.statStage(p.foe(), "def", 1,
				"the stat drop should have turned Photon Geyser into a special move")
		})

		g.skip("should always be a special Max Move, never physical", "Dynamax")
		g.skip("should always be a special Z-move, never physical", "Z-moves")

		g.it("should ignore abilities the same way as Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Necrozma", As: "Mewtwo", Moves: mv("photongeyser")}},
				team{{Species: "Zeraora", As: "Jolteon", Ability: "voltabsorb", Moves: mv("electrify")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.damaged(p.foe(), "Electrified Photon Geyser should damage through Volt Absorb")
		})

		g.it("should not ignore abilities when called as a submove of another move", func(p *ps) {
			p.battle(
				team{
					{Species: "Liepard", As: "Persian", Ability: "prankster",
						Moves: mv("assist", "copycat", "sleeptalk", "photongeyser")},
					{Species: "Necrozma", As: "Mewtwo", Moves: mv("photongeyser")},
				},
				team{{Species: "Bruxish", As: "Starmie", Ability: "dazzling",
					Moves: mv("photongeyser", "spore")}},
			)
			if p.state() == nil {
				return
			}
			bruxish := p.foe()
			p.makeChoices("move assist", "move photongeyser")
			p.fullHP(bruxish, "incorrectly ignores abilities through Assist")
			bruxish.HP = bruxish.MaxHP
			p.makeChoices("move copycat", "move spore")
			p.fullHP(bruxish, "incorrectly ignores abilities through Copycat")
			bruxish.HP = bruxish.MaxHP
			p.makeChoices("move sleeptalk", "move photongeyser")
			p.fullHP(bruxish, "incorrectly ignores abilities through Sleep Talk")
		})

		g.it("should ignore abilities when called as a submove by a Pokemon that also has Mold Breaker", func(p *ps) {
			p.battle(
				team{{Species: "Shuckle", Ability: "moldbreaker", Moves: mv("sleeptalk", "photongeyser")}},
				team{{Species: "Shedinja", As: "Hypno", Ability: "disguise", HP: 1, Moves: mv("spore")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.fainted(p.foe(), "Mold Breaker should carry through Sleep Talk and beat Disguise")
		})
	})
}
