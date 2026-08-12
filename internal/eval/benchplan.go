package eval

import (
	"fmt"
	"sort"
	"strings"
)

// A benchmark run is a matrix — every contestant plays every team a fixed
// number of times — and the matrix is enumerated up front rather than
// generated as it goes. That is what makes a run resumable: the plan is a pure
// function of the config, so a run interrupted overnight recomputes exactly the
// same list next morning and only skips what already finished.
//
// The unit of identity is the *contestant*, deliberately not the model. A model
// reached through Claude Code and the same model reached through a bare API
// harness are different contestants, because the agent runtime around the model
// is part of what is being measured (tool-calling loop, retries, context
// handling). Labelling by contestant makes both comparisons fall out of one
// dataset: hold the model fixed and vary the harness to measure harnesses, hold
// the harness fixed and vary the model to measure models.

// Entrant is one competitor: an agent runtime plus the model driving it.
type Entrant struct {
	// ID is the label results are recorded under, and it becomes the trainer
	// name on the battle — so attribution lives in the database rather than in
	// a file beside the run. Convention is "harness/model".
	ID string `json:"id"`
	// Harness selects the runner that drives a game (claude, agy, codex, ...).
	Harness string `json:"harness"`
	// Model is passed through to that harness verbatim.
	Model string `json:"model"`
}

// BenchConfig is the whole description of a run. It is committed alongside the
// results so a published number can be traced to the matrix that produced it.
type BenchConfig struct {
	Entrants     []Entrant `json:"entrants"`
	Teams        []string  `json:"teams"`
	GamesPerTeam int       `json:"games_per_team"`
	Concurrency  int       `json:"concurrency"`
}

// PlannedGame is one game of the matrix. Label is deterministic from the
// coordinates, which is what lets a resumed run recognize completed work
// without a central ledger that could be lost.
type PlannedGame struct {
	Entrant Entrant `json:"entrant"`
	Team    string  `json:"team"`
	Index   int     `json:"index"` // 1-based within (contestant, team)
	Label   string  `json:"label"`
}

// Validate reports why a config cannot produce a run. Checked before any game
// is played, because discovering a typo'd team name after an eight-hour batch
// is a bad way to find out.
func (c BenchConfig) Validate() error {
	if len(c.Entrants) == 0 {
		return fmt.Errorf("no contestants")
	}
	if len(c.Teams) == 0 {
		return fmt.Errorf("no teams")
	}
	if c.GamesPerTeam < 1 {
		return fmt.Errorf("games_per_team must be >= 1, got %d", c.GamesPerTeam)
	}
	seen := map[string]bool{}
	for i, ct := range c.Entrants {
		switch {
		case ct.ID == "":
			return fmt.Errorf("contestant %d: empty id", i)
		case ct.Harness == "":
			return fmt.Errorf("contestant %q: empty harness", ct.ID)
		case ct.Model == "":
			return fmt.Errorf("contestant %q: empty model", ct.ID)
		case seen[ct.ID]:
			// Duplicate ids would silently pool two different configurations
			// into one row of the results table.
			return fmt.Errorf("duplicate contestant id %q", ct.ID)
		}
		seen[ct.ID] = true
	}
	// Ids are unique, but labels are built from the *sanitized* id, and
	// sanitizing is lossy: "a/b" and "a-b" are different entrants that would
	// share a filename. Two entrants writing the same result file is silent
	// data loss — the second overwrites the first, and resume then treats one
	// game as covering both. Reject it here, where the fix is to rename.
	byLabel := map[string]string{}
	for _, ct := range c.Entrants {
		key := pathSafe(ct.ID)
		if prev, clash := byLabel[key]; clash {
			return fmt.Errorf("entrant ids %q and %q both reduce to %q and would share result files; rename one",
				prev, ct.ID, key)
		}
		byLabel[key] = ct.ID
	}
	return nil
}

// BuildPlan enumerates every game the config calls for, in a stable order.
func BuildPlan(c BenchConfig) []PlannedGame {
	var out []PlannedGame
	for _, ct := range c.Entrants {
		for _, team := range c.Teams {
			for i := 1; i <= c.GamesPerTeam; i++ {
				out = append(out, PlannedGame{
					Entrant: ct,
					Team:    team,
					Index:   i,
					Label:   GameLabel(ct.ID, team, i),
				})
			}
		}
	}
	return out
}

// GameLabel is the deterministic name for one cell of the matrix. It is the
// results filename and the resume key, so it must stay stable across runs and
// be safe as a path segment.
func GameLabel(contestantID, team string, index int) string {
	return fmt.Sprintf("%s__%s__g%d", pathSafe(contestantID), pathSafe(team), index)
}

// pathSafe reduces a label to characters that are safe in a filename on every
// platform we run on. It is intentionally lossy-but-stable: the same input
// always yields the same output, so resume keeps working.
func pathSafe(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// Remaining returns the games still to play, given the set of labels already
// complete. Order is preserved so a resumed run continues predictably.
//
// Interleave matters here: the plan groups all of a contestant's games
// together, which means an interrupted run leaves the last contestants with no
// games at all. Callers that care about getting a usable partial result from a
// run that may not finish should use Interleaved.
func Remaining(plan []PlannedGame, done map[string]bool) []PlannedGame {
	var out []PlannedGame
	for _, g := range plan {
		if !done[g.Label] {
			out = append(out, g)
		}
	}
	return out
}

// Interleaved reorders a plan round-robin across contestants, so that any
// prefix of the run is a balanced sample.
//
// This is not cosmetic. A long batch is likely to be cut short — a laptop
// sleeps, a rate limit trips, the run is stopped by hand — and with the plan in
// its natural order that leaves contestant 1 complete and contestant 3 with
// nothing, which is not a comparison at all. Interleaved means an interrupted
// run still yields every contestant the same number of games, and the partial
// result is usable rather than discarded.
func Interleaved(plan []PlannedGame) []PlannedGame {
	byContestant := map[string][]PlannedGame{}
	var order []string
	for _, g := range plan {
		if _, ok := byContestant[g.Entrant.ID]; !ok {
			order = append(order, g.Entrant.ID)
		}
		byContestant[g.Entrant.ID] = append(byContestant[g.Entrant.ID], g)
	}
	var out []PlannedGame
	for round := 0; ; round++ {
		added := false
		for _, id := range order {
			games := byContestant[id]
			if round < len(games) {
				out = append(out, games[round])
				added = true
			}
		}
		if !added {
			return out
		}
	}
}

// PlanSummary describes a plan for the operator, before anything runs.
type PlanSummary struct {
	Entrants   int
	Teams      int
	Total      int
	PerEntrant int
	Remaining  int
}

// Summarize describes a plan and how much of it is left.
func Summarize(plan []PlannedGame, done map[string]bool) PlanSummary {
	contestants := map[string]bool{}
	teams := map[string]bool{}
	remaining := 0
	for _, g := range plan {
		contestants[g.Entrant.ID] = true
		teams[g.Team] = true
		if !done[g.Label] {
			remaining++
		}
	}
	s := PlanSummary{
		Entrants:  len(contestants),
		Teams:     len(teams),
		Total:     len(plan),
		Remaining: remaining,
	}
	if s.Entrants > 0 {
		s.PerEntrant = s.Total / s.Entrants
	}
	return s
}

// SortContestantIDs returns the contestant ids in a plan, sorted, for stable
// reporting.
func SortContestantIDs(plan []PlannedGame) []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range plan {
		if !seen[g.Entrant.ID] {
			seen[g.Entrant.ID] = true
			out = append(out, g.Entrant.ID)
		}
	}
	sort.Strings(out)
	return out
}
