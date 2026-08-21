package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// behaviour_helpers_test.go is the shared vocabulary for the *_behaviour_test.go
// files: build a battle, teach a Pokémon its moves, name an action, play a turn.
//
// It exists because these tests are meant to be read as a specification. They
// are what a reimplementation of this engine gets written against — translate
// the test, then make it pass — and seven files each inventing their own words
// for "two Pokémon, these moves, one turn" makes that reading harder than it
// needs to be. One vocabulary, defined once.
//
// Everything here is fixture arrangement through exported state. None of it
// reaches into the engine: the battles it builds are driven by ResolveTurn like
// any other, which is the whole point of the layer.

// neutralBattle builds a battle with every Pokémon stripped of its ability and
// item, so a test sees only the mechanic it is about. A Snorlax that keeps its
// Immunity silently refuses a poison; a lead that keeps an entry ability moves
// the board before the first turn. Both have cost this suite a debugging
// session.
func neutralBattle(t *testing.T, d *domain.Dex, seed uint64, team0, team1 []int) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "behaviour", "P1", team0, "P2", team1, seed)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for side := range s.Sides {
		for i := range s.Sides[side].Team {
			p := &s.Sides[side].Team[i]
			p.Ability, p.Item = AbilityNone, ItemNone
		}
	}
	return s
}

// teachMoves replaces a Pokémon's move set with exactly these moves at their
// real PP, so a test can address them by index instead of hunting through a
// learnset. An unknown slug fails the test rather than skipping it: a fixture
// naming a move the dataset does not have is a broken fixture, and a skip
// would hide it behind a green run.
func teachMoves(t *testing.T, d *domain.Dex, p *Pokemon, ids ...string) {
	t.Helper()
	p.Moves = nil
	for _, id := range ids {
		m, ok := d.Moves[id]
		if !ok {
			t.Fatalf("move %q is not in the dataset", id)
		}
		p.Moves = append(p.Moves, MoveSlot{MoveID: id, PP: m.PP, MaxPP: m.PP})
	}
}

// moveAt / switchTo name the two things a player can submit.
func moveAt(i int) Action   { return Action{Kind: ActionMove, Index: i} }
func switchTo(i int) Action { return Action{Kind: ActionSwitch, Index: i} }

// playTurn resolves one turn with each side using the move in the given slot.
func playTurn(d *domain.Dex, s *BattleState, slot0, slot1 int) []LogLine {
	return ResolveTurn(d, s, [2]Action{moveAt(slot0), moveAt(slot1)})
}

// speciesBattle is neutralBattle's sibling for tests about abilities the dex
// hands out: it strips items but leaves every Pokémon its natural ability,
// because the fixture's whole point is that a Slowbro comes with Oblivious and
// a Golduck with Damp. Stripping those would leave the test arranging the very
// thing it means to observe.
func speciesBattle(t *testing.T, d *domain.Dex, seed uint64, team0, team1 []int) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "behaviour", "P1", team0, "P2", team1, seed)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for side := range s.Sides {
		for i := range s.Sides[side].Team {
			s.Sides[side].Team[i].Item = ItemNone
		}
	}
	return s
}
