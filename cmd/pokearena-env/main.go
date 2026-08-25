// Command pokearena-env exposes the PokéArena battle engine as a
// line-oriented JSON environment over stdin/stdout — one JSON request object
// per line in, one JSON response object per line out.
//
// It exists so the engine is reachable from outside Go without a server. The
// engine itself is a pure function `(state, actionP1, actionP2) -> (state,
// events)`; this binary is the thinnest honest wrapper around that: no network
// listener, no database, no data directory (the dataset is embedded), no
// background goroutines. A client — the `pokearena` Python package is the
// reference one — spawns it as a subprocess, writes a line, reads a line.
//
// Three properties are the product, and each is enforced here rather than
// assumed:
//
//   - Determinism. The same seed, teams and controllers produce a
//     byte-identical battle, including every observation's state hash. Seeding
//     goes through exactly the path cmd/bench uses, so a number measured here
//     is comparable to a number on the published board.
//   - Fog of war. A side's observation is `ai.View` marshaled — the same single
//     redaction path the MCP server and the PvP WebSocket serialize through.
//     The opponent's bench is not in the bytes, and neither is the active foe's
//     exact HP, ability, item, stats, spread or move PP.
//   - No panics, no bare exits. Every failure — a malformed line, an unknown
//     command, an illegal action, a panic inside the engine — comes back as a
//     JSON error object on stdout.
//
// Usage:
//
//	pokearena-env                       # embedded dataset, embedded team library
//	pokearena-env -data data            # read the dataset from disk instead
//	pokearena-env -teams my-teams.json  # swap the team library
//
// The wire contract is documented in docs/python-env.md.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/eval"
)

// maxLineBytes bounds one request line. A reset carrying two fully specified
// 6-Pokémon rosters is a few kilobytes; 8 MiB is far past any legitimate
// request and keeps a runaway producer from exhausting memory.
const maxLineBytes = 8 << 20

func main() {
	var (
		dataDir    = flag.String("data", "", "dataset directory (default: the dataset embedded in this binary)")
		teamsPath  = flag.String("teams", "", "competitive team library JSON (default: the library embedded in this binary)")
		dataVer    = flag.String("data-version", "embedded", "label recorded as the dataset version")
		depth      = flag.Int("depth", 2, "default fixed search depth for the expectimax baseline")
		printProto = flag.Bool("protocol-version", false, "print the stdio protocol version and exit")
	)
	flag.Parse()

	if *printProto {
		fmt.Println(ProtocolVersion)
		return
	}

	srv, err := newServer(*dataDir, *teamsPath, *dataVer, *depth)
	if err != nil {
		// A startup failure is the one thing that cannot be reported as a
		// response object — there is no request to answer yet. Say so on
		// stderr, in a form a wrapper can surface verbatim, and exit nonzero.
		fmt.Fprintf(os.Stderr, "pokearena-env: %v\n", err)
		os.Exit(1)
	}
	if err := srv.serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "pokearena-env: %v\n", err)
		os.Exit(1)
	}
}

// server holds the per-process, read-only world (dataset + team library) and
// the single live episode. One process is one environment instance; a client
// that wants N parallel environments runs N processes, which is what keeps
// every episode's RNG stream trivially isolated from every other.
type server struct {
	dex     *domain.Dex
	lib     *eval.TeamLibrary
	prov    eval.Provenance
	dataVer string
	depth   int

	ep *episode
}

func newServer(dataDir, teamsPath, dataVer string, depth int) (*server, error) {
	dex, err := loadDex(dataDir, dataVer)
	if err != nil {
		return nil, fmt.Errorf("load dataset: %w", err)
	}
	lib, err := loadTeamLibrary(teamsPath, dex)
	if err != nil {
		return nil, fmt.Errorf("load team library: %w", err)
	}
	prov, err := loadProvenance(dataDir)
	if err != nil {
		return nil, fmt.Errorf("load provenance: %w", err)
	}
	if depth < 1 {
		depth = 1
	}
	return &server{dex: dex, lib: lib, prov: prov, dataVer: dataVer, depth: depth}, nil
}

// serve runs the request loop until stdin closes or a `close` command arrives.
// Reaching either is a clean exit, not an error: a wrapper that closes the pipe
// is the normal way this process ends.
func (s *server) serve(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)

	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false) // species and move names are plain text; keep them readable

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue // blank lines are padding, not requests
		}
		resp, stop := s.handleLine([]byte(line))
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
		if f, ok := out.(interface{ Sync() error }); ok {
			// os.Stdout is unbuffered, but a caller may hand us something that
			// isn't; flush so a client blocked on a read never deadlocks.
			_ = f.Sync()
		}
		if stop {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("request line exceeded %d bytes", maxLineBytes)
		}
		return fmt.Errorf("read stdin: %w", err)
	}
	return nil
}

