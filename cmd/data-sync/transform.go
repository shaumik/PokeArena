package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"pokearena/internal/domain"
)

// knownVolatiles mirrors domain/domain.go's vocabulary. Volatiles outside
// this set are dropped during transform with a warning — the engine doesn't
// model them yet, and passing them through would fail validation. Logging
// at the transform boundary keeps the gap visible.
var knownVolatiles = map[string]bool{
	"confusion": true,
	"flinch":    true,
}

func mapVolatile(name, where string) string {
	if name == "" {
		return ""
	}
	if knownVolatiles[name] {
		return name
	}
	log.Printf("  drop unknown volatile %q (%s)", name, where)
	return ""
}

// transformed is the bundle the stage step writes out: three files plus the
// list of moves actually used by the kept species (so we never ship orphan
// move data).
type transformed struct {
	Pokedex   []domain.Species
	Moves     []domain.Move
	Typechart map[domain.Type]map[domain.Type]float64
}

// movesetSize caps each species's moveset to the canonical four-moves limit.
const movesetSize = 4

// statusMap converts Showdown's 3-letter status codes to our long-form
// vocabulary used in the engine and validator.
var statusMap = map[string]string{
	"brn": "burn",
	"par": "paralysis",
	"psn": "poison",
	"tox": "toxic",
	"slp": "sleep",
	"frz": "freeze",
}

// boostStatMap converts Showdown's short stat codes to ours.
var boostStatMap = map[string]string{
	"atk":      "attack",
	"def":      "defense",
	"spa":      "spatk",
	"spd":      "spdef",
	"spe":      "speed",
	"accuracy": "accuracy",
	"evasion":  "evasion",
}

// flagsAllowlist is the subset of Showdown's `flags` keys our schema knows
// about. Other flags (protect, mirror, metronome, sound — informational only
// today) are dropped so the validator doesn't see unknowns.
var flagsAllowlist = map[string]string{
	"contact": "contact",
	"punch":   "punch",
	"bite":    "bite",
	"sound":   "sound",
	"powder":  "powder",
}

func transform(up *upstream, species []upstreamSpecies) (transformed, error) {
	// Build the Showdown-stripped-id → our-slug map so we can translate the
	// move references inside randombattle sets (which use the stripped form).
	showdownToSlug := make(map[string]string, len(up.Moves))
	for _, m := range up.Moves {
		showdownToSlug[stripShowdownID(m.Name)] = m.ID
	}

	// Pick each kept species's 4-move set and collect every move actually
	// referenced — that's the union we emit to moves.json.
	referenced := map[string]bool{}
	pokedex := make([]domain.Species, 0, len(species))
	for _, sp := range species {
		set, ok := up.Sets[sp.ID]
		if !ok {
			return transformed{}, fmt.Errorf("randombattle set missing for species %q", sp.ID)
		}
		moves, err := pickMoveset(sp.ID, set, showdownToSlug, up.Moves)
		if err != nil {
			return transformed{}, err
		}
		if len(moves) == 0 {
			// Skip species the randombattle pool can't cover (rare; mostly
			// trade-evolution oddities). The filter chain runs before this
			// so any drop here is a "no canonical moveset available" signal.
			continue
		}
		for _, mid := range moves {
			referenced[mid] = true
		}
		pokedex = append(pokedex, domain.Species{
			DexNo: sp.Num,
			Name:  sp.Name,
			Type1: domain.Type(strings.ToLower(sp.Types[0])),
			Type2: secondType(sp.Types),
			Base: domain.Stats{
				HP:  sp.BaseStats.HP,
				Atk: sp.BaseStats.Atk,
				Def: sp.BaseStats.Def,
				SpA: sp.BaseStats.Spa,
				SpD: sp.BaseStats.Spd,
				Spe: sp.BaseStats.Spe,
			},
			Moves: moves,
		})
	}
	sort.Slice(pokedex, func(i, j int) bool { return pokedex[i].DexNo < pokedex[j].DexNo })

	// Transform exactly the moves we referenced, in deterministic order.
	moveIDs := make([]string, 0, len(referenced))
	for id := range referenced {
		moveIDs = append(moveIDs, id)
	}
	sort.Strings(moveIDs)
	moves := make([]domain.Move, 0, len(moveIDs))
	for _, id := range moveIDs {
		um, ok := up.Moves[id]
		if !ok {
			return transformed{}, fmt.Errorf("referenced move %q missing from upstream snapshot", id)
		}
		out, err := transformMove(um)
		if err != nil {
			return transformed{}, fmt.Errorf("transform move %s: %w", id, err)
		}
		moves = append(moves, out)
	}

	// Type chart — lowercase keys for our schema.
	chart := make(map[domain.Type]map[domain.Type]float64, len(up.Typechart))
	for atk, row := range up.Typechart {
		lowered := make(map[domain.Type]float64, len(row))
		for def, mult := range row {
			lowered[domain.Type(strings.ToLower(def))] = mult
		}
		chart[domain.Type(strings.ToLower(atk))] = lowered
	}

	return transformed{Pokedex: pokedex, Moves: moves, Typechart: chart}, nil
}

