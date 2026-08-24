package main

// The broker had no tests at all, which made it the only untested layer in the
// path a tournament match travels: the engine is covered, ai.MakeView's
// fog-of-war projection is covered by internal/ai, and the component that
// actually enforces fair play between two agent processes was not. Every test
// here pins a rule the harness is trusted to keep — what a player may see, who
// may run the referee commands, what an action submission is allowed to do,
// and how a capped match is decided.
//
// The commands print to stdout and take argv slices, so the tests drive them
// the way an agent does rather than reaching past them into helpers.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

const (
	testData  = "../../data"
	testTeams = "../../royale/teams"
	testToken = "judge-token-for-tests"
	// Long enough that a resolution which is going to happen has happened,
	// short enough that the deliberate half-submissions below do not stall
	// the suite: a lone submission always waits out this timeout.
	testWait = "250ms"
)

// capture swaps os.Stdout for a pipe while fn runs. The commands write with
// fmt.Printf, so this is the only way to assert on what an agent is shown.
func capture(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	ferr := fn()
	w.Close()
	os.Stdout = old
	out := <-done
	r.Close()
	return out, ferr
}

func teamPath(name string) string { return filepath.Join(testTeams, name) }

// newMatch creates a match in a temp root from two of the tournament rosters.
// They are the real files on purpose: the identities these tests check are
// hidden are exactly the ones that leaked in the tournament.
func newMatch(t *testing.T, extra ...string) string {
	t.Helper()
	root := t.TempDir()
	args := append([]string{
		"-root", root, "-data", testData, "-id", "t1", "-round", "test",
		"-p1", teamPath("meridian.json"), "-p2", teamPath("the-low-ceiling.json"),
		"-seed", "7", "-token", testToken,
	}, extra...)
	if _, err := capture(t, func() error { return cmdNew(args) }); err != nil {
		t.Fatalf("new: %v", err)
	}
	return root
}

func readMeta(t *testing.T, root string) Meta {
	t.Helper()
	var m Meta
	if err := readJSON(filepath.Join(matchDir(root, "t1"), "meta.json"), &m); err != nil {
		t.Fatalf("read meta: %v", err)
	}
	return m
}

// submit files one action. A side submitting alone waits out testWait and
// returns a timeout error with its action still queued, which is how these
// tests drive a turn to resolution one seat at a time.
func submit(t *testing.T, root, slot, action, why string) (string, error) {
	t.Helper()
	args := []string{
		"-root", root, "-data", testData, "-id", "t1",
		"-slot", slot, "-action", action, "-timeout", testWait,
	}
	if why != "" {
		args = append(args, "-why", why)
	}
	return capture(t, func() error { return cmdAct(args) })
}

func view(t *testing.T, root, slot string) string {
	t.Helper()
	out, err := capture(t, func() error {
		return cmdView([]string{"-root", root, "-data", testData, "-id", "t1", "-slot", slot})
	})
	if err != nil {
		t.Fatalf("view %s: %v", slot, err)
	}
	return out
}

// --- fog of war: identity ---

// TestFoeSeesCodenameNotIdentity: the tournament's team names were the
// archetype — the champion said "The Low Ceiling" told it Trick Room before
// turn one — so the harness shows the other seat a codename and nothing else.
// This walks every surface a player reads and asserts the foe's name and theme
// appear on none of them.
func TestFoeSeesCodenameNotIdentity(t *testing.T) {
	root := newMatch(t)
	meta := readMeta(t, root)

	// p1 is Meridian/Cobalt, p2 is The Low Ceiling/Indigo.
	foe := meta.Trainers[1]
	if foe.Codename == "" || strings.EqualFold(foe.Codename, foe.Name) {
		t.Fatalf("fixture roster has no usable codename: %+v", foe.Codename)
	}

	teamOut, err := capture(t, func() error {
		return cmdTeam([]string{"-root", root, "-data", testData, "-id", "t1", "-slot", "p1"})
	})
	if err != nil {
		t.Fatalf("team: %v", err)
	}
	viewOut := view(t, root, "p1")

	for _, surface := range []struct{ what, text string }{
		{"team", teamOut},
		{"view", viewOut},
	} {
		if strings.Contains(surface.text, foe.Name) {
			t.Errorf("%s leaked the foe's name %q:\n%s", surface.what, foe.Name, surface.text)
		}
		// The theme is a paragraph; its first clause is the archetype label
		// ("TRICK ROOM — every Pokémon here…") and is the part that gives the
		// game away on its own.
		if head, _, _ := strings.Cut(foe.Theme, " — "); head != "" && strings.Contains(surface.text, head) {
			t.Errorf("%s leaked the foe's theme %q:\n%s", surface.what, head, surface.text)
		}
		if !strings.Contains(surface.text, foe.Codename) {
			t.Errorf("%s never names the foe by codename %q:\n%s", surface.what, foe.Codename, surface.text)
		}
	}

	// Your own identity is still yours to read: it is your brief.
	if !strings.Contains(teamOut, meta.Trainers[0].Name) {
		t.Errorf("team hid the reader's own name:\n%s", teamOut)
	}
}