// handleLine parses and dispatches one request, converting any panic into an
// internal error response. It returns the response plus whether the loop should
// stop.
func (s *server) handleLine(line []byte) (resp Response, stop bool) {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Response{OK: false, Error: errorf(ErrBadRequest, "malformed request line: %v", err)}, false
	}

	defer func() {
		// A panic inside the engine is a bug, but it must not take the process
		// down: the client would see a closed pipe and no explanation. Turn it
		// into an error response naming the command that caused it.
		if r := recover(); r != nil {
			resp = Response{ID: req.ID, Cmd: req.Cmd, OK: false,
				Error: errorf(ErrInternal, "panic handling %q: %v", req.Cmd, r)}
			stop = false
			// The episode is no longer trustworthy after a panic mid-resolve;
			// drop it so the next reset starts clean rather than compounding.
			s.ep = nil
		}
	}()

	result, stopNow, errObj := s.dispatch(req)
	if errObj != nil {
		return Response{ID: req.ID, Cmd: req.Cmd, OK: false, Error: errObj}, false
	}
	return Response{ID: req.ID, Cmd: req.Cmd, OK: true, Result: result}, stopNow
}

func (s *server) dispatch(req Request) (result any, stop bool, errObj *ErrorObject) {
	switch req.Cmd {
	case "handshake", "info":
		return s.handshake(), false, nil
	case "reset":
		r, e := s.reset(req.Args)
		return r, false, e
	case "step":
		r, e := s.step(req.Args)
		return r, false, e
	case "legal_actions":
		r, e := s.legalActions(req.Args)
		return r, false, e
	case "observe":
		r, e := s.observe(req.Args)
		return r, false, e
	case "close":
		s.ep = nil
		return map[string]any{"closed": true}, true, nil
	case "":
		return nil, false, errorf(ErrBadRequest, `request has no "cmd"`)
	default:
		return nil, false, errorf(ErrUnknownCommand, "unknown command %q (known: handshake, reset, step, legal_actions, observe, close)", req.Cmd)
	}
}

// --- commands -------------------------------------------------------------

// Handshake is the provenance record: everything a third party needs to say
// which engine, which dataset and which rules produced a trajectory. The
// benchmark makes the same promise for its runs (docs/benchmark.md §8) and this
// carries it through to the Python side, so an RL result can name its substrate
// as precisely as a benchmark result can.
type Handshake struct {
	ProtocolVersion string   `json:"protocol_version"`
	EngineRevision  string   `json:"engine_revision"`
	Level           int      `json:"level"`
	Ruleset         string   `json:"ruleset"`
	Dataset         Dataset  `json:"dataset"`
	TeamLibrary     Library  `json:"team_library"`
	ActionSpace     ActSpace `json:"action_space"`
	Agents          []string `json:"agents"`
	RewardModes     []string `json:"reward_modes"`
	Commands        []string `json:"commands"`
	MaxTurns        int      `json:"max_turns"`
}

// Dataset names the exact data the engine is running on.
type Dataset struct {
	Version     string `json:"version"`
	SimVersion  string `json:"sim_version"`
	CurationSHA string `json:"curation_sha"`
	SourceGen   int    `json:"source_gen"`
	SyncedAt    string `json:"synced_at,omitempty"`
	Species     int    `json:"species"`
	Moves       int    `json:"moves"`
	Items       int    `json:"items"`
}

// Library names the team library and what is in it.
type Library struct {
	Version string   `json:"version"`
	Teams   []string `json:"teams"`
	Profile string   `json:"profile"`
}

// ActSpace describes the fixed discrete action space so a client can build its
// spaces without hardcoding the layout.
type ActSpace struct {
	N          int `json:"n"`
	MoveSlots  int `json:"move_slots"`
	Struggle   int `json:"struggle_index"`
	SwitchBase int `json:"switch_base"`
	TeamSize   int `json:"team_size"`
}

func (s *server) handshake() Handshake {
	return Handshake{
		ProtocolVersion: ProtocolVersion,
		EngineRevision:  eval.EngineRevision(),
		Level:           engine.Level,
		Ruleset:         eval.Ruleset(),
		Dataset: Dataset{
			Version:     s.dataVer,
			SimVersion:  s.prov.SimVersion,
			CurationSHA: s.prov.CurationSHA,
			SourceGen:   s.prov.SourceGen,
			SyncedAt:    s.prov.SyncedAt,
			Species:     len(s.dex.Species),
			Moves:       len(s.dex.Moves),
			Items:       len(s.dex.Items),
		},
		TeamLibrary: Library{
			Version: s.lib.Version,
			Teams:   teamNames(s.lib),
			Profile: eval.TeamProfile(s.lib.Teams),
		},
		ActionSpace: ActSpace{
			N:          FlatActionCount,
			MoveSlots:  engine.MovesMax,
			Struggle:   flatStruggle,
			SwitchBase: flatSwitchBase,
			TeamSize:   engine.TeamSize,
		},
		Agents:      baselineNames(),
		RewardModes: []string{rewardWinLoss, rewardHPDelta},
		Commands:    []string{"handshake", "reset", "step", "legal_actions", "observe", "close"},
		MaxTurns:    300,
	}
}

