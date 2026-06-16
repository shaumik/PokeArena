package engine

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

const itemCoverageFixture = "testdata/item_coverage.json"

// TestItemCoverage is the held-item parity guard, mirroring TestMoveCoverage.
// The committed fixture is the snapshot of catalog items the engine doesn't
// model yet (inert holds). Any drift — a new inert item added to the catalog,
// or an item whose behavior got wired (dropping it from the list) — fails
// until the fixture is regenerated and reviewed:
//
//	go test ./internal/engine -run TestItemCoverage -update-coverage
//
// (reuses the -update-coverage flag declared in coverage_test.go).
func TestItemCoverage(t *testing.T) {
	d := loadDex(t)
	gaps := AuditItems(d)

	got, err := json.MarshalIndent(gaps, "", "  ")
	if err != nil {
		t.Fatalf("marshal gaps: %v", err)
	}
	got = append(got, '\n')

	if *updateCoverage {
		if err := os.WriteFile(itemCoverageFixture, got, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		t.Logf("rewrote %s (%d inert items)", itemCoverageFixture, len(gaps))
		return
	}

	want, err := os.ReadFile(itemCoverageFixture)
	if err != nil {
		t.Fatalf("read fixture: %v (run with -update-coverage to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("item coverage report changed (%d inert items now). "+
			"Run:\n  go test ./internal/engine -run TestItemCoverage -update-coverage\n"+
			"then review the diff in %s before committing.", len(gaps), itemCoverageFixture)
	}
}

// TestItemRegistrySubsetOfCatalog guards the reverse direction: the engine must
// not model an item the curated catalog doesn't ship (an unreachable hook or a
// slug typo). Every itemRegistry key must exist in data/items.json.
func TestItemRegistrySubsetOfCatalog(t *testing.T) {
	d := loadDex(t)
	for _, k := range itemRegistryKinds() {
		if _, ok := d.Items[k]; !ok {
			t.Errorf("itemRegistry models %q but it is not in the catalog (data/items.json)", k)
		}
	}
}
