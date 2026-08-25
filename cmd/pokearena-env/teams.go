package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	pokearena "github.com/shaumik/PokeArena"
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/eval"
)

// TeamSpec is how a client names a team. Exactly one of the three routes must
// be set; they mirror what cmd/bench accepts, plus a raw-picks escape hatch:
//
//	{"library":"Genesis"}                  a named team from the curated library
//	{"dex":[150,149,26,6,9,3]}             an ad-hoc dex-number list (bench -team)
//	{"picks":[{"dex_no":150,"moves":[…]}]} full engine.TeamPick control
//
// Two shorthands decode into the same type, because they are what people
// actually type: a bare string is a library name, and a bare array of integers
// is a dex list.
type TeamSpec struct {
	Library string            `json:"library,omitempty"`
	Dex     []int             `json:"dex,omitempty"`
	Picks   []engine.TeamPick `json:"picks,omitempty"`
}

// UnmarshalJSON accepts the object form plus the two shorthands.
func (t *TeamSpec) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err == nil {
		t.Library = name
		return nil
	}
	var dex []int
	if err := json.Unmarshal(b, &dex); err == nil {
		t.Dex = dex
		return nil
	}
	type alias TeamSpec // strip this method to avoid recursion
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return fmt.Errorf(`team must be a library name, a list of dex numbers, or an object with "library"/"dex"/"picks": %w`, err)
	}
	*t = TeamSpec(a)
	return nil
}

// IsZero reports whether the spec names nothing at all.
func (t TeamSpec) IsZero() bool {
	return t.Library == "" && len(t.Dex) == 0 && len(t.Picks) == 0
}

// resolve turns a spec into a legality-checked roster and the label that names
// it in observations and provenance.
func (t TeamSpec) resolve(dex *domain.Dex, lib *eval.TeamLibrary) (picks []engine.TeamPick, label string, err error) {
	set := 0
	for _, on := range []bool{t.Library != "", len(t.Dex) > 0, len(t.Picks) > 0} {
		if on {
			set++
		}
	}
	if set == 0 {
		return nil, "", fmt.Errorf(`team spec is empty: set one of "library", "dex", or "picks"`)
	}
	if set > 1 {
		return nil, "", fmt.Errorf(`team spec sets more than one of "library", "dex", "picks" — pick exactly one`)
	}

	switch {
	case t.Library != "":
		for _, nt := range lib.Teams {
			if nt.Name == t.Library {
				// Library teams are validated at load; hand back a copy so a
				// later episode cannot mutate the library through the slice.
				return engine.ClonePicks(nt.Picks), nt.Name, nil
			}
		}
		return nil, "", fmt.Errorf("unknown library team %q (available: %v)", t.Library, teamNames(lib))

	case len(t.Dex) > 0:
		// PicksFromDex is the same expansion cmd/bench's -team uses: first
		// MovesMax moves of each species' learn list. Going through it (rather
		// than engine.NewBattle's bare dex numbers) is what keeps the decision
		// space competitive-sized — see internal/eval/team.go.
		p, err := eval.PicksFromDex(dex, t.Dex)
		if err != nil {
			return nil, "", err
		}
		return p, "adhoc", nil

	default:
		p := engine.ClonePicks(t.Picks)
		if err := engine.ValidateTeam(p, dex); err != nil {
			return nil, "", fmt.Errorf("custom picks: %w", err)
		}
		return p, "custom", nil
	}
}

func teamNames(lib *eval.TeamLibrary) []string {
	names := make([]string, 0, len(lib.Teams))
	for _, t := range lib.Teams {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names
}

// loadTeamLibrary reads the curated team library. An explicit path reads from
// disk; the empty path reads the copy embedded at the module root (dataset.go),
// so `pokearena-env` needs no data/ directory beside it. Either way every team
// is legality-checked before it can be played — a benchmark run on an illegal
// team is meaningless.
func loadTeamLibrary(path string, dex *domain.Dex) (*eval.TeamLibrary, error) {
	if path != "" {
		return eval.LoadTeamLibrary(path, dex)
	}
	return eval.LoadTeamLibraryFS(pokearena.DataFS(), "benchmark-teams.json", dex)
}

// loadProvenance reads the dataset's identity record. Like the team library it
// prefers an explicit dataset directory and falls back to the embedded copy, so
// `handshake` can always name the dataset that produced a trajectory.
func loadProvenance(dataDir string) (eval.Provenance, error) {
	if dataDir != "" {
		return eval.LoadProvenance(dataDir)
	}
	return eval.LoadProvenanceFS(pokearena.DataFS())
}

// loadDex loads the dataset: the embedded copy by default (self-contained
// binary, no data/ on disk), or a directory when one is named.
func loadDex(dataDir, version string) (*domain.Dex, error) {
	if dataDir != "" {
		if _, err := os.Stat(dataDir); err != nil {
			return nil, fmt.Errorf("dataset directory %s: %w", dataDir, err)
		}
		return domain.LoadDex(dataDir, version)
	}
	return domain.LoadDexFS(pokearena.DataFS(), version)
}
