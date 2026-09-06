package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/protocol"
)

// local_test.go: proof that an offline battle is a real battle. The claim
// offline mode makes is that an agent needs no gateway, no Docker and no
// second player — so these tests never start a server, and a test that
// accidentally reached for one would fail rather than quietly skip.

// offlineOrSkip loads the embedded dataset the way the server does.
func offlineOrSkip(t *testing.T) *offlineData {
	t.Helper()
	d, err := loadOfflineData()
	if err != nil {
		t.Fatalf("load embedded dataset: %v", err)
	}
	return d
}

// legalTeam builds a six-Pokémon roster from the dex, taking the first
// MovesMax moves of each species' learn list — the same expansion the curated
// AI teams use, so it is legal by construction.
func legalTeam(t *testing.T, d *offlineData) []engine.TeamPick {
	t.Helper()
	species := d.dex.AllSpecies()
	if len(species) < engine.TeamSize {
		t.Fatalf("dataset has only %d species", len(species))
	}
	picks := make([]engine.TeamPick, 0, engine.TeamSize)
	for _, sp := range species[:engine.TeamSize] {
		moves := sp.Moves
		if len(moves) > engine.MovesMax {
			moves = moves[:engine.MovesMax]
		}
		picks = append(picks, engine.TeamPick{
			DexNo:   sp.DexNo,
			MoveIDs: append([]string(nil), moves...),
		})
	}
	if err := engine.ValidateTeam(picks, d.dex); err != nil {
		t.Fatalf("test fixture team is not legal: %v", err)
	}
	return picks
}

// TestOffline_FullBattleWithoutGateway is the headline claim: start, submit,
// and play a battle to its end with nothing running. If this passes, the two
// stop signs in front of every new user — docker compose and a human creating
// the battle in a browser — are gone.
func TestOffline_FullBattleWithoutGateway(t *testing.T) {
	d := offlineOrSkip(t)
	// A deliberately unreachable gateway: any code path that quietly fell
	// back to the network would fail here instead of passing by accident.
	s := newSession(Config{GatewayURL: "ws://127.0.0.1:1"})
	ctx := context.Background()

	out, err := s.StartLocal(ctx, d, opponentHeuristic, 7)
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	if out.Phase != "open" {
		t.Fatalf("phase = %q, want %q — a local battle must open in the picker", out.Phase, "open")
	}
	if out.OpponentTrainer == "" {
		t.Error("opponent_trainer is empty; the agent cannot tell who it is facing")
	}
	defer func() {
		if err := s.Leave(); err != nil {
			t.Errorf("Leave: %v", err)
		}
	}()

	if err := s.SubmitTeam(legalTeam(t, d)); err != nil {
		t.Fatalf("SubmitTeam: %v", err)
	}

	// Play to the end. The bound is generous but finite: a battle that cannot
	// terminate is a bug, and hanging the suite would hide it.
	const maxTurns = 2000
	terminal := false
	for i := 0; i < maxTurns && !terminal; i++ {
		w, err := s.Wait(ctx, 10)
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		if !w.Ready {
			t.Fatalf("Wait timed out at step %d; the local opponent should move instantly", i)
		}
		if w.Terminal {
			terminal = true
			break
		}
		if w.View == nil {
			t.Fatalf("ready with no view at step %d", i)
		}
		v, err := s.View()
		if err != nil {
			t.Fatalf("View: %v", err)
		}
		// Take whatever the engine says is legal, so the test exercises the
		// battle rather than a strategy.
		legal := legalNow(t, d, v)
		if _, err := s.Act(actionKindWire(legal.Kind), legal.Index, nil); err != nil {
			t.Fatalf("Act: %v", err)
		}
	}
	if !terminal {
		t.Fatalf("battle did not end within %d steps", maxTurns)
	}
}

// TestOffline_RejectedTeamKeepsRoomOpen covers the recovery path an agent will
// actually hit: a team that fails validation must come back as an error with
// the room still open, so the fix is a resubmit rather than a restart.
func TestOffline_RejectedTeamKeepsRoomOpen(t *testing.T) {
	d := offlineOrSkip(t)
	s := newSession(Config{GatewayURL: "ws://127.0.0.1:1"})
	ctx := context.Background()

	if _, err := s.StartLocal(ctx, d, opponentHeuristic, 11); err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer func() { _ = s.Leave() }()

	// Five Pokémon: short of a legal team, and short in a way the validator
	// reports before it looks at any individual pick.
	short := legalTeam(t, d)[:5]
	err := s.SubmitTeam(short)
	if err == nil {
		t.Fatal("SubmitTeam accepted a 5-Pokémon team")
	}
	if !strings.Contains(err.Error(), "6") {
		t.Errorf("rejection %q does not say how many Pokémon are required", err)
	}

	// The room must still be open: the same session resubmits and plays on.
	if err := s.SubmitTeam(legalTeam(t, d)); err != nil {
		t.Fatalf("resubmit after rejection: %v", err)
	}
	w, err := s.Wait(ctx, 10)
	if err != nil {
		t.Fatalf("Wait after resubmit: %v", err)
	}
	if !w.Ready || w.View == nil {
		t.Fatal("battle did not become active after a corrected resubmit")
	}
}

