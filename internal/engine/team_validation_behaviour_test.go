package engine

import (
	"strings"
	"testing"
)

// TestValidateTeamRefusesAnImpossibleGender: a roster may declare a gender, and
// the declaration has to be one the species can actually be. A male Chansey and
// a female Nidoking are not legal Pokémon, and a tournament that accepted one
// would be running a match the dataset says cannot exist.
//
// Driven through ValidateTeam — the exported entry point a team file and the
// royale broker both go through — rather than the internal per-slot check, so
// the rule is stated the way any implementation of this validator has to
// satisfy it. Leaving the field empty stays legal: absent means "unspecified",
// not "invalid".
func TestValidateTeamRefusesAnImpossibleGender(t *testing.T) {
	d := loadDex(t)

	cases := []struct {
		name    string
		dexNo   int
		gender  string
		wantErr bool
	}{
		{"female Nidoking", 34, "female", true},
		{"male Nidoking", 34, "male", false},
		{"male Chansey", 113, "male", true},
		{"female Chansey", 113, "female", false},
		{"unspecified is legal", 34, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp, ok := d.Species[c.dexNo]
			if !ok {
				t.Fatalf("fixture: dex has no species %d", c.dexNo)
			}
			if len(sp.Moves) == 0 {
				t.Fatalf("fixture: %s has no learnset", sp.Name)
			}
			moves := sp.Moves
			if len(moves) > MovesMax {
				moves = moves[:MovesMax]
			}
			picks := validPicks(t, d)
			picks[0] = TeamPick{DexNo: c.dexNo, MoveIDs: append([]string(nil), moves...), Gender: c.gender}

			err := ValidateTeam(picks, d)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ValidateTeam accepted a %s", c.name)
				}
				if !strings.Contains(err.Error(), "gender") {
					t.Errorf("refusal does not name the gender rule: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateTeam refused a legal roster: %v", err)
			}
		})
	}
}
