//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/hydrosteam.js.
//
// Hydro Steam is not in this dataset, so every case here stops at "move
// hydrosteam is not in this dataset". They are written out in full anyway: if
// the move is ever added, these say what it has to do.
//
// Upstream's figures are absolute damage at level 100 and none of them
// transfers to an engine fixed at level 50, so each case measures the same
// attack twice — once with the sun up and once without — and asserts the
// ratio. Read against a clear-weather control, upstream's four numbers say
// 1.5x, 0.5x, 1.5x and unchanged. The bands allow for the damage roll landing
// differently in each half (a 1.5x claim survives as [125%, 180%], 0.5x as
// [41%, 60%], unchanged as [80%, 125%]) and still separate the three answers.
//
// Substitutions. Volcanion is Fire/Water and only the Water half matters here,
// since all it supplies is STAB on Hydro Steam; Blastoise builds instead.
// Koraidon is present purely as an Orichalcum Pulse body that puts the sun up.
// That ability is not in this set, so Ninetales stands in with Drought, and
// the clear-weather half of each measurement swaps Drought for Flash Fire —
// inert against a Water move — so the two battles differ in nothing but the
// weather. Lucky Chant is not in this dataset; the target only ever needed an
// inert move, so it uses Splash.

func TestMovesHydroSteam(t *testing.T) {
	// hit builds one battle and returns the damage Hydro Steam dealt. The
	// target's ability is the only difference between the sunny and the clear
	// half of a measurement.
	hit := func(p *ps, ability, userItem, foeItem string) int {
		p.battle(
			team{{Species: "Volcanion", As: "Blastoise", Ability: "waterabsorb", Item: userItem,
				Moves: mv("hydrosteam")}},
			team{{Species: "Koraidon", As: "Ninetales", Ability: ability, Item: foeItem,
				Moves: mv("splash")}},
		)
		if p.state() == nil {
			return 0 // the battle could not be built; p.battle has already said why
		}
		before := p.foe().HP
		p.turn()
		return before - p.foe().HP
	}

	// ratio is the sunny hit as a percentage of the clear-weather hit, or -1
	// when there is nothing to compare.
	ratio := func(p *ps, userItem, foeItem string) int {
		sun := hit(p, "drought", userItem, foeItem)
		clear := hit(p, "flashfire", userItem, foeItem)
		if p.state() == nil {
			return -1
		}
		p.atLeast(clear, 1, "the clear-weather control hit should have done damage")
		if clear < 1 {
			return -1
		}
		return 100 * sun / clear
	}

	describe(t, "Hydro Steam", func(g *psg) {
		g.it("should have its damage multiplied by 1.5 in Sunny Day", func(p *ps) {
			if r := ratio(p, "", ""); r >= 0 {
				p.bounded(r, 125, 180, "Hydro Steam should hit half again as hard in sun")
			}
		})

		g.it("should have its damaged halved if the user holds a Utility Umbrella", func(p *ps) {
			if r := ratio(p, "utilityumbrella", ""); r >= 0 {
				p.bounded(r, 41, 60,
					"a user under an umbrella loses the boost and keeps the sun's Water penalty")
			}
		})

		g.it("should have its damage multiplied by 1.5 if only the target holds Utility Umbrella", func(p *ps) {
			if r := ratio(p, "", "utilityumbrella"); r >= 0 {
				p.bounded(r, 125, 180, "the umbrella on the target should not cost the user its boost")
			}
		})

		g.it("should not have its damage changed if both the user and target hold Utility Umbrellas", func(p *ps) {
			if r := ratio(p, "utilityumbrella", "utilityumbrella"); r >= 0 {
				p.bounded(r, 80, 125, "two umbrellas should cancel the sun out entirely")
			}
		})
	})
}