// TestBattleLinesCarryCodenames: the engine interpolates the side's trainer
// into its own log text ("X's team became cloaked in a mystical veil!"), and
// those lines are printed to both players. Handing the engine the codename is
// what makes that safe, so this pins the battle state itself.
func TestBattleLinesCarryCodenames(t *testing.T) {
	root := newMatch(t)
	meta := readMeta(t, root)

	var st engine.BattleState
	if err := readJSON(filepath.Join(matchDir(root, "t1"), "state.json"), &st); err != nil {
		t.Fatalf("read state: %v", err)
	}
	for i := range st.Sides {
		if got, want := st.Sides[i].Trainer, meta.Trainers[i].Codename; got != want {
			t.Errorf("side %d trainer = %q, want the codename %q — a battle line "+
				"would print the real name", i, got, want)
		}
	}
}

// TestMissingCodenameFallsBackToNeutralLabel: forgetting to declare one must
// be the safe outcome, not the leaky one. The previous fix for this leak was a
// rule in the runbook asking the organizer for neutral names; the fallback is
// what makes the rule unnecessary.
func TestMissingCodenameFallsBackToNeutralLabel(t *testing.T) {
	anon := writeTeamCopy(t, teamPath("meridian.json"), func(tf map[string]any) {
		delete(tf, "codename")
	})
	root := t.TempDir()
	if _, err := capture(t, func() error {
		return cmdNew([]string{
			"-root", root, "-data", testData, "-id", "t1",
			"-p1", anon, "-p2", teamPath("the-low-ceiling.json"), "-token", testToken,
		})
	}); err != nil {
		t.Fatalf("new: %v", err)
	}
	meta := readMeta(t, root)
	// Spelled out rather than compared against the harness's own helper: a
	// test that asks the code under test what the answer is cannot fail.
	if got := meta.Trainers[0].Codename; got != "Trainer P1" {
		t.Fatalf("codename with none declared = %q, want the neutral seat label %q",
			got, "Trainer P1")
	}
	out := view(t, root, "p2")
	if strings.Contains(out, "Meridian") {
		t.Errorf("a team file with no codename leaked its name to the foe:\n%s", out)
	}
}

// TestCodenameEqualToNameIsRefused: a codename that repeats the identity is
// not an alias, and letting it through would quietly restore the leak.
func TestCodenameEqualToNameIsRefused(t *testing.T) {
	same := writeTeamCopy(t, teamPath("meridian.json"), func(tf map[string]any) {
		tf["codename"] = "meridian" // case-insensitively the team's own name
	})
	root := t.TempDir()
	_, err := capture(t, func() error {
		return cmdNew([]string{
			"-root", root, "-data", testData, "-id", "t1",
			"-p1", same, "-p2", teamPath("the-low-ceiling.json"), "-token", testToken,
		})
	})
	if err == nil || !strings.Contains(err.Error(), "codename repeats the team name") {
		t.Fatalf("new with a codename equal to the name: err = %v, want a refusal", err)
	}
}

// TestBothSeatsCannotShareACodename: two sides under one alias are not
// tellable apart in a battle line or a recap, which would make the fog cost
// the referee rather than the players.
func TestBothSeatsCannotShareACodename(t *testing.T) {
	clash := writeTeamCopy(t, teamPath("the-low-ceiling.json"), func(tf map[string]any) {
		tf["codename"] = "Cobalt" // Meridian's
	})
	root := t.TempDir()
	_, err := capture(t, func() error {
		return cmdNew([]string{
			"-root", root, "-data", testData, "-id", "t1",
			"-p1", teamPath("meridian.json"), "-p2", clash, "-token", testToken,
		})
	})
	if err == nil || !strings.Contains(err.Error(), "both seats claim the codename") {
		t.Fatalf("new with a duplicate codename: err = %v, want a refusal", err)
	}
	// The clash is caught before anything is written.
	if _, statErr := os.Stat(filepath.Join(matchDir(root, "t1"), "meta.json")); statErr == nil {
		t.Error("the match was created despite the duplicate codename")
	}
}

