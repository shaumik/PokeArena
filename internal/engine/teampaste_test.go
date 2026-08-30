package engine

import (
	"strings"
	"testing"
)

// teampaste_test.go: the paste format is only worth anything if the pastes a
// model actually writes go through. These fixtures are written the way one
// does — display names, "252 SpA / 252 Spe", a nature line — not the way the
// JSON API wanted them.

// realPaste is six standard sets in the format a model produces from memory.
// Every species here is in the dataset and every move is one it can learn;
// this is the happy path and it must be exactly that.
//
// Two details are deliberately NOT what a model would write first, because
// this format differs from standard competitive play and the difference is
// worth stating: Gengar has Cursed Body rather than Levitate (which it lost in
// Gen 7, and this dataset follows), and all six items are distinct because the
// Item Clause bans duplicates. Both mistakes are covered by their own tests
// below — this fixture exists to prove the clean path is clean.
const realPaste = `Alakazam @ Life Orb
Ability: Synchronize
EVs: 252 SpA / 4 SpD / 252 Spe
Timid Nature
IVs: 0 Atk
- Psychic
- Shadow Ball
- Focus Blast
- Recover

Gengar @ Choice Specs
Ability: Cursed Body
EVs: 252 SpA / 4 SpD / 252 Spe
Timid Nature
- Shadow Ball
- Sludge Bomb
- Focus Blast
- Thunderbolt

Snorlax @ Leftovers
Ability: Immunity
EVs: 252 HP / 252 Atk / 4 Def
Adamant Nature
- Body Slam
- Earthquake
- Crunch
- Rest

Starmie @ Expert Belt
Ability: Natural Cure
EVs: 252 SpA / 4 Def / 252 Spe
Timid Nature
- Hydro Pump
- Ice Beam
- Thunderbolt
- Rapid Spin

Zapdos @ Heavy-Duty Boots
Ability: Pressure
EVs: 248 HP / 8 SpA / 252 Spe
Timid Nature
- Thunderbolt
- Heat Wave
- Roost
- Toxic

Dragonite @ Focus Sash
Ability: Inner Focus
EVs: 252 Atk / 4 Def / 252 Spe
Adamant Nature
- Dragon Dance
- Outrage
- Earthquake
- Extreme Speed`

// TestCheckTeamPaste_AcceptsARealPaste is the whole promise: a team written
// the way a model writes one, accepted with no lookups first.
func TestCheckTeamPaste_AcceptsARealPaste(t *testing.T) {
	d := loadDex(t)
	picks, rep := CheckTeamPaste(realPaste, d)

	if !rep.OK() {
		t.Fatalf("a realistic paste was rejected:\n%s", rep.Error())
	}
	if len(picks) != TeamSize {
		t.Fatalf("parsed %d Pokémon, want %d", len(picks), TeamSize)
	}
	// The parse must be complete, not merely legal.
	for i, p := range picks {
		if p.DexNo <= 0 {
			t.Errorf("slot %d: species unresolved", i+1)
		}
		if len(p.MoveIDs) != 4 {
			t.Errorf("slot %d: got %d moves, want 4", i+1, len(p.MoveIDs))
		}
		if p.Item == "" {
			t.Errorf("slot %d: item dropped", i+1)
		}
		if p.Nature == "" {
			t.Errorf("slot %d: nature dropped", i+1)
		}
		if p.EVs == nil {
			t.Errorf("slot %d: EVs dropped", i+1)
		}
	}
}

