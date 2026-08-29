package mcpserver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
)

// briefing.go: everything needed to write a team, handed over before writing
// one is attempted.
//
// A model knows Pokémon. It does not know THIS format, and cannot guess it:
// the roster is a curated subset that looks like "Gen 1 final forms" and is
// not — Onix, Seadra, Porygon, Tangela, Lickitung and Farfetch'd are all here,
// and all of them gained evolutions later. A model reasoning from a rule gets
// the roster wrong; a model handed the list cannot.
//
// Measured against the alternative: species, items and natures written from
// memory are wrong often enough to matter, while moves written from memory for
// a species the model was told about are right about 39 times in 40. So the
// briefing carries the three small closed sets and leaves movepools out — they
// are two orders of magnitude larger, and the validator already names the
// legal alternatives when a move misses.
//
// It rides on start_battle, a call the agent makes anyway, so it costs no
// extra round trip.

// rosterListCap is the point past which naming every species stops being the
// right trade. Below it the list is a few hundred tokens and removes all
// guessing; above it the list is the larger cost and a model's own knowledge
// of a near-complete dex is the better tool, so the briefing summarizes and
// points at find_pokemon instead.
//
// Nothing here is hardcoded to today's 80 species: the switch is made by
// counting what is loaded, so growing the dex changes the output and not this
// code.
const rosterListCap = 400

// teamBriefing is the reference an agent needs to write a legal team without
// looking anything up.
type teamBriefing struct {
	Format string `json:"format"`

	// Species is every legal species, by display name, ready to paste into a
	// team. Omitted once the roster outgrows rosterListCap, when SpeciesNote
	// explains how to search instead.
	Species      []string `json:"species,omitempty"`
	SpeciesNote  string   `json:"species_note,omitempty"`
	SpeciesTotal int      `json:"species_total"`

	Items      []string `json:"items,omitempty"`
	ItemsNote  string   `json:"items_note,omitempty"`
	ItemsTotal int      `json:"items_total"`

	// Natures carry their effect inline ("timid (+spe, -atk)") because a
	// nature name without its effect is half the information.
	Natures []string `json:"natures"`

	Rules formatRules `json:"rules"`

	// Clauses are the format rules that differ from ordinary competitive play.
	// Listed because they are what a team written from memory breaks: nothing
	// in standard play stops six Pokémon holding Leftovers, and here the Item
	// Clause does.
	Clauses []string `json:"clauses"`

	// TeamFormat shows the accepted paste shape by example, which is shorter
	// and less ambiguous than describing it.
	TeamFormat string `json:"team_format"`
}

// buildBriefing projects the loaded dex into the briefing. Everything is read
// from the dex and the engine's constants, so a dataset change moves the
// briefing with it.
func buildBriefing(dex *domain.Dex) *teamBriefing {
	b := &teamBriefing{
		Format: fmt.Sprintf(
			"Level %d, %d Pokémon per team, %d–%d moves each, chosen from a curated %d-species roster.",
			engine.Level, engine.TeamSize, engine.MovesMin, engine.MovesMax, len(dex.Species)),
		Rules:      localRules(),
		Clauses:    describeClauses(engine.StandardClauses()),
		TeamFormat: briefingExample,
	}

	species := dex.AllSpecies()
	b.SpeciesTotal = len(species)
	if len(species) <= rosterListCap {
		b.Species = make([]string, 0, len(species))
		for _, sp := range species {
			b.Species = append(b.Species, sp.Name)
		}
	} else {
		b.SpeciesNote = fmt.Sprintf(
			"%d species — too many to list. Use find_pokemon to search by name, then get_pokemon for the legal move list.",
			len(species))
	}

	items := engine.ItemCatalog(dex)
	b.ItemsTotal = len(items)
	if len(items) <= rosterListCap {
		b.Items = make([]string, 0, len(items))
		for _, it := range items {
			b.Items = append(b.Items, it.Name)
		}
	} else {
		b.ItemsNote = fmt.Sprintf("%d items — too many to list. Use list_items.", len(items))
	}

	b.Natures = describeNatures(dex)
	return b
}