// writeTeamCopy writes a temp roster derived from an on-disk one.
func writeTeamCopy(t *testing.T, src string, edit func(map[string]any)) string {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	var tf map[string]any
	if err := json.Unmarshal(b, &tf); err != nil {
		t.Fatalf("parse %s: %v", src, err)
	}
	edit(tf)
	out := filepath.Join(t.TempDir(), filepath.Base(src))
	nb, err := json.Marshal(tf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(out, nb, 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	return out
}

// --- fog of war: the board ---

// TestViewHidesFoeBench: `view` renders nothing but ai.MakeView, and the whole
// premise of the arena is that the projection is the only thing a player
// reads. Every benched foe species, ability and item must be absent from the
// part of the render that describes the foe — and any species the reader does
// not itself carry must be absent from the whole render.
func TestViewHidesFoeBench(t *testing.T) {
	root := newMatch(t)
	meta := readMeta(t, root)
	dex, err := loadDex(testData)
	if err != nil {
		t.Fatalf("dex: %v", err)
	}
	out := view(t, root, "p1")
	_, foeSection, ok := strings.Cut(out, "── FOE ACTIVE ──")
	if !ok {
		t.Fatalf("view has no foe section to check:\n%s", out)
	}

	// Two rosters can legally carry the same item or the same species, and the
	// reader's own half of the render names its own. Anything the reader also
	// holds is therefore checked in the foe section only, where its presence
	// would be a leak whatever the reader carries.
	ownSpecies := map[string]bool{}
	for _, p := range meta.Trainers[0].Picks {
		if sp, ok := dex.Species[p.DexNo]; ok {
			ownSpecies[sp.Name] = true
		}
	}

	for i, p := range meta.Trainers[1].Picks {
		sp, ok := dex.Species[p.DexNo]
		if !ok {
			t.Fatalf("foe pick %d: unknown species %d", i, p.DexNo)
		}
		if i == 0 {
			// The lead is on the field and legitimately visible.
			if !strings.Contains(out, sp.Name) {
				t.Errorf("view never names the foe's active %s:\n%s", sp.Name, out)
			}
			continue
		}
		if strings.Contains(foeSection, sp.Name) {
			t.Errorf("view leaked benched foe species %s:\n%s", sp.Name, foeSection)
		}
		if !ownSpecies[sp.Name] && strings.Contains(out, sp.Name) {
			t.Errorf("benched foe species %s appears in a render that should not "+
				"know it exists:\n%s", sp.Name, out)
		}
		if p.Ability != "" && strings.Contains(foeSection, p.Ability) {
			t.Errorf("view leaked benched foe ability %s:\n%s", p.Ability, foeSection)
		}
		if p.Item != "" && strings.Contains(foeSection, p.Item) {
			t.Errorf("view leaked benched foe item %s:\n%s", p.Item, foeSection)
		}
	}
	if !strings.Contains(out, "species hidden until sent out") {
		t.Errorf("view no longer says the foe's bench is hidden:\n%s", out)
	}
}

// TestViewHidesUnrevealedFoeDetails: the foe's active is visible, but its
// ability and item are only what the engine has revealed — "(unknown)" on
// turn zero, not the roster's declaration.
func TestViewHidesUnrevealedFoeDetails(t *testing.T) {
	root := newMatch(t)
	meta := readMeta(t, root)
	lead := meta.Trainers[1].Picks[0]
	out := view(t, root, "p1")
	_, foeSection, ok := strings.Cut(out, "── FOE ACTIVE ──")
	if !ok {
		t.Fatalf("view has no foe section to check:\n%s", out)
	}
	if lead.Ability != "" && strings.Contains(foeSection, lead.Ability) {
		t.Errorf("view leaked the foe active's undisclosed ability %s:\n%s", lead.Ability, foeSection)
	}
	if lead.Item != "" && strings.Contains(foeSection, lead.Item) {
		t.Errorf("view leaked the foe active's undisclosed item %s:\n%s", lead.Item, foeSection)
	}
	if !strings.Contains(foeSection, "(unknown)") {
		t.Errorf("the foe's ability and item are not marked unknown on turn zero:\n%s", foeSection)
	}
}

// --- the reasoning channel ---

// TestRecapStripsOpponentReasoning: `act -why` records a one-line read for the
// match report, and a leak here hands the opponent the reader's whole read of
// the position every single turn.
//
// The bug this guards is an asymmetry, so both directions are played: the
// recap is printed by whichever side submits second, and each side takes a
// turn at being that side. Driving it through `act` rather than through the
// printer means the rule is stated as "what a player is shown", which is the
// form another implementation of this broker has to satisfy.
func TestRecapStripsOpponentReasoning(t *testing.T) {
	for _, resolver := range []string{"p1", "p2"} {
		t.Run("resolved by "+resolver, func(t *testing.T) {
			first := "p1"
			if resolver == "p1" {
				first = "p2"
			}
			root := newMatch(t)
			meta := readMeta(t, root)

			if _, err := submit(t, root, first, "move:0", "read of "+first); err == nil {
				t.Fatal("a lone submission should have waited out its timeout")
			}
			out, err := submit(t, root, resolver, "move:0", "read of "+resolver)
			if err != nil {
				t.Fatalf("%s act: %v", resolver, err)
			}

			if !strings.Contains(out, "read of "+resolver) {
				t.Errorf("%s lost its own reasoning from its recap:\n%s", resolver, out)
			}
			if strings.Contains(out, "read of "+first) {
				t.Errorf("%s was shown the opponent's reasoning:\n%s", resolver, out)
			}
			// The foe's move itself is public — only the reasoning is not.
			if !strings.Contains(out, "FOE (") {
				t.Errorf("no foe line in the recap at all:\n%s", out)
			}
			side := 0
			if resolver == "p2" {
				side = 1
			}
			if !strings.Contains(out, meta.Trainers[1-side].Codename) {
				t.Errorf("the recap does not name the foe by codename:\n%s", out)
			}
			if strings.Contains(out, meta.Trainers[1-side].Name) {
				t.Errorf("the recap leaked the foe's real name:\n%s", out)
			}
		})
	}
}

// --- referee commands ---

// TestRefereeCommandsRequireJudgeToken: `log` and `report` show everything,
// including both rosters and both sides' reasoning. An absent or wrong token
// has to be refused, or the fog-of-war guarantee is one flag away from gone.
func TestRefereeCommandsRequireJudgeToken(t *testing.T) {
	root := newMatch(t)
	cases := []struct {
		name string
		fn   func([]string) error
	}{
		{"log", cmdLog},
		{"report", cmdReport},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := []string{"-root", root, "-data", testData, "-id", "t1"}
			for _, tok := range []struct{ what, token string }{
				{"absent", ""},
				{"wrong", "not-the-judge-token"},
			} {
				args := base
				if tok.token != "" {
					args = append(append([]string{}, base...), "-token", tok.token)
				}
				out, err := capture(t, func() error { return c.fn(args) })
				if err == nil {
					t.Errorf("%s token accepted:\n%s", tok.what, out)
				}
				if strings.Contains(out, "Meridian") {
					t.Errorf("%s token still printed match identities:\n%s", tok.what, out)
				}
			}
			out, err := capture(t, func() error {
				return c.fn(append(append([]string{}, base...), "-token", testToken))
			})
			if err != nil {
				t.Fatalf("judge token refused: %v", err)
			}
			if !strings.Contains(out, "Meridian") || !strings.Contains(out, "The Low Ceiling") {
				t.Errorf("%s with the judge token does not show both identities:\n%s", c.name, out)
			}
		})
	}
}

