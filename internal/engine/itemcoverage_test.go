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

// TestItemCatalogJoinsRegistry: the catalog endpoint's rows are the legal
// values of TeamPick.Item, so they must be exactly the catalog's items — no
// more (an item the builder offers but ValidateTeam rejects) and no fewer (a
// legal item the builder can't discover). Modeled items must carry a
// description; the empty-Desc case is reserved for inert holds, which
// TestItemCoverage tracks separately.
func TestItemCatalogJoinsRegistry(t *testing.T) {
	d := loadDex(t)
	rows := ItemCatalog(d)

	if len(rows) != len(d.Items) {
		t.Fatalf("ItemCatalog returned %d rows for %d catalog items", len(rows), len(d.Items))
	}
	seen := map[string]bool{}
	for i, r := range rows {
		if i > 0 && rows[i-1].ID >= r.ID {
			t.Errorf("ItemCatalog not sorted by id: %q then %q", rows[i-1].ID, r.ID)
		}
		if seen[r.ID] {
			t.Errorf("ItemCatalog repeated id %q", r.ID)
		}
		seen[r.ID] = true
		cat, ok := d.Items[r.ID]
		if !ok {
			t.Errorf("ItemCatalog offers %q, which is not in the catalog", r.ID)
			continue
		}
		if r.Name != cat.Name {
			t.Errorf("item %q name = %q, want the catalog's %q", r.ID, r.Name, cat.Name)
		}
		if _, modeled := itemRegistry[ItemKind(r.ID)]; modeled && r.Desc == "" {
			t.Errorf("item %q is modeled but has no Desc — the builder would label it cosmetic", r.ID)
		}
	}
	for id := range d.Items {
		if !seen[id] {
			t.Errorf("catalog item %q is missing from ItemCatalog — the builder can't offer it", id)
		}
	}
}

// TestItemNamesMatchCatalog: the registry duplicates each item's display name
// so log lines can be built without a Dex in hand. Duplication is only safe if
// it can't drift, which is what this asserts.
func TestItemNamesMatchCatalog(t *testing.T) {
	d := loadDex(t)
	for _, k := range itemRegistryKinds() {
		reg := itemRegistry[ItemKind(k)]
		cat, ok := d.Items[k]
		if !ok {
			continue // TestItemRegistrySubsetOfCatalog owns this failure
		}
		if reg.Name == "" {
			t.Errorf("itemRegistry[%q] has no Name — log lines would read blank", k)
			continue
		}
		if reg.Name != cat.Name {
			t.Errorf("itemRegistry[%q].Name = %q but the catalog says %q", k, reg.Name, cat.Name)
		}
	}
}

// TestItemRegistryKindMatchesKey: every entry's Kind field must equal its map
// key. A copy-paste entry with the wrong Kind still fires its hooks (lookup is
// by key) but reports the wrong identity to anything that reads Item.Kind,
// which is exactly the kind of bug that survives a full green test run.
func TestItemRegistryKindMatchesKey(t *testing.T) {
	for key, it := range itemRegistry {
		if it.Kind != key {
			t.Errorf("itemRegistry[%q].Kind = %q — must match its key", key, it.Kind)
		}
	}
}
