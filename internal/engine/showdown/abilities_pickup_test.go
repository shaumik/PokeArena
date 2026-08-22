//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/pickup.js.
//
// Pickup is not in this dataset, so the four cases that can be built are real
// questions: the two "should pick up" cases are expected to be red and the
// two "should not pick up" cases pass vacuously (nothing retrieves anything).
//
// Species: Gourgeist and Ambipom are not in this dex and have no stand-in
// row. Ambipom is a plain Normal body, so Persian takes its place. Gourgeist
// is the Pickup holder in the two retrieval cases; Chansey stands in there,
// chosen for its *weak* Sp. Atk — a stronger special attacker turns the 4x
// Flamethrower on Parasect into a KO, and the case needs Parasect alive and
// under half so the Sitrus Berry actually fires. Aron (level 1) is replaced
// by Magneton with its HP set: level is fixed at 50, and what the case needs
// is a Berry Juice holder that one Return puts under half.
//
// The "already retrieved" and "switched out" cases keep upstream's Eject
// Button, Bug Gem, Shadow Sneak and Pain Split, none of which this dataset
// has; they are left named so the report says so.
//
// Three cases are doubles or triples and skip.

func TestAbilitiesPickup(t *testing.T) {
	describe(t, "Pickup", func(g *psg) {
		g.it("should pick up a consumed item", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Ability: "pickup", Moves: mv("flamethrower")}},
				team{{Species: "Paras", Ability: "dryskin", Item: "sitrusberry", Moves: mv("endure")}},
			)
			p.makeChoices("move flamethrower", "move endure")
			p.noItem(p.foe(), "the Sitrus Berry should have been eaten")
			p.holdsItem(p.mine(), "Pick Up should retrieve consumed Sitrus Berry")
		})

		g.it("should pick up flung items", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Ability: "pickup", Moves: mv("endure")}},
				team{{Species: "Clefairy", Ability: "unaware", Item: "airballoon", Moves: mv("fling")}},
			)
			p.makeChoices("move endure", "move fling")
			p.holdsItem(p.mine(), "Pick Up should retrieve flung Air Balloon")
		})

		g.it("should not pick up an item that was knocked off", func(p *ps) {
			p.battle(
				team{{Species: "Persian", Ability: "pickup", Moves: mv("knockoff")}},
				team{{Species: "Machamp", Ability: "noguard", Item: "choicescarf", Moves: mv("bulkup")}},
			)
			p.makeChoices("move knockoff", "move bulkup")
			p.noItem(p.mine(), "Pick Up should not retrieve knocked off Choice Scarf")
		})

		g.it("should not pick up a popped Air Balloon", func(p *ps) {
			p.battle(
				team{{Species: "Persian", Ability: "pickup", Moves: mv("fakeout")}},
				team{{Species: "Scizor", Ability: "swarm", Item: "airballoon", Moves: mv("roost")}},
			)
			p.makeChoices("move fakeout", "move roost")
			p.noItem(p.foe(), "Fake Out should have popped the balloon")
			p.noItem(p.mine(), "Pick Up should not retrieve popped Air Balloon")
		})

		g.skip("should not pick up items from Pokemon that have switched out and back in", "doubles")

		g.it("should not pick up items from Pokemon that have switched out", func(p *ps) {
			p.battle(
				team{{Species: "Chansey", Ability: "pickup", Moves: mv("shadowsneak", "synthesis")}},
				team{
					{Species: "Persian", Ability: "swarm", Item: "buggem", Moves: mv("uturn")},
					{Species: "Gengar", Ability: "pressure", Item: "ejectbutton", Moves: mv("painsplit")},
				},
			)
			p.makeChoices("move synthesis", "move uturn")
			p.noItem(p.mine(), "")
			p.makeChoices("move synthesis", "move painsplit")
			p.makeChoices("move synthesis", "switch 2")
			p.noItem(p.mine(), "")
		})

		g.it("should not pick up items that were already retrieved", func(p *ps) {
			// Magneton starts at 70 of its 125 HP so that one Return leaves it
			// under half, which is where Berry Juice fires; upstream gets the
			// same relationship out of a level 1 Aron.
			p.battle(
				team{{Species: "Persian", Ability: "pickup", Moves: mv("return")}},
				team{{Species: "Magneton", Ability: "sturdy", Item: "berryjuice", Moves: mv("recycle"), HP: 70}},
			)
			p.makeChoices("move return", "move recycle")
			p.holdsItem(p.foe(), "Recycle should have taken the Berry Juice back")
			p.noItem(p.mine(), "")
		})

		g.skip("should pick up items from adjacent allies", "doubles")
		g.skip("should not pick up items from non-adjacent allies and enemies", "triples")
	})
}
