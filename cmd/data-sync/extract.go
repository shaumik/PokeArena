package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// upstreamSpecies mirrors one entry of tools/data-sync/upstream/species.json.
type upstreamSpecies struct {
	Num       int      `json:"num"`
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Types     []string `json:"types"`
	BaseStats struct {
		HP  int `json:"hp"`
		Atk int `json:"atk"`
		Def int `json:"def"`
		Spa int `json:"spa"`
		Spd int `json:"spd"`
		Spe int `json:"spe"`
	} `json:"baseStats"`
	Prevo string   `json:"prevo"`
	Evos  []string `json:"evos"`
}

// upstreamMove mirrors one entry of tools/data-sync/upstream/moves.json. The
// shape preserves Showdown's quirks (e.g. accuracy as a number-or-true) so
// the transform layer is the one place that has to deal with them.
type upstreamMove struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	Category       string          `json:"category"` // Physical | Special | Status
	BasePower      int             `json:"basePower"`
	Accuracy       json.RawMessage `json:"accuracy"` // number, or `true` for bypass-acc
	PP             int             `json:"pp"`
	Priority       int             `json:"priority"`
	Target         string          `json:"target"`
	Flags          map[string]int  `json:"flags"`
	Secondary      *secondaryRaw   `json:"secondary"`
	Secondaries    []secondaryRaw  `json:"secondaries"`
	Self           *selfRaw        `json:"self"`
	Boosts         map[string]int  `json:"boosts"`
	Status         string          `json:"status"`
	VolatileStatus string          `json:"volatileStatus"`
	Recoil         []int           `json:"recoil"` // [num, denom] e.g. [33, 100]
	Drain          []int           `json:"drain"`
	Heal           []int           `json:"heal"`
}

type secondaryRaw struct {
	Chance         int            `json:"chance"`
	Status         string         `json:"status"`
	VolatileStatus string         `json:"volatileStatus"`
	Boosts         map[string]int `json:"boosts"`
	Self           *selfRaw       `json:"self"`
}

type selfRaw struct {
	Boosts         map[string]int `json:"boosts"`
	VolatileStatus string         `json:"volatileStatus"`
}

// upstreamMeta mirrors _meta.json.
type upstreamMeta struct {
	Gen          int    `json:"gen"`
	SimVersion   string `json:"sim_version"`
	RefreshedAt  string `json:"refreshed_at"`
	SpeciesCount int    `json:"species_count"`
	MovesCount   int    `json:"moves_count"`
}

// upstream is the fully-loaded snapshot ready for the rest of the pipeline.
// Learnsets maps species ID → ordered list of Showdown move IDs (the full
// Gen-1 movepool for that species). See refresh.js:dumpLearnsets for shape.
type upstream struct {
	Species   []upstreamSpecies
	Moves     map[string]upstreamMove
	Typechart map[string]map[string]float64
	Learnsets map[string][]string
	Meta      upstreamMeta
}

func loadUpstream(dir string) (*upstream, error) {
	u := &upstream{}

	var species []upstreamSpecies
	if err := readJSON(filepath.Join(dir, "species.json"), &species); err != nil {
		return nil, err
	}
	u.Species = species

	var moves []upstreamMove
	if err := readJSON(filepath.Join(dir, "moves.json"), &moves); err != nil {
		return nil, err
	}
	u.Moves = make(map[string]upstreamMove, len(moves))
	for _, m := range moves {
		u.Moves[m.ID] = m
	}

	if err := readJSON(filepath.Join(dir, "typechart.json"), &u.Typechart); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "learnsets.json"), &u.Learnsets); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "_meta.json"), &u.Meta); err != nil {
		return nil, err
	}

	return u, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
