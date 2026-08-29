package engine

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/shaumik/PokeArena/internal/domain"
)

// teampaste.go: teams written the way people write teams.
//
// The tool surface used to demand a JSON array of dex numbers and kebab-case
// move slugs, which nobody knows from memory — so building a team meant a
// discovery dance: find_pokemon, then get_pokemon once per species, then
// list_items, then list_natures. Nine calls before the first guess.
//
// A model already knows what a Pokémon team looks like. The Showdown paste is
// the format it has actually seen, so accepting it turns nine calls into zero.
//
// The parser deliberately does NOT validate. It resolves what it can and
// passes everything else through as a normalized slug, so a bad move is
// reported by CheckTeam — with the legal alternatives it already knows how to
// name — rather than by a second, worse error path here. The one thing it must
// resolve is the species, because a TeamPick is nothing without a dex number.

// ParseTeamPaste turns a Showdown-style paste into picks. The returned report
// carries only parse problems; run CheckTeam for legality, or call
// CheckTeamPaste to do both.
//
// Accepted shapes, per block:
//
//	Alakazam @ Life Orb          Nickname (Alakazam) (M) @ Life Orb
//	Ability: Magic Guard         Level: 50
//	EVs: 252 SpA / 252 Spe       IVs: 0 Atk
//	Timid Nature
//	- Psychic
//	- Shadow Ball
//
// Blocks are separated by blank lines. Unrecognized `Key: value` lines are
// warned about rather than rejected, so a paste carrying Showdown fields this
// format has no use for (Shiny, Tera Type, Happiness) still works.
func ParseTeamPaste(text string, dex *domain.Dex) ([]TeamPick, *TeamReport) {
	rep := &TeamReport{}
	blocks := splitPasteBlocks(text)
	if len(blocks) == 0 {
		rep.addProblem(Problem{
			Field:   "team",
			Message: "the team text is empty — paste one block per Pokémon, separated by blank lines",
		})
		return nil, rep
	}

	index := newSpeciesIndex(dex)
	picks := make([]TeamPick, 0, len(blocks))
	for i, b := range blocks {
		picks = append(picks, parsePasteBlock(i+1, b, dex, index, rep))
	}
	return picks, rep
}

// CheckTeamPaste parses and validates in one call, which is what a tool
// handler wants: one report covering both, with no finding stated twice.
//
// A slot whose species could not be resolved is reported once, by the parser
// (which can suggest names), and suppressed in the legality pass (which could
// only say "unknown dex_no 0" about a placeholder).
func CheckTeamPaste(text string, dex *domain.Dex) ([]TeamPick, *TeamReport) {
	picks, rep := ParseTeamPaste(text, dex)
	if len(picks) == 0 {
		return nil, rep
	}

	unresolved := make(map[int]bool)
	for _, p := range rep.Problems {
		if p.Field == "species" {
			unresolved[p.Slot] = true
		}
	}

	legality := CheckTeam(picks, dex)
	for _, p := range legality.Problems {
		if unresolved[p.Slot] {
			continue // the parser already said this slot has no species
		}
		rep.addProblem(p)
	}
	rep.Warnings = append(rep.Warnings, legality.Warnings...)
	return picks, rep
}

// unresolvedDexNo marks a block whose species name could not be matched. It is
// never a real dex number, so a slot carrying it cannot be mistaken for a
// buildable pick.
const unresolvedDexNo = -1

// splitPasteBlocks splits on blank lines and drops empty results, so trailing
// newlines and double-spacing between blocks are both fine.
func splitPasteBlocks(text string) [][]string {
	var blocks [][]string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, cur)
			cur = nil
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return blocks
}

