package eval

import (
	"encoding/json"
	"fmt"
	"os"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// A team library is the benchmark's curated, versioned set of competitive
// teams. It is a first-class data artifact (data/benchmark-teams.json), pinned
// to the benchmark version the same way the dex is — so a published result
// names exactly which teams produced it. Every team is legality-checked on
// load; the battle benchmark mirror-matches each team to isolate the policy.

// NamedTeam is one competitive team: a display name, an optional style label,
// and six legality-checked picks. Picks unmarshal directly as engine.TeamPick
// (dex_no + moves), so the file format and the engine's team type never drift.
type NamedTeam struct {
	Name  string            `json:"name"`
	Style string            `json:"style,omitempty"`
	Picks []engine.TeamPick `json:"picks"`
}

// TeamLibrary is the whole versioned collection.
type TeamLibrary struct {
	Version string      `json:"version"`
	Note    string      `json:"note,omitempty"`
	Teams   []NamedTeam `json:"teams"`
}

// LoadTeamLibrary reads and validates a team library file. Every team must pass
// engine.ValidateTeam (6 mons, Species Clause, 1-4 learnset-legal moves each);
// the first illegal team is a hard error naming the team, because a benchmark
// run on an illegal team is meaningless.
func LoadTeamLibrary(path string, dex *domain.Dex) (*TeamLibrary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read team library: %w", err)
	}
	var lib TeamLibrary
	if err := json.Unmarshal(raw, &lib); err != nil {
		return nil, fmt.Errorf("parse team library: %w", err)
	}
	if len(lib.Teams) == 0 {
		return nil, fmt.Errorf("team library %s has no teams", path)
	}
	for i, team := range lib.Teams {
		if team.Name == "" {
			return nil, fmt.Errorf("team #%d has no name", i+1)
		}
		if err := engine.ValidateTeam(team.Picks, dex); err != nil {
			return nil, fmt.Errorf("team %q: %w", team.Name, err)
		}
	}
	return &lib, nil
}

// Mirror returns a team as both sides — the variance-controlled setup the
// battle benchmark runs: identical rosters, so the only free variable is the
// policy.
func (t NamedTeam) Mirror() [2][]engine.TeamPick {
	return [2][]engine.TeamPick{t.Picks, t.Picks}
}
