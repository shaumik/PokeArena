package engine

import (
	"strings"
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// team_report_test.go: the promise is that one submission teaches you
// everything wrong with a team, and that each finding names a way out.

// TestCheckTeam_ReportsEveryProblemAtOnce is the headline. Three independent
// mistakes across three slots used to cost three round trips, because
// validation returned the first error and stopped.
func TestCheckTeam_ReportsEveryProblemAtOnce(t *testing.T) {
	d := loadDex(t)
	picks := validPicks(t, d)

	picks[0].MoveIDs[0] = "blast-burn-of-doom"       // no such move
	picks[2].Ability = "this-ability-does-not-exist" // not this species'
	picks[4].Item = "eviolite"                       // not in the catalog

	rep := CheckTeam(picks, d)
	if rep.OK() {
		t.Fatal("CheckTeam accepted a team with three defects")
	}
	if len(rep.Problems) < 3 {
		t.Fatalf("got %d problems, want at least 3:\n%s", len(rep.Problems), rep.Error())
	}

	// Each mistake must be reported against the slot that actually holds it.
	bySlot := map[int]Problem{}
	for _, p := range rep.Problems {
		bySlot[p.Slot] = p
	}
	for slot, field := range map[int]string{1: "moves", 3: "ability", 5: "item"} {
		p, ok := bySlot[slot]
		if !ok {
			t.Errorf("no problem reported for slot %d:\n%s", slot, rep.Error())
			continue
		}
		if p.Field != field {
			t.Errorf("slot %d: field = %q, want %q", slot, p.Field, field)
		}
		// Every dead end must lead somewhere: either a near-miss worth
		// trying, or a pointer to the list. "eviolite" and "bullet-punch"
		// are real Pokémon things this curated format does not carry, and
		// they have no near neighbor — so the message has to say where the
		// full list is instead of inventing a lookalike.
		if len(p.Legal) == 0 && !namesAWayForward(p.Message) {
			t.Errorf("slot %d (%s): %q offers neither an alternative nor a place to look",
				slot, field, p.Message)
		}
	}
}

// TestCheckTeam_SuggestionsAreActuallyLegal: a suggestion that would itself be
// rejected is worse than no suggestion, because it costs another round trip
// and teaches the wrong thing. Every named alternative must pass.
func TestCheckTeam_SuggestionsAreActuallyLegal(t *testing.T) {
	d := loadDex(t)
	picks := validPicks(t, d)
	sp := d.Species[picks[0].DexNo]

	// A near-miss typo, the realistic case: one character off a real move.
	want := sp.Moves[0]
	picks[0].MoveIDs[0] = want + "x"

	rep := CheckTeam(picks, d)
	var moveProblem *Problem
	for i := range rep.Problems {
		if rep.Problems[i].Slot == 1 && rep.Problems[i].Field == "moves" {
			moveProblem = &rep.Problems[i]
			break
		}
	}
	if moveProblem == nil {
		t.Fatalf("no move problem for a one-character typo:\n%s", rep.Error())
	}
	if len(moveProblem.Legal) == 0 {
		t.Fatal("a one-character typo produced no suggestions")
	}
	learn := map[string]bool{}
	for _, id := range sp.Moves {
		learn[id] = true
	}
	for _, s := range moveProblem.Legal {
		if !learn[s] {
			t.Errorf("suggested %q, which %s cannot learn", s, sp.Name)
		}
	}
	// The move actually meant should be among them.
	if !containsString(moveProblem.Legal, want) {
		t.Errorf("suggestions %v do not include %q, the obvious intent", moveProblem.Legal, want)
	}
}

// TestCheckTeam_UnknownSpeciesDoesNotSuppressTheRest: a bad dex number skips
// its own slot (no learnset to check against) but must not hide defects
// elsewhere on the team.
func TestCheckTeam_UnknownSpeciesDoesNotSuppressTheRest(t *testing.T) {
	d := loadDex(t)
	picks := validPicks(t, d)
	picks[0].DexNo = 99999
	picks[3].Item = "eviolite"

	rep := CheckTeam(picks, d)
	var sawSpecies, sawItem bool
	for _, p := range rep.Problems {
		if p.Slot == 1 && p.Field == "species" {
			sawSpecies = true
			if len(p.Legal) == 0 {
				t.Error("an unknown dex number named no nearby species")
			}
		}
		if p.Slot == 4 && p.Field == "item" {
			sawItem = true
		}
	}
	if !sawSpecies {
		t.Errorf("unknown species not reported:\n%s", rep.Error())
	}
	if !sawItem {
		t.Errorf("the later item defect was suppressed by the unknown species:\n%s", rep.Error())
	}
}

// TestCheckTeam_WarnsNatureFightingItsMoves is the "did I build a good team"
// half. A nature that lowers the stat its holder attacks with is legal and
// quietly costs 10% of the Pokémon's whole job.
func TestCheckTeam_WarnsNatureFightingItsMoves(t *testing.T) {
	d := loadDex(t)
	picks := validPicks(t, d)

	slot, nature, move := findSelfDefeatingSpread(t, d, picks)
	picks[slot].MoveIDs = []string{move}
	picks[slot].Nature = nature

	rep := CheckTeam(picks, d)
	if !rep.OK() {
		t.Fatalf("the team should be LEGAL; a warning must not reject it:\n%s", rep.Error())
	}
	if err := ValidateTeam(picks, d); err != nil {
		t.Fatalf("ValidateTeam rejected a merely-suspect team: %v", err)
	}

	var found bool
	for _, w := range rep.Warnings {
		if w.Slot == slot+1 && w.Field == "nature" {
			found = true
			if !strings.Contains(w.Message, "legal") {
				t.Errorf("warning %q does not make clear the team is still legal", w.Message)
			}
		}
	}
	if !found {
		t.Errorf("no nature warning for a self-defeating spread; warnings = %+v", rep.Warnings)
	}
}

// TestCheckTeam_FixedDamageMovesAreExemptFromTheNatureWarning guards the
// exemption that keeps the warning trustworthy. Seismic Toss deals damage
// equal to the user's level whatever its Attack is, which is exactly why a
// Chansey runs a minus-Attack nature and pays nothing. Flagging that would
// train builders to ignore warnings.
func TestCheckTeam_FixedDamageMovesAreExemptFromTheNatureWarning(t *testing.T) {
	d := loadDex(t)
	picks := validPicks(t, d)

	slot, nature, move := findFixedDamageSpread(t, d, picks)
	picks[slot].MoveIDs = []string{move}
	picks[slot].Nature = nature

	rep := CheckTeam(picks, d)
	for _, w := range rep.Warnings {
		if w.Slot == slot+1 && w.Field == "nature" {
			t.Errorf("warned about %s with a fixed-damage move: %s", nature, w.Message)
		}
	}
}

// TestCheckTeam_WarningsNeverBlock: warnings must not turn into problems by
// any route, because a builder who cannot submit a legal team will stop
// trusting the distinction.
func TestCheckTeam_WarningsNeverBlock(t *testing.T) {
	d := loadDex(t)
	picks := validPicks(t, d)
	slot, nature, move := findSelfDefeatingSpread(t, d, picks)
	picks[slot].MoveIDs = []string{move}
	picks[slot].Nature = nature

	rep := CheckTeam(picks, d)
	if len(rep.Warnings) == 0 {
		t.Skip("fixture produced no warning; nothing to assert")
	}
	if !rep.OK() {
		t.Errorf("warnings blocked a legal team:\n%s", rep.Error())
	}
	if ValidateTeam(picks, d) != nil {
		t.Error("ValidateTeam rejected a team whose only findings were warnings")
	}
}

// TestTeamReport_ErrorListsEveryProblem: a caller that only ever prints the
// error — which is most of them — must still see the whole list.
func TestTeamReport_ErrorListsEveryProblem(t *testing.T) {
	d := loadDex(t)
	picks := validPicks(t, d)
	// Near-misses, so both findings carry suggestions: this test is about
	// Error() rendering the whole list, suggestions included.
	picks[0].MoveIDs[0] = d.Species[picks[0].DexNo].Moves[1] + "x"
	picks[1].Item = "lief-orb"

	err := ValidateTeam(picks, d)
	if err == nil {
		t.Fatal("expected a rejection")
	}
	msg := err.Error()
	if strings.Count(msg, "slot ") < 2 {
		t.Errorf("Error() names fewer than both slots:\n%s", msg)
	}
	if !strings.Contains(msg, "try:") {
		t.Errorf("Error() drops the suggestions:\n%s", msg)
	}
}

// TestValidateTeam_LegalTeamReturnsUntypedNil guards the classic typed-nil
// trap: returning a (*TeamReport)(nil) through an error interface would make
// every legal team read as rejected at all fifteen call sites.
func TestValidateTeam_LegalTeamReturnsUntypedNil(t *testing.T) {
	d := loadDex(t)
	if err := ValidateTeam(validPicks(t, d), d); err != nil {
		t.Fatalf("legal team rejected: %v", err)
	}
}

// --- fixture helpers -------------------------------------------------------

// findSelfDefeatingSpread finds a slot, a nature and a damaging move where the
// nature lowers exactly the stat the move scales off. Derived from the dataset
// rather than hardcoded, so it keeps working as the dex grows.
func findSelfDefeatingSpread(t *testing.T, d *domain.Dex, picks []TeamPick) (slot int, nature, move string) {
	t.Helper()
	for i, p := range picks {
		sp := d.Species[p.DexNo]
		for _, mid := range sp.Moves {
			m, ok := d.Moves[mid]
			if !ok || m.HasFlag("fixed-damage-level") {
				continue
			}
			stat, damaging := statForCategory[m.Category]
			if !damaging {
				continue
			}
			for _, n := range d.Natures {
				if n.Minus == stat {
					return i, n.ID, mid
				}
			}
		}
	}
	t.Fatal("no self-defeating spread available in this dataset")
	return 0, "", ""
}

// findFixedDamageSpread finds the exempt case: a fixed-damage move paired with
// a nature that lowers the stat it would otherwise have used.
func findFixedDamageSpread(t *testing.T, d *domain.Dex, picks []TeamPick) (slot int, nature, move string) {
	t.Helper()
	for i, p := range picks {
		sp := d.Species[p.DexNo]
		for _, mid := range sp.Moves {
			m, ok := d.Moves[mid]
			if !ok || !m.HasFlag("fixed-damage-level") {
				continue
			}
			stat, damaging := statForCategory[m.Category]
			if !damaging {
				continue
			}
			for _, n := range d.Natures {
				if n.Minus == stat {
					return i, n.ID, mid
				}
			}
		}
	}
	t.Skip("no fixed-damage move on any fixture species; nothing to assert")
	return 0, "", ""
}

// namesAWayForward reports whether a message tells the reader where to find
// the legal values, for the cases where no near-miss exists.
func namesAWayForward(msg string) bool {
	for _, hint := range []string{"get_pokemon", "list_items", "list_natures", "find_pokemon", "briefing"} {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

func containsString(haystack []string, want string) bool {
	for _, h := range haystack {
		if h == want {
			return true
		}
	}
	return false
}
