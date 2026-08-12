package eval

import (
	"strings"
	"testing"
)

func testConfig() BenchConfig {
	return BenchConfig{
		Entrants: []Entrant{
			{ID: "claude-code/opus", Harness: "claude", Model: "opus"},
			{ID: "gemini-cli/3.1-pro", Harness: "agy", Model: "Gemini 3.1 Pro (High)"},
			{ID: "codex/gpt-5", Harness: "codex", Model: "gpt-5"},
		},
		Teams:        []string{"Genesis", "Keystone"},
		GamesPerTeam: 2,
	}
}

func TestBuildPlan_EnumeratesTheWholeMatrix(t *testing.T) {
	plan := BuildPlan(testConfig())
	if want := 3 * 2 * 2; len(plan) != want {
		t.Fatalf("plan has %d games, want %d", len(plan), want)
	}
	// Labels are the resume key, so they must be unique across the matrix.
	seen := map[string]bool{}
	for _, g := range plan {
		if seen[g.Label] {
			t.Fatalf("duplicate label %q — resume would skip a game that never ran", g.Label)
		}
		seen[g.Label] = true
	}
}

// The plan must be a pure function of the config: a run resumed tomorrow has to
// rebuild exactly the same list, or the labels stop matching and finished games
// are replayed.
func TestBuildPlan_IsDeterministic(t *testing.T) {
	a := BuildPlan(testConfig())
	b := BuildPlan(testConfig())
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Label != b[i].Label {
			t.Fatalf("game %d differs between builds: %q vs %q", i, a[i].Label, b[i].Label)
		}
	}
}

// A label becomes a filename, so it must survive ids and team names containing
// spaces, slashes, and mixed case.
func TestGameLabel_IsPathSafeAndStable(t *testing.T) {
	got := GameLabel("claude-code/opus", "Team Alpha", 3)
	if strings.ContainsAny(got, " /\\") {
		t.Errorf("label %q contains a path-unsafe character", got)
	}
	if again := GameLabel("claude-code/opus", "Team Alpha", 3); again != got {
		t.Errorf("label not stable: %q vs %q", got, again)
	}
	// Sanitizing is lossy, so distinct ids CAN collide into one label. That is
	// caught at config validation rather than papered over here — see
	// TestBenchConfig_Validate/colliding ids. Different indexes and teams,
	// though, must always stay distinct.
	if GameLabel("x", "T", 1) == GameLabel("x", "T", 2) {
		t.Error("game index does not affect the label")
	}
	if GameLabel("x", "A", 1) == GameLabel("x", "B", 1) {
		t.Error("team does not affect the label")
	}
}

func TestRemaining_SkipsCompletedGames(t *testing.T) {
	plan := BuildPlan(testConfig())
	done := map[string]bool{plan[0].Label: true, plan[5].Label: true}

	left := Remaining(plan, done)
	if len(left) != len(plan)-2 {
		t.Fatalf("remaining %d, want %d", len(left), len(plan)-2)
	}
	for _, g := range left {
		if done[g.Label] {
			t.Errorf("completed game %q is still in the remaining list", g.Label)
		}
	}
}

// A long batch usually gets cut short. In plan order that leaves the first
// contestant complete and the last with nothing, which is not a comparison —
// so any prefix of the interleaved order must be balanced.
func TestInterleaved_AnyPrefixIsABalancedSample(t *testing.T) {
	plan := Interleaved(BuildPlan(testConfig()))
	if len(plan) != 12 {
		t.Fatalf("interleaving changed the game count: %d", len(plan))
	}

	counts := map[string]int{}
	for i, g := range plan {
		counts[g.Entrant.ID]++
		// After every complete round (3 contestants), the counts must be level.
		if (i+1)%3 == 0 {
			var min, max int
			first := true
			for _, n := range counts {
				if first {
					min, max, first = n, n, false
				}
				if n < min {
					min = n
				}
				if n > max {
					max = n
				}
			}
			if max-min > 0 {
				t.Fatalf("after %d games the sample is unbalanced: %v", i+1, counts)
			}
		}
	}
	// And no game may be lost or duplicated by the reordering.
	if len(counts) != 3 {
		t.Errorf("want 3 contestants, got %d", len(counts))
	}
	for id, n := range counts {
		if n != 4 {
			t.Errorf("contestant %q has %d games, want 4", id, n)
		}
	}
}

func TestInterleaved_HandlesUnevenContestants(t *testing.T) {
	// One contestant with fewer games must not stall the round-robin.
	plan := []PlannedGame{
		{Entrant: Entrant{ID: "a"}, Label: "a1"},
		{Entrant: Entrant{ID: "a"}, Label: "a2"},
		{Entrant: Entrant{ID: "a"}, Label: "a3"},
		{Entrant: Entrant{ID: "b"}, Label: "b1"},
	}
	got := Interleaved(plan)
	if len(got) != 4 {
		t.Fatalf("lost games: got %d, want 4", len(got))
	}
	if got[0].Label != "a1" || got[1].Label != "b1" || got[2].Label != "a2" || got[3].Label != "a3" {
		var labels []string
		for _, g := range got {
			labels = append(labels, g.Label)
		}
		t.Errorf("unexpected order: %v", labels)
	}
}

func TestBenchConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*BenchConfig)
		wantErr string
	}{
		{"valid", func(*BenchConfig) {}, ""},
		{"no contestants", func(c *BenchConfig) { c.Entrants = nil }, "no contestants"},
		{"no teams", func(c *BenchConfig) { c.Teams = nil }, "no teams"},
		{"zero games", func(c *BenchConfig) { c.GamesPerTeam = 0 }, "games_per_team"},
		{"empty id", func(c *BenchConfig) { c.Entrants[0].ID = "" }, "empty id"},
		{"empty harness", func(c *BenchConfig) { c.Entrants[0].Harness = "" }, "empty harness"},
		{"empty model", func(c *BenchConfig) { c.Entrants[0].Model = "" }, "empty model"},
		{
			// Two rows with the same id would pool different configurations
			// into one line of the results table.
			"duplicate id",
			func(c *BenchConfig) { c.Entrants[1].ID = c.Entrants[0].ID },
			"duplicate contestant id",
		},
		{
			// Distinct ids that reduce to the same filename would silently
			// overwrite each other's results.
			"colliding ids",
			func(c *BenchConfig) {
				c.Entrants[0].ID = "a/b"
				c.Entrants[1].ID = "a-b"
			},
			"would share result files",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := testConfig()
			c.mutate(&cfg)
			err := cfg.Validate()
			switch {
			case c.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case c.wantErr != "" && err == nil:
				t.Errorf("want error containing %q, got nil", c.wantErr)
			case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
				t.Errorf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestSummarize_CountsRemaining(t *testing.T) {
	plan := BuildPlan(testConfig())
	done := map[string]bool{plan[0].Label: true, plan[1].Label: true, plan[2].Label: true}

	s := Summarize(plan, done)
	if s.Entrants != 3 || s.Teams != 2 || s.Total != 12 {
		t.Errorf("summary shape wrong: %+v", s)
	}
	if s.PerEntrant != 4 {
		t.Errorf("per-contestant = %d, want 4", s.PerEntrant)
	}
	if s.Remaining != 9 {
		t.Errorf("remaining = %d, want 9", s.Remaining)
	}
}