// parsePasteBlock reads one Pokémon.
func parsePasteBlock(slot int, lines []string, dex *domain.Dex, index *speciesIndex, rep *TeamReport) TeamPick {
	pick := TeamPick{DexNo: unresolvedDexNo}

	name, gender, item := parsePasteHeader(lines[0])
	if sp, ok := index.lookup(name); ok {
		pick.DexNo = sp.DexNo
	} else {
		// Two different mistakes wear the same shape here. A misspelling has
		// a near neighbor worth naming; a Pokémon that simply is not in this
		// curated roster has none, and inventing one would read as a wrong
		// answer. Say which case it is.
		near := index.nearest(name)
		msg := fmt.Sprintf("slot %d: no Pokémon named %q in this dataset", slot, name)
		if len(near) == 0 {
			msg += fmt.Sprintf(" — this format uses a curated roster of %d species, not the full Pokédex; "+
				"see the species list in start_battle's briefing, or call find_pokemon", index.count())
		}
		rep.addProblem(Problem{
			Slot: slot, Field: "species",
			Message: msg,
			Legal:   near,
		})
	}
	pick.Gender = gender
	if item != "" {
		pick.Item = slugify(item)
	}

	for _, line := range lines[1:] {
		switch {
		case strings.HasPrefix(line, "-"), strings.HasPrefix(line, "–"), strings.HasPrefix(line, "—"):
			move := strings.TrimSpace(strings.TrimLeft(line, "-–—"))
			if move == "" {
				continue
			}
			pick.MoveIDs = append(pick.MoveIDs, slugify(move))

		case strings.HasSuffix(strings.ToLower(line), " nature"):
			pick.Nature = slugify(line[:len(line)-len(" nature")])

		case hasPrefixFold(line, "Ability:"):
			pick.Ability = slugify(afterColon(line))

		case hasPrefixFold(line, "EVs:"):
			// Unlisted stats are 0 in a Showdown paste, which is also the
			// zero value, so an explicit struct is the faithful reading.
			evs := domain.Stats{}
			parseStatLine(slot, afterColon(line), "EVs", &evs, rep)
			pick.EVs = &evs

		case hasPrefixFold(line, "IVs:"):
			// Unlisted IVs are perfect, NOT zero — the one place where the
			// paste format's default differs from Go's.
			ivs := domain.Stats{HP: MaxIV, Atk: MaxIV, Def: MaxIV, SpA: MaxIV, SpD: MaxIV, Spe: MaxIV}
			parseStatLine(slot, afterColon(line), "IVs", &ivs, rep)
			pick.IVs = &ivs

		case hasPrefixFold(line, "Level:"):
			if lv, err := strconv.Atoi(strings.TrimSpace(afterColon(line))); err == nil && lv != Level {
				rep.addWarning(Warning{
					Slot: slot, Field: "level",
					Message: fmt.Sprintf("slot %d: this format is fixed at level %d, so the %d was ignored",
						slot, Level, lv),
				})
			}

		case isIgnorablePasteField(line):
			// Shiny / Happiness / Tera Type and friends: real Showdown fields
			// this format has no use for. Silently fine — rejecting a paste
			// over them would defeat the point of accepting pastes.

		default:
			rep.addWarning(Warning{
				Slot: slot, Field: "text",
				Message: fmt.Sprintf("slot %d: ignored the line %q — it is not a move, ability, spread, nature or level", slot, line),
			})
		}
	}
	return pick
}

// parsePasteHeader splits the first line into species name, gender and item.
// Handles "Nickname (Species) (M) @ Item" and every shorter form of it.
func parsePasteHeader(line string) (name, gender, item string) {
	head := line
	if at := strings.LastIndex(head, "@"); at >= 0 {
		item = strings.TrimSpace(head[at+1:])
		head = strings.TrimSpace(head[:at])
	}

	// Trailing "(M)" / "(F)" / "(N)" is the gender marker. Checked before the
	// nickname parens so a nicknamed, gendered Pokémon reads correctly.
	if strings.HasSuffix(head, ")") {
		if open := strings.LastIndex(head, "("); open >= 0 {
			switch strings.ToUpper(strings.TrimSpace(head[open+1 : len(head)-1])) {
			case "M":
				gender, head = "male", strings.TrimSpace(head[:open])
			case "F":
				gender, head = "female", strings.TrimSpace(head[:open])
			case "N":
				gender, head = "genderless", strings.TrimSpace(head[:open])
			}
		}
	}

	// What remains is either "Species" or "Nickname (Species)".
	if strings.HasSuffix(head, ")") {
		if open := strings.LastIndex(head, "("); open >= 0 {
			return strings.TrimSpace(head[open+1 : len(head)-1]), gender, item
		}
	}
	return strings.TrimSpace(head), gender, item
}