// TestOffline_SameSeedSameBattle pins reproducibility, which is the project's
// central claim and has to survive the move in-process. Same seed and same
// team must produce the same opening position.
func TestOffline_SameSeedSameBattle(t *testing.T) {
	d := offlineOrSkip(t)

	open := func() string {
		s := newSession(Config{GatewayURL: "ws://127.0.0.1:1"})
		ctx := context.Background()
		if _, err := s.StartLocal(ctx, d, opponentHeuristic, 4242); err != nil {
			t.Fatalf("StartLocal: %v", err)
		}
		defer func() { _ = s.Leave() }()
		if err := s.SubmitTeam(legalTeam(t, d)); err != nil {
			t.Fatalf("SubmitTeam: %v", err)
		}
		w, err := s.Wait(ctx, 10)
		if err != nil || !w.Ready {
			t.Fatalf("Wait: %v ready=%v", err, w.Ready)
		}
		v, err := s.View()
		if err != nil {
			t.Fatalf("View: %v", err)
		}
		// The foe's species is drawn from the seeded team pool, and the active
		// slot from the seeded engine — both must land the same way twice.
		return fmt.Sprintf("foe=%d self=%d", v.Foe.DexNo, v.Self.Team[v.Self.Active].DexNo)
	}

	if a, b := open(), open(); a != b {
		t.Errorf("same seed gave different openings:\n  %s\n  %s", a, b)
	}
}

// TestOffline_UnknownOpponentIsRejected: an unrecognized opponent name must
// fail loudly rather than silently seating the default, so a typo in a tool
// call is not mistaken for a weak AI.
func TestOffline_UnknownOpponentIsRejected(t *testing.T) {
	d := offlineOrSkip(t)
	s := newSession(Config{GatewayURL: "ws://127.0.0.1:1"})
	_, err := s.StartLocal(context.Background(), d, "grandmaster", 1)
	if err == nil {
		t.Fatal("StartLocal accepted an unknown opponent")
	}
	if !strings.Contains(err.Error(), opponentHeuristic) {
		t.Errorf("error %q does not name the valid opponents", err)
	}
}

// TestOffline_LeaveReleasesTheSession: leaving must free the session for the
// next battle. One process plays many sequential battles by design, and a
// leaked binding would strand every one after the first.
func TestOffline_LeaveReleasesTheSession(t *testing.T) {
	d := offlineOrSkip(t)
	s := newSession(Config{GatewayURL: "ws://127.0.0.1:1"})
	ctx := context.Background()

	if _, err := s.StartLocal(ctx, d, opponentHeuristic, 1); err != nil {
		t.Fatalf("first StartLocal: %v", err)
	}
	if _, err := s.StartLocal(ctx, d, opponentHeuristic, 2); err == nil {
		t.Fatal("second StartLocal succeeded while a battle was bound")
	}
	if err := s.Leave(); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if _, err := s.StartLocal(ctx, d, opponentHeuristic, 3); err != nil {
		t.Fatalf("StartLocal after Leave: %v", err)
	}
	_ = s.Leave()
}