// --- action submission ---

// TestActRefusesIllegalAction: the broker asks the engine, so an action the
// engine calls illegal never reaches the state — and the refusal lists what
// was legal instead, because an agent that cannot recover just stalls.
func TestActRefusesIllegalAction(t *testing.T) {
	root := newMatch(t)
	for _, bad := range []string{"move:9", "switch:0", "move:-1"} {
		out, err := submit(t, root, "p1", bad, "")
		if err == nil {
			t.Errorf("act %s was accepted:\n%s", bad, out)
			continue
		}
		if !strings.Contains(err.Error(), "illegal action") {
			t.Errorf("act %s: err = %v, want an illegal-action refusal", bad, err)
		}
		if !strings.Contains(err.Error(), "legal right now") {
			t.Errorf("act %s: refusal does not list the legal actions: %v", bad, err)
		}
	}
	// Nothing illegal was recorded on the way past.
	var pend Pending
	if err := readJSON(filepath.Join(matchDir(root, "t1"), "pending.json"), &pend); err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if pend.Actions[0] != nil {
		t.Errorf("an illegal action was queued anyway: %+v", pend.Actions[0])
	}
}

// TestActRefusesSecondSubmission: one action per side per decision point. A
// second submission would let an agent see the opponent's move land and then
// change its own, which is the one thing the two-file protocol has to prevent.
func TestActRefusesSecondSubmission(t *testing.T) {
	root := newMatch(t)
	if _, err := submit(t, root, "p1", "move:0", "first"); err == nil {
		t.Fatal("a lone submission should have waited out its timeout")
	}
	out, err := submit(t, root, "p1", "move:1", "second thoughts")
	if err == nil {
		t.Fatalf("a second submission was accepted:\n%s", out)
	}
	if !strings.Contains(err.Error(), "already submitted") {
		t.Errorf("err = %v, want an already-submitted refusal", err)
	}
	// The first choice stands.
	var pend Pending
	if err := readJSON(filepath.Join(matchDir(root, "t1"), "pending.json"), &pend); err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if pend.Actions[0] == nil || pend.Actions[0].Index != 0 {
		t.Errorf("queued action = %+v, want the first submission (move:0)", pend.Actions[0])
	}
}

// TestActRefusesWhenNoActionIsOwed: outside a decision point there is nothing
// to submit, and saying so is what keeps a confused agent looping on `view`
// rather than writing into a phase the engine is not in.
func TestActRefusesWhenNoActionIsOwed(t *testing.T) {
	root := newMatch(t)
	dir := matchDir(root, "t1")
	var st engine.BattleState
	if err := readJSON(filepath.Join(dir, "state.json"), &st); err != nil {
		t.Fatalf("read state: %v", err)
	}
	st.Phase = engine.PhaseReplace
	st.Replace = [2]bool{false, false}
	if err := writeJSON(filepath.Join(dir, "state.json"), &st); err != nil {
		t.Fatalf("write state: %v", err)
	}
	_, err := submit(t, root, "p1", "move:0", "")
	if err == nil || !strings.Contains(err.Error(), "do not owe an action") {
		t.Fatalf("err = %v, want a not-your-turn refusal", err)
	}
}

// --- the turn cap ---