// TestParseTeamPaste_ReadsTheHeaderForms covers the header shapes a paste
// arrives in. A nickname or a gender marker must not be mistaken for the
// species.
func TestParseTeamPaste_ReadsTheHeaderForms(t *testing.T) {
	d := loadDex(t)
	cases := []struct {
		name       string
		header     string
		wantName   string
		wantGender string
		wantItem   string
	}{
		{"bare", "Snorlax", "Snorlax", "", ""},
		{"item", "Snorlax @ Leftovers", "Snorlax", "", "leftovers"},
		{"nickname", "Chonk (Snorlax) @ Leftovers", "Snorlax", "", "leftovers"},
		{"gender", "Snorlax (M) @ Leftovers", "Snorlax", "male", "leftovers"},
		{"nickname and gender", "Chonk (Snorlax) (F) @ Leftovers", "Snorlax", "female", "leftovers"},
		{"punctuated species", "Mr. Mime @ Leftovers", "Mr. Mime", "", "leftovers"},
		{"tight punctuation", "Mr Mime", "Mr. Mime", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			picks, rep := ParseTeamPaste(c.header+"\n- Body Slam", d)
			if len(picks) != 1 {
				t.Fatalf("parsed %d blocks", len(picks))
			}
			sp, ok := d.Species[picks[0].DexNo]
			if !ok {
				t.Fatalf("species unresolved from %q:\n%s", c.header, rep.Error())
			}
			if sp.Name != c.wantName {
				t.Errorf("species = %q, want %q", sp.Name, c.wantName)
			}
			if picks[0].Gender != c.wantGender {
				t.Errorf("gender = %q, want %q", picks[0].Gender, c.wantGender)
			}
			if picks[0].Item != c.wantItem {
				t.Errorf("item = %q, want %q", picks[0].Item, c.wantItem)
			}
		})
	}
}

// TestParseTeamPaste_ApostropheSpecies: Farfetch'd is written with a straight
// quote, a curly quote, or no quote at all depending on who is typing. All
// three have to land on the same Pokémon.
func TestParseTeamPaste_ApostropheSpecies(t *testing.T) {
	d := loadDex(t)
	var want int
	for no, sp := range d.Species {
		if strings.HasPrefix(sp.Name, "Farfetch") {
			want = no
		}
	}
	if want == 0 {
		t.Skip("no Farfetch'd in this dataset")
	}
	for _, spelling := range []string{"Farfetch'd", "Farfetch’d", "Farfetchd"} {
		picks, rep := ParseTeamPaste(spelling+"\n- Peck", d)
		if len(picks) == 0 || picks[0].DexNo != want {
			t.Errorf("%q resolved to %d, want %d:\n%s", spelling, picks[0].DexNo, want, rep.Error())
		}
	}
}

// TestParseTeamPaste_IVDefaultIsPerfect guards the one place where the paste
// format's default differs from Go's zero value. An omitted IV is 31, not 0 —
// reading it as 0 would silently hand back a crippled team that still passes
// every legality check.
func TestParseTeamPaste_IVDefaultIsPerfect(t *testing.T) {
	d := loadDex(t)
	picks, _ := ParseTeamPaste("Snorlax\nIVs: 0 Atk\n- Body Slam", d)
	if len(picks) != 1 || picks[0].IVs == nil {
		t.Fatal("IVs not parsed")
	}
	ivs := *picks[0].IVs
	if ivs.Atk != 0 {
		t.Errorf("Atk IV = %d, want the stated 0", ivs.Atk)
	}
	for _, c := range []struct {
		name string
		got  int
	}{{"HP", ivs.HP}, {"Def", ivs.Def}, {"SpA", ivs.SpA}, {"SpD", ivs.SpD}, {"Spe", ivs.Spe}} {
		if c.got != MaxIV {
			t.Errorf("%s IV = %d, want %d — an unlisted IV is perfect, not zero", c.name, c.got, MaxIV)
		}
	}
}

// TestParseTeamPaste_EVDefaultIsZero is the mirror: unlisted EVs really are 0.
func TestParseTeamPaste_EVDefaultIsZero(t *testing.T) {
	d := loadDex(t)
	picks, _ := ParseTeamPaste("Snorlax\nEVs: 252 HP\n- Body Slam", d)
	if len(picks) != 1 || picks[0].EVs == nil {
		t.Fatal("EVs not parsed")
	}
	if picks[0].EVs.HP != 252 {
		t.Errorf("HP EV = %d, want 252", picks[0].EVs.HP)
	}
	if picks[0].EVs.Atk != 0 {
		t.Errorf("Atk EV = %d, want 0", picks[0].EVs.Atk)
	}
}