// parseStatLine reads "252 SpA / 4 SpD / 252 Spe" into dst. An unreadable
// entry is a problem rather than a silent zero: a dropped EV allocation would
// produce a legal team that quietly is not the one that was asked for.
func parseStatLine(slot int, spec, label string, dst *domain.Stats, rep *TeamReport) {
	for _, part := range strings.Split(spec, "/") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Fields(part)
		if len(fields) != 2 {
			rep.addProblem(Problem{
				Slot: slot, Field: strings.ToLower(label),
				Message: fmt.Sprintf("slot %d: could not read %q in the %s line — each entry looks like \"252 SpA\"",
					slot, part, label),
			})
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			rep.addProblem(Problem{
				Slot: slot, Field: strings.ToLower(label),
				Message: fmt.Sprintf("slot %d: %q is not a number in the %s line", slot, fields[0], label),
			})
			continue
		}
		if !setStatByName(dst, fields[1], n) {
			rep.addProblem(Problem{
				Slot: slot, Field: strings.ToLower(label),
				Message: fmt.Sprintf("slot %d: unknown stat %q in the %s line", slot, fields[1], label),
				Legal:   []string{"HP", "Atk", "Def", "SpA", "SpD", "Spe"},
			})
		}
	}
}

// setStatByName writes one stat by its paste-format abbreviation, accepting
// the spellings that appear in the wild alongside the canonical ones.
func setStatByName(s *domain.Stats, name string, v int) bool {
	switch strings.ToLower(name) {
	case "hp":
		s.HP = v
	case "atk", "attack":
		s.Atk = v
	case "def", "defense":
		s.Def = v
	case "spa", "spatk", "spattack", "specialattack":
		s.SpA = v
	case "spd", "spdef", "spdefense", "specialdefense":
		s.SpD = v
	case "spe", "speed":
		s.Spe = v
	default:
		return false
	}
	return true
}

// isIgnorablePasteField reports whether a line is a Showdown field this format
// simply does not model. Listed explicitly so a genuine typo still gets a
// warning rather than disappearing.
func isIgnorablePasteField(line string) bool {
	for _, k := range []string{"Shiny:", "Happiness:", "Tera Type:", "Gigantamax:", "Dynamax Level:", "Hidden Power:"} {
		if hasPrefixFold(line, k) {
			return true
		}
	}
	return false
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func afterColon(line string) string {
	if i := strings.Index(line, ":"); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

// slugify turns display text into the kebab-case slug the dataset keys on:
// lowercase, every run of non-alphanumerics becomes one hyphen. "Body Slam"
// becomes "body-slam", "Will-O-Wisp" stays "will-o-wisp".
func slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	return b.String()
}

// tightSlug drops separators entirely: "Farfetch'd" becomes "farfetchd" and
// "Mr. Mime" becomes "mrmime". Showdown writes some species that way, so
// species lookup indexes both this and slugify's output.
func tightSlug(s string) string {
	return strings.ReplaceAll(slugify(s), "-", "")
}

// speciesIndex resolves a written species name to a species. It keys on both
// slug forms because the two names in this dataset carrying punctuation —
// Farfetch'd and Mr. Mime — are written inconsistently in the wild, including
// by Showdown itself.
type speciesIndex struct {
	byName map[string]domain.Species
	names  []string // display names, dex order, for suggestions
}

func newSpeciesIndex(dex *domain.Dex) *speciesIndex {
	all := dex.AllSpecies()
	idx := &speciesIndex{
		byName: make(map[string]domain.Species, len(all)*2),
		names:  make([]string, 0, len(all)),
	}
	for _, sp := range all {
		idx.byName[slugify(sp.Name)] = sp
		idx.byName[tightSlug(sp.Name)] = sp
		idx.names = append(idx.names, sp.Name)
	}
	return idx
}

func (i *speciesIndex) lookup(name string) (domain.Species, bool) {
	if sp, ok := i.byName[slugify(name)]; ok {
		return sp, true
	}
	sp, ok := i.byName[tightSlug(name)]
	return sp, ok
}

// nearest names the species that look most like what was written, so a
// misspelling or an out-of-format Pokémon gets pointed at the real roster.
func (i *speciesIndex) nearest(name string) []string {
	return nearest(slugify(name), i.names, slugify)
}

// count is how many species the roster holds.
func (i *speciesIndex) count() int { return len(i.names) }