// pickMoveset chooses up to four moves from a species's randombattle pool
// using a deterministic heuristic: first the main `moves` pool, then fill
// from essentials, exclusives, and combos until four are picked. Showdown
// ids are translated to our slugs via showdownToSlug.
func pickMoveset(speciesID string, set randomSet, showdownToSlug map[string]string, moves map[string]upstreamMove) ([]string, error) {
	pool := append([]string{}, set.Moves...)
	pool = append(pool, set.EssentialMoves...)
	pool = append(pool, set.ExclusiveMoves...)
	pool = append(pool, set.ComboMoves...)

	seen := map[string]bool{}
	out := make([]string, 0, movesetSize)
	for _, sid := range pool {
		slug, ok := showdownToSlug[sid]
		if !ok {
			return nil, fmt.Errorf("species %s: randombattle references unknown move %q", speciesID, sid)
		}
		if _, exists := moves[slug]; !exists {
			return nil, fmt.Errorf("species %s: move %q not in upstream moves snapshot", speciesID, slug)
		}
		if seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, slug)
		if len(out) >= movesetSize {
			break
		}
	}
	return out, nil
}

// transformMove converts one Showdown-shape move into our schema shape. The
// per-field logic is documented inline; the headline mappings are:
//
//   - top-level boosts/status/volatileStatus → Primary (status moves)
//   - self.{boosts,recoil,drain,heal} (and top-level recoil/drain/heal) → Self
//   - secondary / secondaries → Secondaries[]
//   - flags object → Flags array (only known flag keys)
//   - target "normal" → "foe"; target "self" → "self"
//   - accuracy == true → bypass-acc flag, accuracy 0 (auto-hit convention)
func transformMove(m upstreamMove) (domain.Move, error) {
	out := domain.Move{
		ID:       m.ID,
		Name:     m.Name,
		Type:     domain.Type(strings.ToLower(m.Type)),
		Category: domain.Category(strings.ToLower(m.Category)),
		Power:    m.BasePower,
		PP:       m.PP,
		Priority: m.Priority,
	}

	// Accuracy: Showdown emits either a number or the JSON literal `true`
	// (always hits). We normalize to a number; `true` becomes 0 (the auto-hit
	// convention used elsewhere in our schema) and gets a bypass-acc flag.
	accuracy, alwaysHits, err := parseAccuracy(m.Accuracy)
	if err != nil {
		return domain.Move{}, fmt.Errorf("accuracy: %w", err)
	}
	out.Accuracy = accuracy

	// Flags: filter to our known vocabulary, sorted for deterministic output.
	flags := make([]string, 0)
	for k := range m.Flags {
		if mapped, ok := flagsAllowlist[k]; ok {
			flags = append(flags, mapped)
		}
	}
	if alwaysHits {
		flags = append(flags, "bypass-acc")
	}
	sort.Strings(flags)
	if len(flags) > 0 {
		out.Flags = flags
	}

	// Target.
	switch m.Target {
	case "normal", "any", "allAdjacentFoes", "allAdjacent":
		out.Target = domain.TargetFoe
	case "self":
		out.Target = domain.TargetSelf
	case "":
		// many damage moves don't set target explicitly; default to foe.
		out.Target = domain.TargetFoe
	default:
		// keep going — schema requires target only for status moves; damage
		// moves default to foe. If this is a status move with unknown target,
		// the validator will catch it.
		out.Target = domain.TargetFoe
	}

	if m.Category == "Status" {
		// Status moves: emit a Primary block from top-level boosts / status /
		// volatileStatus. Damage moves never have these as primaries.
		primary, err := buildStatusPrimary(m)
		if err != nil {
			return domain.Move{}, fmt.Errorf("primary: %w", err)
		}
		out.Primary = primary
	}

	// Self block: collect guaranteed self-effects on damage moves (recoil,
	// drain, heal, and explicit self.boosts/volatileStatus).
	self, err := buildSelf(m)
	if err != nil {
		return domain.Move{}, fmt.Errorf("self: %w", err)
	}
	out.Self = self

	// Secondaries: prefer m.Secondaries (array); fall back to a single
	// m.Secondary if only that's set. Each is mapped one-to-one.
	rawSecs := m.Secondaries
	if rawSecs == nil && m.Secondary != nil {
		rawSecs = []secondaryRaw{*m.Secondary}
	}
	for _, s := range rawSecs {
		mapped, err := buildSecondary(s)
		if err != nil {
			return domain.Move{}, fmt.Errorf("secondary: %w", err)
		}
		out.Secondaries = append(out.Secondaries, mapped)
	}

	return out, nil
}