// TestTurnCapDecidesOnPokemonThenHPThenDraw: a stall mirror can legitimately
// run forever and a tournament needs a result, so past the cap the match is
// decided on Pokémon standing, then on total HP, then called a draw. The order
// is the whole rule: HP must never overturn a Pokémon-count lead.
//
// Each case sets up the position in state.json and then plays the capped turn
// through `act`, so what is pinned is the verdict both agents are shown rather
// than the return value of the function that computes it. state.json is the
// broker's documented interface — it is how the match is stored between
// commands — so writing a position into it is using the harness, not going
// around it.
func TestTurnCapDecidesOnPokemonThenHPThenDraw(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(*engine.BattleState)
		want    string
		winner  int
	}{
		{
			// Side 1 is a Pokémon down and ahead on HP everywhere else. The
			// count still decides it, which is the ordering this case exists
			// for: if HP were consulted first the verdict would flip.
			name: "pokemon standing wins first",
			arrange: func(s *engine.BattleState) {
				benchFaint(s, 1)
				for i := range s.Sides[0].Team {
					if !s.Sides[0].Team[i].Fainted {
						s.Sides[0].Team[i].HP = 1
					}
				}
			},
			want:   "wins on Pokémon remaining",
			winner: 0,
		},
		{
			name: "total HP breaks a tie on count",
			arrange: func(s *engine.BattleState) {
				for i := range s.Sides[0].Team {
					s.Sides[0].Team[i].HP = s.Sides[0].Team[i].MaxHP / 4
				}
			},
			want:   "wins on total HP remaining",
			winner: 1,
		},
		{
			name:    "dead heat is a draw",
			arrange: func(s *engine.BattleState) {},
			want:    "draw",
			winner:  2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := newMatch(t, "-max-turns", "1")
			meta := readMeta(t, root)
			dir := matchDir(root, "t1")

			var st engine.BattleState
			if err := readJSON(filepath.Join(dir, "state.json"), &st); err != nil {
				t.Fatalf("read state: %v", err)
			}
			c.arrange(&st)
			if err := writeJSON(filepath.Join(dir, "state.json"), &st); err != nil {
				t.Fatalf("write state: %v", err)
			}

			// Both sides idle into the cap with a move that deals no damage,
			// so the position the case arranged is the position adjudicated.
			// Picking that move off the dex rather than hardcoding a slot
			// keeps the case honest if a roster is ever re-cut: an attack here
			// would decide the match by damage and the test would pass while
			// proving nothing.
			if _, err := submit(t, root, "p1", idleAction(t, &st, 0), ""); err == nil {
				t.Fatal("a lone submission should have waited out its timeout")
			}
			out, err := submit(t, root, "p2", idleAction(t, &st, 1), "")
			if err != nil {
				t.Fatalf("p2 act: %v", err)
			}
			if !strings.Contains(out, "Turn cap 1 reached") {
				t.Fatalf("the cap did not adjudicate:\n%s", out)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("verdict does not read %q:\n%s", c.want, out)
			}

			var after engine.BattleState
			if err := readJSON(filepath.Join(dir, "state.json"), &after); err != nil {
				t.Fatalf("read state: %v", err)
			}
			if after.Winner != c.winner {
				t.Errorf("winner = %d, want %d\n%s", after.Winner, c.winner, out)
			}
			// The verdict is printed to both players, so it names codenames.
			for side := 0; side < 2; side++ {
				if strings.Contains(out, meta.Trainers[side].Name) {
					t.Errorf("the decision leaked %q to the players:\n%s",
						meta.Trainers[side].Name, out)
				}
			}
		})
	}
}

// idleAction names a move the active knows that cannot change anyone's HP.
func idleAction(t *testing.T, s *engine.BattleState, side int) string {
	t.Helper()
	dex, err := loadDex(testData)
	if err != nil {
		t.Fatalf("dex: %v", err)
	}
	act := s.Sides[side].Team[s.Sides[side].Active]
	for i, ms := range act.Moves {
		m, ok := dex.Moves[ms.MoveID]
		if ok && m.Power == 0 {
			return fmt.Sprintf("move:%d", i)
		}
	}
	t.Fatalf("side %d's active knows no damage-free move; this case needs one", side)
	return ""
}

// benchFaint knocks out one benched Pokémon, leaving both actives standing so
// the match stays in the choosing phase.
func benchFaint(s *engine.BattleState, side int) {
	sd := &s.Sides[side]
	for i := range sd.Team {
		if i == sd.Active {
			continue
		}
		sd.Team[i].HP = 0
		sd.Team[i].Fainted = true
		return
	}
}

// TestTurnCapEndsTheMatch: the cap is enforced by the broker, not the engine,
// so a match that reaches it has to come out of `act` already over.
func TestTurnCapEndsTheMatch(t *testing.T) {
	root := newMatch(t, "-max-turns", "1")
	if _, err := submit(t, root, "p1", "move:0", ""); err == nil {
		t.Fatal("a lone submission should have waited out its timeout")
	}
	out, err := submit(t, root, "p2", "move:0", "")
	if err != nil {
		t.Fatalf("p2 act: %v", err)
	}
	if !strings.Contains(out, "Turn cap 1 reached") {
		t.Errorf("the cap did not adjudicate:\n%s", out)
	}
	if !strings.Contains(out, "BATTLE OVER") {
		t.Errorf("the match did not end at the cap:\n%s", out)
	}
	var st engine.BattleState
	if err := readJSON(filepath.Join(matchDir(root, "t1"), "state.json"), &st); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if !st.Ended() {
		t.Errorf("state after the cap: phase %s, winner %d — want an ended match",
			st.Phase, st.Winner)
	}
}

// --- validate ---