// TestCheckTeamPaste_UnknownSpeciesSuggestsRealOnes: the roster is a curated
// subset, so the commonest failure is a Pokémon that simply is not here. The
// answer has to point at what is.
func TestCheckTeamPaste_UnknownSpeciesSuggestsRealOnes(t *testing.T) {
	d := loadDex(t)
	_, rep := CheckTeamPaste("Landorus-Therian @ Choice Scarf\n- Earthquake", d)
	if rep.OK() {
		t.Fatal("accepted a Pokémon that is not in the dataset")
	}
	var p *Problem
	for i := range rep.Problems {
		if rep.Problems[i].Field == "species" {
			p = &rep.Problems[i]
			break
		}
	}
	if p == nil {
		t.Fatalf("no species problem:\n%s", rep.Error())
	}
	// Landorus is nothing like anything on this roster, so inventing a
	// lookalike would read as a wrong answer. The message must point at the
	// real roster instead.
	if len(p.Legal) != 0 {
		t.Errorf("suggested %v for a Pokémon with no near neighbor here", p.Legal)
	}
	if !strings.Contains(p.Message, "curated roster") {
		t.Errorf("message %q does not explain that the roster is a subset", p.Message)
	}
	if !strings.Contains(p.Message, "briefing") && !strings.Contains(p.Message, "find_pokemon") {
		t.Errorf("message %q does not say where the real roster is", p.Message)
	}
}

// TestCheckTeamPaste_MisspeltSpeciesGetsTheRealName is the other half: a name
// that IS on the roster, typed wrong, must be corrected by suggestion — with
// display names that paste straight back in.
func TestCheckTeamPaste_MisspeltSpeciesGetsTheRealName(t *testing.T) {
	d := loadDex(t)
	_, rep := CheckTeamPaste("Alakazm @ Life Orb\n- Psychic", d)

	var p *Problem
	for i := range rep.Problems {
		if rep.Problems[i].Field == "species" {
			p = &rep.Problems[i]
			break
		}
	}
	if p == nil {
		t.Fatalf("a one-character misspelling was accepted:\n%s", rep.Error())
	}
	if !containsString(p.Legal, "Alakazam") {
		t.Fatalf("suggestions %v do not include Alakazam", p.Legal)
	}
	// Every suggestion must parse back as a species, or it is not usable.
	idx := newSpeciesIndex(d)
	for _, name := range p.Legal {
		if _, ok := idx.lookup(name); !ok {
			t.Errorf("suggested %q, which does not parse back as a species", name)
		}
	}
}

// TestCheckTeamPaste_ReportsEveryBadMoveAtOnce: the paste path must inherit
// collect-all, not regress to first-error.
func TestCheckTeamPaste_ReportsEveryBadMoveAtOnce(t *testing.T) {
	d := loadDex(t)
	paste := strings.Replace(realPaste, "- Recover", "- Bullet Punch", 1)
	paste = strings.Replace(paste, "- Rapid Spin", "- Roost", 1)

	_, rep := CheckTeamPaste(paste, d)
	if rep.OK() {
		t.Skip("both substitutions happened to be legal in this dataset")
	}
	moveProblems := 0
	for _, p := range rep.Problems {
		if p.Field == "moves" {
			moveProblems++
			if len(p.Legal) == 0 {
				t.Errorf("move problem %q named no alternatives", p.Message)
			}
		}
	}
	if moveProblems < 2 {
		t.Errorf("got %d move problems, want both reported in one pass:\n%s", moveProblems, rep.Error())
	}
}

