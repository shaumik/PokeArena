package engine

import (
	"strings"
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// picksWithMove returns a legal six-mon team whose first slot actually learns
// moveID, so a test can plant a clause-breaking move without hardcoding a
// roster. Searches the whole dex rather than requiring the default fixture to
// happen to learn it.
func picksWithMove(t *testing.T, d *domain.Dex, moveID string) []TeamPick {
	t.Helper()
	var learner domain.Species
	for _, sp := range d.AllSpecies() {
		for _, m := range sp.Moves {
			if m == moveID {
				learner = sp
				break
			}
		}
		if learner.DexNo != 0 {
			break
		}
	}
	if learner.DexNo == 0 {
		t.Skipf("no species in the dex learns %s", moveID)
	}
	picks := validPicks(t, d)
	// Replace whichever slot the learner already occupies, if any, so the
	// Species Clause doesn't fire on the substitution.
	slot := 0
	for i, p := range picks {
		if p.DexNo == learner.DexNo {
			slot = i
		}
	}
	picks[slot] = TeamPick{DexNo: learner.DexNo, MoveIDs: []string{moveID}}
	return picks
}

// TestEvasionClauseRefusesEvasionMoves: six Double Team was legal. Evasion
// turns a game of prediction into a game of dice, which is why every
// competitive format bans it.
func TestEvasionClauseRefusesEvasionMoves(t *testing.T) {
	d := loadDex(t)
	for _, moveID := range []string{"double-team", "minimize"} {
		picks := picksWithMove(t, d, moveID)

		err := ValidateTeam(picks, d)
		if err == nil {
			t.Fatalf("%s should be refused by the Evasion Clause", moveID)
		}
		if !strings.Contains(err.Error(), "Evasion Clause") {
			t.Errorf("%s: error should name the clause, got %v", moveID, err)
		}

		// And it is the clause doing it, not ordinary legality.
		relaxed := StandardClauses()
		relaxed.Evasion = false
		if err := ValidateTeamWithClauses(picks, d, relaxed); err != nil {
			t.Errorf("%s should be legal with the clause off, got %v", moveID, err)
		}
	}
}

// TestEvasionClauseAllowsLoweringTheFoesEvasion: the clause is about becoming
// hard to hit. Sweet Scent drops the *foe's* evasion and is fine — reading
// the boosts block without checking the target would ban it.
func TestEvasionClauseAllowsLoweringTheFoesEvasion(t *testing.T) {
	d := loadDex(t)
	m, ok := d.Moves["sweet-scent"]
	if !ok {
		t.Skip("sweet-scent not in dataset")
	}
	if moveRaisesEvasion(m) {
		t.Error("Sweet Scent lowers the foe's evasion and must not trip the clause")
	}
	if got := m.Primary.Boosts["evasion"]; got >= 0 {
		t.Fatalf("fixture assumption broken: sweet-scent evasion boost = %d, want negative", got)
	}
}

// TestOHKOClauseRefusesOHKOMoves: a 30% coin flip that ignores every stat on
// the board.
func TestOHKOClauseRefusesOHKOMoves(t *testing.T) {
	d := loadDex(t)
	for _, moveID := range []string{"fissure", "horn-drill", "guillotine", "sheer-cold"} {
		if _, ok := d.Moves[moveID]; !ok {
			continue
		}
		picks := picksWithMove(t, d, moveID)

		err := ValidateTeam(picks, d)
		if err == nil {
			t.Fatalf("%s should be refused by the OHKO Clause", moveID)
		}
		if !strings.Contains(err.Error(), "OHKO Clause") {
			t.Errorf("%s: error should name the clause, got %v", moveID, err)
		}
	}
}

// TestItemClauseRefusesDuplicates: six Leftovers was legal.
func TestItemClauseRefusesDuplicates(t *testing.T) {
	d := loadDex(t)
	picks := validPicks(t, d)
	picks[0].Item = "leftovers"
	picks[3].Item = "leftovers"

	err := ValidateTeam(picks, d)
	if err == nil {
		t.Fatal("two Leftovers should be refused by the Item Clause")
	}
	if !strings.Contains(err.Error(), "Item Clause") {
		t.Errorf("error should name the clause, got %v", err)
	}

	// Distinct items are fine, and so is any number of empty hands.
	picks[3].Item = "choice-band"
	if err := ValidateTeam(picks, d); err != nil {
		t.Errorf("distinct items should be legal, got %v", err)
	}
	for i := range picks {
		picks[i].Item = ""
	}
	if err := ValidateTeam(picks, d); err != nil {
		t.Errorf("an empty-handed team should be legal, got %v", err)
	}
}

// TestBaselineTeamStaysLegal: the clauses must not reject an ordinary team.
// validPicks takes the first four moves of each species' learnset, so this
// also catches a clause that is far too broad.
func TestBaselineTeamStaysLegal(t *testing.T) {
	d := loadDex(t)
	picks := validPicks(t, d)
	for i := range picks {
		filtered := picks[i].MoveIDs[:0]
		for _, id := range picks[i].MoveIDs {
			m := d.Moves[id]
			if !moveRaisesEvasion(m) && m.OHKO == "" {
				filtered = append(filtered, id)
			}
		}
		if len(filtered) == 0 {
			t.Skip("fixture roster is all clause-banned moves")
		}
		picks[i].MoveIDs = filtered
	}
	if err := ValidateTeam(picks, d); err != nil {
		t.Errorf("an ordinary team should pass every clause, got %v", err)
	}
}

// --- Sleep Clause ---

// sleepBattle puts two live Pokémon on side 1's bench so the clause has
// something to protect.
func sleepBattle(t *testing.T, d *domain.Dex) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143, 3, 6}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	return s
}

