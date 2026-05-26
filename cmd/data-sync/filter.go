package main

import "log"

// SpeciesFilter is one stage in the filter chain. Name is the label that
// appears in the summary log; Keep returns whether to retain a species.
//
// To add a filter: write a constructor that returns a SpeciesFilter, append
// it to defaultFilters, and (if it has parameters) document the choice.
// To remove a filter: delete the line in defaultFilters.
type SpeciesFilter struct {
	Name string
	Keep func(upstreamSpecies) bool
}

// defaultFilters is the policy chain applied to the upstream species list.
// Order matters only for logging; predicates are independent.
var defaultFilters = []SpeciesFilter{
	GenAtMost(1),
	NotPreEvolution(),
}

// GenAtMost keeps species with Pokédex number ≤ n.
func GenAtMost(n int) SpeciesFilter {
	return SpeciesFilter{
		Name: "GenAtMost",
		Keep: func(s upstreamSpecies) bool { return s.Num <= 151 && n >= 1 },
	}
}

// NotPreEvolution drops species that have a further evolution. Pikachu (evos
// to Raichu) is excluded by this filter; Raichu is kept. Species with no
// evolution chain at all (Mewtwo, Tauros, etc.) are kept.
func NotPreEvolution() SpeciesFilter {
	return SpeciesFilter{
		Name: "NotPreEvolution",
		Keep: func(s upstreamSpecies) bool { return len(s.Evos) == 0 },
	}
}

// applyFilters runs the chain over a species list, logging a one-line summary
// of how many entries each predicate dropped.
func applyFilters(in []upstreamSpecies, chain []SpeciesFilter) []upstreamSpecies {
	current := in
	for _, f := range chain {
		var kept []upstreamSpecies
		for _, sp := range current {
			if f.Keep(sp) {
				kept = append(kept, sp)
			}
		}
		log.Printf("  filter %s: %d -> %d (%d dropped)",
			f.Name, len(current), len(kept), len(current)-len(kept))
		current = kept
	}
	return current
}
