package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shaumik/PokeArena/internal/engine"
)

// Trainer is one seat in a royale match: a named agent with a theme and the
// roster it brought.
//
// Name and Theme are private to their own pilot and to the referee. Codename
// is the seat's public alias — the only identity the opposing agent is ever
// shown, and the name the engine itself carries for the side, so no battle
// line can print the real one either. cmdNew resolves it (falling back to a
// neutral seat label) before writing meta.json, so it is never empty in a
// match created by this binary; publicName still defaults it for a meta
// written before the field existed.
type Trainer struct {
	Name     string            `json:"name"`
	Codename string            `json:"codename,omitempty"`
	Theme    string            `json:"theme"`
	Team     string            `json:"team"`
	Picks    []engine.TeamPick `json:"picks"`
}

// Meta is the immutable header of a match — who is playing, under what seed,
// and the judge token that gates the referee-only commands.
type Meta struct {
	ID         string     `json:"id"`
	Round      string     `json:"round"`
	Trainers   [2]Trainer `json:"trainers"`
	Seed       uint64     `json:"seed"`
	MaxTurns   int        `json:"max_turns"`
	JudgeToken string     `json:"judge_token"`
	Created    string     `json:"created"`
}

// Pending holds the actions submitted for the current decision point. A nil
// entry means that side has not chosen yet. The engine resolves only once
// every side that owes an action has filed one.
type Pending struct {
	Actions [2]*engine.Action `json:"actions"`
	Labels  [2]string         `json:"labels"`
}

// MonSnap is one Pokémon frozen at the end of a resolution — enough to render
// a replay without re-simulating.
type MonSnap struct {
	Name    string `json:"name"`
	HP      int    `json:"hp"`
	MaxHP   int    `json:"max_hp"`
	Status  string `json:"status,omitempty"`
	Fainted bool   `json:"fainted,omitempty"`
	Active  bool   `json:"active,omitempty"`
}

// SideSnap is one side's public position after a resolution.
type SideSnap struct {
	Trainer string    `json:"trainer"`
	Team    []MonSnap `json:"team"`
	Hazards string    `json:"hazards,omitempty"`
	Screens string    `json:"screens,omitempty"`
}

// Snapshot is the whole board after a resolution.
type Snapshot struct {
	Turn    int         `json:"turn"`
	Phase   string      `json:"phase"`
	Weather string      `json:"weather,omitempty"`
	Terrain string      `json:"terrain,omitempty"`
	Sides   [2]SideSnap `json:"sides"`
}

// Record is one resolved decision point: what both sides chose, what the
// engine said about it, and the board that came out the other side.
type Record struct {
	N       int              `json:"n"`
	Turn    int              `json:"turn"`
	Phase   string           `json:"phase"`
	Actions [2]string        `json:"actions"`
	Lines   []engine.LogLine `json:"lines"`
	After   Snapshot         `json:"after"`
	Winner  int              `json:"winner"`
	Verdict string           `json:"verdict,omitempty"`
}

func matchDir(root, id string) string { return filepath.Join(root, "battles", id) }

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// writeJSON writes atomically: a torn state.json would poison every later
// command, and two agents are writing this directory concurrently.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// lock is a cross-process mutex over one match directory, built on the
// atomicity of O_EXCL create. Both player agents and the judge run as
// separate processes against the same files, so every read-modify-write of
// state.json has to be serialized. A lock older than lockStale is broken:
// an agent killed mid-command must not wedge the match forever.
const lockStale = 90 * time.Second

func acquireLock(dir string) (func(), error) {
	path := filepath.Join(dir, "lock")
	deadline := time.Now().Add(120 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if st, serr := os.Stat(path); serr == nil && time.Since(st.ModTime()) > lockStale {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for the match lock (%s)", path)
		}
		time.Sleep(120 * time.Millisecond)
	}
}

// appendRecord adds one resolution to the match's log.
func appendRecord(dir string, r Record) error {
	f, err := os.OpenFile(filepath.Join(dir, "log.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func readRecords(dir string) ([]Record, error) {
	b, err := os.ReadFile(filepath.Join(dir, "log.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, line := range splitLines(b) {
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("corrupt log line: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
