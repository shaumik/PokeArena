package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/eval"
)

// --- harness --------------------------------------------------------------

// driver exercises the binary through the exact code path a subprocess client
// hits: a JSON line in, a JSON line out. Nothing in these tests reaches around
// the protocol to poke at the episode directly, because the protocol is the
// product.
type driver struct {
	t   *testing.T
	srv *server
}

func newDriver(t *testing.T) *driver {
	t.Helper()
	srv, err := newServer("", "", "test", 2)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	return &driver{t: t, srv: srv}
}

// call sends one request and returns the decoded response. It fails the test
// if the response is not valid JSON — a client can only recover from a
// well-formed error object.
func (d *driver) call(cmd string, args any) Response {
	d.t.Helper()
	req := map[string]any{"cmd": cmd}
	if args != nil {
		req["args"] = args
	}
	line, err := json.Marshal(req)
	if err != nil {
		d.t.Fatalf("marshal request: %v", err)
	}
	resp, _ := d.srv.handleLine(line)
	// Round-trip through JSON so the tests see exactly the bytes a client sees.
	raw, err := json.Marshal(resp)
	if err != nil {
		d.t.Fatalf("marshal response: %v", err)
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		d.t.Fatalf("response is not valid JSON: %v", err)
	}
	return out
}

// mustCall requires success and decodes the result into dst.
func (d *driver) mustCall(cmd string, args any, dst any) {
	d.t.Helper()
	resp := d.call(cmd, args)
	if !resp.OK {
		d.t.Fatalf("%s failed: %+v", cmd, resp.Error)
	}
	if dst == nil {
		return
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		d.t.Fatalf("marshal result: %v", err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		d.t.Fatalf("decode %s result: %v", cmd, err)
	}
}

// stepResultOf decodes a reset/step result.
func (d *driver) stepResultOf(cmd string, args any) StepResult {
	d.t.Helper()
	var sr StepResult
	d.mustCall(cmd, args, &sr)
	return sr
}

// hasObs reports whether an observation slot actually carries one. A response
// that crossed the wire renders an absent observation as the four bytes "null"
// rather than as a nil slice, so a plain nil check would silently pass.
func hasObs(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// firstLegal returns the first legal flat action index for a side, which is the
// same deterministic fallback the engine and eval.RunGame use.
func firstLegal(t *testing.T, sr StepResult, side int) int {
	t.Helper()
	legal := sr.LegalActions[side]
	if len(legal) == 0 {
		t.Fatalf("side %d has no legal actions at turn %d phase %s", side, sr.Turn, sr.Phase)
	}
	return legal[0].Index
}

// --- determinism ----------------------------------------------------------

// TestDeterminism_SameSeedSameTrajectory is the headline promise: the same seed
// and the same policy produce a byte-identical game. It compares whole response
// lines, not summaries, so a divergence anywhere — an event's wording, a state
// hash, an HP figure — fails it.
func TestDeterminism_SameSeedSameTrajectory(t *testing.T) {
	run := func() []string {
		d := newDriver(t)
		var lines []string
		sr := d.stepResultOf("reset", map[string]any{
			"seed": 12345, "team": "Blitz", "agents": []string{"external", "heuristic"},
		})
		lines = append(lines, mustJSON(t, sr))
		for !sr.Terminated && !sr.Truncated {
			sr = d.stepResultOf("step", map[string]any{"action": firstLegal(t, sr, 0)})
			lines = append(lines, mustJSON(t, sr))
		}
		return lines
	}

	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatalf("trajectory lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("step %d differs between identical-seed runs:\n first: %s\nsecond: %s", i, a[i], b[i])
		}
	}
	if len(a) < 2 {
		t.Fatalf("expected a multi-step game, got %d steps", len(a))
	}
}

// TestDeterminism_DifferentSeedsDiverge guards the other direction: if every
// seed produced the same game the determinism test above would pass vacuously.
func TestDeterminism_DifferentSeedsDiverge(t *testing.T) {
	trace := func(seed int) string {
		d := newDriver(t)
		var b strings.Builder
		sr := d.stepResultOf("reset", map[string]any{
			"seed": seed, "team": "Blitz", "agents": []string{"external", "heuristic"},
		})
		for i := 0; i < 8 && !sr.Terminated && !sr.Truncated; i++ {
			sr = d.stepResultOf("step", map[string]any{"action": firstLegal(t, sr, 0)})
			b.WriteString(mustJSON(t, sr.Events))
		}
		return b.String()
	}
	if trace(1) == trace(2) {
		t.Fatal("seeds 1 and 2 produced identical event logs; the seed is not reaching the engine RNG")
	}
}

// TestMatchesEvalRunGame pins this binary's driver to the benchmark's. It plays
// the same pairing two ways — through eval.RunGame (what cmd/bench runs) and
// through the stdio protocol — and requires the same winner, the same turn
// count, and the same per-decision state hashes.
//
// The pairing is random-vs-random on purpose: a stochastic agent is the only
// one that can detect a wrong seed or a wrong side-salt, and the side salt is a
// value this package copies from internal/eval. If that copy ever drifts, this
// test is what says so.
func TestMatchesEvalRunGame(t *testing.T) {
	const seed = 4242

	d := newDriver(t)
	teamPicks, _, err := TeamSpec{Library: "Bastion"}.resolve(d.srv.dex, d.srv.lib)
	if err != nil {
		t.Fatalf("resolve team: %v", err)
	}

	// The benchmark's own pairing: side 0 seeded from the game seed, side 1
	// from seed ^ sideSalt (internal/eval/match.go resolvedGame).
	agents := [2]ai.Agent{ai.NewRandomAgent(seed), ai.NewRandomAgent(seed ^ sideSalt)}
	want, err := eval.RunGame(d.srv.dex, agents, [2][]engine.TeamPick{teamPicks, teamPicks}, seed, 0)
	if err != nil {
		t.Fatalf("eval.RunGame: %v", err)
	}

	// The same game over the wire, with side 0 driven externally by an
	// identically seeded RandomAgent and side 1 left to the binary's own.
	external := ai.NewRandomAgent(seed)
	sr := d.stepResultOf("reset", map[string]any{
		"seed": seed, "team": "Bastion", "agents": []string{"external", "random"},
	})

	var gotHashes []string
	for !sr.Terminated && !sr.Truncated {
		gotHashes = append(gotHashes, sr.Info.StateHash[0])
		var v ai.View
		if err := json.Unmarshal(sr.Observations[0], &v); err != nil {
			t.Fatalf("decode observation: %v", err)
		}
		act, err := external.Decide(context.Background(), v)
		if err != nil {
			t.Fatalf("external agent: %v", err)
		}
		sr = d.stepResultOf("step", map[string]any{"actions": []any{act, nil}})
	}

	if sr.Winner != want.Winner {
		t.Errorf("winner: env %d, eval.RunGame %d", sr.Winner, want.Winner)
	}
	if sr.Turn != want.Turns {
		t.Errorf("turns: env %d, eval.RunGame %d", sr.Turn, want.Turns)
	}

	var wantHashes []string
	for _, dec := range want.Decisions {
		if dec.Side == 0 {
			wantHashes = append(wantHashes, dec.StateHash)
		}
	}
	if len(gotHashes) != len(wantHashes) {
		t.Fatalf("side-0 decision points: env %d, eval.RunGame %d", len(gotHashes), len(wantHashes))
	}
	for i := range gotHashes {
		if gotHashes[i] != wantHashes[i] {
			t.Fatalf("state hash at side-0 decision %d: env %s, eval.RunGame %s", i, gotHashes[i], wantHashes[i])
		}
	}
}

// --- fog of war -----------------------------------------------------------

// foeForbiddenKeys are the fields the wire projection must never carry for the
// opponent's active Pokémon. Each one is a free read the games do not give you:
// exact HP and max HP name the foe's HP investment, stats/EVs/IVs/nature are a
// damage calculator, and ability/item are inferred in canon, never announced.
var foeForbiddenKeys = []string{
	"hp", "max_hp", "stats", "evs", "ivs", "nature", "ability", "item", "last_consumed_item",
}

// TestFogOfWar_NoHiddenFieldsLeak walks a full agent-vs-agent game and audits
// every observation on both sides: the opponent's bench must not appear at all,
// and the opponent's active must not carry any of the hidden fields.
//
// The teams are deliberately asymmetric. A mirror match would make this test
// pass for the wrong reason — every species name on the board would be on both
// teams, so a leaked bench species would look like the viewer's own.
func TestFogOfWar_NoHiddenFieldsLeak(t *testing.T) {
	d := newDriver(t)

	sr := d.stepResultOf("reset", map[string]any{
		"seed":          9,
		"team":          map[string]any{"dex": []int{150, 149, 143}},
		"opponent_team": map[string]any{"dex": []int{6, 9, 3}},
		"agents":        []string{"external", "external"},
	})

	// Resolve each side's roster so the test knows what must not appear in the
	// other side's bytes.
	rosters := [2][]string{}
	for side := 0; side < 2; side++ {
		obs := sr.Observations[side]
		if !hasObs(obs) {
			t.Fatalf("side %d has no opening observation", side)
		}
		var v struct {
			Self struct {
				Team   []struct{ Name string } `json:"team"`
				Active int                     `json:"active"`
			} `json:"self"`
		}
		if err := json.Unmarshal(obs, &v); err != nil {
			t.Fatalf("decode observation: %v", err)
		}
		for _, p := range v.Self.Team {
			rosters[side] = append(rosters[side], p.Name)
		}
	}
	if len(rosters[0]) != 3 || len(rosters[1]) != 3 {
		t.Fatalf("expected 3v3, got %d vs %d", len(rosters[0]), len(rosters[1]))
	}

	checked := 0
	audit := func(sr StepResult) {
		for side := 0; side < 2; side++ {
			obs := sr.Observations[side]
			if !hasObs(obs) {
				continue
			}
			checked++
			auditFog(t, obs, rosters[1-side], sr.Turn, side)
		}
	}

	audit(sr)
	for steps := 0; !sr.Terminated && !sr.Truncated; steps++ {
		if steps > 400 {
			t.Fatal("game did not terminate")
		}
		actions := make([]any, 2)
		for _, side := range sr.ToMove {
			actions[side] = firstLegal(t, sr, side)
		}
		sr = d.stepResultOf("step", map[string]any{"actions": actions})
		audit(sr)
	}
	if checked < 10 {
		t.Fatalf("audited only %d observations; the game was too short to be evidence", checked)
	}
	t.Logf("audited %d observations across %d turns", checked, sr.Turn)
}

// auditFog asserts one observation carries nothing it should not.
func auditFog(t *testing.T, obs json.RawMessage, foeRoster []string, turn, side int) {
	t.Helper()

	var v struct {
		Foe map[string]any `json:"foe"`
	}
	if err := json.Unmarshal(obs, &v); err != nil {
		t.Fatalf("decode observation: %v", err)
	}
	for _, k := range foeForbiddenKeys {
		if _, ok := v.Foe[k]; ok {
			t.Fatalf("turn %d side %d: observation leaks foe.%s (value %v)", turn, side, k, v.Foe[k])
		}
	}
	if _, ok := v.Foe["hp_pct"]; !ok {
		t.Fatalf("turn %d side %d: foe carries no hp_pct; the redaction dropped the public HP too", turn, side)
	}

	// The whole opponent bench must be absent from the bytes. The active foe is
	// the one name allowed through, so it is excluded from the search.
	activeName, _ := v.Foe["name"].(string)
	text := string(obs)
	for _, name := range foeRoster {
		if name == activeName {
			continue
		}
		if strings.Contains(text, name) {
			t.Fatalf("turn %d side %d: observation leaks benched opponent %q", turn, side, name)
		}
	}
}

// TestFogOfWar_SingleAgentGivesNoOpponentObservation covers the other half of
// the guarantee: in the single-agent shape the opponent's observation is not
// merely redacted, it is not in the response at all.
func TestFogOfWar_SingleAgentGivesNoOpponentObservation(t *testing.T) {
	d := newDriver(t)
	sr := d.stepResultOf("reset", map[string]any{
		"seed": 3, "team": "Spectrum", "agents": []string{"external", "expectimax@1"},
	})
	if !hasObs(sr.Observations[0]) {
		t.Fatal("side 0 (external) got no observation")
	}
	if hasObs(sr.Observations[1]) {
		t.Fatalf("side 1 is a built-in baseline but its observation was returned: %s", sr.Observations[1])
	}
	if sr.LegalActions[1] != nil {
		t.Fatalf("side 1's legal actions were returned: %v", sr.LegalActions[1])
	}
	if sr.Info.StateHash[1] != "" {
		t.Fatalf("side 1's state hash was returned: %q", sr.Info.StateHash[1])
	}
}

// TestObserve_RejectsNothingButStillRedacts checks the standalone observe
// command goes through the same projection as a step observation.
func TestObserve_RejectsNothingButStillRedacts(t *testing.T) {
	d := newDriver(t)
	d.stepResultOf("reset", map[string]any{
		"seed": 5, "team": "Keystone", "agents": []string{"external", "heuristic"},
	})
	var out struct {
		Side        int             `json:"side"`
		Observation json.RawMessage `json:"observation"`
		StateHash   string          `json:"state_hash"`
	}
	d.mustCall("observe", map[string]any{"side": 0}, &out)
	var v struct {
		Foe map[string]any `json:"foe"`
	}
	if err := json.Unmarshal(out.Observation, &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range foeForbiddenKeys {
		if _, ok := v.Foe[k]; ok {
			t.Fatalf("observe leaks foe.%s", k)
		}
	}
	if out.StateHash == "" {
		t.Fatal("observe returned no state hash")
	}
}

// --- legality -------------------------------------------------------------

// TestIllegalAction_RejectedAndStateUnchanged requires that a bad action is a
// clean, recoverable error: named code, the legal set attached, and — the part
// that matters most — an episode still sitting on the same decision point, so
// the client can simply try again.
func TestIllegalAction_RejectedAndStateUnchanged(t *testing.T) {
	d := newDriver(t)
	sr := d.stepResultOf("reset", map[string]any{
		"seed": 11, "team": "Bruiser", "agents": []string{"external", "heuristic"},
	})
	if sr.ActionMask[0][flatSwitchBase] != 0 {
		t.Fatal("expected switching to the already-active slot 0 to be illegal")
	}

	resp := d.call("step", map[string]any{"action": flatSwitchBase}) // switch to the active Pokémon
	if resp.OK {
		t.Fatal("switching to the already-active Pokémon was accepted")
	}
	if resp.Error.Code != ErrIllegalAction {
		t.Fatalf("error code = %q, want %q (%s)", resp.Error.Code, ErrIllegalAction, resp.Error.Message)
	}
	details, _ := resp.Error.Details.(map[string]any)
	if _, ok := details["legal_actions"]; !ok {
		t.Fatalf("illegal_action carried no legal_actions: %+v", resp.Error.Details)
	}
	if _, ok := details["action_mask"]; !ok {
		t.Fatalf("illegal_action carried no action_mask: %+v", resp.Error.Details)
	}

	// The rejection must not have advanced the battle.
	after := d.stepResultOf("step", map[string]any{"action": 0})
	if after.Info.DecisionIndex != 1 {
		t.Fatalf("decision_index = %d after one rejected + one accepted action, want 1", after.Info.DecisionIndex)
	}
	if after.Turn != 1 {
		t.Fatalf("turn = %d, want 1: the rejected action moved the battle", after.Turn)
	}
}

// TestIllegalAction_OutOfRangeIndex is the other rejection path: an integer
// that is not in the discrete space at all fails at decode time, as a
// bad_request rather than an illegal_action.
func TestIllegalAction_OutOfRangeIndex(t *testing.T) {
	d := newDriver(t)
	d.stepResultOf("reset", map[string]any{"seed": 1, "team": "Blitz"})
	resp := d.call("step", map[string]any{"action": FlatActionCount + 5})
	if resp.OK {
		t.Fatal("out-of-range action index was accepted")
	}
	if resp.Error.Code != ErrBadRequest {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrBadRequest)
	}
}

// TestStep_RejectsActionForNonMovingSide keeps the single-agent contract
// honest: a client cannot reach across and play the baseline's side.
func TestStep_RejectsActionForNonMovingSide(t *testing.T) {
	d := newDriver(t)
	d.stepResultOf("reset", map[string]any{
		"seed": 2, "team": "Genesis", "agents": []string{"external", "heuristic"},
	})
	resp := d.call("step", map[string]any{"actions": []any{0, 0}})
	if resp.OK {
		t.Fatal("an action for the baseline-controlled side was accepted")
	}
	if resp.Error.Code != ErrBadRequest {
		t.Fatalf("error code = %q, want %q (%s)", resp.Error.Code, ErrBadRequest, resp.Error.Message)
	}
}

// TestLegalActions_MatchesEngine cross-checks the enumerated set against the
// engine's own ruling, so the mask can never advertise something the engine
// would refuse.
func TestLegalActions_MatchesEngine(t *testing.T) {
	d := newDriver(t)
	sr := d.stepResultOf("reset", map[string]any{
		"seed": 77, "team": "Bastion", "agents": []string{"external", "heuristic"},
	})
	for steps := 0; !sr.Terminated && !sr.Truncated; steps++ {
		if steps > 400 {
			t.Fatal("game did not terminate")
		}
		var out struct {
			LegalActions []LegalAction `json:"legal_actions"`
			ActionMask   []int         `json:"action_mask"`
		}
		d.mustCall("legal_actions", map[string]any{"side": 0}, &out)
		if len(out.LegalActions) == 0 {
			t.Fatalf("turn %d: no legal actions", sr.Turn)
		}
		for _, la := range out.LegalActions {
			if !engine.ActionAllowed(d.srv.dex, d.srv.ep.state, 0, la.Action) {
				t.Fatalf("turn %d: enumerated %s but the engine refuses it", sr.Turn, describeAction(la.Action))
			}
			if la.Index < 0 || la.Index >= FlatActionCount || out.ActionMask[la.Index] != 1 {
				t.Fatalf("turn %d: action %s has flat index %d, not set in the mask", sr.Turn, describeAction(la.Action), la.Index)
			}
			if la.Label == "" {
				t.Fatalf("turn %d: action %s has no label", sr.Turn, describeAction(la.Action))
			}
		}
		sr = d.stepResultOf("step", map[string]any{"action": out.LegalActions[0].Index})
	}
}

// --- full games -----------------------------------------------------------

// TestFullGame_ReachesTermination plays complete games against each built-in
// baseline and checks the terminal contract: a winner, a nonzero turn count,
// a final observation, an empty to_move, and a ±1 reward that agrees with the
// winner.
func TestFullGame_ReachesTermination(t *testing.T) {
	for _, opponent := range []string{"random", "heuristic", "expectimax@1"} {
		t.Run(opponent, func(t *testing.T) {
			d := newDriver(t)
			sr := d.stepResultOf("reset", map[string]any{
				"seed": 2024, "team": "Genesis", "agents": []string{"external", opponent},
			})
			steps := 0
			for !sr.Terminated && !sr.Truncated {
				if steps++; steps > 1000 {
					t.Fatal("game did not terminate within 1000 steps")
				}
				sr = d.stepResultOf("step", map[string]any{"action": firstLegal(t, sr, 0)})
			}
			if !sr.Terminated {
				t.Fatalf("game truncated rather than terminated at turn %d", sr.Turn)
			}
			if sr.Winner < 0 || sr.Winner > 2 {
				t.Fatalf("winner = %d, want 0, 1 or 2", sr.Winner)
			}
			if sr.Turn == 0 {
				t.Fatal("game ended on turn 0")
			}
			if len(sr.ToMove) != 0 {
				t.Fatalf("to_move = %v after termination, want empty", sr.ToMove)
			}
			if sr.Observations[0] == nil {
				t.Fatal("no final observation for the external side")
			}
			want := map[int]float64{0: 1, 1: -1, 2: 0}[sr.Winner]
			if sr.Rewards[0] != want {
				t.Fatalf("reward[0] = %v with winner %d, want %v", sr.Rewards[0], sr.Winner, want)
			}
			if sr.Rewards[0]+sr.Rewards[1] != 0 {
				t.Fatalf("rewards %v are not zero-sum", sr.Rewards)
			}
			t.Logf("%s: winner=%d turns=%d steps=%d", opponent, sr.Winner, sr.Turn, steps)
		})
	}
}

// TestFullGame_ZeroExternalSidesPlaysOut covers the baseline-vs-baseline mode:
// with nobody external, reset itself plays the battle to the end. This is the
// shape the reproducibility check uses.
func TestFullGame_ZeroExternalSidesPlaysOut(t *testing.T) {
	d := newDriver(t)
	sr := d.stepResultOf("reset", map[string]any{
		"seed": 8, "team": "Blitz", "agents": []string{"heuristic", "random"},
	})
	if !sr.Terminated {
		t.Fatalf("reset with no external sides did not play out: phase %s turn %d", sr.Phase, sr.Turn)
	}
	if len(sr.Events) == 0 {
		t.Fatal("no events from a whole battle")
	}
	if hasObs(sr.Observations[0]) || hasObs(sr.Observations[1]) {
		t.Fatal("observations were returned for sides nobody external controls")
	}
}

// TestTruncation_MaxTurns checks the client-imposed time limit reports as a
// truncation rather than a termination, per the Gymnasium distinction.
func TestTruncation_MaxTurns(t *testing.T) {
	d := newDriver(t)
	sr := d.stepResultOf("reset", map[string]any{
		"seed": 6, "team": "Bastion", "agents": []string{"external", "heuristic"}, "max_turns": 3,
	})
	for !sr.Terminated && !sr.Truncated {
		sr = d.stepResultOf("step", map[string]any{"action": firstLegal(t, sr, 0)})
	}
	if !sr.Truncated {
		t.Fatalf("expected truncation at max_turns=3, got terminated=%v turn=%d", sr.Terminated, sr.Turn)
	}
	if sr.Terminated {
		t.Fatal("a turn-limit stop must not report as terminated")
	}
	if sr.Turn < 3 {
		t.Fatalf("truncated at turn %d, want >= 3", sr.Turn)
	}
	if sr.Info.TurnLimit != 3 {
		t.Fatalf("info.turn_limit = %d, want 3", sr.Info.TurnLimit)
	}
	resp := d.call("step", map[string]any{"action": 0})
	if resp.OK || resp.Error.Code != ErrEpisodeOver {
		t.Fatalf("stepping a finished episode: ok=%v err=%+v, want %s", resp.OK, resp.Error, ErrEpisodeOver)
	}
}

// TestRewardHPDelta checks the opt-in dense reward is zero-sum and actually
// moves when damage happens.
func TestRewardHPDelta(t *testing.T) {
	d := newDriver(t)
	sr := d.stepResultOf("reset", map[string]any{
		"seed": 31, "team": "Blitz", "agents": []string{"external", "heuristic"}, "reward": "hp_delta",
	})
	var total float64
	nonzero := false
	for !sr.Terminated && !sr.Truncated {
		sr = d.stepResultOf("step", map[string]any{"action": firstLegal(t, sr, 0)})
		if sr.Rewards[0]+sr.Rewards[1] != 0 {
			t.Fatalf("hp_delta rewards %v are not zero-sum at turn %d", sr.Rewards, sr.Turn)
		}
		if sr.Rewards[0] != 0 {
			nonzero = true
		}
		total += sr.Rewards[0]
	}
	if !nonzero {
		t.Fatal("hp_delta produced no nonzero step reward across a whole game")
	}
	t.Logf("hp_delta return = %.3f, winner = %d", total, sr.Winner)
}

// --- protocol -------------------------------------------------------------

// TestProtocol_ErrorsAreObjectsNotCrashes walks the failure surface. Every one
// of these must come back as a well-formed error object; none may kill the
// process or produce a non-JSON line.
func TestProtocol_ErrorsAreObjectsNotCrashes(t *testing.T) {
	d := newDriver(t)

	cases := []struct {
		name string
		line string
		code string
	}{
		{"malformed json", `{"cmd":"reset"`, ErrBadRequest},
		{"not an object", `[1,2,3]`, ErrBadRequest},
		{"no cmd", `{"args":{}}`, ErrBadRequest},
		{"unknown cmd", `{"cmd":"teleport"}`, ErrUnknownCommand},
		{"step before reset", `{"cmd":"step","args":{"action":0}}`, ErrNoEpisode},
		{"observe before reset", `{"cmd":"observe"}`, ErrNoEpisode},
		{"legal_actions before reset", `{"cmd":"legal_actions"}`, ErrNoEpisode},
		{"reset with no team", `{"cmd":"reset","args":{"seed":1}}`, ErrBadRequest},
		{"reset with unknown team", `{"cmd":"reset","args":{"team":"Nonesuch"}}`, ErrBadRequest},
		{"reset with unknown agent", `{"cmd":"reset","args":{"team":"Blitz","agents":["external","oracle"]}}`, ErrBadRequest},
		{"reset with one agent", `{"cmd":"reset","args":{"team":"Blitz","agents":["external"]}}`, ErrBadRequest},
		{"reset with bad reward", `{"cmd":"reset","args":{"team":"Blitz","reward":"vibes"}}`, ErrBadRequest},
		{"reset with unknown dex number", `{"cmd":"reset","args":{"team":{"dex":[99999]}}}`, ErrBadRequest},
		{"reset with typo'd arg", `{"cmd":"reset","args":{"team":"Blitz","sead":1}}`, ErrBadRequest},
		{"reset with two team routes", `{"cmd":"reset","args":{"team":{"library":"Blitz","dex":[1]}}}`, ErrBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, stop := d.srv.handleLine([]byte(tc.line))
			if stop {
				t.Fatal("a failing request asked the loop to stop")
			}
			if resp.OK {
				t.Fatalf("expected failure, got ok with %v", resp.Result)
			}
			if resp.Error == nil {
				t.Fatal("failure carried no error object")
			}
			if resp.Error.Code != tc.code {
				t.Fatalf("code = %q, want %q (%s)", resp.Error.Code, tc.code, resp.Error.Message)
			}
			if resp.Error.Message == "" {
				t.Fatal("error carried no message")
			}
			if _, err := json.Marshal(resp); err != nil {
				t.Fatalf("error response does not marshal: %v", err)
			}
		})
	}
}

