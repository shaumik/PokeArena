package engine

import "testing"

// newRampageBattle pits a Dragonite holding only Outrage against a pure-Fairy
// Clefable (immune to Dragon). Immunity means Outrage deals 0 damage so the
// foe never faints and the battle stays in PhaseChoosing across the whole
// rampage — and it exercises the canonical Gen-6+ behavior that Outrage into a
// Fairy still locks the user in and still ends in fatigue. The bench Charizard
// gives the user a live switch target the lock must suppress.
func newRampageBattle(t *testing.T, seed uint64) *BattleState {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{149, 6}, "P2", []int{36}, seed) // Dragonite+Charizard vs Clefable
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "outrage", PP: 10, MaxPP: 10}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	return s
}

// TestOutrageLocksUserIn: using Outrage sets the lock and pins the user to that
// one move — no switch is offered even though a live bench member exists.
func TestOutrageLocksUserIn(t *testing.T) {
	d := loadDex(t)
	s := newRampageBattle(t, 7)

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Active(0).Volatiles.LockedMove == nil {
		t.Fatal("LockedMove should be set after using Outrage")
	}
	la := LegalActions(s, 0)
	if len(la) != 1 || la[0].Kind != ActionMove || la[0].Index != 0 {
		t.Errorf("locked LegalActions = %+v, want the single Outrage move with no switch", la)
	}
}

// TestOutrageRampageConfusesAndSpendsOnePP drives the rampage to its end and
// checks the three observable contracts: the user is forced to repeat Outrage
// for 2-3 turns, can't switch while locked, spends PP exactly once, and is left
// confused ("fatigue") with the lock cleared once it runs out.
func TestOutrageRampageConfusesAndSpendsOnePP(t *testing.T) {
	d := loadDex(t)
	s := newRampageBattle(t, 7)

	uses := 0
	fatigueTurn := -1
	for turn := 1; turn <= 4 && fatigueTurn == -1; turn++ {
		if s.Phase != PhaseChoosing {
			t.Fatalf("turn %d: battle left PhaseChoosing (phase=%s)", turn, s.Phase)
		}
		lockedBefore := s.Active(0).Volatiles.LockedMove != nil
		if lockedBefore {
			for _, a := range LegalActions(s, 0) {
				if a.Kind == ActionSwitch {
					t.Fatalf("turn %d: a switch was offered while locked into Outrage", turn)
				}
			}
		}

		log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		if logHas(log, "used Outrage") {
			uses++
		}
		if logHas(log, "confused due to fatigue") {
			fatigueTurn = turn
			if s.Active(0).Volatiles.LockedMove != nil {
				t.Error("LockedMove should clear once the rampage ends in fatigue")
			}
			if s.Active(0).Volatiles.Confusion == nil {
				t.Error("user should be confused after the rampage ends")
			}
		}
	}

	if fatigueTurn == -1 {
		t.Fatalf("rampage never ended in fatigue within 4 turns (used %d times)", uses)
	}
	if uses < 2 || uses > 3 {
		t.Errorf("Outrage was used %d times across the rampage, want 2 or 3", uses)
	}
	if got := s.Active(0).Moves[0].PP; got != 9 {
		t.Errorf("Outrage PP = %d, want 9 (a rampage spends PP only on the first turn)", got)
	}
}

// TestLockedMoveClearsOnSwitchOut: a forced switch (faint replacement) wipes the
// rampage so the incoming Pokémon isn't locked into a move it doesn't have.
func TestLockedMoveClearsOnSwitchOut(t *testing.T) {
	d := loadDex(t)
	s := newRampageBattle(t, 7)

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.Active(0).Volatiles.LockedMove == nil {
		t.Fatal("precondition: user should be locked after Outrage")
	}

	// Manually switch the user out to its bench Charizard.
	var log []LogLine
	doSwitch(s, 0, 1, &log)
	if s.Active(0).Volatiles.LockedMove != nil {
		t.Error("LockedMove should be cleared on switch-out")
	}
}
