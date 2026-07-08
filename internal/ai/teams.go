package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// TeamPool serves curated, validated AI teams. Loaded once at startup;
// immutable thereafter. Backs the picker-room auto-submit for mode=live,
// per docs/team-picker-room.md §8.
type TeamPool struct {
	teams [][]engine.TeamPick // list of full pick rosters
}

// LoadTeamPool reads ai-teams.json from path, expands each entry's
// species list into engine.TeamPick (first MovesMax moves from each
// species' learn list), and validates every result via
// engine.ValidateTeam. A bad team fails startup — curated data is
// curated, and surfacing the error at boot is the only acceptable
// behavior.
func LoadTeamPool(dex *domain.Dex, path string) (*TeamPool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ai teams: %w", err)
	}
	var f teamPoolFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse ai teams: %w", err)
	}

	p := &TeamPool{}
	for i, e := range f.Teams {
		picks, err := expandTeamEntry(dex, e)
		if err != nil {
			return nil, fmt.Errorf("ai teams[%d] (%s): %w", i, e.Name, err)
		}
		if err := engine.ValidateTeam(picks, dex); err != nil {
			return nil, fmt.Errorf("ai teams[%d] (%s): invalid: %w", i, e.Name, err)
		}
		p.teams = append(p.teams, picks)
	}
	if len(p.teams) == 0 {
		return nil, fmt.Errorf("ai teams: no teams declared")
	}
	return p, nil
}

// Pick returns a deep copy of a random team from the pool. The copy
// isolates callers from mutations into the engine state.
func (p *TeamPool) Pick(rng *rand.Rand) ([]engine.TeamPick, error) {
	if len(p.teams) == 0 {
		return nil, fmt.Errorf("no AI teams available")
	}
	src := p.teams[rng.Intn(len(p.teams))]
	out := make([]engine.TeamPick, len(src))
	for i, s := range src {
		out[i] = engine.TeamPick{
			DexNo:   s.DexNo,
			MoveIDs: append([]string(nil), s.MoveIDs...),
		}
	}
	return out, nil
}

// teamEntry is the JSON shape of one curated team. A curator gives EITHER
// "species" (dex numbers; moves are auto-derived from each species' learnset at
// load time, so nobody has to think about legality) OR "picks" (explicit
// dex_no + moves, same shape as the competitive team library) when the roster
// needs tuned movesets rather than the first legal four. Exactly one of the two
// must be present.
type teamEntry struct {
	Name    string            `json:"name"`
	Species []int             `json:"species,omitempty"`
	Picks   []engine.TeamPick `json:"picks,omitempty"`
}

type teamPoolFile struct {
	Teams []teamEntry `json:"teams"`
}

func expandTeamEntry(dex *domain.Dex, e teamEntry) ([]engine.TeamPick, error) {
	// Explicit tuned picks take precedence: a curator who lists moves wants
	// exactly those, not the learnset's first four. Legality is checked by the
	// caller's engine.ValidateTeam, same as the species path.
	if len(e.Picks) > 0 {
		if len(e.Species) > 0 {
			return nil, fmt.Errorf("give either species or picks, not both")
		}
		return append([]engine.TeamPick(nil), e.Picks...), nil
	}
	if len(e.Species) != engine.TeamSize {
		return nil, fmt.Errorf("species list must have %d entries, got %d", engine.TeamSize, len(e.Species))
	}
	picks := make([]engine.TeamPick, 0, engine.TeamSize)
	for _, n := range e.Species {
		sp, ok := dex.Species[n]
		if !ok {
			return nil, fmt.Errorf("unknown species dex_no=%d", n)
		}
		moves := sp.Moves
		if len(moves) > engine.MovesMax {
			moves = moves[:engine.MovesMax]
		}
		picks = append(picks, engine.TeamPick{
			DexNo:   n,
			MoveIDs: append([]string(nil), moves...),
		})
	}
	return picks, nil
}
