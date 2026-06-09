// Package domain holds the static Pokémon reference data — species, moves,
// and the type chart — loaded once from the curated JSON dataset. It has no
// battle logic and no I/O beyond reading the dataset files.
package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"sort"

	"pokearena/internal/specs"
)

// Type is one of the 18 elemental types.
type Type string

// Category classifies how a move deals (or does not deal) damage.
type Category string

const (
	CatPhysical Category = "physical"
	CatSpecial  Category = "special"
	CatStatus   Category = "status"
)

// Stats is a stat spread. For a Species it is the base stats; for a battle
// Pokémon it is the derived stats. HP is carried for convenience.
type Stats struct {
	HP  int `json:"hp"`
	Atk int `json:"atk"`
	Def int `json:"def"`
	SpA int `json:"spatk"`
	SpD int `json:"spdef"`
	Spe int `json:"speed"`
}

// Species is one entry in the Pokédex.
//
// Abilities is ordered [slot0, slot1?, slotH?]: slot 0 is the picker default,
// slot 1 (if present) is the second normal ability, slot H (if present) is
// the hidden ability. Empty for species the engine doesn't model abilities
// for (none today, but the field is omitempty so older curated dumps
// without it still load). The engine treats slugs not present in its
// ability registry as no-ops, so the dataset can carry every Showdown
// ability ahead of engine support.
type Species struct {
	DexNo     int      `json:"dex_no"`
	Name      string   `json:"name"`
	Type1     Type     `json:"type1"`
	Type2     Type     `json:"type2"`
	Base      Stats    `json:"base"`
	Abilities []string `json:"abilities,omitempty"`
	Moves     []string `json:"moves"`
}

// Target is who a move points at: the foe (default for damage moves) or the
// user (status moves like Swords Dance, Recover, Agility).
type Target string

const (
	TargetFoe  Target = "foe"
	TargetSelf Target = "self"
)

// Effect is the unified effect block used by Move.Primary (guaranteed effect
// of a status move), Move.Self (guaranteed self-effect of a damaging move),
// and each entry of Move.Secondaries (rolled riders on a damaging move). A
// single block may set multiple fields; the engine applies them in a fixed
// order — see docs/battle-state.md.
type Effect struct {
	Chance   int            `json:"chance,omitempty"`
	Status   string         `json:"status,omitempty"`
	Volatile string         `json:"volatile,omitempty"`
	Boosts   map[string]int `json:"boosts,omitempty"`
	Heal     float64        `json:"heal,omitempty"`
	Drain    float64        `json:"drain,omitempty"`
	Recoil   float64        `json:"recoil,omitempty"`
	Cure     bool           `json:"cure,omitempty"`
	Rest     bool           `json:"rest,omitempty"`
}