// briefingExample is the paste format, shown rather than described. One block
// per Pokémon, blank line between; every line after the first is optional.
const briefingExample = `Showdown paste, one block per Pokémon, separated by blank lines. ` +
	`Only the species line and at least one move are required:

Alakazam @ Life Orb
Ability: Synchronize
EVs: 252 SpA / 4 SpD / 252 Spe
Timid Nature
IVs: 0 Atk
- Psychic
- Shadow Ball
- Focus Blast
- Recover

Snorlax @ Leftovers
- Body Slam
- Earthquake
- Rest`

// describeNatures renders each nature with what it does, sorted by id so the
// order is stable across runs.
func describeNatures(dex *domain.Dex) []string {
	out := make([]string, 0, len(dex.Natures))
	for _, n := range dex.Natures {
		switch {
		case n.Plus == "" || n.Minus == "":
			out = append(out, fmt.Sprintf("%s (neutral)", n.ID))
		default:
			out = append(out, fmt.Sprintf("%s (+%s, -%s)", n.ID, n.Plus, n.Minus))
		}
	}
	sort.Strings(out)
	return out
}

// describeClauses states each active clause in terms of what it forbids. Only
// the build-time clauses appear: Sleep Clause is enforced during the battle,
// not on the team, so listing it here would suggest a team could break it.
func describeClauses(c engine.Clauses) []string {
	var out []string
	if c.Species {
		out = append(out, "Species Clause: no two Pokémon of the same species.")
	}
	if c.Item {
		out = append(out, "Item Clause: no two Pokémon holding the same item — unlike standard competitive play, so this is the rule a team written from memory breaks most often.")
	}
	if c.Evasion {
		out = append(out, "Evasion Clause: no moves that raise evasion.")
	}
	if c.OHKO {
		out = append(out, "OHKO Clause: no one-hit-KO moves.")
	}
	return out
}

// reportWire is a TeamReport rendered for a tool response. Problems and
// warnings stay separate all the way out: an agent has to be able to tell "fix
// this before the battle starts" from "this is legal but probably weaker than
// you meant".
type reportWire struct {
	Problems []problemWire `json:"problems,omitempty"`
	Warnings []warningWire `json:"warnings,omitempty"`
	// Summary is the same content as one human-readable string, for a client
	// that surfaces text to the model rather than structured fields.
	Summary string `json:"summary,omitempty"`
}

type problemWire struct {
	Slot    int      `json:"slot,omitempty"`
	Species string   `json:"species,omitempty"`
	Field   string   `json:"field"`
	Message string   `json:"message"`
	Legal   []string `json:"legal,omitempty"`
}

type warningWire struct {
	Slot    int    `json:"slot,omitempty"`
	Species string `json:"species,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// toWire renders a report for a tool response.
func toWire(rep *engine.TeamReport) reportWire {
	out := reportWire{}
	for _, p := range rep.Problems {
		out.Problems = append(out.Problems, problemWire{
			Slot: p.Slot, Species: p.Species, Field: p.Field,
			Message: p.Message, Legal: p.Legal,
		})
	}
	for _, w := range rep.Warnings {
		out.Warnings = append(out.Warnings, warningWire{
			Slot: w.Slot, Species: w.Species, Field: w.Field, Message: w.Message,
		})
	}
	out.Summary = summarize(rep)
	return out
}

// summarize renders problems and warnings as one readable block.
func summarize(rep *engine.TeamReport) string {
	var b strings.Builder
	if len(rep.Problems) > 0 {
		b.WriteString(rep.Error())
	}
	if len(rep.Warnings) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		if len(rep.Warnings) == 1 {
			b.WriteString("1 warning (the team is still legal):")
		} else {
			fmt.Fprintf(&b, "%d warnings (the team is still legal):", len(rep.Warnings))
		}
		for _, w := range rep.Warnings {
			b.WriteString("\n  " + w.Message)
		}
	}
	return b.String()
}
