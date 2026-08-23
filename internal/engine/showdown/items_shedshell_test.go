//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/shedshell.js.
//
// All three cases came across. Two substitutions a reader should check:
//
// The first case uses Gothitelle's Shadow Tag, which this engine does not model
// at all — and an unmodeled ability is inert rather than refused, so keeping it
// would have produced a case that passed without ever trapping anything. Arena
// Trap on Dugtrio is the dex's trapping ability and Starmie is grounded, so the
// claim under test ("Shed Shell beats a trapping ability") is unchanged.
//
// The third case's Sky Drop is not in this dataset. It is the subject rather
// than filler, so it is kept and the missing-move failure is the finding.
//
// Heatran and Magnezone have no stand-in row: Ninetales carries the fire typing
// and Flash Fire that Heatran is here for, and Magneton keeps Magnezone's
// electric/steel and Sturdy. Both are only bodies. Sleep Talk is inert filler in
// the third case and Splash stands in for it.

func TestItemsShedShell(t *testing.T) {
	describe(t, "Shed Shell", func(g *psg) {
		g.it("should allow Pokemon to escape trapping abilities", func(p *ps) {
			p.battle(
				team{{Species: "Dugtrio", Ability: "arenatrap", Moves: mv("calmmind")}},
				team{
					{Species: "Starmie", Ability: "naturalcure", Item: "shedshell", Moves: mv("recover")},
					{Species: "Ninetales", Ability: "flashfire", Moves: mv("rest")},
				},
			)
			p.notTrapped(1, "Shed Shell should beat a trapping ability")
			p.makeChoices("move calmmind", "switch 2")
			p.species(p.foe(), "Ninetales", "")
		})

		g.it("should allow Pokemon to escape from most moves that would trap them", func(p *ps) {
			p.battle(
				team{{Species: "Gengar", Ability: "levitate", Moves: mv("meanlook")}},
				team{
					{Species: "Venusaur", Ability: "overgrow", Item: "shedshell", Moves: mv("ingrain")},
					{Species: "Ninetales", Ability: "flashfire", Moves: mv("rest")},
				},
			)
			p.makeChoices("move meanlook", "move ingrain")
			p.notTrapped(1, "Shed Shell should beat both Mean Look and the holder's own Ingrain")
			p.makeChoices("move meanlook", "switch 2")
			p.species(p.foe(), "Ninetales", "")
		})

		g.it("should not allow Pokemon to escape from Sky Drop", func(p *ps) {
			p.battle(
				team{{Species: "Dragonite", Ability: "multiscale", Moves: mv("skydrop")}},
				team{
					{Species: "Magneton", Ability: "sturdy", Item: "shedshell", Moves: mv("splash")},
					{Species: "Ninetales", Ability: "flashfire", Moves: mv("rest")},
				},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move skydrop", "move splash")
			p.trapped(1, "Sky Drop should hold its target even through Shed Shell")
			p.makeChoices("move skydrop", "switch 2")
			p.species(p.foe(), "Magneton", "")
		})
	})
}
