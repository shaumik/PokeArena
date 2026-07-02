package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pokearena"
	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

// decodeBattleFrame builds a real battle, projects side 0's fog-of-war view,
// and round-trips it through the gateway wire so tests render exactly what a
// live client would receive.
func decodeBattleFrame(t *testing.T, dex *domain.Dex) *battleView {
	t.Helper()
	state, err := engine.NewBattle(dex, "battle123", "Blue", []int{3, 6, 9}, "Red", []int{12, 15, 18}, 7)
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
	return f.View
}

// TestRenderBattleScreen renders the active-battle screen and asserts it does
// not panic and surfaces the key affordances. The renderer indexes into team
// slices and the legal-action set, so a panic here would be a real crash in a
// live battle.
func TestRenderBattleScreen(t *testing.T) {
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	m := newModel(nil, dex, "battle123", "p1")
	m.view = decodeBattleFrame(t, dex)
	m.meSide = 0
	m.screen = screenBattle
	m.needsAction = true
	m.log = []engine.LogLine{
		{Type: "info", Side: -1, Text: "Turn 1"},
		{Type: "move", Side: 1, Text: "Foe used Tackle!"},
	}

	out := m.View()
	for _, want := range []string{"your move", "switch", "log", "PokéArena"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
			t.Errorf("battle screen missing %q\n---\n%s", want, out)
		}
	}
}

// TestRechargeMenuSaysRechargeNotStruggle: if a recharge turn ever does reach
// the menu (auto-act normally plays it first — e.g. after a rejected send),
// the engine's -1 sentinel must be labelled "Recharge", never "Struggle".
func TestRechargeMenuSaysRechargeNotStruggle(t *testing.T) {
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	m := newModel(nil, dex, "battle123", "p1")
	v := decodeBattleFrame(t, dex)
	v.Self.Team[v.Self.Active].Volatiles.MustRecharge = true
	m.setView(v)
	m.screen = screenBattle
	m.needsAction = true

	out := m.View()
	if !strings.Contains(out, "Recharge") {
		t.Errorf("recharge turn menu missing a Recharge row\n---\n%s", out)
	}
	if strings.Contains(out, "Struggle") {
		t.Errorf("recharge turn must not be labelled Struggle\n---\n%s", out)
	}
}

// TestTypeAbbr guards the move-menu column tags: three uppercase letters,
// distinct across the confusable pairs.
func TestTypeAbbr(t *testing.T) {
	for in, want := range map[domain.Type]string{
		"grass": "GRA", "ground": "GRO",
		"fire": "FIR", "fighting": "FIG",
		"dark": "DAR", "dragon": "DRA",
		"ice": "ICE", "": "",
	} {
		if got := typeAbbr(in); got != want {
			t.Errorf("typeAbbr(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRenderRoomScreen renders the picker with an auto-drafted team.
func TestRenderRoomScreen(t *testing.T) {
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	m := newModel(nil, dex, "battle123", "p1")
	m.team, m.teamView = autoTeam(dex)
	m.screen = screenRoom
	m.room = &protocol.RoomUpdate{
		Phase:      protocol.RoomPhaseOpen,
		You:        protocol.RoomSlot{Attached: true, Submitted: false, Trainer: "Blue"},
		Them:       protocol.RoomSlot{Attached: false},
		DeadlineMS: 300000,
	}
	m.deadlineAt = time.Now().Add(5 * time.Minute)

	if len(m.team) != engine.TeamSize {
		t.Fatalf("auto team size = %d, want %d", len(m.team), engine.TeamSize)
	}
	if err := engine.ValidateTeam(m.team, dex); err != nil {
		t.Fatalf("auto team is not legal: %v", err)
	}
	out := m.View()
	for _, want := range []string{"picker", "your team", "submit"} {
		if !strings.Contains(strings.ToLower(out), want) {
			t.Errorf("room screen missing %q\n---\n%s", want, out)
		}
	}
}

// TestRenderEndedScreen renders win and loss banners.
func TestRenderEndedScreen(t *testing.T) {
	m := newModel(nil, nil, "battle123", "p1")
	m.screen = screenEnded
	m.meSide = 0
	win := 0
	m.winner = &win
	if !strings.Contains(m.View(), "won") {
		t.Errorf("expected win banner, got:\n%s", m.View())
	}
	loss := 1
	m.winner = &loss
	if !strings.Contains(m.View(), "lost") {
		t.Errorf("expected loss banner, got:\n%s", m.View())
	}
}

// TestAutoTeamAlwaysLegal hammers the drafter: every roll must pass
// ValidateTeam (6 distinct species, 1-4 learnable moves), including per-slot
// re-rolls, so the picker can never produce an unsubmittable team.
func TestAutoTeamAlwaysLegal(t *testing.T) {
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	for i := 0; i < 200; i++ {
		picks, mons := autoTeam(dex)
		if err := engine.ValidateTeam(picks, dex); err != nil {
			t.Fatalf("roll %d invalid: %v", i, err)
		}
		if len(mons) != len(picks) {
			t.Fatalf("roll %d: teamView/picks length mismatch", i)
		}
		// Re-roll each slot and re-validate.
		for s := 0; s < engine.TeamSize; s++ {
			picks, mons = rerollSlot(dex, picks, mons, s)
			if err := engine.ValidateTeam(picks, dex); err != nil {
				t.Fatalf("roll %d slot %d invalid after reroll: %v", i, s, err)
			}
		}
	}
}
