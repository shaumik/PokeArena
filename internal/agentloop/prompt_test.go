package agentloop

import (
	"encoding/json"
	"strings"
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

// stubDex builds a minimal Dex with only the moves the prompt renderer
// will look up. The renderer never touches species or the type chart.
func stubDex() *domain.Dex {
	return &domain.Dex{
		Moves: map[string]domain.Move{
			"flamethrower": {ID: "flamethrower", Name: "Flamethrower", Type: "Fire", Category: "special", Power: 90, Accuracy: 100},
			"wing-attack":  {ID: "wing-attack", Name: "Wing Attack", Type: "Flying", Category: "physical", Power: 60, Accuracy: 100},
		},
	}
}

func sampleView() ai.View {
	return ai.View{
		Me:   0,
		Turn: 3,
		Self: engine.Side{
			Trainer: "Red",
			Active:  0,
			Team: []engine.Pokemon{
				{
					Name: "Charizard", Type1: "Fire", Type2: "Flying",
					HP: 192, MaxHP: 192, Status: engine.StatusNone,
					Moves: []engine.MoveSlot{
						{MoveID: "flamethrower", PP: 14, MaxPP: 15},
						{MoveID: "wing-attack", PP: 35, MaxPP: 35},
					},
				},
				{Name: "Blastoise", Type1: "Water", HP: 200, MaxHP: 200, Status: engine.StatusNone},
			},
		},
		Foe: engine.Pokemon{
			Name: "Vileplume", Type1: "Grass", Type2: "Poison",
			HP: 150, MaxHP: 200, Status: engine.StatusBurn,
		},
		FoeBenchAlive: 4,
	}
}

func TestRenderUserPrompt_IncludesViewAndActions(t *testing.T) {
	v := sampleView()
	acts := []engine.Action{
		{Kind: engine.ActionMove, Index: 0},
		{Kind: engine.ActionMove, Index: 1},
		{Kind: engine.ActionSwitch, Index: 1},
	}

	got := RenderUserPrompt(stubDex(), v, pctHP(v.Foe.HP, v.Foe.MaxHP), acts)

	for _, want := range []string{
		"Turn 3",
		"YOUR ACTIVE: Charizard (Fire/Flying)",
		"HP 192/192",
		"OPPONENT ACTIVE: Vileplume (Grass/Poison)",
		"HP ~75%", // foe HP is fog-bucketed: a percentage, never a precise-looking count
		"[burn]",
		"Opponent reserve: 4",
		"[0] Move: Flamethrower",
		"[1] Move: Wing Attack",
		"[2] Switch to Blastoise",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q\n---full output---\n%s", want, got)
		}
	}
}

// TestRenderUserPrompt_PublicBattleState: everything a human player sees on
// the Showdown UI — field state, boosts, screens/hazards, the foe's revealed
// moves — must reach the LLM too. Deciding without Trick Room or a +2 Atk
// boost in view is deciding blind.
func TestRenderUserPrompt_PublicBattleState(t *testing.T) {
	v := sampleView()
	v.Weather = &engine.WeatherState{Kind: engine.WeatherRain, TurnsLeft: 3}
	v.Terrain = &engine.TerrainState{Kind: engine.TerrainGrassy, TurnsLeft: 4}
	v.PseudoWeather.TrickRoom = &engine.PWTimer{TurnsLeft: 2}
	v.Self.Team[0].Stages.Atk = 2
	v.Self.Conditions.Reflect = &engine.ScreenState{TurnsLeft: 4}
	v.Foe.Stages.Spe = -1
	v.FoeConditions.Hazards.Spikes = 2
	v.Foe.Moves = []engine.MoveSlot{
		{MoveID: "flamethrower", PP: 12, MaxPP: 15}, // revealed
		{}, {}, {}, // unrevealed (blanked by the fog filter)
	}
	v.Self.SlotConditions.Wish = &engine.WishState{Healer: "Blastoise", Amount: 100, TurnsLeft: 1}
	v.FoeSlotConditions.Wish = &ai.FoeWishState{Healer: "Vileplume", TurnsLeft: 2}

	got := RenderUserPrompt(stubDex(), v, pctHP(v.Foe.HP, v.Foe.MaxHP), []engine.Action{{Kind: engine.ActionMove, Index: 0}})

	for _, want := range []string{
		"FIELD: rain (3 turns left), grassy terrain (4 turns left), Trick Room (2 turns left)",
		"[+2 Atk]",
		"(your side: Reflect 4t)",
		"[-1 Spe]",
		"(their side: Spikes x2)",
		"[Wish lands in 1t, +100 HP]", // our own Wish: full knowledge
		"[their Wish lands in 2t]",    // foe's Wish: event + countdown only
		"Opponent's revealed moves: Flamethrower (1 of 4 slots revealed)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q\n---full output---\n%s", want, got)
		}
	}
	// The foe's Wish heal amount is hidden — it must never reach the prompt.
	if strings.Contains(got, "their Wish lands in 2t, +") {
		t.Errorf("foe wish amount leaked into the prompt:\n%s", got)
	}
}