// inertBenchTeam is a purpose-built roster carrying one ability the engine
// still does not model (Hypno's Forewarn).
//
// The two tests below used to point at the-caltrops.json, because that roster's
// Weezing was the last inert pick in the tournament. Implementing Neutralizing
// Gas emptied it, and both tests failed — not because `validate` broke, but
// because the fixture had quietly been "whichever real team happens to still be
// built on nothing". That is a fixture that expires. This one does not: it
// exists to be unsound, and the day the slug is implemented the fix is to move
// this one slot to the next inert one rather than to go hunting. That has now
// happened once as designed: the slot was Aerodactyl's Unnerve until Unnerve
// was implemented, and moving it to Forewarn took one line of fixture.
func inertBenchTeam() string { return filepath.Join("testdata", "inert-bench.json") }

// TestValidateWarnsOnInertMechanics: a legal roster can still be built on
// nothing. The Caltrops brought Weezing for Neutralizing Gas, switched it in to
// suppress an ability, and lost the Pokémon to an ability that was never
// suppressed — `validate` said the team was legal and nothing said the
// mechanic was inert.
func TestValidateWarnsOnInertMechanics(t *testing.T) {
	out, err := capture(t, func() error {
		return cmdValidate([]string{"-data", testData, "-team", inertBenchTeam()})
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !strings.Contains(out, "WARN") || !strings.Contains(out, "forewarn") {
		t.Errorf("validate did not warn about the inert ability:\n%s", out)
	}
	// A warning is not a failure: the roster is legal and still prints.
	if !strings.Contains(out, "legal under standard clauses") {
		t.Errorf("validate refused a legal roster over a warning:\n%s", out)
	}
}

// TestValidateStrictFailsOnInertMechanics: -strict is the form the organizer
// can put in front of a tournament, where "legal" is not the bar.
func TestValidateStrictFailsOnInertMechanics(t *testing.T) {
	_, err := capture(t, func() error {
		return cmdValidate([]string{
			"-data", testData, "-strict",
			"-team", inertBenchTeam(),
		})
	})
	if err == nil {
		t.Fatal("-strict passed a roster that depends on an inert ability")
	}
	if !strings.Contains(err.Error(), "does not model") {
		t.Errorf("err = %v, want it to name the inert-mechanic failure", err)
	}
}

// TestCaltropsNoLongerWarns is the other half of the Neutralizing Gas work,
// asserted where the team would actually have found out. The Caltrops' Weezing
// was the single warning left on the six tournament rosters; it is the pick
// that cost them a Pokémon, and it is now backed by a real mechanic.
//
// This is deliberately about that roster and not about the slug: if Neutralizing
// Gas ever regresses to inert, the roster that was harmed by it is the one that
// says so, and -strict — the form an organizer gates a bracket with — is the
// voice it says it in.
func TestCaltropsNoLongerWarns(t *testing.T) {
	out, err := capture(t, func() error {
		return cmdValidate([]string{
			"-data", testData, "-strict",
			"-team", teamPath("the-caltrops.json"),
		})
	})
	if err != nil {
		t.Fatalf("-strict rejected The Caltrops: %v\n%s", err, out)
	}
	if strings.Contains(out, "WARN") {
		t.Errorf("The Caltrops still warns — Neutralizing Gas is inert again:\n%s", out)
	}
}

// TestValidateStaysQuietOnASoundRoster: the warning has to be worth reading,
// which means a roster whose mechanics all exist gets none. Meridian is the
// case that matters: it was the roster built on Harvest, and it is clean now
// only because Harvest was implemented.
func TestValidateStaysQuietOnASoundRoster(t *testing.T) {
	out, err := capture(t, func() error {
		return cmdValidate([]string{
			"-data", testData, "-strict",
			"-team", teamPath("meridian.json"),
		})
	})
	if err != nil {
		t.Fatalf("validate -strict on a sound roster: %v", err)
	}
	if strings.Contains(out, "WARN") {
		t.Errorf("a roster with no inert mechanics still warned:\n%s", out)
	}
}

// --- end to end ---

// playToEnd drives both seats through a whole match the way two agent
// processes would: pick a legal action, submit it, let the broker resolve, and
// go again — including the replace phase, where only the side that lost a
// Pokémon owes anything.
//
// It returns one transcript per seat: everything that seat was shown, and
// nothing the other was. That separation is the point — a player reading its
// own name and theme is its brief, and the same string in the other seat's
// transcript is the leak. The first side to file gets a 1ms wait so its
// submission queues and returns instead of blocking in waitForResolution;
// stdout is process-global, so the two commands cannot be captured apart while
// they overlap. The recap that side would have seen on resolving is covered
// directly by TestPrintRecordsStripsOpponentReasoning, in both directions.
func playToEnd(t *testing.T, root string, maxDecisions int) ([2]string, *engine.BattleState) {
	t.Helper()
	dex, err := loadDex(testData)
	if err != nil {
		t.Fatalf("dex: %v", err)
	}
	var seen [2]strings.Builder
	slots := [2]string{"p1", "p2"}
	for n := 0; n < maxDecisions; n++ {
		var st engine.BattleState
		if err := readJSON(filepath.Join(matchDir(root, "t1"), "state.json"), &st); err != nil {
			t.Fatalf("read state: %v", err)
		}
		if st.Ended() {
			// The ended board is a surface too: both seats render it.
			for side := 0; side < 2; side++ {
				seen[side].WriteString(view(t, root, slots[side]))
			}
			return [2]string{seen[0].String(), seen[1].String()}, &st
		}
		// What each player can see right now goes into its own transcript: a
		// view rendered mid-match is the surface an agent actually reads.
		for side := 0; side < 2; side++ {
			seen[side].WriteString(view(t, root, slots[side]))
		}

		owed := [2]bool{owesAction(&st, 0), owesAction(&st, 1)}
		if !owed[0] && !owed[1] {
			t.Fatalf("decision %d: phase %s and neither side owes an action", n, st.Phase)
		}
		// The side that files first only queues; the second one resolves.
		var order []int
		for side := 0; side < 2; side++ {
			if owed[side] {
				order = append(order, side)
			}
		}
		for i, side := range order {
			wait := "30s"
			if i < len(order)-1 {
				wait = "1ms"
			}
			out, err := capture(t, func() error {
				return cmdAct(actArgs(root, slots[side], firstLegal(t, dex, &st, side), wait))
			})
			seen[side].WriteString(out)
			if err != nil && i == len(order)-1 {
				t.Fatalf("decision %d: %s could not resolve: %v", n, slots[side], err)
			}
		}
	}
	t.Fatalf("match did not finish inside %d decisions", maxDecisions)
	return [2]string{}, nil
}

func actArgs(root, slot, action, timeout string) []string {
	return []string{
		"-root", root, "-data", testData, "-id", "t1",
		"-slot", slot, "-action", action, "-why", "reasoning for " + slot,
		"-timeout", timeout,
	}
}

// firstLegal picks deterministically from what the engine allows, so the match
// plays the same way every run.
func firstLegal(t *testing.T, dex *domain.Dex, s *engine.BattleState, side int) string {
	t.Helper()
	legal := engine.LegalActionsDex(dex, s, side)
	if len(legal) == 0 {
		t.Fatalf("side %d owes an action in phase %s but has none legal", side, s.Phase)
	}
	return fmt.Sprintf("%s:%d", legal[0].Kind, legal[0].Index)
}

// TestFullMatchNeverLeaksIdentity plays a match from `new` to a result through
// the real commands and checks each seat's whole transcript at once.
//
// The per-command tests each pin one surface; this is the one that says those
// surfaces are the *only* surfaces. It is also the only test that exercises a
// match end to end — the replace phase after a faint, the engine's own victory
// line, and the trainer name the engine interpolates into battle text, which
// is the reason the engine is handed a codename at all.
func TestFullMatchNeverLeaksIdentity(t *testing.T) {
	root := newMatch(t, "-max-turns", "40")
	meta := readMeta(t, root)
	seen, st := playToEnd(t, root, 200)

	if !st.Ended() {
		t.Fatalf("match did not end: phase %s", st.Phase)
	}
	// The match has to be a real one, not a one-turn stub: a Pokémon fainted
	// (so the replace phase ran), and it ended on a knockout rather than the
	// turn cap, which is what puts the engine's own victory line in front of
	// both players.
	if !strings.Contains(seen[0]+seen[1], "phase replace") {
		t.Errorf("no replace phase in the whole match — the faint path went untested")
	}
	if st.Winner != 0 && st.Winner != 1 {
		t.Errorf("match ended without a winner (%d); this test wants the KO path", st.Winner)
	}
	if st.Turn < 5 {
		t.Errorf("match ended on turn %d — too short to have exercised much", st.Turn)
	}
	if !strings.Contains(seen[0]+seen[1], "── FOE ACTIVE ──") {
		t.Fatalf("no view was rendered in a whole match")
	}

	for side := 0; side < 2; side++ {
		mine, foe := meta.Trainers[side], meta.Trainers[1-side]
		mySeen := seen[side]
		if strings.Contains(mySeen, foe.Name) {
			t.Errorf("p%d's transcript leaked the opponent's real name %q", side+1, foe.Name)
		}
		if head, _, _ := strings.Cut(foe.Theme, " — "); head != "" && strings.Contains(mySeen, head) {
			t.Errorf("p%d's transcript leaked the opponent's theme %q", side+1, head)
		}
		if !strings.Contains(mySeen, foe.Codename) {
			t.Errorf("p%d never sees the opponent's codename %q", side+1, foe.Codename)
		}
		// Its own brief is still its own.
		if !strings.Contains(mySeen, mine.Name) {
			t.Errorf("p%d cannot see its own name anywhere in a whole match", side+1)
		}
		// Nothing it was shown carries the other seat's reasoning.
		if strings.Contains(mySeen, "reasoning for "+[2]string{"p2", "p1"}[side]) {
			t.Errorf("p%d saw the opponent's reasoning", side+1)
		}
	}

	// The engine's own end-of-battle line interpolates the side's trainer.
	// Reaching a player is the point; reaching one with a real name in it is
	// the leak.
	if st.Winner == 0 || st.Winner == 1 {
		win := meta.Trainers[st.Winner]
		both := seen[0] + seen[1]
		if !strings.Contains(both, "WINNER: "+win.Codename) {
			t.Errorf("the winner was not announced to the players by codename")
		}
		if strings.Contains(both, "WINNER: "+win.Name) {
			t.Errorf("the winner was announced to players by real name")
		}
	}
}

// TestFullMatchRecordIsReadableByTheReportPipeline: log.jsonl is what
// royale/digest.py folds into tournament.json, and it reads each side's
// trainer out of the snapshot. The engine now carries codenames, so snapshot
// takes meta and writes the real name — if that ever silently flipped, the
// published report would be a page of color names.
func TestFullMatchRecordIsReadableByTheReportPipeline(t *testing.T) {
	root := newMatch(t, "-max-turns", "40")
	meta := readMeta(t, root)
	playToEnd(t, root, 200)

	recs, err := readRecords(matchDir(root, "t1"))
	if err != nil {
		t.Fatalf("read records: %v", err)
	}
	if len(recs) < 3 {
		t.Fatalf("a whole match produced %d resolutions", len(recs))
	}
	for _, r := range recs {
		for side := 0; side < 2; side++ {
			if got, want := r.After.Sides[side].Trainer, meta.Trainers[side].Name; got != want {
				t.Fatalf("record %d side %d trainer = %q, want the real name %q "+
					"(digest.py reads this field)", r.N, side, got, want)
			}
			if r.Actions[side] != "" && !strings.Contains(r.Actions[side], "// ") {
				t.Errorf("record %d side %d dropped the reasoning the report needs: %q",
					r.N, side, r.Actions[side])
			}
		}
	}
	if last := recs[len(recs)-1]; last.Winner != 0 && last.Winner != 1 && last.Winner != 2 {
		t.Errorf("the last record has no result: winner = %d", last.Winner)
	}

	// And the judge, unlike the players, sees both identities and both reads.
	out, err := capture(t, func() error {
		return cmdLog([]string{"-root", root, "-data", testData, "-id", "t1", "-token", testToken})
	})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	for _, want := range []string{
		meta.Trainers[0].Name, meta.Trainers[1].Name,
		"reasoning for p1", "reasoning for p2", "codenames:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the referee log is missing %q", want)
		}
	}
}