// Move is one battle move.
//
// Weather, if set, identifies the field condition this move spawns when used
// (Rain Dance → "rain", Sunny Day → "sun", Sandstorm → "sandstorm", Hail /
// Snowscape → "snow"). The engine applies it through the status-move path;
// see internal/engine/weather.go for the modifier table.
//
// Terrain, if set, identifies the terrain this move spawns ("electric",
// "grassy", "misty", "psychic"). Like Weather it lives for 5 turns and is
// dispatched through the status-move path; see internal/engine/terrain.go.
//
// SideCondition, if set, identifies a per-side condition this move spawns
// on the user's side ("reflect", "lightscreen", "auroraveil"). 5-turn
// duration; dispatched through the status-move path; see
// internal/engine/screens.go.
type Move struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Type          Type     `json:"type"`
	Category      Category `json:"category"`
	Power         int      `json:"power"`
	Accuracy      int      `json:"accuracy"`
	PP            int      `json:"pp"`
	Priority      int      `json:"priority"`
	Target        Target   `json:"target,omitempty"`
	Flags         []string `json:"flags,omitempty"`
	Weather       string   `json:"weather,omitempty"`
	Terrain       string   `json:"terrain,omitempty"`
	SideCondition string   `json:"side_condition,omitempty"`
	// PseudoWeather, if set, identifies a field-wide condition this
	// move spawns ("trickroom", "wonderroom", "magicroom", "gravity").
	// Distinct from Weather/Terrain — pseudo-weathers coexist with both
	// and with each other. 5-turn duration; dispatched through the
	// status-move path; see internal/engine/pseudoweather.go.
	PseudoWeather string `json:"pseudo_weather,omitempty"`
	// MinHits / MaxHits encode multi-strike moves. Both zero (the default
	// for single-strike moves) is equivalent to "hits once". When set,
	// MinHits ≤ MaxHits ≥ 2; the engine rolls a per-turn count between
	// them, with the canonical Gen-5+ distribution for the [2,5] range
	// (Bullet Seed, Pin Missile, ...). Fixed counts (Bonemerang [2],
	// Triple Axel [3]) set MinHits == MaxHits.
	MinHits int `json:"min_hits,omitempty"`
	MaxHits int `json:"max_hits,omitempty"`
	// OHKO marks this move as a one-hit KO move: on a hit it sets the
	// target's HP to zero outright (subject to Sturdy). Empty means not
	// OHKO. "any" means OHKO with no extra immunity (Fissure, Horn Drill,
	// Guillotine). A type slug ("ice") encodes an extra type immunity
	// stacked on top of the normal chart — Sheer Cold's ohko: "ice" means
	// Ice-type targets shrug it off even though Ice vs Ice is a normal
	// 0.5× matchup. Accuracy is the standard 30 (or 20 for Sheer Cold off
	// an Ice attacker, but our level-100 fixed engine elides the
	// level-difference accuracy formula entirely).
	OHKO string `json:"ohko,omitempty"`
	// ThawsTarget cures the target's Freeze status on a connecting hit.
	// Fire-type damaging moves already thaw by virtue of their type, so
	// this flag is only meaningful on non-Fire moves whose canonical
	// behavior includes thaw (Scald, Scorching Sands).
	ThawsTarget bool `json:"thaws_target,omitempty"`
	// IgnoreEvasion makes the accuracy roll treat the target's evasion
	// stage as min(0, eva) — positive boosts are ignored, drops still
	// help the attacker. Chip Away, Darkest Lariat, Sacred Sword.
	IgnoreEvasion bool `json:"ignore_evasion,omitempty"`
	// IgnoreDefensive treats the target's Def / Sp.Def stages as min(0,
	// stage) in the damage formula — positive defensive boosts are
	// erased, drops still amplify damage as usual. Same set of moves
	// as IgnoreEvasion in practice; canonical "chip through the buff".
	IgnoreDefensive bool `json:"ignore_defensive,omitempty"`
	// SelfSwitch marks a move that returns the user to its bench after it
	// resolves and brings in a teammate. Empty means no self-switch;
	// "normal" is the plain U-turn / Volt Switch / Flip Turn / Teleport
	// variant — stages and volatiles reset on the outgoing as usual.
	// "copyvolatile" is Baton Pass: the outgoing's stat stages and
	// confusion clock transfer to the incoming. The new active runs
	// OnSwitchIn hooks (Intimidate, weather setters) before any
	// subsequent mover this turn, matching canonical Showdown ordering.
	SelfSwitch  string   `json:"self_switch,omitempty"`
	Primary     *Effect  `json:"primary,omitempty"`
	Self        *Effect  `json:"self,omitempty"`
	Secondaries []Effect `json:"secondaries,omitempty"`
}

// IsMultihit reports whether this move strikes more than once per use.
func (m Move) IsMultihit() bool { return m.MaxHits >= 2 }

