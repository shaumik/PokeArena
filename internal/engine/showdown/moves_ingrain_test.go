//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/ingrain.js.
//
// Seismic Toss deals the user's level in damage, so upstream's `- 100` figures
// become `- 50` here. Everything else in the first case is stated as
// `maxhp/16`, which transfers unchanged.
//
// Speed, not Prankster. The first case needs three different orderings: Baton
// Pass must go before the foe's Seismic Toss so the *incoming* Pokemon eats it,
// and U-turn must go after it so the *outgoing* one does. Upstream buys the
// first with Prankster on Cradily, which this engine does not model. The same
// ordering falls out of the speed tiers if the three bodies are picked for it,
// so Cradily is built as Venusaur, Lileep as Parasect, and Miltank as Chansey —
// fastest, slowest, and in between. Rock is lost from the Cradily line and
// Normal is kept for Miltank; neither is read, since Seismic Toss is fixed
// damage and Ingrain's heal is typeless.
//
// The third case does turn on typing, so it names its own bodies: Tropius
// becomes Pidgeot for the Flying Ground-immunity and Carnivine becomes Weezing,
// which carries Levitate natively.
//
// U-turn. Upstream answers a forced-switch request after the pivot; this engine
// resolves the self-switch inside the turn and brings in the lowest live
// teammate, which is the same Pokemon.
//
// `sleeptalk` is not in this dataset; `splash` stands in for it.

func TestMovesIngrain(t *testing.T) {
	describe(t, "Ingrain", func(g *psg) {
		g.it("should heal the user by 1/16 of its max HP at the end of each turn", func(p *ps) {
			p.battle(
				team{
					{Species: "Cradily", As: "Venusaur", Moves: mv("ingrain", "batonpass")},
					{Species: "Lileep", As: "Parasect", Ability: "stormdrain", Moves: mv("ingrain", "uturn")},
				},
				team{{Species: "Miltank", As: "Chansey", Ability: "thickfat", Moves: mv("seismictoss", "protect")}},
			)
			if p.state() == nil {
				return
			}
			cradily, lileep := p.slot(0, 1), p.slot(0, 2)

			p.makeChoices("move ingrain", "move seismictoss")
			p.equal(cradily.HP, cradily.MaxHP-50+cradily.MaxHP/16,
				"Ingrain should heal 1/16 of max HP at the end of the turn")
			rooted := cradily.HP

			// should be passed by Baton Pass
			p.makeChoices("move batonpass", "move seismictoss")
			p.equal(lileep.HP, lileep.MaxHP-50+lileep.MaxHP/16,
				"Baton Pass should carry Ingrain onto the incoming Pokemon")

			// should not be passed by U-turn
			p.makeChoices("move uturn", "move seismictoss")
			p.equal(lileep.HP, lileep.MaxHP-100+lileep.MaxHP/16,
				"the pivoting Pokemon takes the Seismic Toss and leaves before the heal")
			p.equal(cradily.HP, rooted,
				"U-turn should not carry Ingrain, so the incoming Pokemon does not heal")

			// should be gone after switching out and back in
			p.makeChoices("switch 2", "move protect")
			p.equal(lileep.HP, lileep.MaxHP-100+lileep.MaxHP/16,
				"Ingrain should be gone after switching out and back in")
		})

		g.it("should prevent the user from being forced out or switching out", func(p *ps) {
			p.battle(
				team{
					{Species: "Cradily", As: "Venusaur", Ability: "stormdrain", Moves: mv("ingrain")},
					{Species: "Pikachu", Ability: "static", Moves: mv("thunder")},
				},
				team{{Species: "Arcanine", Ability: "flashfire", Moves: mv("splash", "roar")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move ingrain", "move roar")
			p.species(p.mine(), "Venusaur", "Roar should not move a rooted Pokemon")
			p.trapped(0, "Ingrain should refuse a voluntary switch as well")
		})

		g.it(`should remove the users' Ground immunities`, func(p *ps) {
			p.battle(
				team{{Species: "Tropius", As: "Pidgeot", Ability: "noability", Moves: mv("earthquake", "ingrain")}},
				team{{Species: "Carnivine", As: "Weezing", Ability: "levitate", Moves: mv("earthquake", "ingrain")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move ingrain", "move ingrain")
			p.makeChoices("move earthquake", "move earthquake")
			p.damaged(p.mine(), "Ingrain should ground a Flying-type")
			p.damaged(p.foe(), "Ingrain should ground a Levitate holder")
		})
	})
}