// TestOffline_SeedsCachesForReferenceTools proves the four reference tools
// answer with no gateway. They proxy the gateway's REST API, so without the
// seeded caches they would fail exactly when offline mode is in use.
func TestOffline_SeedsCachesForReferenceTools(t *testing.T) {
	// Unreachable gateway: every fetch below must be served from the cache.
	srv := New(Config{GatewayURL: "ws://127.0.0.1:1"})
	if srv.offline == nil {
		t.Fatalf("embedded dataset did not load: %v", srv.offlineErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dex, err := srv.fetchDex(ctx)
	if err != nil {
		t.Fatalf("fetchDex: %v", err)
	}
	if len(dex) == 0 {
		t.Fatal("fetchDex returned no species")
	}
	// Species detail has to be complete enough to build a team from: a
	// truncated projection would send agents back to a gateway for movepools.
	for _, e := range dex {
		if len(e.Moves) == 0 {
			t.Fatalf("%s has no moves; submit_team would be unbuildable", e.Name)
		}
		if e.Base.HP == 0 {
			t.Fatalf("%s has no base stats", e.Name)
		}
	}

	items, err := srv.fetchItems(ctx)
	if err != nil || len(items) == 0 {
		t.Fatalf("fetchItems: %v (%d items)", err, len(items))
	}
	natures, err := srv.fetchNatures(ctx)
	if err != nil || len(natures) == 0 {
		t.Fatalf("fetchNatures: %v (%d natures)", err, len(natures))
	}
	rules, err := srv.fetchRules(ctx)
	if err != nil {
		t.Fatalf("fetchRules: %v", err)
	}
	if rules.TeamSize != engine.TeamSize || rules.EVMaxTotal != engine.MaxEVTotal {
		t.Errorf("rules %+v disagree with the engine's constants", rules)
	}
}

// legalNow returns an action the engine currently allows, so the battle test
// drives real play instead of guessing at move slots. It fails rather than
// returns a zero value: no legal action at all would mean the session woke the
// agent for a decision that was not theirs, which is the exact bug the run
// loop's opponent-only-replace handling exists to prevent.
func legalNow(t *testing.T, d *offlineData, v ai.View) engine.Action {
	t.Helper()
	legal := ai.LegalActionsDex(d.dex, v)
	if len(legal) == 0 {
		t.Fatalf("turn %d: no legal actions for the side that was asked to move", v.Turn)
	}
	return legal[0]
}

// actionKindWire maps an engine action kind back to the wire string act()
// takes, mirroring kindFromWire in the other direction.
func actionKindWire(k engine.ActionKind) string {
	if k == engine.ActionSwitch {
		return protocol.ActionKindSwitch
	}
	return protocol.ActionKindMove
}

// TestStartBattleTool_Defaults exercises the tool handler an agent actually
// calls, rather than the session method beneath it: the opponent defaults, and
// the seed is echoed back even when the caller omitted one. Reporting a
// generated seed is what keeps an unseeded battle replayable.
func TestStartBattleTool_Defaults(t *testing.T) {
	srv := New(Config{GatewayURL: "ws://127.0.0.1:1"})
	if srv.offline == nil {
		t.Fatalf("embedded dataset did not load: %v", srv.offlineErr)
	}
	defer func() { _ = srv.session.Leave() }()

	_, out, err := srv.startBattle(context.Background(), nil, startBattleIn{})
	if err != nil {
		t.Fatalf("start_battle: %v", err)
	}
	if out.Phase != "open" {
		t.Errorf("phase = %q, want open", out.Phase)
	}
	if out.Opponent != opponentHeuristic {
		t.Errorf("opponent = %q, want the %q default", out.Opponent, opponentHeuristic)
	}
	if out.Seed == 0 {
		t.Error("seed is 0; an omitted seed must come back filled in or the battle cannot be replayed")
	}
	if out.NextStep == "" {
		t.Error("next_step is empty; the picker phase is where an agent stalls")
	}
	if out.OpponentTrainer == "" {
		t.Error("opponent_trainer is empty")
	}
}

// TestStartBattleTool_SeedRoundTrips: an explicitly passed seed must be the one
// used and the one reported, so quoting it back reproduces the battle.
func TestStartBattleTool_SeedRoundTrips(t *testing.T) {
	srv := New(Config{GatewayURL: "ws://127.0.0.1:1"})
	defer func() { _ = srv.session.Leave() }()

	const want = 99991
	_, out, err := srv.startBattle(context.Background(), nil,
		startBattleIn{Seed: want, Opponent: opponentExpectimax})
	if err != nil {
		t.Fatalf("start_battle: %v", err)
	}
	if out.Seed != want {
		t.Errorf("seed = %d, want %d", out.Seed, want)
	}
	if out.Opponent != opponentExpectimax {
		t.Errorf("opponent = %q, want %q", out.Opponent, opponentExpectimax)
	}
}

// TestOffline_RefusedActionComesBackImmediately is a regression guard on the
// costliest possible failure in the turn loop.
//
// When an action is refused the turn is still owed, but Act has already
// cleared needsAction — so a dispatcher that merely stops clearing it leaves
// the flag false and the next wait blocks for its whole timeout and then
// reports nothing. One illegal action used to cost a minute of silence. It
// must now come straight back, saying what was wrong and what was legal, with
// the turn still the agent's to take.
func TestOffline_RefusedActionComesBackImmediately(t *testing.T) {
	d := offlineOrSkip(t)
	s := newSession(Config{GatewayURL: "ws://127.0.0.1:1"})
	ctx := context.Background()

	if _, err := s.StartLocal(ctx, d, opponentHeuristic, 7); err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer func() { _ = s.Leave() }()
	if err := s.SubmitTeam(legalTeam(t, d)); err != nil {
		t.Fatalf("SubmitTeam: %v", err)
	}

	// Play until a replacement is due — the ordinary way a move becomes
	// illegal — then insist on a move anyway.
	const maxSteps = 400
	for i := 0; i < maxSteps; i++ {
		w, err := s.Wait(ctx, 10)
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		if !w.Ready {
			t.Fatalf("Wait timed out at step %d", i)
		}
		if w.Terminal {
			t.Skip("battle ended before a replacement was ever due; nothing to assert")
		}
		v, err := s.View()
		if err != nil {
			t.Fatalf("View: %v", err)
		}
		if !v.Replace {
			legal := legalNow(t, d, v)
			if _, err := s.Act(actionKindWire(legal.Kind), legal.Index, nil); err != nil {
				t.Fatalf("Act: %v", err)
			}
			continue
		}

		// A replacement is due, so a move is illegal. Send one.
		if _, err := s.Act("move", 0, nil); err != nil {
			t.Fatalf("Act(move) during replace: %v", err)
		}

		start := time.Now()
		w, err = s.Wait(ctx, 10)
		if err != nil {
			t.Fatalf("Wait after refused action: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("Wait took %v after a refused action; it must return at once, not stall to the timeout", elapsed)
		}
		if !w.Ready {
			t.Fatal("Wait reported not-ready after a refused action; the turn is still owed")
		}
		if w.Error == "" {
			t.Fatal("Wait carried no error after a refused action; the agent has no way to learn what was wrong")
		}
		// The message has to name the way out, not just the problem.
		if !strings.Contains(w.Error, "switch") {
			t.Errorf("error %q does not name the legal actions", w.Error)
		}
		if w.View == nil {
			t.Error("no view alongside the refusal; the agent cannot pick a replacement")
		}

		// The error is consumed: a stale complaint must not resurface.
		sw := legalNow(t, d, mustView(t, s))
		if _, err := s.Act(actionKindWire(sw.Kind), sw.Index, nil); err != nil {
			t.Fatalf("Act(recovery): %v", err)
		}
		w, err = s.Wait(ctx, 10)
		if err != nil {
			t.Fatalf("Wait after recovery: %v", err)
		}
		if w.Error != "" {
			t.Errorf("stale error %q repeated after a successful action", w.Error)
		}
		return
	}
	t.Fatalf("no replacement phase within %d steps", maxSteps)
}

func mustView(t *testing.T, s *session) ai.View {
	t.Helper()
	v, err := s.View()
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	return v
}

// TestAct_ReturnsTheNextView is the one-call turn. act used to be
// fire-and-forget: it acknowledged the send and the agent then paid a second
// call to wait for the consequence. Every turn cost at least two round trips
// for one decision.
func TestAct_ReturnsTheNextView(t *testing.T) {
	d := offlineOrSkip(t)
	s := newSession(Config{GatewayURL: "ws://127.0.0.1:1"})
	ctx := context.Background()

	if _, err := s.StartLocal(ctx, d, opponentHeuristic, 7); err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer func() { _ = s.Leave() }()
	if err := s.SubmitTeam(legalTeam(t, d)); err != nil {
		t.Fatalf("SubmitTeam: %v", err)
	}

	// One wait to reach the first turn; after that act carries the loop.
	w, err := s.Wait(ctx, 10)
	if err != nil || !w.Ready {
		t.Fatalf("Wait: %v ready=%v", err, w.Ready)
	}
	before := mustView(t, s)

	legal := legalNow(t, d, before)
	out, err := s.ActAndWait(ctx, actionKindWire(legal.Kind), legal.Index, 10, nil)
	if err != nil {
		t.Fatalf("ActAndWait: %v", err)
	}
	if !out.Accepted {
		t.Fatal("action not accepted")
	}
	if !out.Ready {
		t.Fatal("act did not wait for the result; the agent still has to call wait")
	}
	if out.Terminal {
		return // a one-turn battle is legal, just nothing more to assert
	}
	if out.View == nil {
		t.Fatal("act returned no view, so the next decision needs another call")
	}
	after := mustView(t, s)
	if after.Turn == before.Turn && after.Phase == before.Phase {
		t.Errorf("state did not advance: still turn %d, phase %s", after.Turn, after.Phase)
	}
}

// TestAct_DrivesAWholeBattleAlone: after the opening wait, act is the entire
// loop. This is the shape the tool surface now promises.
func TestAct_DrivesAWholeBattleAlone(t *testing.T) {
	d := offlineOrSkip(t)
	s := newSession(Config{GatewayURL: "ws://127.0.0.1:1"})
	ctx := context.Background()

	if _, err := s.StartLocal(ctx, d, opponentHeuristic, 21); err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer func() { _ = s.Leave() }()
	if err := s.SubmitTeam(legalTeam(t, d)); err != nil {
		t.Fatalf("SubmitTeam: %v", err)
	}

	w, err := s.Wait(ctx, 10)
	if err != nil || !w.Ready {
		t.Fatalf("opening Wait: %v ready=%v", err, w.Ready)
	}

	calls := 1 // the opening wait
	const maxTurns = 500
	var out actOut
	for i := 0; i < maxTurns; i++ {
		legal := legalNow(t, d, mustView(t, s))
		out, err = s.ActAndWait(ctx, actionKindWire(legal.Kind), legal.Index, 10, nil)
		calls++
		if err != nil {
			t.Fatalf("ActAndWait at step %d: %v", i, err)
		}
		if !out.Ready {
			t.Fatalf("act timed out at step %d against a local opponent", i)
		}
		if out.Terminal {
			break
		}
	}
	if !out.Terminal {
		t.Fatalf("battle did not end within %d acts", maxTurns)
	}
	// The battle is over, so the result has to be legible without the agent
	// tracking which side it was.
	if out.Outcome == "" {
		t.Error("the battle ended with no outcome; the agent cannot tell if it won")
	}
	if out.Winner == nil {
		t.Error("terminal act carried no winner")
	}
	t.Logf("battle finished in %d tool calls, outcome %q", calls, out.Outcome)
}

// TestAct_RefusedActionKeepsTheTurn: an illegal action must come back through
// act itself — same call, with the reason and the view — rather than costing
// the agent a separate wait to discover it.
func TestAct_RefusedActionKeepsTheTurn(t *testing.T) {
	d := offlineOrSkip(t)
	s := newSession(Config{GatewayURL: "ws://127.0.0.1:1"})
	ctx := context.Background()

	if _, err := s.StartLocal(ctx, d, opponentHeuristic, 7); err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	defer func() { _ = s.Leave() }()
	if err := s.SubmitTeam(legalTeam(t, d)); err != nil {
		t.Fatalf("SubmitTeam: %v", err)
	}
	if w, err := s.Wait(ctx, 10); err != nil || !w.Ready {
		t.Fatalf("opening Wait: %v ready=%v", err, w.Ready)
	}

	for i := 0; i < 400; i++ {
		v := mustView(t, s)
		if !v.Replace {
			legal := legalNow(t, d, v)
			out, err := s.ActAndWait(ctx, actionKindWire(legal.Kind), legal.Index, 10, nil)
			if err != nil {
				t.Fatalf("ActAndWait: %v", err)
			}
			if out.Terminal {
				t.Skip("battle ended before a replacement was due")
			}
			continue
		}

		// A replacement is due, so a move is illegal. One call must return
		// the refusal, the reason, and the turn.
		start := time.Now()
		out, err := s.ActAndWait(ctx, "move", 0, 10, nil)
		if err != nil {
			t.Fatalf("ActAndWait(illegal): %v", err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("took %v; a refusal must come back at once", elapsed)
		}
		if !out.Ready {
			t.Fatal("not ready after a refusal; the turn is still owed")
		}
		if out.Error == "" {
			t.Fatal("act refused the action without saying why")
		}
		if !strings.Contains(out.Error, "switch") {
			t.Errorf("error %q does not name the legal actions", out.Error)
		}
		if out.View == nil {
			t.Fatal("no view alongside the refusal; nothing to choose a replacement from")
		}
		// And the recovery goes through the same one call.
		sw := legalNow(t, d, mustView(t, s))
		out, err = s.ActAndWait(ctx, actionKindWire(sw.Kind), sw.Index, 10, nil)
		if err != nil {
			t.Fatalf("ActAndWait(recovery): %v", err)
		}
		if out.Error != "" {
			t.Errorf("stale error %q repeated after a good action", out.Error)
		}
		return
	}
	t.Skip("no replacement phase arose in this battle")
}
