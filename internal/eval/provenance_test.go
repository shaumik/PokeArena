package eval

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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