func TestRenderUserPrompt_ReplacePhase(t *testing.T) {
	v := sampleView()
	v.Replace = true
	v.Self.Team[0].Fainted = true

	acts := []engine.Action{{Kind: engine.ActionSwitch, Index: 1}}
	got := RenderUserPrompt(stubDex(), v, pctHP(v.Foe.HP, v.Foe.MaxHP), acts)

	if !strings.Contains(got, "replace your fainted Pokémon") {
		t.Errorf("replace prompt missing the replace banner:\n%s", got)
	}
}

func TestRenderUserPrompt_Struggle(t *testing.T) {
	v := sampleView()
	// Strip all PP so the only move action is Struggle.
	v.Self.Team[0].Moves[0].PP = 0
	v.Self.Team[0].Moves[1].PP = 0
	acts := []engine.Action{{Kind: engine.ActionMove, Index: -1}}
	got := RenderUserPrompt(stubDex(), v, pctHP(v.Foe.HP, v.Foe.MaxHP), acts)
	if !strings.Contains(got, "[0] Struggle") {
		t.Errorf("struggle action not rendered:\n%s", got)
	}
}

// TestLiveHarness_FoeHPPctSurvivesWireDecode is the regression the hand-built
// fixtures above never hit: on the live WS path the view is decoded off the
// wire, which zeroes the foe's HP (hp/max_hp are never sent — only hp_pct).
// Rendering pctHP over that typed view prints "HP ~0%" for a healthy foe. The
// live loop must recover the percentage from the raw frame bytes so the model
// sees the real number.
func TestLiveHarness_FoeHPPctSurvivesWireDecode(t *testing.T) {
	// Server side: the sample foe is a live Vileplume at 150/200. MarshalJSON
	// redacts it to the wire form (hp_pct:75, no hp/max_hp).
	wire, err := json.Marshal(sampleView())
	if err != nil {
		t.Fatalf("marshal server view: %v", err)
	}
	// Client side: decode the frame exactly as the live loop does.
	var mu protocol.MatchUpdate
	if err := json.Unmarshal([]byte(`{"type":"turn","view":`+string(wire)+`}`), &mu); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	// The trap: the typed decode really did zero the foe's HP.
	if mu.View == nil || mu.View.Foe.HP != 0 || mu.View.Foe.MaxHP != 0 {
		t.Fatalf("precondition: decode should zero foe HP, got %+v", mu.View.Foe)
	}
	// The naive path — what the bug rendered — reads 0%.
	if got := pctHP(mu.View.Foe.HP, mu.View.Foe.MaxHP); got != 0 {
		t.Fatalf("sanity: naive pctHP over decoded view should be 0, got %d", got)
	}
	// The fix recovers 75% from the raw bytes.
	if got := foeHPPctFromWire(*mu.View, mu.RawView); got != 75 {
		t.Errorf("foeHPPctFromWire = %d, want 75", got)
	}

	got := RenderUserPrompt(stubDex(), *mu.View, foeHPPctFromWire(*mu.View, mu.RawView),
		[]engine.Action{{Kind: engine.ActionMove, Index: 0}})
	if !strings.Contains(got, "HP ~75%") {
		t.Errorf("live-path prompt should show foe HP ~75%%, got:\n%s", got)
	}
	if strings.Contains(got, "HP ~0%") {
		t.Errorf("live-path prompt still shows the fainted-foe bug:\n%s", got)
	}
}

func TestSystemPromptIsStable(t *testing.T) {
	// The system prompt is what adapters cache. If it accidentally
	// gains per-turn substitution, prompt caching breaks silently.
	// Catch the regression here.
	if strings.Contains(SystemPrompt, "%") || strings.Contains(SystemPrompt, "{{") {
		t.Errorf("SystemPrompt contains a format placeholder — it must be static for cache hits")
	}
}
