package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// upstreamSpecies mirrors one entry of tools/data-sync/upstream/species.json.
// Abilities is shaped {"0": "...", "1": "...", "H": "..."} (1 and H optional)
// — the slot-0 ability is the default; the picker may select 1 or H.
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
	Abilities map[string]string `json:"abilities"`
	Prevo     string            `json:"prevo"`
	Evos      []string          `json:"evos"`
}

// upstreamMove mirrors one entry of tools/data-sync/upstream/moves.json. The
// shape preserves Showdown's quirks (e.g. accuracy as a number-or-true) so
// the transform layer is the one place that has to deal with them.
//
// The "modern statics" block at the bottom is what the static dump can
// carry that we currently mostly drop — these surface in the audit step
// (issue #30 step 2) for engine-flag / sub-ticket / denylist triage.
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

	// Modern statics — captured by refresh.js; transform.go triages them.
	BreaksProtect bool            `json:"breaksProtect"`
	ForceSwitch   bool            `json:"forceSwitch"`
	SelfSwitch    json.RawMessage `json:"selfSwitch"` // bool or string ("copyvolatile", "shedtail")
	SleepUsable   bool            `json:"sleepUsable"`
	Multihit      json.RawMessage `json:"multihit"` // number or [min, max]
	ThawsTarget   bool            `json:"thawsTarget"`
	OHKO          json.RawMessage `json:"ohko"` // bool or string ("Ice")
	WillCrit      bool            `json:"willCrit"`
	CritRatio     int             `json:"critRatio"` // 1 = normal, 2 = high-crit (Stone Edge, Slash, ...)
	// Stat the damage formula reads instead of the category default, in
	// Showdown stat ids: "def" on Body Press (offensive side) and on
	// Psystrike / Psyshock / Secret Sword (defensive side).
	OverrideOffensiveStat string          `json:"overrideOffensiveStat"`
	OverrideDefensiveStat string          `json:"overrideDefensiveStat"`
	IgnoreAbility         bool            `json:"ignoreAbility"`
	IgnoreDefensive       bool            `json:"ignoreDefensive"`
	IgnoreEvasion         bool            `json:"ignoreEvasion"`
	IgnoreImmunity        json.RawMessage `json:"ignoreImmunity"` // bool or object of type→bool
	NoPPBoosts            bool            `json:"noPPBoosts"`
	Weather               string          `json:"weather"`
	Terrain               string          `json:"terrain"`
	PseudoWeather         string          `json:"pseudoWeather"`
	SideCondition         string          `json:"sideCondition"`
	SlotCondition         string          `json:"slotCondition"`
	StallingMove          bool            `json:"stallingMove"`
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

// upstreamItem mirrors one entry of tools/data-sync/upstream/items.json — the
// full standard item catalog dumped by refresh.js. The Go transform filters it
// down to the curated set via the allowlist in transform.go.
type upstreamItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// upstreamNature mirrors one entry of tools/data-sync/upstream/natures.json.
// Plus/Minus are Showdown stat ids (atk/def/spa/spd/spe), remapped to our
// slugs by the transform; empty on the five neutral natures.
type upstreamNature struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Plus  string `json:"plus"`
	Minus string `json:"minus"`
}

// upstreamMeta mirrors _meta.json. NaturesCount is absent from snapshots
// taken before natures were dumped; it is informational only, so a zero
// there is not an error.
type upstreamMeta struct {
	Gen          int    `json:"gen"`
	SimVersion   string `json:"sim_version"`
	RefreshedAt  string `json:"refreshed_at"`
	SpeciesCount int    `json:"species_count"`
	MovesCount   int    `json:"moves_count"`
	ItemsCount   int    `json:"items_count"`
	NaturesCount int    `json:"natures_count"`
}

// upstream is the fully-loaded snapshot ready for the rest of the pipeline.
// Learnsets maps species ID → ordered list of Showdown move IDs (the full
// Gen-1 movepool for that species). See refresh.js:dumpLearnsets for shape.
type upstream struct {
	Species   []upstreamSpecies
	Moves     map[string]upstreamMove
	Items     map[string]upstreamItem
	Natures   []upstreamNature
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

	var items []upstreamItem
	if err := readJSON(filepath.Join(dir, "items.json"), &items); err != nil {
		return nil, err
	}
	u.Items = make(map[string]upstreamItem, len(items))
	for _, it := range items {
		u.Items[it.ID] = it
	}

	if err := readJSON(filepath.Join(dir, "natures.json"), &u.Natures); err != nil {
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
