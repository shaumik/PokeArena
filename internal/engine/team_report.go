package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shaumik/PokeArena/internal/domain"
)

// team_report.go: what is wrong with a team, all of it at once.
//
// Validation used to stop at the first failure. An agent with three mistakes
// learned them one at a time, paying a round trip each — and the message named
// the mistake without naming a way out ("cannot learn %q" and nothing else), so
// a wrong guess could be followed by another wrong guess indefinitely. That is
// the single worst loop in the tool surface, and it gets worse as the dex grows.
//
// A TeamReport fixes both halves: every problem in one pass, and each one
// carrying the legal alternatives read from the loaded dex. Reading from the
// dex rather than any hardcoded list is what makes a dex expansion a pure data
// change — add nine hundred species and the messages get better on their own.
//
// Warnings are the other half of "did I build a good team". A Timid Machamp is
// perfectly legal and quietly terrible: the nature cuts the stat it attacks
// with. Nothing in the engine objected, so nothing told the builder. Warnings
// never block a team; they are the difference between a team that runs and a
// team someone meant to build.

// suggestionCap bounds how many alternatives a single problem names. Enough to
// recognize the one you meant, few enough to read.
const suggestionCap = 5

// Problem is one blocking defect. Slot is 1-indexed to match how the picker
// and every existing error message counts; 0 means the whole team.
type Problem struct {
	Slot    int    `json:"slot,omitempty"`
	Species string `json:"species,omitempty"`
	// Field names what to fix: "team", "species", "moves", "ability", "item",
	// "nature", "evs", "ivs", "gender". Stable enough to branch on.
	Field   string `json:"field"`
	Message string `json:"message"`
	// Legal lists values that would have been accepted here, nearest first.
	// Empty when the field is not a choice from a set (an EV total over
	// budget has no alternative to name, only a number to lower).
	Legal []string `json:"legal,omitempty"`
}

