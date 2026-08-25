package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

// silentDropVolatiles are upstream volatile names we drop without warning
// because we model them another way (e.g. mustrecharge is implemented via
// the `recharge` move flag instead of a tracked Volatile).
var silentDropVolatiles = map[string]bool{
	"mustrecharge": true,
	// Bide is modeled, and modeled by move ID rather than through this
	// channel, because arming the store has to *replace* the move's turn
	// rather than follow it — no declarative payload can say "and then do
	// nothing else". Emitting it would be worse than dropping it: upstream
	// ships Bide as a damaging move, and a damaging move's Primary block is
	// applied to the *foe*, so the data would put the opponent in a Bide.
	"bide": true,
}

// mapVolatile filters upstream volatiles against the engine vocabulary
// (specs.Volatiles, populated by engine init()). Unknown slugs that
// aren't in the silent-drop list emit a warning so the gap is visible
// during sync.
func mapVolatile(name, where string) string {
	if name == "" {
		return ""
	}
	// The silent-drop list is consulted first, because it records a decision
	// about *how* a mechanic is modeled and that outranks the vocabulary's
	// record of *whether* it is. A slug can legitimately be in both: the engine
	// models the condition, and deliberately does not want it delivered through
	// a move's effect block. Bide is the case that made the order matter.
	if silentDropVolatiles[name] {
		return ""
	}
	if specs.Volatiles[name] {
		return name
	}
	log.Printf("  drop unknown volatile %q (%s)", name, where)
	return ""
}

// transformed is the bundle the stage step writes out: the four dataset
// files plus the list of moves actually used by the kept species (so we never
// ship orphan move data).
type transformed struct {
	Pokedex   []domain.Species
	Moves     []domain.Move
	Items     []domain.Item
	Natures   []domain.Nature
	Typechart map[domain.Type]map[domain.Type]float64
}

// showdownStatKeys maps Showdown's stat ids onto the slugs domain.Stats uses
// as JSON keys. Only the five nature-modifiable stats appear — no nature
// touches HP, and a "hp" key arriving here means upstream changed shape.
var showdownStatKeys = map[string]string{
	"atk": "atk",
	"def": "def",
	"spa": "spatk",
	"spd": "spdef",
	"spe": "speed",
}

