//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/mail.js.
//
// Mail is not in this item set. Every case here is about Mail specifically, so
// all four keep it and the missing-item failure is the finding.
//
// Fennekin is only a Magician body; Ninetales carries the ability instead, and
// whether the engine models Magician is reported by the harness. Pangoro is only
// an Iron Fist body that holds or steals things, so Hitmonchan — the in-dex Iron
// Fist user — stands in.

func TestItemsMail(t *testing.T) {
	describe(t, "Mail", func(g *psg) {
		g.it("should not be stolen by most moves or abilities", func(p *ps) {
			p.battle(
				team{{Species: "Blissey", Ability: "naturalcure", Item: "mail", Moves: mv("softboiled")}},
				team{
					{Species: "Fennekin", As: "Ninetales", Ability: "magician", Moves: mv("grassknot")},
					{Species: "Abra", Ability: "synchronize", Moves: mv("trick")},
					{Species: "Lopunny", Ability: "klutz", Moves: mv("switcheroo")},
				},
			)
			if p.state() == nil {
				return
			}
			holder := p.mine()
			p.constant(func() any { return holder.Item }, func() {
				p.makeChoices("move softboiled", "move grassknot")
			}, "Magician should not take Mail")
			p.makeChoices("move softboiled", "switch 2")
			p.constant(func() any { return holder.Item }, func() {
				p.makeChoices("move softboiled", "move trick")
			}, "Trick should not take Mail")
			p.makeChoices("move softboiled", "switch 3")
			p.constant(func() any { return holder.Item }, func() {
				p.makeChoices("move softboiled", "move switcheroo")
			}, "Switcheroo should not take Mail")
		})

		g.it("should not be removed by Fling", func(p *ps) {
			p.battle(
				team{{Species: "Pangoro", As: "Hitmonchan", Ability: "ironfist", Moves: mv("swordsdance")}},
				team{{Species: "Abra", Ability: "synchronize", Item: "mail", Moves: mv("fling")}},
			)
			if p.state() == nil {
				return
			}
			holder := p.foe()
			p.constant(func() any { return holder.Item }, func() {
				p.makeChoices("move swordsdance", "move fling")
			}, "Fling should not throw Mail")
		})

		g.it("should be removed by Knock Off", func(p *ps) {
			p.battle(
				team{{
					Species: "Pangoro", As: "Hitmonchan", Ability: "ironfist", Item: "mail",
					Moves: mv("swordsdance"),
				}},
				team{{Species: "Abra", Ability: "synchronize", Moves: mv("knockoff")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move swordsdance", "move knockoff")
			p.noItem(p.mine(), "Knock Off should take Mail")
		})

		g.it("should be stolen by Thief", func(p *ps) {
			p.battle(
				team{{
					Species: "Pangoro", As: "Hitmonchan", Ability: "ironfist", Item: "mail",
					Moves: mv("swordsdance"),
				}},
				team{{Species: "Abra", Ability: "synchronize", Moves: mv("thief")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move swordsdance", "move thief")
			p.noItem(p.mine(), "Thief should take Mail")
		})
	})
}
