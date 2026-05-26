// Package domain holds the static Pokémon reference data — species, moves,
// and the type chart — loaded once from the curated JSON dataset. It has no
// battle logic and no I/O beyond reading the dataset files.
package domain

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"sort"
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
type Species struct {
	DexNo int      `json:"dex_no"`
	Name  string   `json:"name"`
	Type1 Type     `json:"type1"`
	Type2 Type     `json:"type2"`
	Base  Stats    `json:"base"`
	Moves []string `json:"moves"`
}

// Effect is the optional rider on a move: a status, a stat-stage change,
// HP drain, recoil, healing, or a raised critical-hit ratio.
type Effect struct {
	Kind   string  `json:"kind"`             // status | stat | drain | recoil | heal | crit
	Status string  `json:"status,omitempty"` // burn | poison | paralysis | sleep | freeze
	Chance int     `json:"chance,omitempty"` // percent; 100 for guaranteed
	Stat   string  `json:"stat,omitempty"`   // attack | defense | spatk | spdef | speed
	Stages int     `json:"stages,omitempty"` // signed stage delta
	Target string  `json:"target,omitempty"` // self | foe
	Ratio  float64 `json:"ratio,omitempty"`  // fraction, for drain/recoil/heal
}

// Move is one battle move.
type Move struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     Type     `json:"type"`
	Category Category `json:"category"`
	Power    int      `json:"power"`
	Accuracy int      `json:"accuracy"`
	PP       int      `json:"pp"`
	Priority int      `json:"priority"`
	Effect   *Effect  `json:"effect,omitempty"`
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
	}
	return nil
}

func readJSONFS(fsys fs.FS, name string, v any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	return nil
}