func (s *server) reset(raw json.RawMessage) (any, *ErrorObject) {
	var a resetArgs
	if e := decodeArgs(raw, &a); e != nil {
		return nil, e
	}
	ep, errObj := newEpisode(s.dex, s.lib, s.depth, a)
	if errObj != nil {
		return nil, errObj
	}
	s.ep = ep
	if e := ep.start(); e != nil {
		s.ep = nil
		return nil, e
	}
	return ep.result()
}

func (s *server) step(raw json.RawMessage) (any, *ErrorObject) {
	if s.ep == nil {
		return nil, errorf(ErrNoEpisode, "no episode: call reset first")
	}
	if s.ep.done() {
		// Checked before the arguments are even parsed: a finished episode has
		// no decision point, so "which sides must act" has no answer and a
		// shape complaint about the action would be the wrong diagnosis.
		return nil, errorf(ErrEpisodeOver, "episode already finished (terminated=%v truncated=%v); call reset",
			s.ep.state.Ended(), s.ep.truncated)
	}
	var a stepArgs
	if e := decodeArgs(raw, &a); e != nil {
		return nil, e
	}
	supplied, e := s.ep.collectActions(a)
	if e != nil {
		return nil, e
	}
	if e := s.ep.step(supplied); e != nil {
		return nil, e
	}
	return s.ep.result()
}

// collectActions normalizes the two accepted shapes of a step request into a
// side→action map.
func (ep *episode) collectActions(a stepArgs) (map[int]engine.Action, *ErrorObject) {
	out := map[int]engine.Action{}
	if a.Action != nil && a.Actions != nil {
		return nil, errorf(ErrBadRequest, `set either "action" or "actions", not both`)
	}
	switch {
	case a.Action != nil:
		need := ep.externalToMove()
		if len(need) != 1 {
			return nil, errorf(ErrBadRequest,
				`"action" is shorthand for the single-side case, but %d external sides must move (%v); use "actions"`,
				len(need), need)
		}
		out[need[0]] = a.Action.Action
	case a.Actions != nil:
		if len(a.Actions) != 2 {
			return nil, errorf(ErrBadRequest, `"actions" must be a 2-element array indexed by side (use null for a side that is not acting), got %d`, len(a.Actions))
		}
		for side, in := range a.Actions {
			if in != nil {
				out[side] = in.Action
			}
		}
	}
	return out, nil
}

func (s *server) legalActions(raw json.RawMessage) (any, *ErrorObject) {
	if s.ep == nil {
		return nil, errorf(ErrNoEpisode, "no episode: call reset first")
	}
	side, e := s.resolveSide(raw)
	if e != nil {
		return nil, e
	}
	if s.ep.done() {
		return nil, errorf(ErrEpisodeOver, "episode finished; there are no legal actions")
	}
	legal := s.ep.legalFor(side)
	return map[string]any{
		"side":          side,
		"turn":          s.ep.state.Turn,
		"phase":         s.ep.state.Phase,
		"to_move":       s.ep.toMove(),
		"legal_actions": legal,
		"action_mask":   maskOf(legal),
	}, nil
}

func (s *server) observe(raw json.RawMessage) (any, *ErrorObject) {
	if s.ep == nil {
		return nil, errorf(ErrNoEpisode, "no episode: call reset first")
	}
	side, e := s.resolveSide(raw)
	if e != nil {
		return nil, e
	}
	obs, err := s.ep.observation(side)
	if err != nil {
		return nil, errorf(ErrInternal, "%v", err)
	}
	return map[string]any{
		"side":        side,
		"turn":        s.ep.state.Turn,
		"phase":       s.ep.state.Phase,
		"observation": obs,
		"state_hash":  hashBytes(obs),
		"terminated":  s.ep.state.Ended(),
		"truncated":   s.ep.truncated,
		"winner":      s.ep.state.Winner,
	}, nil
}

// resolveSide reads the optional "side" argument, defaulting to the single
// external side that has to move (the unambiguous case).
func (s *server) resolveSide(raw json.RawMessage) (int, *ErrorObject) {
	var a sideArgs
	if e := decodeArgs(raw, &a); e != nil {
		return 0, e
	}
	if a.Side != nil {
		if *a.Side != 0 && *a.Side != 1 {
			return 0, errorf(ErrBadRequest, `"side" must be 0 or 1, got %d`, *a.Side)
		}
		return *a.Side, nil
	}
	need := s.ep.externalToMove()
	if len(need) == 1 {
		return need[0], nil
	}
	for side := 0; side < 2; side++ {
		if s.ep.ctrl[side].external {
			return side, nil
		}
	}
	return 0, errorf(ErrBadRequest, `no external side to default to; name one with "side"`)
}

// decodeArgs unmarshals a command's arguments, treating an absent args block as
// an empty object so every command's arguments can be optional.
func decodeArgs(raw json.RawMessage, dst any) *ErrorObject {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields() // a typo'd argument is a bug, not a silent default
	if err := dec.Decode(dst); err != nil {
		return errorf(ErrBadRequest, "bad args: %v", err)
	}
	return nil
}
