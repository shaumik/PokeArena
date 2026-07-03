package mcpserver

import (
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
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
	if _, present := foe["hp_pct"]; !present {
		t.Error("foe.hp_pct missing — the public HP bucket must be sent")
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