// TestServe_EndToEndOverPipes drives the real serve loop over an io.Reader /
// io.Writer pair — the same loop main() hands stdin and stdout — and checks the
// output is one JSON object per line, ids echoed, close honoured, and nothing
// written after close.
func TestServe_EndToEndOverPipes(t *testing.T) {
	srv, err := newServer("", "", "test", 2)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	in := strings.NewReader(strings.Join([]string{
		`{"id":"a","cmd":"handshake"}`,
		``, // a blank line is padding, not a request
		`{"id":"b","cmd":"reset","args":{"seed":1,"team":"Blitz"}}`,
		`{"id":"c","cmd":"step","args":{"action":0}}`,
		`{"id":"d","cmd":"close"}`,
		`{"id":"e","cmd":"handshake"}`, // must never be read
	}, "\n") + "\n")

	var out strings.Builder
	if err := srv.serve(in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d response lines, want 4 (close must stop the loop):\n%s", len(lines), out.String())
	}
	wantIDs := []string{"a", "b", "c", "d"}
	for i, line := range lines {
		var r Response
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("line %d is not JSON: %v\n%s", i, err, line)
		}
		if !r.OK {
			t.Fatalf("line %d failed: %+v", i, r.Error)
		}
		var id string
		if err := json.Unmarshal(r.ID, &id); err != nil || id != wantIDs[i] {
			t.Fatalf("line %d id = %s, want %q", i, r.ID, wantIDs[i])
		}
	}
}