// HasFlag reports whether m carries the given flag.
func (m Move) HasFlag(flag string) bool {
	for _, f := range m.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// Dex is the fully loaded, validated dataset.
type Dex struct {
	Species   map[int]Species
	Moves     map[string]Move
	typeChart map[Type]map[Type]float64
	Version   string
}

// LoadDex reads pokedex.json, moves.json, and typechart.json from the
// directory dir on disk. It is a thin wrapper around LoadDexFS for
// callers that work with a filesystem path (services running in
// containers, tests).
func LoadDex(dir, version string) (*Dex, error) {
	return LoadDexFS(os.DirFS(dir), version)
}

// LoadDexFS reads the dataset from an fs.FS. The three required files
// (pokedex.json, moves.json, typechart.json) must be at the root of
// fsys. This signature lets callers embed the dataset with go:embed
// (cmd/pokearena-agent does this so the reference harness ships as a
// single self-contained binary).
func LoadDexFS(fsys fs.FS, version string) (*Dex, error) {
	d := &Dex{
		Species:   map[int]Species{},
		Moves:     map[string]Move{},
		typeChart: map[Type]map[Type]float64{},
		Version:   version,
	}

	var species []Species
	if err := readJSONFS(fsys, "pokedex.json", &species); err != nil {
		return nil, err
	}
	for _, s := range species {
		d.Species[s.DexNo] = s
	}

	var moves []Move
	if err := readJSONFS(fsys, "moves.json", &moves); err != nil {
		return nil, err
	}
	for _, m := range moves {
		d.Moves[m.ID] = m
	}

	if err := readJSONFS(fsys, "typechart.json", &d.typeChart); err != nil {
		return nil, err
	}

	if err := d.validate(); err != nil {
		return nil, err
	}
	return d, nil
}

// Multiplier returns the effectiveness of an attacking type against a single
// defending type. Unlisted pairs are neutral (1.0).
func (d *Dex) Multiplier(atk, def Type) float64 {
	if def == "" {
		return 1
	}
	if row, ok := d.typeChart[atk]; ok {
		if m, ok := row[def]; ok {
			return m
		}
	}
	return 1
}

// Effectiveness returns the combined multiplier against a (possibly dual-typed)
// defender — the product over each defending type.
func (d *Dex) Effectiveness(atk, def1, def2 Type) float64 {
	return d.Multiplier(atk, def1) * d.Multiplier(atk, def2)
}

// AllSpecies returns every species sorted by Pokédex number.
func (d *Dex) AllSpecies() []Species {
	out := make([]Species, 0, len(d.Species))
	for _, s := range d.Species {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DexNo < out[j].DexNo })
	return out
}

// AllMoves returns every move sorted by id.
func (d *Dex) AllMoves() []Move {
	out := make([]Move, 0, len(d.Moves))
	for _, m := range d.Moves {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Vocabularies are sourced from internal/specs — that package is the single
// source of truth shared with the engine and the data-sync filter. Static
// vocabularies (statuses, flags, boost stats) are inline in specs.go;
// dynamic ones (volatiles, side conditions, weather, terrain) are populated
// by the engine's init() functions. Binaries that call LoadDex must
// transitively import internal/engine (most do; cmd/data-validate and
// cmd/data-sync use blank imports for this reason).

func (d *Dex) validate() error {
	if len(d.typeChart) != 18 {
		return fmt.Errorf("type chart has %d types, expected 18", len(d.typeChart))
	}
	if len(d.Species) == 0 {
		return fmt.Errorf("pokedex is empty")
	}
	for _, sp := range d.Species {
		if len(sp.Moves) == 0 {
			return fmt.Errorf("species %s has no moves", sp.Name)
		}
		for _, mid := range sp.Moves {
			if _, ok := d.Moves[mid]; !ok {
				return fmt.Errorf("species %s references unknown move %q", sp.Name, mid)
			}
		}
		for _, ab := range sp.Abilities {
			if !isAbilitySlug(ab) {
				return fmt.Errorf("species %s has malformed ability slug %q (want kebab-case)", sp.Name, ab)
			}
		}
	}
	for _, m := range d.Moves {
		if err := validateMove(m); err != nil {
			return err
		}
	}
	return nil
}

// isAbilitySlug checks that s looks like an ability id our pipeline emits —
// non-empty, kebab-case, [a-z0-9-]+ with no leading/trailing hyphen and no
// double hyphens. We don't validate against an allowlist of known abilities
// because the engine's ability registry decides what's implemented (unknown
// = no-op); this guards against typos / stray whitespace from upstream.
func isAbilitySlug(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	prevHyphen := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			prevHyphen = false
		case r >= '0' && r <= '9':
			prevHyphen = false
		case r == '-':
			if prevHyphen {
				return false
			}
			prevHyphen = true
		default:
			return false
		}
	}
	return true
}

