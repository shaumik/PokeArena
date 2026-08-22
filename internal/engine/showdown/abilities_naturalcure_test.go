//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/naturalcure.js.
//
// Celebi's stand-in Exeggutor keeps the grass/psychic typing, so Thunder Wave
// still lands and Leech Seed is still a move it would sensibly carry; Natural
// Cure is set explicitly. Swampert is not in this dex and is only the body
// Roar drags in — Blastoise takes the slot as a water starter with the Torrent
// upstream meant. ("torrents" in the original is a typo Showdown ignores,
// falling back to the species default; writing the real slug here avoids
// reporting a nonexistent ability as a gap.)
//
// The last line is where the translation is least literal. Upstream reads
// battle.p1.pokemon[1] because Showdown moves the incoming Pokémon to index 0
// of side.pokemon on a switch, which puts the phased-out Celebi at index 1.
// This engine keeps Team in its original order with a separate active index,
// so the same Pokémon is slot 1.

func TestAbilitiesNaturalCure(t *testing.T) {
	describe(t, "Natural Cure", func(g *psg) {
		g.it("should cure even when phased out by Roar", func(p *ps) {
			p.battle(
				team{
					{Species: "Celebi", Ability: "naturalcure", Moves: mv("leechseed")},
					{Species: "Swampert", As: "Blastoise", Ability: "torrent", Moves: mv("surf")},
				},
				team{{Species: "Zapdos", Ability: "pressure", Moves: mv("thunderwave", "roar")}},
			)
			p.makeChoices("move leechseed", "move thunderwave")
			p.makeChoices("move leechseed", "move roar")
			p.notEqual(p.slot(0, 1).Status, "paralysis", "Natural Cure should have cured the Pokémon Roar phased out")
		})
	})
}
