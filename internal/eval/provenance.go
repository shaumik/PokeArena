package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"pokearena/internal/engine"
)

// The run header is the first line of every JSONL trace. It pins everything a
// third party needs to reproduce the run bit-for-bit: the code revision, the
// exact dataset, the ruleset, and the full run configuration. Reproducibility
// is the benchmark's headline claim, and a trace that can't say what produced
// it can't back that claim — so we make the trace self-describing.
//
// The reproducible payload is the game/decision rows: same code revision + same
// dataset + same config yields byte-identical rows. The header's timestamp is
// provenance only and legitimately differs between runs.

// Provenance mirrors data/_provenance.json — the dataset's identity.
type Provenance struct {
	SourceGen   int    `json:"source_gen"`
	SimVersion  string `json:"sim_version"`
	CurationSHA string `json:"curation_sha"`
	SyncedAt    string `json:"synced_at"`
}

// LoadProvenance reads _provenance.json from the dataset directory.
func LoadProvenance(dataDir string) (Provenance, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "_provenance.json"))
	if err != nil {
		return Provenance{}, fmt.Errorf("read provenance: %w", err)
	}
	var p Provenance
	if err := json.Unmarshal(raw, &p); err != nil {
		return Provenance{}, fmt.Errorf("parse provenance: %w", err)
	}
	return p, nil
}

// EngineRevision returns the VCS revision the binary was built from (with a
// "-dirty" suffix when the tree had uncommitted changes), or "unknown" if the
// build carries no VCS stamp. `go build` embeds this by default; it is what
// lets a result name the exact code that produced it.
func EngineRevision() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	var rev string
	var dirty bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "unknown"
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

// Ruleset is the human-readable descriptor of the fixed battle rules. IV 31 /
// EV 0 is baked into the engine's stat formula (see calcStat); nature is
// neutral; no items in battle yet.
func Ruleset() string {
	return fmt.Sprintf("L%d, IV31/EV0, neutral nature, no items, Species Clause, mirror match", engine.Level)
}

// RunHeader is the "run" row: dataset + code + ruleset + full configuration.
type RunHeader struct {
	Type            string   `json:"type"` // always "run"
	EngineRevision  string   `json:"engine_revision"`
	DataSimVersion  string   `json:"data_sim_version"`
	DataCurationSHA string   `json:"data_curation_sha"`
	DataSourceGen   int      `json:"data_source_gen"`
	Level           int      `json:"level"`
	Ruleset         string   `json:"ruleset"`
	TeamLibrary     string   `json:"team_library"`
	Teams           []string `json:"teams"`
	Contestants     []string `json:"contestants"`
	ExpectimaxDepth int      `json:"expectimax_depth"`
	GamesPerPairing int      `json:"games_per_pairing"`
	Orientations    int      `json:"orientations"`
	Seeds           string   `json:"seeds"`
	BudgetMs        int      `json:"budget_ms"`
	Timestamp       string   `json:"timestamp,omitempty"` // provenance only; not part of the reproducible payload
}

// WriteRunHeader emits the header as the first JSONL line of a trace.
func WriteRunHeader(w io.Writer, h RunHeader) error {
	h.Type = "run"
	return json.NewEncoder(w).Encode(h)
}
