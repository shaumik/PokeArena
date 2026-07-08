package eval

import (
	"fmt"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// PicksFromDex expands a list of dex numbers into a legal team of TeamPicks,
// each given its species' first MovesMax moves. This is the same expansion the
// AI team pool uses, and it matters for benchmark validity: building a battle
// from bare dex numbers (engine.NewBattle) hands every Pokémon its FULL
// learnset — a Charizard with ~78 legal moves — which is not competitive
// Pokémon and would make the decision space meaningless. Real battles pick 1–4
// moves per mon, so the benchmark must too.
//
// First-N is a deterministic, legal default, not a claim of optimal sets; a
// curated team file can replace it later without touching the runner, since
// both produce []engine.TeamPick.
func PicksFromDex(dex *domain.Dex, dexNos []int) ([]engine.TeamPick, error) {
	if len(dexNos) < 1 || len(dexNos) > engine.TeamSize {
		return nil, fmt.Errorf("team must have 1 to %d Pokémon, got %d", engine.TeamSize, len(dexNos))
	}
	picks := make([]engine.TeamPick, 0, len(dexNos))
	for _, dn := range dexNos {
		sp, ok := dex.Species[dn]
		if !ok {
			return nil, fmt.Errorf("unknown dex number %d", dn)
		}
		moves := sp.Moves
		if len(moves) > engine.MovesMax {
			moves = moves[:engine.MovesMax]
		}
		if len(moves) == 0 {
			return nil, fmt.Errorf("species %d (%s) has no moves", dn, sp.Name)
		}
		picks = append(picks, engine.TeamPick{
			DexNo:   dn,
			MoveIDs: append([]string(nil), moves...),
		})
	}
	return picks, nil
}

// MirrorPicks builds the same team for both sides — the variance-controlled
// mirror setup: identical rosters, so the only free variable is the policy.
func MirrorPicks(dex *domain.Dex, dexNos []int) ([2][]engine.TeamPick, error) {
	p, err := PicksFromDex(dex, dexNos)
	if err != nil {
		return [2][]engine.TeamPick{}, err
	}
	return [2][]engine.TeamPick{p, p}, nil
}
