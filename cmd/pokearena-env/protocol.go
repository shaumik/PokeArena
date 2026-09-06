package main

import (
	"encoding/json"
	"fmt"

	"github.com/shaumik/PokeArena/internal/engine"
)

// ProtocolVersion is the stdio contract's version, reported by `handshake`.
// It is a semver-ish "major.minor": a minor bump only adds optional fields, a
// major bump may remove or repurpose one. A client that pins the major and
// tolerates unknown fields keeps working across minor bumps.
//
// The contract itself is documented in docs/python-env.md; that file and this
// constant move together.
const ProtocolVersion = "1.0"

// Request is one line of stdin: a command plus its arguments.
//
// ID is echoed back verbatim on the response so a client that pipelines
// requests can match them up. It is opaque to the server — a number, a string,
// or absent — and is carried as a raw JSON value for exactly that reason.
type Request struct {
	ID   json.RawMessage `json:"id,omitempty"`
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Response is one line of stdout. Exactly one of Result or Error is set.
type Response struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Cmd    string          `json:"cmd,omitempty"`
	OK     bool            `json:"ok"`
	Result any             `json:"result,omitempty"`
	Error  *ErrorObject    `json:"error,omitempty"`
}

// ErrorObject is the machine-readable failure. Every failure path in this
// binary — a malformed line, an unknown command, an illegal action, even a
// panic inside the engine — surfaces as one of these. The process never exits
// nonzero on a request-level failure and never lets a panic escape, because a
// wrapper that has to parse a stack trace off stderr is not a protocol.
type ErrorObject struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Details carries structured context where there is any — the legal action
	// set for an illegal_action rejection, for instance.
	Details any `json:"details,omitempty"`
}

// Error codes. These are the stable half of the error contract; Message is
// human-facing and may be reworded.
const (
	ErrBadRequest     = "bad_request"     // unparseable line, or args that don't fit the command
	ErrUnknownCommand = "unknown_command" // no such cmd
	ErrNoEpisode      = "no_episode"      // step/legal_actions/observe before reset
	ErrEpisodeOver    = "episode_over"    // step after terminated/truncated
	ErrIllegalAction  = "illegal_action"  // the submitted action is not in the legal set
	ErrInternal       = "internal"        // a panic or an engine-level failure
)

func errorf(code, format string, a ...any) *ErrorObject {
	return &ErrorObject{Code: code, Message: fmt.Sprintf(format, a...)}
}

// --- Action encoding ------------------------------------------------------
//
// Two encodings are accepted everywhere an action is taken, and both are
// emitted by `legal_actions`:
//
//   - the engine's own object form, {"kind":"move","index":0}, which is the
//     only one that can name a self-switch pivot target; and
//   - a flat integer index into a fixed-size discrete space, which is what a
//     Gymnasium `Discrete` action space needs.
//
// The flat space is deliberately fixed-size (not "however many actions are
// legal right now") so the action space is constant across the episode, which
// is what every RL library assumes. Illegal entries are masked, not renumbered
// — renumbering would make the same integer mean different things on different
// turns and quietly destroy any learned policy.

const (
	// flatStruggle is the index of the Struggle / forced-move sentinel
	// (engine.StruggleMoveIndex == -1), which the engine offers when a
	// Pokémon has no usable move or is spending a recharge turn.
	flatStruggle = engine.MovesMax
	// flatSwitchBase is the first index of the switch block: flatSwitchBase+j
	// switches to team slot j.
	flatSwitchBase = engine.MovesMax + 1
	// FlatActionCount is the size of the discrete action space:
	// 4 move slots + Struggle + 6 team slots = 11.
	FlatActionCount = engine.MovesMax + 1 + engine.TeamSize
)

// encodeFlat maps an engine action onto its fixed discrete index, or -1 if the
// action falls outside the space (which cannot happen for an action the engine
// itself enumerated).
func encodeFlat(a engine.Action) int {
	switch a.Kind {
	case engine.ActionMove:
		if a.Index == engine.StruggleMoveIndex {
			return flatStruggle
		}
		if a.Index >= 0 && a.Index < engine.MovesMax {
			return a.Index
		}
	case engine.ActionSwitch:
		if a.Index >= 0 && a.Index < engine.TeamSize {
			return flatSwitchBase + a.Index
		}
	}
	return -1
}

// decodeFlat maps a discrete index back to an engine action. SwitchTarget is
// left nil — the flat space has no room for a pivot target, so a self-switch
// move chosen this way lets the engine pick the incoming teammate (its
// documented default: the lowest-indexed live one). Name a target with the
// object form if you need to aim a U-turn.
func decodeFlat(i int) (engine.Action, error) {
	switch {
	case i >= 0 && i < engine.MovesMax:
		return engine.Action{Kind: engine.ActionMove, Index: i}, nil
	case i == flatStruggle:
		return engine.Action{Kind: engine.ActionMove, Index: engine.StruggleMoveIndex}, nil
	case i >= flatSwitchBase && i < FlatActionCount:
		return engine.Action{Kind: engine.ActionSwitch, Index: i - flatSwitchBase}, nil
	}
	return engine.Action{}, fmt.Errorf("action index %d out of range [0,%d)", i, FlatActionCount)
}

// ActionInput is an action as it arrives from a client: either a bare integer
// (flat index) or the engine's object form. Anything else is a bad_request.
type ActionInput struct {
	engine.Action
}

// UnmarshalJSON accepts both encodings so a client can use whichever fits.
func (a *ActionInput) UnmarshalJSON(b []byte) error {
	// Integer form first: it is unambiguous (an object never parses as a
	// number) and it is the common case for RL callers.
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		act, err := decodeFlat(n)
		if err != nil {
			return err
		}
		a.Action = act
		return nil
	}
	var obj engine.Action
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("action must be an integer in [0,%d) or an object {\"kind\":\"move\"|\"switch\",\"index\":n}: %w", FlatActionCount, err)
	}
	if obj.Kind != engine.ActionMove && obj.Kind != engine.ActionSwitch {
		return fmt.Errorf("action kind must be %q or %q, got %q", engine.ActionMove, engine.ActionSwitch, obj.Kind)
	}
	a.Action = obj
	return nil
}

// LegalAction is one entry of the legal-action set. It carries every encoding
// at once so no client has to convert: the flat index for a Discrete space,
// the engine object for a faithful replay, and a human/LLM-readable label.
type LegalAction struct {
	Index  int           `json:"index"` // flat discrete index; -1 if unrepresentable
	Action engine.Action `json:"action"`
	Label  string        `json:"label"`
}
