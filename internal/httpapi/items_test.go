package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

func loadDexForItems(t *testing.T) *domain.Dex {
	t.Helper()
	d, err := domain.LoadDex("../../data", "test")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	return d
}

// TestHandleItems_ServesTheLegalItemSet: this endpoint is the team builder's
// only source of truth for what may go in picks[].item, so every row it returns
// has to be something ValidateTeam accepts. An endpoint that advertised an item
// the validator rejects would show up as a builder that lets you assemble a team
// and then refuses to start the battle.
//
// handleItems reads only s.dex, so the handler is exercised directly rather than
// standing up the whole gateway (which wants Postgres, Redis and RabbitMQ).
func TestHandleItems_ServesTheLegalItemSet(t *testing.T) {
	d := loadDexForItems(t)
	s := &Server{dex: d}

	rr := httptest.NewRecorder()
	s.handleItems(rr, httptest.NewRequest(http.MethodGet, "/api/items", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var rows []engine.ItemInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode body: %v (body: %s)", err, rr.Body.String())
	}
	if len(rows) == 0 {
		t.Fatal("no items served — the builder would have nothing to offer")
	}
	if len(rows) != len(d.Items) {
		t.Errorf("served %d items for a catalog of %d", len(rows), len(d.Items))
	}

	// Every advertised item must be legal on a real team, and must carry the
	// name and description a picker needs to render it.
	sp := d.Species[143] // Snorlax; items are not species-restricted
	for i, r := range rows {
		if i > 0 && rows[i-1].ID >= r.ID {
			t.Errorf("rows not sorted by id: %q then %q", rows[i-1].ID, r.ID)
		}
		if r.Name == "" {
			t.Errorf("item %q served with no display name", r.ID)
		}
		if r.Desc == "" {
			t.Errorf("item %q served with no description — the builder labels it cosmetic", r.ID)
		}
		pick := engine.TeamPick{DexNo: sp.DexNo, MoveIDs: []string{sp.Moves[0]}, Item: r.ID}
		team := make([]engine.TeamPick, 0, engine.TeamSize)
		team = append(team, pick)
		// Fill the rest of the team with distinct species so only the item under
		// test can be the reason ValidateTeam refuses.
		for _, dn := range []int{6, 9, 65, 94, 112} {
			other := d.Species[dn]
			team = append(team, engine.TeamPick{DexNo: dn, MoveIDs: []string{other.Moves[0]}})
		}
		if err := engine.ValidateTeam(team, d); err != nil {
			t.Errorf("served item %q is not legal on a team: %v", r.ID, err)
		}
	}
}

// TestHandleItems_RegisteredOnTheRouter guards the wiring, not the handler: a
// handler nobody routed to is invisible until someone opens the builder.
func TestHandleItems_RegisteredOnTheRouter(t *testing.T) {
	s := &Server{dex: loadDexForItems(t), webDir: t.TempDir()}

	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/items", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/items through the router = %d, want 200", rr.Code)
	}
}
