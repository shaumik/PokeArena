//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/painsplit.js.
//
// Pain Split is not in this dataset; the first case reports the missing move
// rather than the averaging it is about.
//
// Shedinja is the one substitution the shared table deliberately refuses, and
// rightly — but nothing in this case needs Wonder Guard as such. What it needs
// is a user sitting on 1 HP that the target's attack cannot touch, and the two
// halves separate cleanly here: the HP field puts the user on 1, and Gengar's
// Ghost typing supplies the immunity to a Normal-type attack that Wonder Guard
// supplied upstream. The ability is stripped rather than named, since it is no
// longer doing the work.
//
// Arceus likewise becomes Snorlax, a large Normal body with no Pokedex entry
// to lose here, and Judgment — absent from the dataset, and Normal-typed in
// upstream's plateless set — becomes Body Slam. Gengar outspeeds Snorlax, so
// Pain Split resolves against a full-HP target either way.

func TestMovesPainSplit(t *testing.T) {
	describe(t, "Pain Split", func(g *psg) {
		g.it("should reduce the HP of the target to the average of the user and target", func(p *ps) {
			p.battle(
				team{{Species: "Shedinja", As: "Gengar", Ability: "noability", HP: 1,
					Moves: mv("painsplit")}},
				team{{Species: "Arceus", As: "Snorlax", Ability: "noability", Moves: mv("bodyslam")}},
			)
			target := p.foe()
			p.makeChoices("move painsplit", "move bodyslam")
			p.equal(target.HP, (target.MaxHP+1)/2,
				"Pain Split should leave the target on the average of the two HP totals")
		})

		g.skip("should calculate HP changes against a dynamaxed target properly", "Dynamax")
	})
}
