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
	"io"
	"os"
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
	args := []string{"-root", root, "-data", testData, "-id", "t1",
		"-slot", slot, "-action", action, "-timeout", testWait}
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
// rule in the runbook asking the organiser for neutral names; the fallback is
// what makes the rule unnecessary.
func TestMissingCodenameFallsBackToNeutralLabel(t *testing.T) {
	anon := writeTeamCopy(t, teamPath("meridian.json"), func(tf map[string]any) {
		delete(tf, "codename")
	})
	root := t.TempDir()
	if _, err := capture(t, func() error {
		return cmdNew([]string{"-root", root, "-data", testData, "-id", "t1",
			"-p1", anon, "-p2", teamPath("the-low-ceiling.json"), "-token", testToken})
	}); err != nil {
		t.Fatalf("new: %v", err)
	}
	meta := readMeta(t, root)
	if got := meta.Trainers[0].Codename; got != defaultCodename(0) {
		t.Fatalf("codename with none declared = %q, want the neutral seat label %q",
			got, defaultCodename(0))
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
		return cmdNew([]string{"-root", root, "-data", testData, "-id", "t1",
			"-p1", same, "-p2", teamPath("the-low-ceiling.json"), "-token", testToken})
	})
	if err == nil || !strings.Contains(err.Error(), "codename repeats the team name") {
		t.Fatalf("new with a codename equal to the name: err = %v, want a refusal", err)
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
		if p.Ability != "" && strings.Contains(foeSection, string(p.Ability)) {
			t.Errorf("view leaked benched foe ability %s:\n%s", p.Ability, foeSection)
		}
		if p.Item != "" && strings.Contains(foeSection, string(p.Item)) {
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
	if lead.Ability != "" && strings.Contains(foeSection, string(lead.Ability)) {
		t.Errorf("view leaked the foe active's undisclosed ability %s:\n%s", lead.Ability, foeSection)
	}
	if lead.Item != "" && strings.Contains(foeSection, string(lead.Item)) {
		t.Errorf("view leaked the foe active's undisclosed item %s:\n%s", lead.Item, foeSection)
	}
	if !strings.Contains(foeSection, "(unknown)") {
		t.Errorf("the foe's ability and item are not marked unknown on turn zero:\n%s", foeSection)
	}
}

// --- the reasoning channel ---

// TestPrintRecordsStripsOpponentReasoning: `act -why` is recorded for the
// match report, and a leak here would hand the opponent the reader's entire
// read of the position every single turn. Both directions are checked from one
// record, because the bug this guards is an asymmetry.
func TestPrintRecordsStripsOpponentReasoning(t *testing.T) {
	meta := Meta{Trainers: [2]Trainer{
		{Name: "Meridian", Codename: "Cobalt"},
		{Name: "The Low Ceiling", Codename: "Indigo"},
	}}
	rec := Record{Turn: 3, Actions: [2]string{
		"Ninetales used Fire Blast  // they always Protect turn one",
		"Mr. Mime used Trick Room  // sun is up, invert the speed race",
	}}

	for side := 0; side < 2; side++ {
		out, _ := capture(t, func() error {
			printRecords([]Record{rec}, meta, side)
			return nil
		})
		mine, theirs := rec.Actions[side], rec.Actions[1-side]
		myWhy := mine[strings.Index(mine, "// ")+3:]
		theirWhy := theirs[strings.Index(theirs, "// ")+3:]
		if !strings.Contains(out, myWhy) {
			t.Errorf("side %d: own reasoning %q missing from its own recap:\n%s", side, myWhy, out)
		}
		if strings.Contains(out, theirWhy) {
			t.Errorf("side %d: opponent's reasoning %q leaked:\n%s", side, theirWhy, out)
		}
		if !strings.Contains(out, meta.Trainers[1-side].Codename) {
			t.Errorf("side %d: recap does not name the foe by codename:\n%s", side, out)
		}
		if strings.Contains(out, meta.Trainers[1-side].Name) {
			t.Errorf("side %d: recap leaked the foe's real name:\n%s", side, out)
		}
	}
}

// TestActRecapStripsOpponentReasoning is the same invariant through the real
// command: p1 files a read, p2 resolves the turn, and p2's recap must carry
// the move p1 made without the reasoning behind it.
func TestActRecapStripsOpponentReasoning(t *testing.T) {
	root := newMatch(t)
	if _, err := submit(t, root, "p1", "move:0", "burn it down before it sets up"); err == nil {
		t.Fatal("a lone submission should have waited out its timeout")
	}
	out, err := submit(t, root, "p2", "move:0", "invert the speed race")
	if err != nil {
		t.Fatalf("p2 act: %v", err)
	}
	if strings.Contains(out, "burn it down") {
		t.Errorf("p2's recap leaked p1's reasoning:\n%s", out)
	}
	if !strings.Contains(out, "invert the speed race") {
		t.Errorf("p2's recap dropped p2's own reasoning:\n%s", out)
	}
	if !strings.Contains(out, "turn 0 resolved") {
		t.Errorf("p2's act did not resolve the turn:\n%s", out)
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

// TestAdjudicateOrdersPokemonThenHPThenDraw: a stall mirror can legitimately
// run forever and a tournament needs a result, so past the cap the match is
// decided on Pokémon standing, then on total HP, then called a draw. The
// order is the whole rule: HP must never overturn a Pokémon-count lead.
func TestAdjudicateOrdersPokemonThenHPThenDraw(t *testing.T) {
	dex, err := loadDex(testData)
	if err != nil {
		t.Fatalf("dex: %v", err)
	}

	t.Run("pokemon standing wins first", func(t *testing.T) {
		s := capState(t, dex)
		faint(&s.Sides[1].Team[0])
		// Side 1 is behind on Pokémon but ahead on HP: the count still wins.
		for i := range s.Sides[0].Team {
			s.Sides[0].Team[i].HP = 1
		}
		w, why := adjudicate(s)
		if w != 0 {
			t.Fatalf("winner = %d, want 0 (5 Pokémon to 4): %s", w, why)
		}
		if !strings.Contains(why, "Pokémon remaining") {
			t.Errorf("verdict = %q, want it decided on Pokémon remaining", why)
		}
	})

	t.Run("total HP breaks a tie on count", func(t *testing.T) {
		s := capState(t, dex)
		for i := range s.Sides[0].Team {
			s.Sides[0].Team[i].HP = s.Sides[0].Team[i].MaxHP / 2
		}
		w, why := adjudicate(s)
		if w != 1 {
			t.Fatalf("winner = %d, want 1 (full HP vs half): %s", w, why)
		}
		if !strings.Contains(why, "total HP remaining") {
			t.Errorf("verdict = %q, want it decided on total HP", why)
		}
	})

	t.Run("dead heat is a draw", func(t *testing.T) {
		s := capState(t, dex)
		w, why := adjudicate(s)
		if w != 2 {
			t.Fatalf("winner = %d, want 2 (draw): %s", w, why)
		}
		if !strings.Contains(why, "draw") {
			t.Errorf("verdict = %q, want a draw", why)
		}
	})

	t.Run("the verdict names the winner by codename", func(t *testing.T) {
		s := capState(t, dex)
		faint(&s.Sides[1].Team[0])
		_, why := adjudicate(s)
		if strings.Contains(why, "Meridian") {
			t.Errorf("the adjudication line is printed to both players and leaked "+
				"a real name: %q", why)
		}
	})
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

// capState builds a battle whose two sides are identical, so each subtest can
// introduce exactly the one asymmetry it is about.
func capState(t *testing.T, dex *domain.Dex) *engine.BattleState {
	t.Helper()
	tf, err := readTeamFile(teamPath("meridian.json"))
	if err != nil {
		t.Fatalf("read team: %v", err)
	}
	s, err := engine.NewBattleFromPicks(dex, "cap", "Cobalt", tf.Picks, "Indigo", tf.Picks, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	return s
}

func faint(p *engine.Pokemon) {
	p.HP = 0
	p.Fainted = true
}
