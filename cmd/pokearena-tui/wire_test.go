package main

import (
	"encoding/json"
	"testing"

	"pokearena"
	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

// TestWireDecodeKeepsFoeHPPct is the regression test for the whole reason this
// binary has its own wire types: ai.View redacts the foe to a percentage on the
// way out (MarshalJSON → foe.hp_pct), but has no field to receive hp_pct on the
// way back in. A client that decodes the foe into engine.Pokemon therefore
// loses the opponent's HP entirely. The TUI's foeView captures it instead — the
// same field the browser SPA reads. This test asserts both halves so the design
// can't silently regress if engine.Pokemon ever changes shape.
func TestWireDecodeKeepsFoeHPPct(t *testing.T) {
	v := ai.View{
		Me: 0,
		Self: engine.Side{
			Trainer: "Blue",
			Active:  0,
			Team: []engine.Pokemon{{
				Name: "Blastoise", Type1: "water", MaxHP: 204, HP: 182,
				Moves: []engine.MoveSlot{{MoveID: "surf", PP: 24, MaxPP: 24}},
			}},
		},
		Foe: engine.Pokemon{
			Name: "Gengar", Type1: "ghost", Type2: "poison",
			MaxHP: 200, HP: 110, // → 55%
			Moves: []engine.MoveSlot{
				{MoveID: "shadowball", PP: 5, MaxPP: 8}, // revealed
				{MoveID: "", PP: 0, MaxPP: 0},           // hidden
			},
		},
		FoeBenchAlive: 3,
		Phase:         engine.PhaseChoosing,
		Turn:          12,
	}

	// Serialize exactly as the gateway would: a MatchUpdate carrying the View.
	data, err := json.Marshal(protocol.MatchUpdate{Type: protocol.FrameState, View: &v})
	if err != nil {
		t.Fatalf("marshal MatchUpdate: %v", err)
	}

	// The TUI's faithful decode keeps the percentage and the revealed move id.
	var f frame
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal into frame: %v", err)
	}
	if f.View == nil {
		t.Fatal("frame.View is nil")
	}
	if got := f.View.Foe.HPPct; got != 55 {
		t.Errorf("foe hp_pct = %d, want 55", got)
	}
	if got := f.View.Foe.Moves[0].MoveID; got != "shadowball" {
		t.Errorf("revealed foe move = %q, want shadowball", got)
	}
	if got := f.View.Foe.Moves[1].MoveID; got != "" {
		t.Errorf("hidden foe move = %q, want empty", got)
	}
	if got := f.View.Self.Team[0].HP; got != 182 {
		t.Errorf("self HP = %d, want exact 182", got)
	}

	// The contrast: decode the SAME bytes into ai.View (what gwclient does) and
	// the foe HP is simply gone — there is no field to hold hp_pct.
	var mu protocol.MatchUpdate
	if err := json.Unmarshal(data, &mu); err != nil {
		t.Fatalf("unmarshal into MatchUpdate: %v", err)
	}
	if mu.View.Foe.HP != 0 || mu.View.Foe.MaxHP != 0 {
		t.Errorf("expected ai.View to LOSE foe HP on decode, got HP=%d MaxHP=%d",
			mu.View.Foe.HP, mu.View.Foe.MaxHP)
	}
}

// TestDecodedViewDrivesLegalActions is an end-to-end-ish check: build a real
// battle, project a fog-of-war view, push it through the gateway's wire
// serialization, decode it with the TUI's types, and confirm the reconstructed
// ai.View still yields legal actions. This proves toAIView feeds the engine's
// one-and-only legality rule set correctly on decoded data.
func TestDecodedViewDrivesLegalActions(t *testing.T) {
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	state, err := engine.NewBattle(dex, "test", "Blue", []int{3, 6, 9}, "Red", []int{12, 15, 18}, 42)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}

	view := ai.MakeView(state, 0)
	data, err := json.Marshal(protocol.MatchUpdate{Type: protocol.FrameState, View: &view})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var f frame
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	acts := ai.LegalActions(f.View.toAIView())
	if len(acts) == 0 {
		t.Fatal("decoded view produced no legal actions")
	}
	// A full-HP foe should read as 100%, and our own HP stays exact.
	if f.View.Foe.HPPct != 100 {
		t.Errorf("full-HP foe hp_pct = %d, want 100", f.View.Foe.HPPct)
	}
	if f.View.Self.Team[0].HP != state.Sides[0].Team[0].HP {
		t.Errorf("self HP not preserved: got %d, want %d",
			f.View.Self.Team[0].HP, state.Sides[0].Team[0].HP)
	}
}
