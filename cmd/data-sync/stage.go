package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// provenance is the sidecar written alongside each refresh. CurationSHA is
// the authoritative identifier for the snapshot — git HEAD at sync time
// pins both the transform code and the curated output together, so there's
// no separate manual data_version to keep in lockstep.
type provenance struct {
	SourceGen    int    `json:"source_gen"`
	SimVersion   string `json:"sim_version"`
	UpstreamMeta string `json:"upstream_refreshed_at"`
	SyncedAt     string `json:"synced_at"`
	CurationSHA  string `json:"curation_sha,omitempty"`
}

// stage writes the transformed bundle plus _provenance.json to a fresh
// .staging/ directory under dataDir. Returns the staging dir path.
func stage(dataDir string, t transformed, meta upstreamMeta) (string, error) {
	staging := filepath.Join(dataDir, ".staging")
	if err := os.RemoveAll(staging); err != nil {
		return "", fmt.Errorf("clear staging: %w", err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", fmt.Errorf("create staging: %w", err)
	}

	if err := writePretty(filepath.Join(staging, "pokedex.json"), t.Pokedex); err != nil {
		return "", err
	}
	if err := writePretty(filepath.Join(staging, "moves.json"), t.Moves); err != nil {
		return "", err
	}
	if err := writePretty(filepath.Join(staging, "typechart.json"), t.Typechart); err != nil {
		return "", err
	}
	if err := writePretty(filepath.Join(staging, "items.json"), t.Items); err != nil {
		return "", err
	}

	prov := provenance{
		SourceGen:    meta.Gen,
		SimVersion:   meta.SimVersion,
		UpstreamMeta: meta.RefreshedAt,
		SyncedAt:     time.Now().UTC().Format(time.RFC3339),
		CurationSHA:  gitSHA(),
	}
	if err := writePretty(filepath.Join(staging, "_provenance.json"), prov); err != nil {
		return "", err
	}

	return staging, nil
}

// gitSHA captures the current git HEAD for the provenance sidecar. Best-
// effort: outside a git checkout we just return "" rather than failing.
func gitSHA() string {
	out, err := exec.CommandContext(context.Background(), "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func writePretty(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