// TestParseTeamPaste_IgnoresShowdownExtras: a paste carrying fields this
// format does not model must still work. Rejecting over "Shiny: Yes" would
// defeat the point of accepting pastes at all.
func TestParseTeamPaste_IgnoresShowdownExtras(t *testing.T) {
	d := loadDex(t)
	paste := "Snorlax @ Leftovers\nAbility: Immunity\nShiny: Yes\nTera Type: Normal\nHappiness: 255\nLevel: 50\n- Body Slam"
	picks, rep := ParseTeamPaste(paste, d)
	if !rep.OK() {
		t.Fatalf("Showdown extras rejected the paste:\n%s", rep.Error())
	}
	if len(picks) != 1 || len(picks[0].MoveIDs) != 1 {
		t.Fatalf("parse dropped content: %+v", picks)
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("known-but-unused fields should be silent, got %+v", rep.Warnings)
	}
}

// TestParseTeamPaste_UnknownLineWarnsButDoesNotBlock: a genuine typo in a
// field name should surface, without costing the submission.
func TestParseTeamPaste_UnknownLineWarnsButDoesNotBlock(t *testing.T) {
	d := loadDex(t)
	picks, rep := ParseTeamPaste("Snorlax\nAbilityy: Immunity\n- Body Slam", d)
	if !rep.OK() {
		t.Fatalf("a typo'd field line blocked the paste:\n%s", rep.Error())
	}
	if len(picks) != 1 {
		t.Fatal("block not parsed")
	}
	if len(rep.Warnings) == 0 {
		t.Error("a misspelled field vanished silently")
	}
}

// TestParseTeamPaste_EmptyText names the mistake rather than panicking.
func TestParseTeamPaste_EmptyText(t *testing.T) {
	d := loadDex(t)
	if _, rep := ParseTeamPaste("   \n\n  ", d); rep.OK() {
		t.Error("empty text was accepted as a team")
	}
}

// TestCheckTeamPaste_ItemClauseIsTheMemoryTrap documents the difference from
// standard play that a memory-written team hits most often. Nothing in
// competitive Pokémon stops six Pokémon holding Leftovers; this format's Item
// Clause does. The rejection therefore has to say WHY, not just "no".
func TestCheckTeamPaste_ItemClauseIsTheMemoryTrap(t *testing.T) {
	d := loadDex(t)
	paste := strings.Replace(realPaste, "Starmie @ Expert Belt", "Starmie @ Life Orb", 1)

	_, rep := CheckTeamPaste(paste, d)
	if rep.OK() {
		t.Fatal("two Pokémon held the same item and the team was accepted")
	}
	var found bool
	for _, p := range rep.Problems {
		if p.Field == "item" && strings.Contains(p.Message, "Item Clause") {
			found = true
			if !strings.Contains(p.Message, "already held by slot") {
				t.Errorf("message %q does not say which other slot holds it", p.Message)
			}
		}
	}
	if !found {
		t.Errorf("the duplicate item was not reported as an Item Clause violation:\n%s", rep.Error())
	}
}

// TestCheckTeamPaste_StaleAbilityKnowledgeIsCorrected: a model writing from
// memory will reach for an ability the species had in an older generation.
// The rejection must name the ones it actually has, or the next guess is
// another guess.
func TestCheckTeamPaste_StaleAbilityKnowledgeIsCorrected(t *testing.T) {
	d := loadDex(t)
	paste := strings.Replace(realPaste, "Ability: Cursed Body", "Ability: Levitate", 1)

	_, rep := CheckTeamPaste(paste, d)
	if rep.OK() {
		t.Skip("this dataset gives Gengar Levitate; nothing to assert")
	}
	for _, p := range rep.Problems {
		if p.Field == "ability" {
			if len(p.Legal) == 0 {
				t.Error("a wrong ability named no alternatives")
			}
			return
		}
	}
	t.Errorf("the wrong ability was not reported:\n%s", rep.Error())
}
