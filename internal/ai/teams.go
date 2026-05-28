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

// TeamPool serves curated, validated AI teams indexed by difficulty.
// Loaded once at startup; immutable thereafter. Backs the picker-room
// auto-submit for mode=live, per docs/team-picker-room.md §8.
type TeamPool struct {
	teams map[string][][]engine.TeamPick // difficulty → list of full pick rosters
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

	p := &TeamPool{teams: map[string][][]engine.TeamPick{}}
	for _, tier := range []struct {
		name    string
		entries []teamEntry
	}{
		{"easy", f.Easy},
		{"hard", f.Hard},
	} {
		for i, e := range tier.entries {
			picks, err := expandTeamEntry(dex, e)
			if err != nil {
				return nil, fmt.Errorf("ai teams[%s][%d] (%s): %w", tier.name, i, e.Name, err)
			}
			if err := engine.ValidateTeam(picks, dex); err != nil {
				return nil, fmt.Errorf("ai teams[%s][%d] (%s): invalid: %w", tier.name, i, e.Name, err)
			}
			p.teams[tier.name] = append(p.teams[tier.name], picks)
		}
		if len(p.teams[tier.name]) == 0 {
			return nil, fmt.Errorf("ai teams: no teams declared for tier %q", tier.name)
		}
	}
	return p, nil
}

// Pick returns a deep copy of a random team from the given tier. The
// copy isolates callers from mutations into the engine state.
func (p *TeamPool) Pick(difficulty string, rng *rand.Rand) ([]engine.TeamPick, error) {
	pool, ok := p.teams[difficulty]
	if !ok || len(pool) == 0 {
		return nil, fmt.Errorf("no AI teams for difficulty %q", difficulty)
	}
	src := pool[rng.Intn(len(pool))]
	out := make([]engine.TeamPick, len(src))
	for i, s := range src {
		out[i] = engine.TeamPick{
			DexNo:   s.DexNo,
			MoveIDs: append([]string(nil), s.MoveIDs...),
		}
	}
	return out, nil
}

// teamEntry is the JSON shape of one curated team. Curators specify
// species only; moves are auto-derived from each species' learnset at
// load time so a curator never has to think about move legality.
type teamEntry struct {
	Name    string `json:"name"`
	Species []int  `json:"species"`
}

type teamPoolFile struct {
	Easy []teamEntry `json:"easy"`
	Hard []teamEntry `json:"hard"`
}

func expandTeamEntry(dex *domain.Dex, e teamEntry) ([]engine.TeamPick, error) {
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

