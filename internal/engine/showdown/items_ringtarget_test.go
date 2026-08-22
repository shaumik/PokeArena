//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/ringtarget.js.
//
// Sleep Talk is not in this dataset and is inert filler for the holder, so
// Splash stands in for it. Upstream reads `|-supereffective|` / `|-resisted|`
// off the protocol; the port asserts on this engine's prose plus the damage the
// line describes.
//
// The first case walks four immunities. Two of them have an in-dex shape:
// Thundurus is an Electric/Flying body immune to Ground, and Zapdos is the
// in-dex Electric/Flying; Drifblim is a Ghost whose other half resists Fighting,
// and Gengar is Ghost/Poison, which resists Fighting the same way. The other two
// do not survive the roster. Girafarig is Normal/Psychic, so upstream can watch
// a negated Ghost immunity turn into a super-effective hit off the Psychic half;
// this dex has no Normal/Psychic, so Kangaskhan carries the Normal half alone
// and the port asserts only that the hit lands, not that it is super effective.
// Absol's leg is dropped outright: it needs a Dark type, and there is none here.
//
// Klefki is only a Magnet Rise user holding a Ring Target; Magneton keeps the
// Steel half, which is what makes the ignored Ground hit worth measuring.

func TestItemsRingTarget(t *testing.T) {
	describe(t, "Ring Target", func(g *psg) {
		g.it("should negate natural immunities and deal normal type effectiveness with the other type(s)", func(p *ps) {
			p.battle(
				team{{Species: "Smeargle", Ability: "owntempo",
					Moves: mv("earthquake", "vitalthrow", "shadowball", "psychic")}},
				team{
					{Species: "Thundurus", As: "Zapdos", Ability: "pressure", Item: "ringtarget",
						Moves: mv("rest")},
					{Species: "Drifblim", As: "Gengar", Ability: "noability", Item: "ringtarget",
						Moves: mv("rest")},
					{Species: "Girafarig", As: "Kangaskhan", Ability: "noability", Item: "ringtarget",
						Moves: mv("rest")},
				},
			)

			p.makeChoices("move earthquake", "move rest")
			p.logHas("It's super effective!", "Ground should reach a Flying-type Ring Target holder")
			p.damaged(p.foe(), "")

			p.makeChoices("move vitalthrow", "switch 2")
			p.logHas("It's not very effective...", "Fighting should reach a Ghost, resisted by its Poison half")
			p.damaged(p.foe(), "")

			p.makeChoices("move shadowball", "switch 3")
			p.damaged(p.foe(), "Ghost should reach a Normal-type Ring Target holder")
		})

		g.it("should not affect ability-based immunities", func(p *ps) {
			p.battle(
				team{{Species: "Hariyama", As: "Machamp", Moves: mv("earthquake")}},
				team{
					{Species: "Mismagius", As: "Gengar", Ability: "levitate", Item: "ringtarget",
						Moves: mv("splash")},
					{Species: "Rotom-Fan", As: "Zapdos", Ability: "levitate", Item: "ringtarget",
						Moves: mv("splash")},
				},
			)

			p.turn()
			p.fullHP(p.foe(), "Levitate should still refuse the Ground move")

			// even if Rotom-Fan
			p.makeChoices("move earthquake", "switch 2")
			p.fullHP(p.foe(), "Levitate should still refuse the Ground move")
		})

		g.it("should not affect Magnet Rise", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("earthquake")}},
				team{{Species: "Klefki", As: "Magneton", Item: "ringtarget", Moves: mv("magnetrise")}},
			)

			p.turn()
			p.fullHP(p.foe(), "Magnet Rise should still refuse the Ground move")
		})
	})
}
