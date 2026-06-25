package main

import (
	"math/rand"
	"sort"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// teamMon is the display-side companion to an engine.TeamPick: the species
// name and types so the picker can show the drafted team without re-resolving
// the dex on every render.
type teamMon struct {
	dexNo    int
	name     string
	t1, t2   domain.Type
	moveIDs  []string
	abilSlug string
}

// autoTeam drafts a random legal 6-Pokémon team from the dex. Species are
// distinct (Species Clause) and each carries a coverage-biased moveset. Given a
// dex with at least 6 fieldable species (those with ≥1 learnable move) — which
// the curated dataset always satisfies — the result passes engine.ValidateTeam,
// so the picker can submit it directly. The picker validates again before
// sending (model.go handleRoomKey), so even a degenerate dex surfaces a clear
// error instead of a malformed submission. Re-rolling simply calls this again.
func autoTeam(dex *domain.Dex) ([]engine.TeamPick, []teamMon) {
	ids := speciesIDs(dex)
	rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })

	picks := make([]engine.TeamPick, 0, engine.TeamSize)
	mons := make([]teamMon, 0, engine.TeamSize)
	for _, id := range ids {
		if len(picks) == engine.TeamSize {
			break
		}
		sp := dex.Species[id]
		moves := pickMoves(dex, sp)
		if len(moves) < engine.MovesMin {
			continue // a species with no usable moves can't be fielded
		}
		picks = append(picks, engine.TeamPick{DexNo: id, MoveIDs: moves})
		mons = append(mons, teamMon{dexNo: id, name: sp.Name, t1: sp.Type1, t2: sp.Type2, moveIDs: moves})
	}
	return picks, mons
}

// rerollSlot replaces the team member at idx with a fresh distinct species,
// leaving the rest of the team untouched. Returns the updated slices.
func rerollSlot(dex *domain.Dex, picks []engine.TeamPick, mons []teamMon, idx int) ([]engine.TeamPick, []teamMon) {
	if idx < 0 || idx >= len(picks) {
		return picks, mons
	}
	used := make(map[int]bool, len(picks))
	for i, p := range picks {
		if i != idx {
			used[p.DexNo] = true
		}
	}
	ids := speciesIDs(dex)
	rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	for _, id := range ids {
		if used[id] {
			continue
		}
		sp := dex.Species[id]
		moves := pickMoves(dex, sp)
		if len(moves) < engine.MovesMin {
			continue
		}
		picks[idx] = engine.TeamPick{DexNo: id, MoveIDs: moves}
		mons[idx] = teamMon{dexNo: id, name: sp.Name, t1: sp.Type1, t2: sp.Type2, moveIDs: moves}
		break
	}
	return picks, mons
}

// pickMoves chooses up to 4 distinct moves for a species, biased toward attack
// coverage: prefer damaging moves, allow one status move for utility. Falls
// back to whatever the species can learn when it has few options.
func pickMoves(dex *domain.Dex, sp domain.Species) []string {
	var damaging, status []string
	for _, id := range sp.Moves {
		m, ok := dex.Moves[id]
		if !ok {
			continue
		}
		if m.Category == domain.CatStatus || m.Power == 0 {
			status = append(status, id)
		} else {
			damaging = append(damaging, id)
		}
	}
	rand.Shuffle(len(damaging), func(i, j int) { damaging[i], damaging[j] = damaging[j], damaging[i] })
	rand.Shuffle(len(status), func(i, j int) { status[i], status[j] = status[j], status[i] })

	picked := []string{}
	for _, id := range damaging {
		if len(picked) >= 3 {
			break
		}
		picked = append(picked, id)
	}
	if len(status) > 0 && len(picked) < engine.MovesMax {
		picked = append(picked, status[0])
	}
	// Top up from any remaining learnable move if we're still short.
	for _, id := range append(damaging, status...) {
		if len(picked) >= engine.MovesMax {
			break
		}
		if !contains(picked, id) {
			picked = append(picked, id)
		}
	}
	return picked
}

func speciesIDs(dex *domain.Dex) []int {
	ids := make([]int, 0, len(dex.Species))
	for id := range dex.Species {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