// Warning is legal but probably not what was meant. Never blocks.
type Warning struct {
	Slot    int    `json:"slot,omitempty"`
	Species string `json:"species,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// TeamReport is the full verdict on a submitted team. It implements error, so
// every existing caller that treats validation as a plain error keeps working
// while a caller that wants the detail can type-assert for it.
type TeamReport struct {
	Problems []Problem `json:"problems,omitempty"`
	Warnings []Warning `json:"warnings,omitempty"`
}

// OK reports whether the team is legal. Warnings do not make a team illegal.
func (r *TeamReport) OK() bool { return len(r.Problems) == 0 }

// Error renders every problem as one message, newline-separated, so a caller
// that only ever prints the error still shows the whole list rather than the
// first item. Warnings are omitted here: Error explains a rejection, and a
// warning is not a reason for one.
func (r *TeamReport) Error() string {
	if len(r.Problems) == 0 {
		return "team is legal"
	}
	head := fmt.Sprintf("%d problems with this team:", len(r.Problems))
	if len(r.Problems) == 1 {
		head = "1 problem with this team:"
	}
	lines := make([]string, 0, len(r.Problems)+1)
	lines = append(lines, head)
	for _, p := range r.Problems {
		line := "  " + p.Message
		if len(p.Legal) > 0 {
			line += " — try: " + strings.Join(p.Legal, ", ")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// pointTo appends a where-to-look hint to msg. Used whenever nothing in the
// dataset is close enough to suggest: "eviolite" and "bullet-punch" are real
// Pokémon things that this curated format simply does not carry, and the
// useful answer there is the list, not a nearest neighbor that is nothing
// like what was asked for.
func pointTo(msg string, near []string, where string) string {
	if len(near) > 0 {
		return msg
	}
	return msg + " — " + where
}

// addProblem appends one problem. Kept as a method so the validators read as a
// list of checks rather than a list of slice appends.
func (r *TeamReport) addProblem(p Problem) { r.Problems = append(r.Problems, p) }

// addWarning appends one warning.
func (r *TeamReport) addWarning(w Warning) { r.Warnings = append(r.Warnings, w) }

// CheckTeam validates picks and returns everything wrong with them, plus
// anything legal-but-suspect. Unlike ValidateTeam it never stops early, so one
// round trip is enough to learn every mistake.
//
// Use ValidateTeam when a plain error is all you need; use this when you are
// reporting back to whoever built the team.
func CheckTeam(picks []TeamPick, dex *domain.Dex) *TeamReport {
	return CheckTeamWithClauses(picks, dex, StandardClauses())
}

// CheckTeamWithClauses is CheckTeam with the format rules selectable, matching
// ValidateTeamWithClauses.
func CheckTeamWithClauses(picks []TeamPick, dex *domain.Dex, c Clauses) *TeamReport {
	rep := &TeamReport{}

	if len(picks) != TeamSize {
		// Team size is the one check that cannot continue: with the wrong
		// number of slots, per-slot findings would be reported against
		// positions the builder did not mean. Every other check runs on.
		rep.addProblem(Problem{
			Field:   "team",
			Message: fmt.Sprintf("team must have %d Pokémon, got %d", TeamSize, len(picks)),
		})
		return rep
	}

	seenSpecies := make(map[int]bool, TeamSize)
	seenItems := make(map[string]int, TeamSize)

	for i, p := range picks {
		slot := i + 1
		sp, ok := dex.Species[p.DexNo]
		if !ok {
			// No species means no learnset, no ability list and no base
			// stats, so every later check on this slot would be noise. Skip
			// the slot, keep the rest of the team.
			rep.addProblem(Problem{
				Slot:  slot,
				Field: "species",
				Message: fmt.Sprintf("slot %d: no Pokémon with dex_no %d (the dex holds %d species)",
					slot, p.DexNo, len(dex.Species)),
				Legal: nearestSpeciesNames(dex, p.DexNo),
			})
			continue
		}

		if c.Species && seenSpecies[p.DexNo] {
			rep.addProblem(Problem{
				Slot: slot, Species: sp.Name, Field: "species",
				Message: fmt.Sprintf("slot %d: %s is already on the team (Species Clause)", slot, sp.Name),
			})
		}
		seenSpecies[p.DexNo] = true

		checkMoves(slot, sp, p.MoveIDs, dex, rep)
		checkAbility(slot, sp, p.Ability, rep)
		checkItem(slot, sp, p.Item, dex, rep)
		checkGender(slot, sp, p.Gender, rep)
		checkClauseMoves(slot, sp, p.MoveIDs, dex, c, rep)
		if c.Item {
			checkItemClause(slot, sp, p.Item, seenItems, rep)
		}
		checkSpread(slot, sp, p, dex, rep)

		warnTeamPick(slot, sp, p, dex, rep)
	}
	return rep
}

// checkMoves gates the move list: count, duplicates, existence and
// learnability. Every offending move is reported, each with the nearest things
// the species can actually learn.
func checkMoves(slot int, sp domain.Species, moveIDs []string, dex *domain.Dex, rep *TeamReport) {
	if len(moveIDs) < MovesMin || len(moveIDs) > MovesMax {
		rep.addProblem(Problem{
			Slot: slot, Species: sp.Name, Field: "moves",
			Message: fmt.Sprintf("slot %d (%s): must pick %d–%d moves, got %d",
				slot, sp.Name, MovesMin, MovesMax, len(moveIDs)),
		})
		// Fall through: the individual moves are still worth checking, so a
		// team with five moves and a typo learns about both at once.
	}

	learn := make(map[string]bool, len(sp.Moves))
	for _, id := range sp.Moves {
		learn[id] = true
	}
	seen := make(map[string]bool, len(moveIDs))
	for _, mid := range moveIDs {
		switch {
		case seen[mid]:
			rep.addProblem(Problem{
				Slot: slot, Species: sp.Name, Field: "moves",
				Message: fmt.Sprintf("slot %d (%s): move %q is listed twice", slot, sp.Name, mid),
			})
		case !learn[mid]:
			// Unknown to the dataset and known-but-unlearnable are the same
			// problem from the builder's side: this Pokémon cannot use it.
			// The distinction still shapes the wording, because "no such
			// move" and "not in its learnset" are different mistakes.
			msg := fmt.Sprintf("slot %d (%s): %s cannot learn %q", slot, sp.Name, sp.Name, mid)
			if _, known := dex.Moves[mid]; !known {
				msg = fmt.Sprintf("slot %d (%s): unknown move %q", slot, sp.Name, mid)
			}
			near := nearestMoves(sp, dex, mid)
			rep.addProblem(Problem{
				Slot: slot, Species: sp.Name, Field: "moves",
				Message: pointTo(msg, near, fmt.Sprintf(
					"nothing in %s's learnset is close; call get_pokemon(%d) for its %d legal moves",
					sp.Name, sp.DexNo, len(sp.Moves))),
				Legal: near,
			})
		}
		seen[mid] = true
	}
}

// checkAbility allows empty (slot 0 default) and otherwise requires the slug
// to be one this species has. The full list is named because it is at most a
// handful of entries.
func checkAbility(slot int, sp domain.Species, ability string, rep *TeamReport) {
	if ability == "" {
		return
	}
	for _, a := range sp.Abilities {
		if a == ability {
			return
		}
	}
	rep.addProblem(Problem{
		Slot: slot, Species: sp.Name, Field: "ability",
		Message: fmt.Sprintf("slot %d (%s): ability %q is not in this species' list",
			slot, sp.Name, ability),
		Legal: append([]string(nil), sp.Abilities...),
	})
}

// checkItem allows empty (no held item) and otherwise requires catalog
// membership. Items are not species-restricted, so the only check is whether
// the slug exists.
func checkItem(slot int, sp domain.Species, item string, dex *domain.Dex, rep *TeamReport) {
	if item == "" {
		return
	}
	if _, ok := dex.Items[item]; ok {
		return
	}
	near := nearestItems(dex, item)
	rep.addProblem(Problem{
		Slot: slot, Species: sp.Name, Field: "item",
		Message: pointTo(
			fmt.Sprintf("slot %d (%s): there is no item %q in the catalog", slot, sp.Name, item),
			near, fmt.Sprintf(
				"this format carries a curated %d-item catalog, not every item in Pokémon; see the item list in start_battle's briefing, or call list_items",
				len(dex.Items))),
		Legal: near,
	})
}

// checkGender refuses a gender the species cannot be. Absent is legal and
// means "let the battle decide".
func checkGender(slot int, sp domain.Species, gender string, rep *TeamReport) {
	if gender == "" || sp.CanBeGender(gender) {
		return
	}
	rep.addProblem(Problem{
		Slot: slot, Species: sp.Name, Field: "gender",
		Message: fmt.Sprintf("slot %d (%s): gender %q is not possible for %s",
			slot, sp.Name, gender, sp.Name),
		Legal: append([]string(nil), sp.Genders...),
	})
}

// checkSpread enforces the EV/IV caps and the nature slug. Each stat is
// reported separately, so a spread with three stats over the cap is fixed in
// one pass rather than three.
func checkSpread(slot int, sp domain.Species, p TeamPick, dex *domain.Dex, rep *TeamReport) {
	if p.EVs != nil {
		for _, key := range domain.StatKeys {
			v, _ := p.EVs.Get(key)
			if v < 0 || v > MaxEVPerStat {
				rep.addProblem(Problem{
					Slot: slot, Species: sp.Name, Field: "evs",
					Message: fmt.Sprintf("slot %d (%s): %s EVs %d out of range 0–%d",
						slot, sp.Name, key, v, MaxEVPerStat),
				})
			}
		}
		if total := p.EVs.Total(); total > MaxEVTotal {
			rep.addProblem(Problem{
				Slot: slot, Species: sp.Name, Field: "evs",
				Message: fmt.Sprintf("slot %d (%s): EVs total %d, over the %d budget by %d",
					slot, sp.Name, total, MaxEVTotal, total-MaxEVTotal),
			})
		}
	}
	if p.IVs != nil {
		for _, key := range domain.StatKeys {
			v, _ := p.IVs.Get(key)
			if v < 0 || v > MaxIV {
				rep.addProblem(Problem{
					Slot: slot, Species: sp.Name, Field: "ivs",
					Message: fmt.Sprintf("slot %d (%s): %s IV %d out of range 0–%d",
						slot, sp.Name, key, v, MaxIV),
				})
			}
		}
	}
	if p.Nature != "" {
		if _, ok := dex.Natures[p.Nature]; !ok {
			near := nearestNatures(dex, p.Nature)
			rep.addProblem(Problem{
				Slot: slot, Species: sp.Name, Field: "nature",
				Message: pointTo(
					fmt.Sprintf("slot %d (%s): there is no nature %q", slot, sp.Name, p.Nature),
					near, "call list_natures for all 25"),
				Legal: near,
			})
		}
	}
}

// nearestMoves names the moves this species can learn that look most like what
// was asked for. Sourced from the species' own learnset, so the suggestion is
// always something that would actually be accepted here.
func nearestMoves(sp domain.Species, _ *domain.Dex, want string) []string {
	return nearest(want, sp.Moves, identity)
}

// identity is the key function for candidates that already are their own
// comparison text — move, item and nature slugs.
func identity(s string) string { return s }

func nearestItems(dex *domain.Dex, want string) []string {
	ids := make([]string, 0, len(dex.Items))
	for id := range dex.Items {
		ids = append(ids, id)
	}
	sort.Strings(ids) // stable input order, so equal-distance ties resolve the same way every run
	return nearest(want, ids, identity)
}

func nearestNatures(dex *domain.Dex, want string) []string {
	ids := make([]string, 0, len(dex.Natures))
	for id := range dex.Natures {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return nearest(want, ids, identity)
}

// nearestSpeciesNames helps with a dex number that does not exist. A number
// carries no spelling to compare, so this offers the numerically closest
// species instead — usually the off-by-one that was meant.
func nearestSpeciesNames(dex *domain.Dex, want int) []string {
	type cand struct {
		no   int
		name string
	}
	all := make([]cand, 0, len(dex.Species))
	for no, sp := range dex.Species {
		all = append(all, cand{no, sp.Name})
	}
	sort.Slice(all, func(i, j int) bool {
		di, dj := abs(all[i].no-want), abs(all[j].no-want)
		if di != dj {
			return di < dj
		}
		return all[i].no < all[j].no
	})
	if len(all) > suggestionCap {
		all = all[:suggestionCap]
	}
	out := make([]string, 0, len(all))
	for _, c := range all {
		out = append(out, fmt.Sprintf("%d (%s)", c.no, c.name))
	}
	return out
}

// nearest ranks candidates by edit distance to want and returns the closest
// few, VERBATIM. key maps a candidate to the string to compare against, which
// is not always the candidate itself: species are compared as slugs so a typo
// matches, but returned as display names so the suggestion can be pasted back
// as-is. Ties break on the candidate's own order, which callers pre-sort, so
// the same typo always draws the same suggestions.
func nearest(want string, candidates []string, key func(string) string) []string {
	if len(candidates) == 0 {
		return nil
	}
	type scored struct {
		id   string
		dist int
		ord  int
	}
	// Only near-misses are worth naming. Past this, a "suggestion" is an
	// unrelated entry dressed up as help — offering Kangaskhan to someone who
	// asked for Landorus-Therian teaches nothing and reads as a wrong answer.
	// Beyond the threshold the caller says something more useful instead.
	limit := len(want) / 2
	if limit < 3 {
		limit = 3
	}
	all := make([]scored, 0, len(candidates))
	for i, c := range candidates {
		d := editDistance(want, key(c))
		if d > limit {
			continue
		}
		all = append(all, scored{id: c, dist: d, ord: i})
	}
	if len(all) == 0 {
		return nil
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].dist != all[j].dist {
			return all[i].dist < all[j].dist
		}
		return all[i].ord < all[j].ord
	})
	if len(all) > suggestionCap {
		all = all[:suggestionCap]
	}
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, s.id)
	}
	return out
}

// editDistance is Levenshtein over bytes, with a two-row buffer. Slugs are
// ASCII kebab-case, so bytes and characters agree and the simple version is
// correct here.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