func parseAccuracy(raw json.RawMessage) (int, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 100, false, nil
	}
	if string(raw) == "true" {
		return 0, true, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false, fmt.Errorf("unrecognized accuracy %s: %w", string(raw), err)
	}
	return n, false, nil
}

func buildStatusPrimary(m upstreamMove) (*domain.Effect, error) {
	e := &domain.Effect{}
	any := false
	if len(m.Boosts) > 0 {
		boosts, err := mapBoosts(m.Boosts)
		if err != nil {
			return nil, err
		}
		e.Boosts = boosts
		any = true
	}
	if m.Status != "" {
		mapped, ok := statusMap[m.Status]
		if !ok {
			return nil, fmt.Errorf("unknown status %q", m.Status)
		}
		e.Status = mapped
		any = true
	}
	if v := mapVolatile(m.VolatileStatus, "primary "+m.ID); v != "" {
		e.Volatile = v
		any = true
	}
	if len(m.Heal) == 2 {
		e.Heal = float64(m.Heal[0]) / float64(m.Heal[1])
		any = true
	}
	if !any {
		return nil, nil
	}
	return e, nil
}

func buildSelf(m upstreamMove) (*domain.Effect, error) {
	e := &domain.Effect{}
	any := false
	if len(m.Recoil) == 2 {
		e.Recoil = float64(m.Recoil[0]) / float64(m.Recoil[1])
		any = true
	}
	if len(m.Drain) == 2 {
		e.Drain = float64(m.Drain[0]) / float64(m.Drain[1])
		any = true
	}
	// Some status moves carry top-level heal in addition to a Primary; for
	// damaging moves Heal is rare but we honor it on Self for completeness.
	if len(m.Heal) == 2 && m.Category != "Status" {
		e.Heal = float64(m.Heal[0]) / float64(m.Heal[1])
		any = true
	}
	if m.Self != nil {
		if len(m.Self.Boosts) > 0 {
			boosts, err := mapBoosts(m.Self.Boosts)
			if err != nil {
				return nil, err
			}
			e.Boosts = boosts
			any = true
		}
		if v := mapVolatile(m.Self.VolatileStatus, "self "+m.ID); v != "" {
			e.Volatile = v
			any = true
		}
	}
	if !any {
		return nil, nil
	}
	return e, nil
}

func buildSecondary(s secondaryRaw) (domain.Effect, error) {
	out := domain.Effect{Chance: s.Chance}
	if s.Status != "" {
		mapped, ok := statusMap[s.Status]
		if !ok {
			return domain.Effect{}, fmt.Errorf("unknown status %q", s.Status)
		}
		out.Status = mapped
	}
	if v := mapVolatile(s.VolatileStatus, "secondary"); v != "" {
		out.Volatile = v
	}
	if len(s.Boosts) > 0 {
		boosts, err := mapBoosts(s.Boosts)
		if err != nil {
			return domain.Effect{}, err
		}
		out.Boosts = boosts
	}
	return out, nil
}

func mapBoosts(in map[string]int) (map[string]int, error) {
	out := make(map[string]int, len(in))
	for k, v := range in {
		mapped, ok := boostStatMap[k]
		if !ok {
			return nil, fmt.Errorf("unknown boost stat %q", k)
		}
		out[mapped] = v
	}
	return out, nil
}

func secondType(types []string) domain.Type {
	if len(types) < 2 {
		return ""
	}
	return domain.Type(strings.ToLower(types[1]))
}

// stripShowdownID undoes Showdown's "name -> internal id" canonicalization so
// we can map between names like "Fire Blast" (which we slugify to
// "fire-blast") and Showdown's "fireblast" form used inside randombattle
// move pools.
func stripShowdownID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