// TestActionEncoding_RoundTrips pins the flat discrete space. The mapping is
// part of the protocol: renumbering it would silently change what every integer
// in a saved trajectory means.
func TestActionEncoding_RoundTrips(t *testing.T) {
	if FlatActionCount != 11 {
		t.Fatalf("action space size = %d, want 11 (4 moves + Struggle + 6 switches)", FlatActionCount)
	}
	for i := 0; i < FlatActionCount; i++ {
		a, err := decodeFlat(i)
		if err != nil {
			t.Fatalf("decodeFlat(%d): %v", i, err)
		}
		if got := encodeFlat(a); got != i {
			t.Fatalf("encodeFlat(decodeFlat(%d)) = %d", i, got)
		}
	}
	if _, err := decodeFlat(-1); err == nil {
		t.Fatal("decodeFlat(-1) should fail")
	}
	if _, err := decodeFlat(FlatActionCount); err == nil {
		t.Fatalf("decodeFlat(%d) should fail", FlatActionCount)
	}
	if got := encodeFlat(engine.Action{Kind: engine.ActionMove, Index: engine.StruggleMoveIndex}); got != flatStruggle {
		t.Fatalf("Struggle encodes to %d, want %d", got, flatStruggle)
	}
}

// TestActionInput_AcceptsBothEncodings checks the object form is accepted
// alongside the integer form, including the self-switch pivot target that only
// the object form can express.
func TestActionInput_AcceptsBothEncodings(t *testing.T) {
	var flat ActionInput
	if err := json.Unmarshal([]byte(`6`), &flat); err != nil {
		t.Fatalf("integer form: %v", err)
	}
	if flat.Kind != engine.ActionSwitch || flat.Index != 1 {
		t.Fatalf("6 decoded to %s", describeAction(flat.Action))
	}

	var obj ActionInput
	if err := json.Unmarshal([]byte(`{"kind":"move","index":2,"switch_target":3}`), &obj); err != nil {
		t.Fatalf("object form: %v", err)
	}
	if obj.Kind != engine.ActionMove || obj.Index != 2 || obj.SwitchTarget == nil || *obj.SwitchTarget != 3 {
		t.Fatalf("object form decoded to %s", describeAction(obj.Action))
	}

	var bad ActionInput
	if err := json.Unmarshal([]byte(`{"kind":"forfeit","index":0}`), &bad); err == nil {
		t.Fatal("an unknown action kind was accepted")
	}
}