// TestFullMatchDigestsIntoTheReportPipeline runs the real royale/digest.py over
// a match this test just played. The Go assertions above pin the fields the
// digest reads; this one pins that the digest actually reads them — it is the
// only check that crosses the language boundary, and the snapshot change that
// came with codenames lands squarely on that boundary.
func TestFullMatchDigestsIntoTheReportPipeline(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	root := newMatch(t, "-max-turns", "40")
	meta := readMeta(t, root)
	playToEnd(t, root, 200)

	script, err := filepath.Abs(filepath.Join("..", "..", "royale", "digest.py"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	cmd := exec.Command(py, script, "t1")
	cmd.Env = append(os.Environ(), "ROYALE_ROOT="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("digest.py failed: %v\n%s", err, out)
	}

	var digested struct {
		Matches []struct {
			ID       string `json:"id"`
			Winner   string `json:"winner"`
			Ended    bool   `json:"ended"`
			Trainers []struct {
				Name string `json:"name"`
			} `json:"trainers"`
			TurnsLog []struct {
				Lines   []string `json:"lines"`
				Actions []struct {
					Trainer string `json:"trainer"`
					Why     string `json:"why"`
				} `json:"actions"`
				Sides []struct {
					Trainer string `json:"trainer"`
				} `json:"sides"`
			} `json:"turns_log"`
		} `json:"matches"`
	}
	if err := readJSON(filepath.Join(root, "tournament.json"), &digested); err != nil {
		t.Fatalf("read tournament.json: %v (digest said: %s)", err, out)
	}
	if len(digested.Matches) != 1 {
		t.Fatalf("digest produced %d matches, want 1:\n%s", len(digested.Matches), out)
	}
	m := digested.Matches[0]
	if !m.Ended || m.Winner == "" {
		t.Errorf("digest recorded no result: ended=%v winner=%q", m.Ended, m.Winner)
	}
	if len(m.TurnsLog) < 3 {
		t.Fatalf("digest folded %d turns out of a whole match", len(m.TurnsLog))
	}

	// Everything the report shows reads in real names: the labels come from
	// meta, and the engine's own line text is de-anonymized on the way in.
	for side := 0; side < 2; side++ {
		name, code := meta.Trainers[side].Name, meta.Trainers[side].Codename
		if m.Trainers[side].Name != name {
			t.Errorf("digest trainer %d = %q, want %q", side, m.Trainers[side].Name, name)
		}
		for _, turn := range m.TurnsLog {
			if turn.Sides[side].Trainer != name {
				t.Errorf("digest side %d labeled %q, want the real name %q",
					side, turn.Sides[side].Trainer, name)
			}
			for _, line := range turn.Lines {
				if strings.Contains(line, code) {
					t.Errorf("a report line still carries the codename %q: %q", code, line)
				}
			}
		}
	}

	// Both sides' private reasoning reaches the report, which is the whole
	// reason -why is recorded.
	var withWhy int
	for _, turn := range m.TurnsLog {
		for _, a := range turn.Actions {
			if strings.HasPrefix(a.Why, "reasoning for ") {
				withWhy++
			}
		}
	}
	if withWhy < 2 {
		t.Errorf("only %d actions carried reasoning into the report", withWhy)
	}
}
