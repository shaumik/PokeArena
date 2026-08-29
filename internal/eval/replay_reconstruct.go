package eval

import (
	"encoding/json"
	"fmt"

	"github.com/shaumik/PokeArena/internal/engine"
)

// StoredTurn is one persisted turn of a live battle: the engine state after the
// turn resolved and the turn's log lines, each as raw JSON exactly as the live
// coordinator wrote them (json.Marshal of *engine.BattleState and
// []engine.LogLine).
type StoredTurn struct {
	State json.RawMessage
	Log   json.RawMessage
}

// ReplayFromStored reconstructs a watchable Replay from a battle's persisted
// turns — no re-simulation needed. Because the stored state digest IS a
// marshaled engine.BattleState, each turn unmarshals straight back and projects
// through the same frameFromState the live-capture path uses, so a reconstructed
// replay is identical in shape to a simulated one. This is how a live model's
// game against the reference — played over the gateway, never re-simulable from
// a seed — still becomes a board-and-log replay in the report.
//
// side0/side1 name the two trainers for the report (the persisted trainer names
// are the generic seat labels "Agent"/"AI"); winner is the winning side's name.
func ReplayFromStored(seed uint64, side0, side1, winner string, turns []StoredTurn) (Replay, error) {
	rep := Replay{Seed: seed, Side0: side0, Side1: side1, Winner: winner}
	for i, t := range turns {
		var st engine.BattleState
		if err := json.Unmarshal(t.State, &st); err != nil {
			return Replay{}, fmt.Errorf("turn %d: unmarshal state: %w", i+1, err)
		}
		var logs []engine.LogLine
		if len(t.Log) > 0 {
			_ = json.Unmarshal(t.Log, &logs)
		}
		rep.Frames = append(rep.Frames, frameFromState(&st, logs, "turn", [2]string{}))
	}
	if n := len(rep.Frames); n > 0 {
		rep.Turns = rep.Frames[n-1].Turn
	}
	return rep, nil
}
