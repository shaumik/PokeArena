package mcpserver

import (
	"encoding/json"
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
	"pokearena/internal/gwclient"
	"pokearena/internal/protocol"
)

// TestViewWire_RedactsFoeSoSchemaCannotRequireHiddenFields is the regression
// guard for the bug that blocked all live MCP play: the tools declared their
// output as a typed ai.View, so the SDK reflected a schema requiring foe move
// pp/max_pp (and exact foe HP) — fields the fog-of-war MarshalJSON omits. Every
// view containing a foe was then rejected as "missing properties: pp, max_pp".
// viewWire must emit the redacted wire form as a generic object: foe moves carry
// move_id only, and the foe carries hp_pct rather than exact hp. If any of these
// hidden fields reappears, the typed-schema mismatch is back.
func TestViewWire_RedactsFoeSoSchemaCannotRequireHiddenFields(t *testing.T) {
	v := ai.View{
		Turn:          3,
		FoeBenchAlive: 2,
		Foe: engine.Pokemon{
			Name:  "Gengar",
			Type1: "ghost",
			Type2: "poison",
			HP:    120,
			MaxHP: 240,
			Moves: []engine.MoveSlot{
				{MoveID: "shadow-ball", PP: 15, MaxPP: 15}, // revealed
				{MoveID: "", PP: 0, MaxPP: 0},              // unrevealed slot
			},
		},
	}

	m := viewWire(v)

	foe, ok := m["foe"].(map[string]any)
	if !ok {
		t.Fatalf("foe missing or not an object: %T", m["foe"])
	}

	// Exact foe HP is hidden; only the bucketed percentage is public.
	if _, present := foe["hp"]; present {
		t.Error("foe.hp present — exact foe HP must be redacted")
	}
	if _, present := foe["max_hp"]; present {
		t.Error("foe.max_hp present — exact foe max HP must be redacted")
	}
	// Assert the VALUE, not just presence: 120/240 must read as 50%. A
	// presence-only check let the hp_pct:0 regression (a live foe reading as
	// fainted) slip past a green suite once already.
	if got := foe["hp_pct"]; got != float64(50) {
		t.Errorf("foe.hp_pct = %v, want 50 (120/240)", got)
	}

	moves, ok := foe["moves"].([]any)
	if !ok {
		t.Fatalf("foe.moves missing or not an array: %T", foe["moves"])
	}
	if len(moves) != 2 {
		t.Fatalf("foe.moves length = %d, want 2 (slot count is preserved)", len(moves))
	}
	for i, raw := range moves {
		mv, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("foe.moves[%d] not an object: %T", i, raw)
		}
		if _, present := mv["move_id"]; !present {
			t.Errorf("foe.moves[%d] missing move_id", i)
		}
		// The exact fields the SDK schema demanded — they must NOT appear, or
		// the reflected-schema rejection returns.
		if _, present := mv["pp"]; present {
			t.Errorf("foe.moves[%d] leaks pp — foe move PP is hidden information", i)
		}
		if _, present := mv["max_pp"]; present {
			t.Errorf("foe.moves[%d] leaks max_pp — foe move PP is hidden information", i)
		}
	}

	// The revealed slot keeps its identity; the unrevealed one is blank.
	if got := moves[0].(map[string]any)["move_id"]; got != "shadow-ball" {
		t.Errorf("revealed move_id = %v, want shadow-ball", got)
	}
	if got := moves[1].(map[string]any)["move_id"]; got != "" {
		t.Errorf("unrevealed move_id = %v, want empty", got)
	}
}

// TestViewTool_PreservesFoeHPPctThroughWireDecode reproduces the live path the
// hand-built fixture above never exercises: the gateway marshals a redacted
// view (foe carries hp_pct, no exact hp), the client DECODES that frame, and
// the tool sends the view onward. The `view` tool forwards the raw server bytes
// (ViewWire → wireOut → latestRaw) so the redaction survives byte-for-byte; and
// because ai.View.UnmarshalJSON now recovers hp_pct into the typed foe, even the
// re-marshal fallback is correct — the decode boundary is no longer lossy.
func TestViewTool_PreservesFoeHPPctThroughWireDecode(t *testing.T) {
	// Server side: a fresh view with a live foe at 120/240. MarshalJSON redacts
	// it into the wire form (hp_pct:50, no hp/max_hp).
	serverView := ai.View{
		Turn: 5,
		Foe: engine.Pokemon{
			Name: "Snorlax", HP: 120, MaxHP: 240,
			Moves: []engine.MoveSlot{{MoveID: "body-slam", PP: 20, MaxPP: 20}},
		},
	}
	wire, err := json.Marshal(serverView)
	if err != nil {
		t.Fatalf("marshal server view: %v", err)
	}

	// Client side: decode the frame exactly as the dispatcher does, capturing
	// both the typed View and the raw bytes.
	frame := []byte(`{"type":"state","view":` + string(wire) + `}`)
	var mu protocol.MatchUpdate
	if err := json.Unmarshal(frame, &mu); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	// The typed decode recovers the foe's public HP as hp_pct out of 100 (no
	// exact hp on the wire), so a live foe is never zeroed to a fainted reading.
	if mu.View == nil || mu.View.Foe.HP != 50 || mu.View.Foe.MaxHP != 100 {
		t.Fatalf("typed decode should recover foe HP as 50/100, got %+v", mu.View)
	}

	s := &session{client: &gwclient.Client{}, latest: mu.View, latestRaw: mu.RawView}
	m, err := s.ViewWire()
	if err != nil {
		t.Fatalf("ViewWire: %v", err)
	}
	foe, ok := m["foe"].(map[string]any)
	if !ok {
		t.Fatalf("foe missing or not an object: %T", m["foe"])
	}
	if got := foe["hp_pct"]; got != float64(50) {
		t.Errorf("foe hp_pct = %v, want 50 — the view tool dropped the wire HP%%", got)
	}
	// Exact HP stays redacted on the forwarded bytes.
	if _, present := foe["hp"]; present {
		t.Error("foe.hp leaked through the raw forward")
	}

	// Belt-and-suspenders: re-marshaling the decoded typed view now ALSO yields
	// the right percentage (50), because UnmarshalJSON recovered it. The raw
	// forward remains the primary path (it preserves the server's exact bytes),
	// but the re-marshal fallback is no longer a latent hp_pct:0 trap.
	remarshaled := viewWire(*mu.View)["foe"].(map[string]any)
	if got := remarshaled["hp_pct"]; got != float64(50) {
		t.Errorf("re-marshaled decoded view hp_pct = %v, want 50 (decode is lossless now)", got)
	}
}
