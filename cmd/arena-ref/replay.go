package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// The replay command re-runs a finished battle from its seed and the two
// trainers' submitted actions, emitting one structured record per resolved
// step. It exists so a write-up of a match is generated from the engine rather
// than transcribed from scrollback — the HP figures, hazard layers and log
// lines in a report are then the engine's, not a human's memory of them.
//
// Determinism is what makes this sound: the same seed and the same action list
// reproduce the battle exactly, so the export can be regenerated and diffed.

// stepInput is one submitted pair of actions. During a replace phase only the
// side that must replace supplies a value; the other is empty.
type stepInput struct {
	A1 string `json:"a1"`
	A2 string `json:"a2"`
}

// monRecord is one Pokémon's public-facing state at the end of a step.
type monRecord struct {
	Slot     int    `json:"slot"`
	Name     string `json:"name"`
	Types    string `json:"types"`
	HP       int    `json:"hp"`
	MaxHP    int    `json:"max_hp"`
	Status   string `json:"status,omitempty"`
	Fainted  bool   `json:"fainted,omitempty"`
	Active   bool   `json:"active,omitempty"`
	Item     string `json:"item,omitempty"`
	Ability  string `json:"ability,omitempty"`
	Stages   string `json:"stages,omitempty"`
	Volatile string `json:"volatiles,omitempty"`
}

type sideRecord struct {
	Trainer     string      `json:"trainer"`
	Team        []monRecord `json:"team"`
	StealthRock bool        `json:"stealth_rock,omitempty"`
	Spikes      int         `json:"spikes,omitempty"`
	ToxicSpikes int         `json:"toxic_spikes,omitempty"`
}

type stepRecord struct {
	Step    int           `json:"step"`
	Turn    int           `json:"turn"`
	Phase   string        `json:"phase"`
	Replace bool          `json:"replace,omitempty"`
	A1      string        `json:"a1,omitempty"`
	A2      string        `json:"a2,omitempty"`
	Log     []logRecord   `json:"log"`
	Sides   [2]sideRecord `json:"sides"`
	Winner  int           `json:"winner"`
}

type logRecord struct {
	Side int    `json:"side"`
	Text string `json:"text"`
	Type string `json:"type"`
}

func cmdReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	p1 := fs.String("p1", "", "side 0 team JSON")
	p2 := fs.String("p2", "", "side 1 team JSON")
	n1 := fs.String("n1", "P1", "side 0 trainer name")
	n2 := fs.String("n2", "P2", "side 1 trainer name")
	seed := fs.Uint64("seed", 1, "RNG seed — must match the original battle")
	actions := fs.String("actions", "", "JSON array of {\"a1\":...,\"a2\":...} steps")
	out := fs.String("out", "", "output JSON file (default stdout)")
	dataDir := fs.String("data", "data", "dataset directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dex, err := loadDex(*dataDir)
	if err != nil {
		return err
	}
	picks1, err := loadPicks(*p1)
	if err != nil {
		return err
	}
	picks2, err := loadPicks(*p2)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*actions)
	if err != nil {
		return fmt.Errorf("read actions: %w", err)
	}
	var steps []stepInput
	if err := json.Unmarshal(raw, &steps); err != nil {
		return fmt.Errorf("decode actions: %w", err)
	}

	s, err := engine.NewBattleFromPicks(dex, "replay", *n1, picks1, *n2, picks2, *seed)
	if err != nil {
		return err
	}

	records := []stepRecord{{
		Step:   0,
		Turn:   0,
		Phase:  string(s.Phase),
		Log:    []logRecord{{Side: -1, Text: "Battle start.", Type: "start"}},
		Sides:  snapshotSides(dex, s),
		Winner: s.Winner,
	}}

	for i, in := range steps {
		if s.Ended() {
			return fmt.Errorf("step %d: battle already ended", i+1)
		}
		replace := s.Phase == engine.PhaseReplace
		var acts [2]engine.Action
		var swp [2]*engine.Action
		for side, rawAct := range []string{in.A1, in.A2} {
			if replace && !s.Replace[side] {
				continue
			}
			if rawAct == "" {
				return fmt.Errorf("step %d: side %d has no action", i+1, side)
			}
			a, err := parseAction(rawAct)
			if err != nil {
				return fmt.Errorf("step %d side %d: %w", i+1, side, err)
			}
			if err := checkLegal(dex, s, side, a); err != nil {
				return fmt.Errorf("step %d side %d: %w", i+1, side, err)
			}
			acts[side] = a
			aa := a
			swp[side] = &aa
		}

		var log []engine.LogLine
		if replace {
			log = engine.ResolveReplace(s, swp)
		} else {
			log = engine.ResolveTurn(dex, s, acts)
		}
		if err := engine.ValidateStateInvariants(s); err != nil {
			return fmt.Errorf("step %d: INVARIANT VIOLATION: %w", i+1, err)
		}

		records = append(records, stepRecord{
			Step:    i + 1,
			Turn:    s.Turn,
			Phase:   string(s.Phase),
			Replace: replace,
			A1:      in.A1,
			A2:      in.A2,
			Log:     toLogRecords(log),
			Sides:   snapshotSides(dex, s),
			Winner:  s.Winner,
		})
	}

	blob, err := json.MarshalIndent(records, "", " ")
	if err != nil {
		return err
	}
	if *out == "" {
		fmt.Println(string(blob))
		return nil
	}
	if err := os.WriteFile(*out, blob, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ replayed %d steps → %s (winner: %d)\n", len(steps), *out, s.Winner)
	return nil
}

func toLogRecords(log []engine.LogLine) []logRecord {
	out := make([]logRecord, len(log))
	for i, l := range log {
		out[i] = logRecord{Side: l.Side, Text: l.Text, Type: l.Type}
	}
	return out
}

func snapshotSides(_ *domain.Dex, s *engine.BattleState) [2]sideRecord {
	var out [2]sideRecord
	for i := range s.Sides {
		sd := &s.Sides[i]
		rec := sideRecord{
			Trainer:     sd.Trainer,
			StealthRock: sd.Conditions.Hazards.StealthRock,
			Spikes:      sd.Conditions.Hazards.Spikes,
			ToxicSpikes: sd.Conditions.Hazards.ToxicSpikes,
		}
		for j := range sd.Team {
			p := &sd.Team[j]
			rec.Team = append(rec.Team, monRecord{
				Slot:     j,
				Name:     p.Name,
				Types:    typeLine(p),
				HP:       p.HP,
				MaxHP:    p.MaxHP,
				Status:   string(p.Status),
				Fainted:  p.Fainted,
				Active:   j == sd.Active,
				Item:     string(p.Item),
				Ability:  string(p.Ability),
				Stages:   stageLine(p.Stages),
				Volatile: volatileLine(p),
			})
		}
		out[i] = rec
	}
	return out
}