// transformNatures remaps the upstream nature table into our schema. The
// output is sorted by id so the staged file is byte-stable across runs.
func transformNatures(natures []upstreamNature) ([]domain.Nature, error) {
	out := make([]domain.Nature, 0, len(natures))
	for _, n := range natures {
		// Neutral natures carry neither key upstream and neither here.
		if (n.Plus == "") != (n.Minus == "") {
			return nil, fmt.Errorf("nature %s: upstream set only one of plus/minus (%q/%q)", n.ID, n.Plus, n.Minus)
		}
		dn := domain.Nature{ID: n.ID, Name: n.Name}
		if n.Plus != "" {
			plus, ok := showdownStatKeys[n.Plus]
			if !ok {
				return nil, fmt.Errorf("nature %s: unknown upstream stat id %q in plus", n.ID, n.Plus)
			}
			minus, ok := showdownStatKeys[n.Minus]
			if !ok {
				return nil, fmt.Errorf("nature %s: unknown upstream stat id %q in minus", n.ID, n.Minus)
			}
			dn.Plus, dn.Minus = plus, minus
		}
		out = append(out, dn)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// curatedItems is the allowlist of held items the engine models — the
// inverse of denylistMoves. Items are universal (no learnset to scope
// against), so the upstream dump carries the whole 500+ catalog and this
// list is the curated subset that ships to data/items.json. Display names
// come from upstream, so this is just the set of slugs; add a slug here as
// each item's engine behavior lands. The transform errors if a slug isn't in
// the upstream catalog, catching typos and upstream renames.
//
// Grouped the way internal/engine groups its registry files, so a reviewer can
// check "is this list the same set the engine wires up?" group by group. The
// engine's TestItemCoverage fails on any slug here it doesn't model, and
// TestItemRegistrySubsetOfCatalog fails on any item it models that isn't here.
var curatedItems = map[string]bool{
	// Always-on stat and damage modifiers (engine: items_core.go).
	"leftovers":    true,
	"choice-band":  true,
	"choice-specs": true,
	"choice-scarf": true,
	"life-orb":     true,
	"focus-sash":   true,

	// Berries and Berry Juice — one-shot consumables (engine: items_berries.go).
	// HP restore.
	"oran-berry":   true,
	"sitrus-berry": true,
	"berry-juice":  true,
	"figy-berry":   true,
	"wiki-berry":   true,
	"mago-berry":   true,
	"aguav-berry":  true,
	"iapapa-berry": true,
	// Status and PP cure.
	"cheri-berry":  true,
	"chesto-berry": true,
	"pecha-berry":  true,
	"rawst-berry":  true,
	"aspear-berry": true,
	"persim-berry": true,
	"lum-berry":    true,
	"leppa-berry":  true,
	// Pinch (a quarter HP or less).
	"liechi-berry": true,
	"ganlon-berry": true,
	"petaya-berry": true,
	"apicot-berry": true,
	"salac-berry":  true,
	"starf-berry":  true,
	"custap-berry": true,
	"micle-berry":  true,
	// Damage reaction.
	"enigma-berry":  true,
	"jaboca-berry":  true,
	"rowap-berry":   true,
	"kee-berry":     true,
	"maranga-berry": true,
	// Type resist — one per type.
	"occa-berry":   true,
	"passho-berry": true,
	"wacan-berry":  true,
	"rindo-berry":  true,
	"yache-berry":  true,
	"chople-berry": true,
	"kebia-berry":  true,
	"shuca-berry":  true,
	"coba-berry":   true,
	"payapa-berry": true,
	"tanga-berry":  true,
	"charti-berry": true,
	"kasib-berry":  true,
	"haban-berry":  true,
	"colbur-berry": true,
	"babiri-berry": true,
	"roseli-berry": true,
	"chilan-berry": true,

	// Always-on modifiers (engine: items_modifiers.go).
	// Type boosters — one per type, x1.2 to the matching type.
	"silk-scarf":     true,
	"charcoal":       true,
	"mystic-water":   true,
	"magnet":         true,
	"miracle-seed":   true,
	"never-melt-ice": true,
	"black-belt":     true,
	"poison-barb":    true,
	"soft-sand":      true,
	"sharp-beak":     true,
	"twisted-spoon":  true,
	"silver-powder":  true,
	"hard-stone":     true,
	"spell-tag":      true,
	"dragon-fang":    true,
	"black-glasses":  true,
	"metal-coat":     true,
	"fairy-feather":  true,
	// Category and coverage boosters.
	"expert-belt":    true,
	"muscle-band":    true,
	"wise-glasses":   true,
	"punching-glove": true,
	"metronome":      true,
	// Defensive and recovery.
	"assault-vest": true,
	"rocky-helmet": true,
	"shell-bell":   true,
	"big-root":     true,
	"focus-band":   true,
	// Critical-hit ratio, including the species-locked relics.
	"scope-lens":  true,
	"razor-claw":  true,
	"lucky-punch": true,
	"leek":        true,
	"thick-club":  true,

	// Rule-changing utility (engine: items_field.go).
	// Self-inflicted status and typing-keyed residuals.
	"flame-orb":    true,
	"toxic-orb":    true,
	"black-sludge": true,
	"sticky-barb":  true,
	// Field-duration extenders.
	"light-clay":       true,
	"damp-rock":        true,
	"heat-rock":        true,
	"smooth-rock":      true,
	"icy-rock":         true,
	"terrain-extender": true,
	// Partial-trap modifiers.
	"binding-band": true,
	"grip-claw":    true,
	// Immunity grants and removals.
	"heavy-duty-boots": true,
	"safety-goggles":   true,
	"shed-shell":       true,
	"air-balloon":      true,
	"iron-ball":        true,
	"ring-target":      true,
	"utility-umbrella": true,
	"protective-pads":  true,
	"covert-cloak":     true,
	"clear-amulet":     true,

	// Event reactions, accuracy and turn order (engine: items_reactive.go).
	"weakness-policy": true,
	"absorb-bulb":     true,
	"cell-battery":    true,
	"luminous-moss":   true,
	"snowball":        true,
	"throat-spray":    true,
	"blunder-policy":  true,
	"white-herb":      true,
	"mental-herb":     true,
	"power-herb":      true,
	"king-s-rock":     true,
	"razor-fang":      true,
	"wide-lens":       true,
	"zoom-lens":       true,
	"bright-powder":   true,
	"lax-incense":     true,
	"quick-claw":      true,
	"lagging-tail":    true,
	"full-incense":    true,
	"loaded-dice":     true,
}

// transformItems resolves the curated allowlist against the upstream catalog,
// pulling each item's canonical display name from upstream. Sorted by id for a
// deterministic, diff-friendly output.
func transformItems(up *upstream) ([]domain.Item, error) {
	out := make([]domain.Item, 0, len(curatedItems))
	for slug := range curatedItems {
		it, ok := up.Items[slug]
		if !ok {
			return nil, fmt.Errorf("curated item %q not in upstream catalog (typo or upstream rename?)", slug)
		}
		out = append(out, domain.Item{ID: it.ID, Name: it.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

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
// about. Other flags (mirror, metronome — informational only to Showdown's own
// callback system) are dropped so the validator doesn't see unknowns.
//
// This header used to name `protect` among the dropped ones, and kept saying so
// after `protect` was added below. The lesson is worth leaving here: a flag is
// "informational" only until some rule reads it, and every entry in this map
// arrived because a rule turned out to *be* the flag. Check the consumer before
// deciding a flag is decoration.
//
// `charge` and `recharge` are Showdown's marks for two-turn / recharge moves
// (Solar Beam, Sky Attack, Hyper Beam, ...) and our engine consumes them
// directly under our own slug names.
//
// The "ability/item hook anchors" block lands flags whose behavior is inert
// today but expected by future ability/item systems (#30 audit (a) bucket).
// They appear in `data/moves.json`, pass validation, and read as documentation
// until the matching ability/item is wired up.
var flagsAllowlist = map[string]string{
	"contact":   "contact",
	"punch":     "punch",
	"bite":      "bite",
	"sound":     "sound",
	"powder":    "powder",
	"charge":    "two-turn",
	"recharge":  "recharge",
	"bullet":    "bullet",
	"slicing":   "slicing",
	"wind":      "wind",
	"dance":     "dance",
	"pulse":     "pulse",
	"heal":      "heal",
	"defrost":   "defrost",
	"bypasssub": "bypass-sub",
	// `protect` is the whole of Protect's rule, not an annotation. Showdown's
	// Battle#checkMoveBypassesProtect blocks a move if and only if it carries
	// this flag — which is why entry hazards, the field moves and Roar go
	// straight through a shield. Dropping it left protect.go with nothing to
	// read, so it inverted the predicate and defaulted to blocking. 432 of the
	// curated moves carry it; the 106 that do not are 104 status moves plus
	// Feint and Phantom Force, which carry bypass-protect instead.
	"protect": "protect",
	// `gravity` is the whole of Gravity's move ban. Showdown's gravity
	// condition reads this flag and nothing else, in three places — onDisableMove
	// (the move is greyed out at selection), onBeforeMove and onModifyMove (a
	// move that reaches resolution anyway, including one another move called, is
	// refused). Without it there is no way to tell Fly from Tackle at runtime.
	// Seven of the curated moves carry it: bounce, fly, high-jump-kick,
	// jump-kick, magnet-rise, splash and telekinesis. Sky Drop and Flying Press
	// carry it upstream and are not in this dataset.
	"gravity": "gravity",
}

// weatherSlug maps Showdown's weather identifier (the value of upstreamMove
// .Weather) to our engine slug. Hail (legacy) and Snowscape (Gen 9) both
// resolve to "snow" — modernization-plan call to unify them under the Gen-9
// behavior (Ice-type Def boost, no residual chip damage).
var weatherSlug = map[string]string{
	"RainDance": "rain",
	"sunnyday":  "sun",
	"Sandstorm": "sandstorm",
	"snowscape": "snow",
	"hail":      "snow",
}

// terrainSlug maps Showdown's terrain identifier (upstreamMove.Terrain) to
// our engine slug. Kept in sync with engine/terrain.go's TerrainKind set;
// new terrain values from upstream surface here as an "unknown terrain"
// error rather than shipping silently as no-ops.
var terrainSlug = map[string]string{
	"electricterrain": "electric",
	"grassyterrain":   "grassy",
	"mistyterrain":    "misty",
	"psychicterrain":  "psychic",
}

// pseudoWeatherSlug is identity today — Showdown's pseudoWeather IDs
// already match our engine slugs ("trickroom", "wonderroom",
// "magicroom", "gravity"). Filtered against specs.PseudoWeathers so
// unmodeled pseudo-weathers surface in the coverage audit rather than
// shipping as a validator error.

// manualMoveFlags injects engine flags for behaviors Showdown encodes via JS
// callbacks rather than the static `flags`/effect blocks the dump captures
// — so re-running data-sync won't quietly drop them. Move IDs are our slugs
// (post-transform). See the engine for what each flag means.
var manualMoveFlags = map[string][]string{
	"explosion":     {"selfdestruct"},       // user faints on use, hit or miss
	"self-destruct": {"selfdestruct"},       //  "
	"seismic-toss":  {"fixed-damage-level"}, // damage == user level
	"night-shade":   {"fixed-damage-level"}, //  "
	// Showdown's `selfdestruct` field is a *static* one, but the upstream
	// refresh script does not capture it (see refresh-upstream/refresh.js's
	// field list), so both of its values have to be injected here. The two
	// are not interchangeable: `always` detonates the user before the hit
	// step and is what Damp refuses by name, while `ifHit` faints it only
	// once the move has reached its target — so Memento into a Substitute
	// costs nothing and a Final Gambit at a Ghost is a wasted turn, not a
	// suicide. Mapping these onto the `selfdestruct` flag above would get
	// both of those backwards.
	"memento":      {"selfdestruct-if-hit"},
	"final-gambit": {"selfdestruct-if-hit"},
}

// denylistMoves are stripped from every species's learnset at sync time.
// These are moves that either depend on mechanics out of #30 scope
// (doubles, pledges, calls-another-move, type/identity changes) or are
// deferred behind a sub-ticket that hasn't landed yet. The principle:
// don't ship a move whose behavior we can't honor — the alternative is
// a "does-nothing" pick that confuses the picker UX and the engine logs.
//
// Moves come back off this list as their mechanics land. Source of
// truth for the rationale is docs/modernization-audit.md (c bucket).
var denylistMoves = map[string]bool{
	// Doubles-only (no allies in singles)
	"helping-hand": true,
	"follow-me":    true,
	"rage-powder":  true,
	"spotlight":    true,
	"ally-switch":  true,
	"after-you":    true,
	"quash":        true,
	"decorate":     true,
	"dragon-cheer": true,
	// The other two adjacentAlly moves, missed when this block was written.
	// Showdown's getRandomTarget returns null for adjacentAlly in singles and
	// the move fails outright (battle.ts:2504), so there is nothing to map
	// them to: `foe` hands the opponent a free boost and `self` fabricates one
	// for the user. Neither is the move.
	"coaching":   true,
	"hold-hands": true,
	// Pledge combos (doubles)
	"fire-pledge":  true,
	"water-pledge": true,
	"grass-pledge": true,
	// Doom Desire is Future Sight's sibling and is denied for nothing: no kept
	// species learns it, so removing the entry would change no bytes. Left as a
	// marker of the pair.
	"doom-desire": true,
	// Reactive damage (needs "damage taken this turn" register)
	"metal-burst": true,
	// Calls-another-move mini-engines
	"mimic":       true,
	"mirror-move": true,
	"copycat":     true,
	"sketch":      true,
	"assist":      true,
	"me-first":    true,
	"metronome":   true,
	"sleep-talk":  true,
	"snore":       true,
	// Type / identity changes.
	//
	// Soak, Reflect Type and the two Conversions have come off; what is left is
	// Transform, and it is left for a reason that is not about the move. Ditto's
	// entire Gen-1 learnset is ["transform"], so denying the move empties its
	// learnset and transform() skips the species — which is the only reason this
	// dex is 80 species and not 81. Un-denylisting Transform would add a Pokemon
	// to the roster as a side effect, and the roster is not this ticket's to
	// change. Camouflage stays because no kept species learns it, so removing it
	// would change nothing at all.
	"transform":  true,
	"camouflage": true,
	// Doubles-flavored two-turn
	"sky-drop": true,
	// Custom HP arithmetic / sacrifice
	// Pre-terrain pseudoweather, superseded by Terrain
	"mud-sport":   true,
	"water-sport": true,
}

func transform(up *upstream, species []upstreamSpecies) (transformed, error) {
	// Build the Showdown-stripped-id → our-slug map so we can translate the
	// move references inside learnsets (which use the stripped form).
	showdownToSlug := make(map[string]string, len(up.Moves))
	for _, m := range up.Moves {
		showdownToSlug[stripShowdownID(m.Name)] = m.ID
	}

	// Emit each kept species's full Gen-1 learnset (every move reachable
	// via level-up / TM / HM / tutor in Gen 1) and collect every move
	// actually referenced — that's the union we emit to moves.json. The
	// picker room (see docs/team-picker-room.md) gives the user 1-4 picks
	// from this set.
	referenced := map[string]bool{}
	pokedex := make([]domain.Species, 0, len(species))
	for _, sp := range species {
		ids, ok := up.Learnsets[sp.ID]
		if !ok {
			return transformed{}, fmt.Errorf("learnset missing for species %q", sp.ID)
		}
		moves, err := translateLearnset(sp.ID, ids, showdownToSlug, up.Moves)
		if err != nil {
			return transformed{}, err
		}
		if len(moves) == 0 {
			// No Gen-1-reachable moves — should never happen for an in-scope
			// species (every Gen 1 Pokémon learns at least one Gen 1 move),
			// but we skip rather than ship a moveless species to validation.
			continue
		}
		for _, mid := range moves {
			referenced[mid] = true
		}
		genders, maleRatio, err := speciesGenders(sp)
		if err != nil {
			return transformed{}, fmt.Errorf("species %s: %w", sp.Name, err)
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
			Abilities: orderAbilities(sp.Abilities),
			Genders:   genders,
			MaleRatio: maleRatio,
			Moves:     moves,
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

	items, err := transformItems(up)
	if err != nil {
		return transformed{}, err
	}

	natures, err := transformNatures(up.Natures)
	if err != nil {
		return transformed{}, err
	}

	return transformed{Pokedex: pokedex, Moves: moves, Items: items, Natures: natures, Typechart: chart}, nil
}

// translateLearnset maps Showdown move IDs from the upstream learnset
// to our slugs, dedupes, drops denylisted moves, and preserves input
// order. The upstream snapshot chose the order (lowest-level-up first;
// see refresh.js:dumpLearnsets) so the picker UI's default "first 4"
// is a sensible early-progression moveset rather than an alphabetical
// jumble.
func translateLearnset(speciesID string, ids []string, showdownToSlug map[string]string, moves map[string]upstreamMove) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	dropped := 0
	for _, sid := range ids {
		slug, ok := showdownToSlug[sid]
		if !ok {
			return nil, fmt.Errorf("species %s: learnset references unknown move %q", speciesID, sid)
		}
		if _, exists := moves[slug]; !exists {
			return nil, fmt.Errorf("species %s: move %q not in upstream moves snapshot", speciesID, slug)
		}
		if denylistMoves[slug] {
			dropped++
			continue
		}
		if seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, slug)
	}
	if dropped > 0 {
		log.Printf("  denylist: %s dropped %d moves", speciesID, dropped)
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
//   - weather → Move.Weather (engine slug; see weatherSlug map)
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

	if m.Weather != "" {
		slug, ok := weatherSlug[m.Weather]
		if !ok {
			return domain.Move{}, fmt.Errorf("unknown weather %q", m.Weather)
		}
		out.Weather = slug
	}

	if m.Terrain != "" {
		slug, ok := terrainSlug[m.Terrain]
		if !ok {
			return domain.Move{}, fmt.Errorf("unknown terrain %q", m.Terrain)
		}
		out.Terrain = slug
	}

	// Showdown is inconsistent about the casing of these three condition
	// fields: sideCondition/pseudoWeather arrive already stripped-lowercase
	// ("reflect", "trickroom") but slotCondition arrives as the display name
	// ("Wish"). Normalize all three to the id form before the vocab check, or
	// "Wish" silently fails specs.SlotConditions["wish"] and the slot condition
	// is dropped (which is exactly the Wish/Healing Wish regression this fixes).
	if slug := stripShowdownID(m.SideCondition); slug != "" && specs.SideConditions[slug] {
		// Filter against the engine vocabulary (single source in
		// internal/specs, populated by engine init). Side conditions
		// the engine doesn't model (Sticky Web, Quick/Wide Guard, ...)
		// fall through silently — the move ships without the effect
		// and surfaces in the coverage audit.
		out.SideCondition = slug
	}

	if slug := stripShowdownID(m.PseudoWeather); slug != "" && specs.PseudoWeathers[slug] {
		// Same filter shape as SideCondition. Unmodeled pseudo-weathers
		// (none today) would fall through silently.
		out.PseudoWeather = slug
	}

	if slug := stripShowdownID(m.SlotCondition); slug != "" && specs.SlotConditions[slug] {
		// Slot conditions (Wish, Healing Wish): same filter shape as
		// PseudoWeather. Unmodeled slugs fall through silently and
		// surface in the audit.
		out.SlotCondition = slug
	}

	// Accuracy: Showdown emits either a number or the JSON literal `true`
	// (always hits). We normalize to a number; `true` becomes 0 (the auto-hit
	// convention used elsewhere in our schema) and gets a bypass-acc flag.
	accuracy, alwaysHits, err := parseAccuracy(m.Accuracy)
	if err != nil {
		return domain.Move{}, fmt.Errorf("accuracy: %w", err)
	}
	out.Accuracy = accuracy

	// Flags: filter to our known vocabulary, then layer in any manual
	// additions for moves Showdown encodes via JS callbacks. Sorted for
	// deterministic output.
	flagSet := make(map[string]bool, len(m.Flags))
	for k := range m.Flags {
		if mapped, ok := flagsAllowlist[k]; ok {
			flagSet[mapped] = true
		}
	}
	if alwaysHits {
		flagSet["bypass-acc"] = true
	}
	// ignoreImmunity is a per-move static (bool true, or an object naming
	// specific types), and Showdown *derives* it rather than reading it: a move
	// that says nothing resolves to `category === 'Status'`. So the flag lands
	// on essentially every status move — 166 of the 167 curated ones — and the
	// engine consumes it as "this move is not refused by the type chart"
	// (resolveStatusMoveTypeImmunity). Thunder Wave is the one status move that
	// opts back in, and the only reason the flag is interesting at all.
	//
	// Not Foresight / Scrappy, which an earlier version of this comment
	// promised: those are implemented against the type chart directly, in
	// effectivenessWithLifts.
	//
	// The collapse to a single bool is lossy — upstream's object form
	// (Thousand Arrows' `ignoreImmunity: { Ground: true }`) would become a
	// blanket "ignores everything". No curated move uses the object form today
	// (zero non-status moves carry the flag at all), so the loss is currently
	// theoretical; it stops being theoretical the moment one is synced in.
	if isTruthyRaw(m.IgnoreImmunity) {
		flagSet["ignore-immunity"] = true
	}
	// breaksProtect is a per-move static bool that says "this move ignores
	// Protect / Detect on the target." Engine reads it as the bypass-protect
	// flag through the same dispatch substitute uses for sound + bypass-sub.
	if m.BreaksProtect {
		flagSet["bypass-protect"] = true
	}
	// critRatio is Showdown's crit-stage offset: 1 is normal, 2 is the
	// boosted rate Stone Edge / Slash / Night Slash carry. The engine models
	// crit as a single stage table, so any ratio above 1 collapses to one
	// "high-crit" flag (+1 stage in computeDamage). Ratios of 3+ don't occur
	// on the curated roster; if upstream ever ships one it still reads as
	// high-crit rather than being silently dropped.
	if m.CritRatio > 1 {
		flagSet["high-crit"] = true
	}
	for _, f := range manualMoveFlags[m.ID] {
		flagSet[f] = true
	}
	flags := make([]string, 0, len(flagSet))
	for f := range flagSet {
		flags = append(flags, f)
	}
	sort.Strings(flags)
	if len(flags) > 0 {
		out.Flags = flags
	}

	// Target. Showdown has fifteen values here and this engine has two, so
	// every mapping is a claim that somebody checked the collapse. The default
	// used to be `foe`, and it was not a claim about anything — it swept up the
	// ally-facing targets and handed Howl, Coaching and Life Dew to the
	// opponent, and it marked the entry hazards as attacks, which is half of
	// why Protect walled off the hazard game. An unrecognized target is an
	// error now.
	//
	//	foe   the opponent, its side, or the whole field. `foeSide` (hazards)
	//	      and `all` (weather, terrain, the rooms, Haze, Perish Song) are
	//	      handler-driven — the setters pick their own side — but foe is
	//	      what keeps Magic Coat bouncing hazards and Pressure charging
	//	      them, both canon (moves.ts `reflectable` / `mustpressure`).
	//	self  the user, its side, or its party. Showdown resolves allies,
	//	      allySide, allyTeam and adjacentAllyOrSelf to the user in singles
	//	      (battle.ts#getRandomTarget, battle-actions.ts:419).
	//
	// `adjacentAlly` is deliberately absent: getRandomTarget returns null for
	// it in singles and the move fails, so there is no honest two-value
	// mapping. Its moves are denylisted instead.
	switch m.Target {
	case "", "normal", "any", "adjacentFoe", "randomNormal",
		"allAdjacentFoes", "allAdjacent", "all", "foeSide", "scripted":
		out.Target = domain.TargetFoe
	case "self", "allies", "allySide", "allyTeam", "adjacentAllyOrSelf":
		out.Target = domain.TargetSelf
	default:
		return domain.Move{}, fmt.Errorf("unknown target %q: no singles mapping. "+
			"Map it in this switch once somebody has checked what it should collapse to, "+
			"or denylist the move", m.Target)
	}

	if m.Category == "Status" {
		// Status moves: emit a Primary block from top-level boosts / status /
		// volatileStatus.
		primary, err := buildStatusPrimary(m)
		if err != nil {
			return domain.Move{}, fmt.Errorf("primary: %w", err)
		}
		out.Primary = primary
	} else if v := mapVolatile(m.VolatileStatus, "damage-primary "+m.ID); v != "" {
		// Damage moves with a top-level volatileStatus (partial-trap moves
		// like Bind, Fire Spin) land it as a guaranteed primary effect on
		// the foe, after damage. Distinct from a 100% Secondary because the
		// engine's Primary path bypasses Shield Dust / Sheer Force, matching
		// Showdown's "top-level volatileStatus isn't a secondary" treatment.
		out.Primary = &domain.Effect{Volatile: v}
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

	// Multihit: Showdown encodes as null (single-hit), int N (always N hits),
	// or [low, high] (rolls per Gen-5+ distribution at battle time). We
	// normalise to MinHits/MaxHits — fixed counts get equal values, ranges
	// pass through. Engine consults these to size the strike loop.
	minHits, maxHits, err := parseMultihit(m.Multihit)
	if err != nil {
		return domain.Move{}, fmt.Errorf("multihit: %w", err)
	}
	out.MinHits = minHits
	out.MaxHits = maxHits

	// OHKO: bool true → "any" (Fissure / Horn Drill / Guillotine); a string
	// payload is a type-name immunity that stacks on the normal chart
	// (Sheer Cold's "Ice" → "ice"). Falsy → "" (move is not OHKO).
	ohko, err := parseOHKO(m.OHKO)
	if err != nil {
		return domain.Move{}, fmt.Errorf("ohko: %w", err)
	}
	out.OHKO = ohko

	// thawsTarget: per-move thaw flag. Fire-type damaging moves thaw
	// canonically via their type and don't need it; this surfaces the
	// special-case thaws on non-Fire moves (Scald, Scorching Sands).
	out.ThawsTarget = m.ThawsTarget

	// ignoreEvasion / ignoreDefensive: passthrough bools. Both currently
	// only co-occur on Chip Away and Darkest Lariat, but the schema
	// fields are independent so future moves with only one don't need
	// a re-spin of the data pipeline.
	out.IgnoreEvasion = m.IgnoreEvasion
	out.IgnoreDefensive = m.IgnoreDefensive

	// overrideOffensiveStat / overrideDefensiveStat: which stat the damage
	// formula reads instead of the category default. Only the four battle
	// stats are meaningful — a Showdown value outside boostStatMap, or one
	// naming Speed / accuracy / evasion, is an error rather than a silent
	// drop, since shipping the move without its override is exactly the
	// failure this pipeline is meant to catch (Psystrike quietly became an
	// ordinary special Psychic move that way).
	if out.OverrideOffensiveStat, err = parseStatOverride(m.OverrideOffensiveStat); err != nil {
		return domain.Move{}, fmt.Errorf("overrideOffensiveStat: %w", err)
	}
	if out.OverrideDefensiveStat, err = parseStatOverride(m.OverrideDefensiveStat); err != nil {
		return domain.Move{}, fmt.Errorf("overrideDefensiveStat: %w", err)
	}

	// selfSwitch: bool true → "normal" (U-turn / Volt Switch / Flip Turn /
	// Teleport); "copyvolatile" → Baton Pass; "shedtail" (Shed Tail, Gen 9)
	// is not yet modeled and is dropped with a warning so we ship a no-op
	// rather than mis-promote it to plain self-switch.
	selfSwitch, err := parseSelfSwitch(m.ID, m.SelfSwitch)
	if err != nil {
		return domain.Move{}, fmt.Errorf("selfSwitch: %w", err)
	}
	out.SelfSwitch = selfSwitch

	// forceSwitch: per-move static bool. Roar / Whirlwind / Circle
	// Throw / Dragon Tail. Engine fires applyForceSwitch after the
	// move resolves; see forceswitch.go.
	out.ForceSwitch = m.ForceSwitch

	return out, nil
}

// parseStatOverride maps a Showdown stat id from overrideOffensiveStat /
// overrideDefensiveStat to our slug. Empty stays empty ("use the category
// default"). Only the four stats the damage formula reads are legal —
// Speed, accuracy and evasion never appear on either field upstream, and
// an override naming one would mean Showdown had changed shape under us.
func parseStatOverride(showdownStat string) (string, error) {
	if showdownStat == "" {
		return "", nil
	}
	slug, ok := boostStatMap[showdownStat]
	if !ok {
		return "", fmt.Errorf("unknown stat %q", showdownStat)
	}
	switch slug {
	case "attack", "defense", "spatk", "spdef":
		return slug, nil
	default:
		return "", fmt.Errorf("stat %q is not a damage-formula stat", showdownStat)
	}
}

// speciesGenders turns Showdown's gender pair into the legal gender set and
// the male birth share. Showdown states a fixed gender as 'M' / 'F' / 'N' and
// gives everything else a ratio; a species with neither is the ordinary 50/50.
//
// The set is ordered likeliest-first, because Genders[0] is what a team build
// falls back to when nothing picks or rolls a gender. An unknown gender letter
// is an error rather than a silent genderless — a whole species quietly
// becoming immune to Attract is exactly the class of gap this pipeline keeps
// producing.
func speciesGenders(sp upstreamSpecies) ([]string, float64, error) {
	switch sp.Gender {
	case "N":
		return []string{domain.GenderGenderless}, 0, nil
	case "M":
		return []string{domain.GenderMale}, 1, nil
	case "F":
		return []string{domain.GenderFemale}, 0, nil
	case "":
	default:
		return nil, 0, fmt.Errorf("unknown gender %q", sp.Gender)
	}
	male := 0.5
	if sp.GenderRatio != nil {
		male = sp.GenderRatio.M
	}
	switch {
	case male >= 1:
		return []string{domain.GenderMale}, 1, nil
	case male <= 0:
		return []string{domain.GenderFemale}, 0, nil
	case male >= 0.5:
		return []string{domain.GenderMale, domain.GenderFemale}, male, nil
	default:
		return []string{domain.GenderFemale, domain.GenderMale}, male, nil
	}
}

// parseOHKO normalises Showdown's ohko field. null/false → "" (not an OHKO
// move); true → "any"; a JSON string is the slug of the type that's extra-
// immune (Sheer Cold's "Ice" → "ice"). Any other shape is a hard error so
// future Showdown payload surprises don't ship silently.
func parseOHKO(raw json.RawMessage) (string, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" || s == "false" {
		return "", nil
	}
	if s == "true" {
		return "any", nil
	}
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return "", fmt.Errorf("unrecognized ohko %s: %w", s, err)
	}
	if str == "" {
		return "", nil
	}
	return strings.ToLower(str), nil
}

// parseSelfSwitch normalises Showdown's selfSwitch field. null/false → ""
// (move doesn't self-switch); true → "normal" (U-turn / Volt Switch / Flip
// Turn / Teleport); "copyvolatile" → "copyvolatile" (Baton Pass — stages
// and confusion carry to the incoming). Other string variants ("shedtail")
// aren't modeled yet; we drop them with a warning so the move ships
// without the effect rather than silently masquerading as plain self-switch.
func parseSelfSwitch(moveID string, raw json.RawMessage) (string, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" || s == "false" {
		return "", nil
	}
	if s == "true" {
		return "normal", nil
	}
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return "", fmt.Errorf("unrecognized selfSwitch %s: %w", s, err)
	}
	switch str {
	case "copyvolatile":
		return "copyvolatile", nil
	case "":
		return "", nil
	default:
		log.Printf("  drop unmodeled selfSwitch %q on %s", str, moveID)
		return "", nil
	}
}

// parseMultihit normalises Showdown's flexible multihit field. Returns
// (0, 0) for null / falsy values, (N, N) for a fixed count, (lo, hi) for
// a range. A single-element array [N] is treated as a fixed count (used
// by Triple Axel and similar).
func parseMultihit(raw json.RawMessage) (int, int, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "false" {
		return 0, 0, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		if n < 2 {
			return 0, 0, nil
		}
		return n, n, nil
	}
	var arr []int
	if err := json.Unmarshal(raw, &arr); err != nil {
		return 0, 0, fmt.Errorf("unrecognized multihit %s: %w", string(raw), err)
	}
	switch len(arr) {
	case 1:
		if arr[0] < 2 {
			return 0, 0, nil
		}
		return arr[0], arr[0], nil
	case 2:
		if arr[0] < 1 || arr[1] < arr[0] {
			return 0, 0, fmt.Errorf("invalid multihit range %v", arr)
		}
		if arr[1] < 2 {
			return 0, 0, nil
		}
		return arr[0], arr[1], nil
	default:
		return 0, 0, fmt.Errorf("multihit array must be length 1 or 2, got %v", arr)
	}
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
	// A secondary's `self` block aims its payload at the user instead of the
	// target — Rapid Spin's +1 Speed, Power-Up Punch's +1 Atk, Ancient
	// Power's 10% omniboost. Dropping it was leaving eleven curated moves
	// shipping a bare {"chance": N} that rolled and then did nothing.
	//
	// Showdown keeps user- and target-side payloads in separate entries, each
	// with its own roll, so one entry carrying both would mean upstream had
	// changed shape — and silently picking a side is how this class of bug
	// happens. Refuse it instead.
	if s.Self != nil {
		if out.Status != "" || out.Volatile != "" || len(out.Boosts) > 0 {
			return domain.Effect{}, fmt.Errorf("secondary carries both a self and a target payload")
		}
		if len(s.Self.Boosts) > 0 {
			boosts, err := mapBoosts(s.Self.Boosts)
			if err != nil {
				return domain.Effect{}, err
			}
			out.Boosts = boosts
			out.Self = true
		}
		if v := mapVolatile(s.Self.VolatileStatus, "secondary self"); v != "" {
			out.Volatile = v
			out.Self = true
		}
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

// isTruthyRaw decides whether a json.RawMessage from upstream represents
// a "set" value. Used for fields Showdown emits as either a bool, an
// object, or a string ({"Ghost": true}, "copyvolatile", etc.) — for the
// (a) bucket we collapse all non-falsy variants to one flag.
func isTruthyRaw(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	s := string(raw)
	return s != "false" && s != "null" && s != `""` && s != "0"
}

// orderAbilities converts the upstream {0, 1?, H?} map into a deterministic
// slice [slot0, slot1?, slotH?]. Slot 0 is always present and is the picker
// default; slot 1 (when set) is the second normal ability; slot H is the
// hidden ability. Names are slugified to our engine convention (lowercase,
// spaces / punctuation → hyphens): "Thick Fat" → "thick-fat", "Magic
// Bounce" → "magic-bounce". The engine's ability registry looks up by this
// slug and no-ops on miss, so we can pipe every species's abilities through
// even before the engine implements them.
func orderAbilities(in map[string]string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, 3)
	for _, k := range []string{"0", "1", "H"} {
		if name, ok := in[k]; ok && name != "" {
			out = append(out, slugifyAbility(name))
		}
	}
	return out
}

// slugifyAbility kebab-cases an ability name from its display form. Matches
// the slugify in tools/data-sync/refresh-upstream/refresh.js so species and
// move slugs stay consistent across the pipeline.
func slugifyAbility(name string) string {
	var b strings.Builder
	prevHyphen := true // suppress leading hyphen
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	s := b.String()
	return strings.TrimSuffix(s, "-")
}

// stripShowdownID undoes Showdown's "name -> internal id" canonicalization so
// we can map between names like "Fire Blast" (which we slugify to
// "fire-blast") and Showdown's "fireblast" form used inside the upstream
// learnsets and move IDs.
func stripShowdownID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
