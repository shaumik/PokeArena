package ai

import (
	"encoding/json"
	"testing"

	"github.com/shaumik/PokeArena/internal/engine"
)

func TestPublishedViewHasLegalActionsAndPublicMoveMetadata(t *testing.T) {
	d := loadDex(t)
	s, err := engine.NewBattle(d, "view", "Red", []int{6, 9}, "Blue", []int{3, 65}, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.Sides[0].Team[0].Moves = []engine.MoveSlot{{MoveID: "flamethrower", PP: 15, MaxPP: 15}, {MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Sides[0].Team[0].Item = engine.ItemAssaultVest
	s.Sides[1].Team[0].Moves = []engine.MoveSlot{{MoveID: "tackle", PP: 34, MaxPP: 35}, {MoveID: "solar-beam", PP: 10, MaxPP: 10}}
	v := MakeViewDex(d, s, 0)
	for _, a := range v.LegalActions {
		if a.Kind == engine.ActionMove && a.Index == 1 {
			t.Fatal("Assault Vest status move was offered")
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	own := wire["self"].(map[string]any)["team"].([]any)[0].(map[string]any)["moves"].([]any)[0].(map[string]any)
	if own["bp"] != float64(90) || own["accuracy"] != float64(100) || own["type"] != "fire" || own["category"] != "special" || own["pp"] != float64(15) {
		t.Fatalf("own metadata: %v", own)
	}
	foe := wire["foe"].(map[string]any)
	moves := foe["moves"].([]any)
	revealed := moves[0].(map[string]any)
	if revealed["bp"] != float64(40) || revealed["category"] != "physical" {
		t.Fatalf("revealed metadata: %v", revealed)
	}
	for _, key := range []string{"pp", "max_pp"} {
		if _, ok := revealed[key]; ok {
			t.Fatalf("foe leaked %s", key)
		}
	}
	hidden := moves[1].(map[string]any)
	if len(hidden) != 1 || hidden["move_id"] != "" {
		t.Fatalf("hidden move leaked metadata: %v", hidden)
	}
	for _, key := range []string{"hp", "max_hp", "stats", "item", "ability"} {
		if _, ok := foe[key]; ok {
			t.Fatalf("foe leaked %s", key)
		}
	}
}

func TestPublishedLegalActionsHandleDecisionPhases(t *testing.T) {
	d := loadDex(t)
	fresh := func() *engine.BattleState {
		s, e := engine.NewBattle(d, "v", "R", []int{6, 9}, "B", []int{3}, 1)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	t.Run("struggle", func(t *testing.T) {
		s := fresh()
		for i := range s.Sides[0].Team[0].Moves {
			s.Sides[0].Team[0].Moves[i].PP = 0
		}
		actions := MakeViewDex(d, s, 0).LegalActions
		if len(actions) == 0 || actions[0].Kind != engine.ActionMove || actions[0].Index != -1 {
			t.Fatalf("missing Struggle: %v", actions)
		}
	})
	t.Run("replacement", func(t *testing.T) {
		s := fresh()
		s.Phase = engine.PhaseReplace
		s.Replace[0] = true
		actions := MakeViewDex(d, s, 0).LegalActions
		if len(actions) != 1 || actions[0].Kind != engine.ActionSwitch || actions[0].Index != 1 {
			t.Fatalf("replacement: %v", actions)
		}
		if len(MakeViewDex(d, s, 1).LegalActions) != 0 {
			t.Fatal("non-replacing side offered actions")
		}
	})
	t.Run("terminal", func(t *testing.T) {
		s := fresh()
		s.Phase = engine.PhaseEnded
		s.Winner = 0
		raw, e := json.Marshal(MakeViewDex(d, s, 0))
		if e != nil {
			t.Fatal(e)
		}
		var wire map[string]any
		if e = json.Unmarshal(raw, &wire); e != nil {
			t.Fatal(e)
		}
		a, ok := wire["legal_actions"].([]any)
		if !ok || len(a) != 0 {
			t.Fatalf("terminal actions: %v", wire["legal_actions"])
		}
	})
	t.Run("pivot", func(t *testing.T) {
		s := fresh()
		s.Sides[0].Team[0].Moves = []engine.MoveSlot{{MoveID: "u-turn", PP: 20, MaxPP: 20}}
		for _, a := range MakeViewDex(d, s, 0).LegalActions {
			if a.Kind == engine.ActionMove && a.SwitchTarget != nil && *a.SwitchTarget == 1 {
				return
			}
		}
		t.Fatal("self-switch destination missing")
	})
}
