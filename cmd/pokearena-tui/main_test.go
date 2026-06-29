package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokearena/internal/protocol"
)

// TestCreateLiveBattlePostsLiveMode pins the --vs-ai contract with the gateway:
// a POST to /api/battles carrying mode=live and the trainer name, and a parsed
// battle_id plus a ws:// origin back. mode=live is what puts the built-in CPU
// on the other side (it takes slot p2 server-side).
func TestCreateLiveBattlePostsLiveMode(t *testing.T) {
	var gotMethod, gotPath, gotMode, gotName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		gotMode, _ = req["mode"].(string)
		gotName, _ = req["p1_name"].(string)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"battle_id":"abc123","mode":"live","ws_url":"/api/battles/abc123/play"}`))
	}))
	defer srv.Close()

	wsBase, battleID, err := createLiveBattle(srv.URL, "Ash")
	if err != nil {
		t.Fatalf("createLiveBattle: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/battles" {
		t.Errorf("request = %s %s, want POST /api/battles", gotMethod, gotPath)
	}
	if gotMode != "live" {
		t.Errorf("mode = %q, want \"live\" (anything else is not a CPU battle)", gotMode)
	}
	if gotName != "Ash" {
		t.Errorf("p1_name = %q, want \"Ash\"", gotName)
	}
	if battleID != "abc123" {
		t.Errorf("battleID = %q, want abc123", battleID)
	}
	if !strings.HasPrefix(wsBase, "ws://") {
		t.Errorf("wsBase = %q, want a ws:// origin", wsBase)
	}
}

// TestCreateLiveBattleSurfacesGatewayError makes sure a non-201 from the gateway
// becomes an error the caller can die() on, rather than a silent empty battle.
func TestCreateLiveBattleSurfacesGatewayError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no AI team available", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, _, err := createLiveBattle(srv.URL, "Ash"); err == nil {
		t.Fatal("expected an error when the gateway rejects the create, got nil")
	}
}

// TestLiveJoinPathIsTokenless guards the one property that makes a live join
// reach the CPU handler: no slot and no token. A slot/token query (the live_pvp
// shape, protocol.PlayPath) would instead route to the PvP handler and be
// rejected because the battle's mode is "live".
func TestLiveJoinPathIsTokenless(t *testing.T) {
	live := liveJoinPath("abc123")
	if strings.ContainsAny(live, "?") || strings.Contains(live, "slot=") || strings.Contains(live, "token=") {
		t.Errorf("live join path %q must carry no query/slot/token", live)
	}
	if live != "/api/battles/abc123/play" {
		t.Errorf("live join path = %q, want /api/battles/abc123/play", live)
	}
	// Contrast: the live_pvp path does carry slot+token. If that ever stops
	// being true, the two paths have converged and this guard is meaningless.
	pvp := protocol.PlayPath("abc123", "p1", "tok")
	if !strings.Contains(pvp, "slot=") || !strings.Contains(pvp, "token=") {
		t.Fatalf("protocol.PlayPath %q unexpectedly dropped slot/token", pvp)
	}
}