// TestSleepClauseAllowsOnePerSide is the reported gap: sleep is 2–4 turns of
// doing nothing, and without a cap a team can legally be slept in its
// entirety. Combined with Spore being 100% accurate, that is the whole game.
func TestSleepClauseAllowsOnePerSide(t *testing.T) {
	d := loadDex(t)
	s := sleepBattle(t, d)
	var log []LogLine

	// First sleep lands.
	if !inflictStatusFrom(s.Active(1), 1, 0, StatusSleep, s, NewRNG(1), &log) {
		t.Fatal("the first sleep on a side should land")
	}
	if s.Active(1).Status != StatusSleep {
		t.Fatal("the target should be asleep")
	}

	// A second one, on a different team member, is refused.
	bench := &s.Sides[1].Team[1]
	log = nil
	if inflictStatusFrom(bench, 1, 0, StatusSleep, s, NewRNG(1), &log) {
		t.Error("a second sleep on the same side should be refused by the Sleep Clause")
	}
	if bench.Status == StatusSleep {
		t.Error("the second target should still be awake")
	}
	if !logHas(log, "Sleep Clause") {
		t.Errorf("the refusal should say why; got %v", logTexts(log))
	}
}

// TestSleepClauseIsPerSide: one side's sleeper doesn't shield the other.
func TestSleepClauseIsPerSide(t *testing.T) {
	d := loadDex(t)
	s := sleepBattle(t, d)
	var log []LogLine

	if !inflictStatusFrom(s.Active(1), 1, 0, StatusSleep, s, NewRNG(1), &log) {
		t.Fatal("setup: side 1 should fall asleep")
	}
	if !inflictStatusFrom(s.Active(0), 0, 1, StatusSleep, s, NewRNG(1), &log) {
		t.Error("side 0 has nobody asleep; its own sleep should still land")
	}
}

