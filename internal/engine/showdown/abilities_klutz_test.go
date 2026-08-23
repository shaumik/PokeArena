//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/klutz.js.
//
// Species. Lopunny and Deoxys both resolve through the stand-in table
// (Kangaskhan and Mewtwo). Giratina and Genesect do not: Giratina becomes
// Gengar, which keeps the ghost typing and takes Pressure explicitly, and
// Genesect becomes Magneton, which keeps steel. Neither case turns on
// anything else about them.
//
// Levels. Upstream's Sitrus case runs a level-1 Lopunny so one Psychic is
// lethal and Endure has something to save it from. Level is fixed at 50 here,
// so the same relationship is arranged with a starting HP low enough that the
// hit would kill.
//
// Moves and items. Belly Drum and Shadow Sneak are not in this dataset. Both
// were only scaffolding for "the holder loses HP and Leftovers must not give
// any back", so that case is restated directly: a damaged holder sits through
// a turn and must not heal. Sea Incense is not in the item set either and
// becomes Lax Incense, another item with no Fling effect of its own. Douse
// Drive and Ability Shield genuinely have no counterpart, and those two cases
// report them.

func TestAbilitiesKlutz(t *testing.T) {
	describe(t, "Klutz", func(g *psg) {
		g.it("should negate residual healing events", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Ability: "klutz", Item: "leftovers", Moves: mv("splash"), HP: 100}},
				team{{Species: "Giratina", As: "Gengar", Ability: "pressure", Moves: mv("splash")}},
			)
			p.constant(func() any { return p.mine().HP }, func() {
				p.turn()
			}, "Leftovers healing should not apply")
		})

		g.it("should prevent items from being consumed", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Ability: "klutz", Item: "sitrusberry", Moves: mv("endure"), HP: 20}},
				team{{Species: "Deoxys", Ability: "noguard", Moves: mv("psychic")}},
			)
			p.constant(func() any { return p.mine().Item }, func() {
				p.makeChoices("move endure", "move psychic")
			}, "Klutz should have kept the Sitrus Berry uneaten")
			p.equal(p.mine().HP, 1, "Endure should have held it at 1 HP")
		})

		g.it("should ignore the effects of items that disable moves", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Ability: "klutz", Item: "assaultvest", Moves: mv("protect")}},
				team{{Species: "Deoxys", Ability: "noguard", Moves: mv("psychic")}},
			)
			p.canMove(0, "protect", "Klutz should switch off the Assault Vest's status-move lock")
			p.makeChoices("move protect", "move psychic")
			// Upstream reads lastMove.id; the engine's equivalent evidence
			// that the move ran is its own line.
			p.logHas("protected itself", "Protect should have gone through")
		})

		g.it("should not ignore item effects that prevent item removal", func(p *ps) {
			p.battle(
				team{{Species: "Genesect", As: "Magneton", Ability: "klutz", Item: "dousedrive", Moves: mv("calmmind")}},
				team{{Species: "Deoxys", Ability: "noguard", Moves: mv("trick")}},
			)
			p.constant(func() any { return p.mine().Item }, func() {
				p.makeChoices("move calmmind", "move trick")
			}, "a Drive should stay put even through Klutz")
		})

		g.it("should cause Fling to fail", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Ability: "klutz", Item: "laxincense", Moves: mv("fling")}},
				team{{Species: "Deoxys", Ability: "noguard", Moves: mv("calmmind")}},
			)
			p.constant(func() any { return p.mine().Item }, func() {
				p.makeChoices("move fling", "move calmmind")
			}, "Klutz should have made Fling fail with the item still held")
		})

		g.it("should cause Fling to fail even if the item ignores Klutz", func(p *ps) {
			p.battle(
				team{{Species: "Lopunny", Ability: "klutz", Item: "abilityshield", Moves: mv("fling")}},
				team{{Species: "Deoxys", Ability: "noguard", Moves: mv("calmmind")}},
			)
			p.constant(func() any { return p.mine().Item }, func() {
				p.makeChoices("move fling", "move calmmind")
			}, "Klutz should have made Fling fail with the item still held")
		})

		g.skip("should not prevent Pokemon from Mega Evolving", "mega evolution")

		// Upstream nests this describe inside Klutz; the ledger key keeps the
		// inner name verbatim.
		describe(t, "[Gen 4]", func(g *psg) {
			g.skip("should not cause Fling to fail", "gen 4 mechanics")
		})
	})
}
