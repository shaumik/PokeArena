package engine

import (
	"sort"

	"github.com/shaumik/PokeArena/internal/domain"
)

// itemcoverage.go is the held-item analog of coverage.go's move audit. Where
// AuditUpstream lists curated moves whose declared behavior the engine doesn't
// model, AuditItems lists curated catalog items the engine ships but doesn't
// model yet — items a Pokémon can hold that currently do nothing ("inert
// holds"). The committed fixture testdata/item_coverage.json is the contract;
// TestItemCoverage fails on any drift, so adding a new inert item — or wiring
// one up — is always a reviewed, visible change rather than a silent surprise.
//
// An item counts as modeled once it has an itemRegistry entry (i.e. at least
// one engine hook). Until then it's a gap. As items get wired in later phases
// they drop off the list, the same way the move gap list shrank to empty.

// ItemGap is one curated catalog item with no engine behavior. Mirrors MoveGap.
type ItemGap struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AuditItems returns the catalog items the engine doesn't model yet, sorted by
// id. The reverse direction — a registry entry with no catalog item — is a
// misconfiguration asserted separately in TestItemRegistrySubsetOfCatalog.
func AuditItems(dex *domain.Dex) []ItemGap {
	// Non-nil so the all-clear state marshals to "[]" (every catalog item
	// modeled), not "null".
	gaps := []ItemGap{}
	for id, it := range dex.Items {
		if _, modeled := itemRegistry[ItemKind(id)]; !modeled {
			gaps = append(gaps, ItemGap{ID: it.ID, Name: it.Name})
		}
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].ID < gaps[j].ID })
	return gaps
}

// itemRegistryKinds returns the slugs the engine models, sorted. Used by the
// coverage test to assert the registry never models an item the catalog
// doesn't ship.
func itemRegistryKinds() []string {
	out := make([]string, 0, len(itemRegistry))
	for k := range itemRegistry {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}