// TestTeamSpec_Shorthands covers the two shorthands, since they are what people
// actually type.
func TestTeamSpec_Shorthands(t *testing.T) {
	var byName TeamSpec
	if err := json.Unmarshal([]byte(`"Genesis"`), &byName); err != nil || byName.Library != "Genesis" {
		t.Fatalf("bare string: %v %+v", err, byName)
	}
	var byDex TeamSpec
	if err := json.Unmarshal([]byte(`[150,149]`), &byDex); err != nil || len(byDex.Dex) != 2 {
		t.Fatalf("bare array: %v %+v", err, byDex)
	}
	var obj TeamSpec
	if err := json.Unmarshal([]byte(`{"dex":[6,9,3]}`), &obj); err != nil || len(obj.Dex) != 3 {
		t.Fatalf("object: %v %+v", err, obj)
	}
	var empty TeamSpec
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil || !empty.IsZero() {
		t.Fatalf("empty: %v %+v", err, empty)
	}
}

// TestReset_CustomPicksAreValidated makes sure the raw-picks escape hatch still
// goes through engine.ValidateTeam rather than trusting the client.
func TestReset_CustomPicksAreValidated(t *testing.T) {
	d := newDriver(t)
	resp := d.call("reset", map[string]any{
		"seed": 1,
		"team": map[string]any{"picks": []map[string]any{
			{"dex_no": 150, "moves": []string{"splash-that-does-not-exist"}},
		}},
	})
	if resp.OK {
		t.Fatal("a team with an illegal move was accepted")
	}
	if resp.Error.Code != ErrBadRequest {
		t.Fatalf("code = %q, want %q (%s)", resp.Error.Code, ErrBadRequest, resp.Error.Message)
	}
}

// TestReset_RestartsCleanly checks a second reset does not inherit anything
// from the first — the property that makes a training loop's episodes
// independent.
func TestReset_RestartsCleanly(t *testing.T) {
	d := newDriver(t)
	args := map[string]any{"seed": 99, "team": "Spectrum", "agents": []string{"external", "heuristic"}}

	first := d.stepResultOf("reset", args)
	for i := 0; i < 5 && !first.Terminated && !first.Truncated; i++ {
		first = d.stepResultOf("step", map[string]any{"action": firstLegal(t, first, 0)})
	}
	second := d.stepResultOf("reset", args)
	if second.Turn != 0 || second.Info.DecisionIndex != 0 {
		t.Fatalf("reset returned turn %d decision %d, want 0/0", second.Turn, second.Info.DecisionIndex)
	}

	fresh := newDriver(t).stepResultOf("reset", args)
	if mustJSON(t, second) != mustJSON(t, fresh) {
		t.Fatal("a reset after a partial episode differs from a reset in a fresh process")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