// TestSleepClauseFreesUpOnWake: the cap is on Pokémon currently asleep, not
// on sleeps per battle.
func TestSleepClauseFreesUpOnWake(t *testing.T) {
	d := loadDex(t)
	s := sleepBattle(t, d)
	var log []LogLine

	if !inflictStatusFrom(s.Active(1), 1, 0, StatusSleep, s, NewRNG(1), &log) {
		t.Fatal("setup: the first sleep should land")
	}
	clearStatus(s.Active(1))
	if !inflictStatusFrom(&s.Sides[1].Team[1], 1, 0, StatusSleep, s, NewRNG(1), &log) {
		t.Error("with the sleeper awake, the next sleep should land")
	}
}

// TestSleepClauseIgnoresFaintedSleepers: a Pokémon that fainted asleep is not
// holding the slot hostage.
func TestSleepClauseIgnoresFaintedSleepers(t *testing.T) {
	d := loadDex(t)
	s := sleepBattle(t, d)
	s.Sides[1].Team[1].Status = StatusSleep
	s.Sides[1].Team[1].Fainted = true
	s.Sides[1].Team[1].HP = 0

	var log []LogLine
	if !inflictStatusFrom(s.Active(1), 1, 0, StatusSleep, s, NewRNG(1), &log) {
		t.Error("a fainted sleeper should not block the clause slot")
	}
}

// TestRestBypassesSleepClause: self-inflicted sleep is exempt in canon, and
// here that falls out of the call graph — Rest goes through inflictStatus
// while the clause sits in inflictStatusFrom.
func TestRestBypassesSleepClause(t *testing.T) {
	d := loadDex(t)
	s := sleepBattle(t, d)
	var log []LogLine

	if !inflictStatusFrom(s.Active(1), 1, 0, StatusSleep, s, NewRNG(1), &log) {
		t.Fatal("setup: the first sleep should land")
	}
	// Rest's path: sourceless, straight to inflictStatus.
	bench := &s.Sides[1].Team[1]
	if !inflictStatus(bench, 1, StatusSleep, s, NewRNG(1), &log) {
		t.Error("Rest should still work while a teammate is asleep")
	}
}

// TestSleepClauseOnlyGatesSleep: every other status is unaffected.
func TestSleepClauseOnlyGatesSleep(t *testing.T) {
	d := loadDex(t)
	s := sleepBattle(t, d)
	var log []LogLine

	if !inflictStatusFrom(s.Active(1), 1, 0, StatusSleep, s, NewRNG(1), &log) {
		t.Fatal("setup: the first sleep should land")
	}
	bench := &s.Sides[1].Team[1]
	if !inflictStatusFrom(bench, 1, 0, StatusParalysis, s, NewRNG(1), &log) {
		t.Error("paralysis has no clause and should land")
	}
}

// TestEvasionClauseReadsSelfTargetedSecondaries: a secondary's boosts
// normally land on the target, but they can be aimed at the user
// (Effect.Self). No curated move raises evasion that way today, so this is
// the only thing standing between the clause and a future move that walks
// around it — a synthetic move, because the dataset can't supply one.
func TestEvasionClauseReadsSelfTargetedSecondaries(t *testing.T) {
	selfRaise := domain.Move{
		Name: "test-self-evasion", Category: domain.CatPhysical, Power: 50,
		Secondaries: []domain.Effect{{Self: true, Chance: 100, Boosts: map[string]int{"evasion": 1}}},
	}
	if !moveRaisesEvasion(selfRaise) {
		t.Error("a self-targeted evasion boost on a secondary should trip the Evasion Clause")
	}

	// The same boost aimed at the target is a gift to the opponent, not an
	// evasion strategy, and must not be refused.
	foeRaise := domain.Move{
		Name: "test-foe-evasion", Category: domain.CatPhysical, Power: 50,
		Secondaries: []domain.Effect{{Chance: 100, Boosts: map[string]int{"evasion": 1}}},
	}
	if moveRaisesEvasion(foeRaise) {
		t.Error("a target-side evasion boost should not trip the clause")
	}
}