func validateMove(m Move) error {
	if m.Target != "" && m.Target != TargetFoe && m.Target != TargetSelf {
		return fmt.Errorf("move %s: invalid target %q", m.ID, m.Target)
	}
	if m.Category == CatStatus && m.Target == "" {
		return fmt.Errorf("move %s: status moves require an explicit target", m.ID)
	}
	if m.Category == CatStatus && m.Power > 0 {
		return fmt.Errorf("move %s: status moves must have power 0, got %d", m.ID, m.Power)
	}
	if m.Category == CatStatus && len(m.Secondaries) > 0 {
		return fmt.Errorf("move %s: status moves may not have secondaries", m.ID)
	}
	for _, f := range m.Flags {
		if !specs.Flags[f] {
			return fmt.Errorf("move %s: unknown flag %q", m.ID, f)
		}
	}
	if m.Weather != "" && !specs.Weathers[m.Weather] {
		return fmt.Errorf("move %s: unknown weather %q", m.ID, m.Weather)
	}
	if m.Terrain != "" && !specs.Terrains[m.Terrain] {
		return fmt.Errorf("move %s: unknown terrain %q", m.ID, m.Terrain)
	}
	if m.SideCondition != "" && !specs.SideConditions[m.SideCondition] {
		return fmt.Errorf("move %s: unknown side condition %q", m.ID, m.SideCondition)
	}
	if m.PseudoWeather != "" && !specs.PseudoWeathers[m.PseudoWeather] {
		return fmt.Errorf("move %s: unknown pseudo-weather %q", m.ID, m.PseudoWeather)
	}
	if err := validateEffect(m.ID, "primary", m.Primary, false); err != nil {
		return err
	}
	if err := validateEffect(m.ID, "self", m.Self, false); err != nil {
		return err
	}
	for i := range m.Secondaries {
		if err := validateEffect(m.ID, fmt.Sprintf("secondaries[%d]", i), &m.Secondaries[i], true); err != nil {
			return err
		}
	}
	return nil
}

func validateEffect(moveID, slot string, e *Effect, isSecondary bool) error {
	if e == nil {
		return nil
	}
	if isSecondary {
		if e.Chance < 1 || e.Chance > 100 {
			return fmt.Errorf("move %s: %s chance %d must be 1..100", moveID, slot, e.Chance)
		}
	}
	if e.Status != "" && !specs.Statuses[e.Status] {
		return fmt.Errorf("move %s: %s has unknown status %q", moveID, slot, e.Status)
	}
	if e.Volatile != "" && !specs.Volatiles[e.Volatile] {
		return fmt.Errorf("move %s: %s has unknown volatile %q", moveID, slot, e.Volatile)
	}
	for stat := range e.Boosts {
		if !specs.BoostStats[stat] {
			return fmt.Errorf("move %s: %s has unknown boost stat %q", moveID, slot, stat)
		}
	}
	return nil
}

// readJSONFS decodes a JSON file from fsys with strict decoding: unknown
// fields are rejected so typos in the dataset surface at boot rather than as
// silent misbehavior.
func readJSONFS(fsys fs.FS, name string, v any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	return nil
}
