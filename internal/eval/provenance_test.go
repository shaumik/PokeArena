package eval

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// TestLoadProvenance reads the shipped dataset identity and checks the fields
// that pin a result to an exact dataset are present.
func TestLoadProvenance(t *testing.T) {
	p, err := LoadProvenance("../../data")
	if err != nil {
		t.Fatalf("LoadProvenance: %v", err)
	}
	if p.SimVersion == "" {
		t.Fatal("sim_version empty")
	}
	if p.CurationSHA == "" {
		t.Fatal("curation_sha empty")
	}
	if p.SourceGen == 0 {
		t.Fatal("source_gen zero")
	}
}

// TestWriteRunHeader: the header serializes as a single "run"-typed JSON line
// with the config round-tripping intact.
func TestWriteRunHeader(t *testing.T) {
	h := RunHeader{
		EngineRevision:  "abc123",
		DataSimVersion:  "0.10.9",
		Level:           50,
		Ruleset:         Ruleset(),
		TeamLibrary:     "v1",
		Teams:           []string{"Genesis", "Spectrum"},
		Contestants:     []string{"random", "expectimax"},
		ExpectimaxDepth: 2,
		GamesPerPairing: 20,
		Orientations:    2,
		Seeds:           "0..19",
	}
	var buf bytes.Buffer
	if err := WriteRunHeader(&buf, h); err != nil {
		t.Fatalf("WriteRunHeader: %v", err)
	}
	if n := strings.Count(strings.TrimSpace(buf.String()), "\n"); n != 0 {
		t.Fatalf("header must be one line, has %d newlines", n)
	}

	var got RunHeader
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if got.Type != "run" {
		t.Fatalf("type = %q, want run", got.Type)
	}
	if got.DataSimVersion != "0.10.9" || got.ExpectimaxDepth != 2 || len(got.Teams) != 2 {
		t.Fatalf("header did not round-trip: %+v", got)
	}
	if !strings.Contains(got.Ruleset, "L50") {
		t.Fatalf("ruleset missing level: %q", got.Ruleset)
	}
}

// TestRulesetDescribesPermissionsOnly guards the split that PR-3 introduced:
// Ruleset() states what the format allows, TeamProfile() states what a run's
// teams actually did with it. The previous string mixed them and silently went
// stale twice — once when items shipped, once when spreads did.
//
// Asserting the *absence* of the old claims is the point. A future edit that
// re-adds "EV0" or "no items" to the permissions line is exactly the
// regression, and it would otherwise pass every other test in the package.
func TestRulesetDescribesPermissionsOnly(t *testing.T) {
	rs := Ruleset()
	for _, stale := range []string{"EV0", "IV31", "neutral nature", "no items"} {
		if strings.Contains(rs, stale) {
			t.Errorf("ruleset %q claims %q — that describes the teams, not the format; "+
				"put it in TeamProfile", rs, stale)
		}
	}
	// The caps it does state must be the engine's, not literals typed twice.
	for _, want := range []string{"252", "510", "0-31"} {
		if !strings.Contains(rs, want) {
			t.Errorf("ruleset %q does not state %q", rs, want)
		}
	}
}

// TestTeamProfileCounts checks the derived half against hand-built teams, so
// the number in a run header can be trusted without re-reading the library.
func TestTeamProfileCounts(t *testing.T) {
	evs := domain.Stats{HP: 252, Atk: 252, Spe: 4}
	lowSpe := domain.Stats{HP: 31, Atk: 31, Def: 31, SpA: 31, SpD: 31, Spe: 0}
	teams := []NamedTeam{{
		Name: "T",
		Picks: []engine.TeamPick{
			{DexNo: 143, Nature: "adamant", EVs: &evs},
			{DexNo: 6, Nature: "timid"},
			{DexNo: 9, IVs: &lowSpe},
			{DexNo: 3, Item: "leftovers"},
			{DexNo: 12},
		},
	}}
	got := TeamProfile(teams)
	want := "5 picks: 1 EV-trained, 2 natured, 1 custom IVs, 1 holding items"
	if got != want {
		t.Errorf("TeamProfile() = %q, want %q", got, want)
	}

	// A pick carrying an explicit all-31 IV spread is not a "custom" one — it
	// is the default written out longhand, which the web builder never sends
	// but a hand-authored file might.
	dflt := domain.Uniform(31)
	if got := TeamProfile([]NamedTeam{{Picks: []engine.TeamPick{{DexNo: 143, IVs: &dflt}}}}); !strings.Contains(got, "0 custom IVs") {
		t.Errorf("explicit default IVs counted as custom: %q", got)
	}

	if got := TeamProfile(nil); got != "no picks" {
		t.Errorf("TeamProfile(nil) = %q, want %q", got, "no picks")
	}
}
