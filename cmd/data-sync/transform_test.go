package main

import (
	"encoding/json"
	"testing"
)

// TestTransformMoveNormalizesConditionCasing guards the slotCondition casing
// bug: Showdown emits sideCondition/pseudoWeather already stripped-lowercase
// but slotCondition as the display name ("Wish"). The transform must normalize
// all three to the id form before the specs vocab check, or "Wish" silently
// fails specs.SlotConditions["wish"] and Wish/Healing Wish ship with no slot
// condition. (specs vocab is populated by the engine blank-import in main.go.)
func TestTransformMoveNormalizesConditionCasing(t *testing.T) {
	cases := []struct {
		name                           string
		m                              upstreamMove
		wantSide, wantPseudo, wantSlot string
	}{
		{
			name:     "slotCondition arrives capitalized (the bug)",
			m:        statusMove("wish", "Wish", upstreamMove{SlotCondition: "Wish"}),
			wantSlot: "wish",
		},
		{
			name:     "slotCondition healingwish passes through",
			m:        statusMove("healing-wish", "Healing Wish", upstreamMove{SlotCondition: "healingwish"}),
			wantSlot: "healingwish",
		},
		{
			name:     "sideCondition stays mapped",
			m:        statusMove("reflect", "Reflect", upstreamMove{SideCondition: "reflect"}),
			wantSide: "reflect",
		},
		{
			name:       "pseudoWeather stays mapped",
			m:          statusMove("trick-room", "Trick Room", upstreamMove{PseudoWeather: "trickroom"}),
			wantPseudo: "trickroom",
		},
		{
			name: "unmodeled slot condition is dropped silently",
			m:    statusMove("x", "X", upstreamMove{SlotCondition: "SomeFutureCondition"}),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := transformMove(c.m)
			if err != nil {
				t.Fatalf("transformMove: %v", err)
			}
			if out.SideCondition != c.wantSide {
				t.Errorf("SideCondition = %q, want %q", out.SideCondition, c.wantSide)
			}
			if out.PseudoWeather != c.wantPseudo {
				t.Errorf("PseudoWeather = %q, want %q", out.PseudoWeather, c.wantPseudo)
			}
			if out.SlotCondition != c.wantSlot {
				t.Errorf("SlotCondition = %q, want %q", out.SlotCondition, c.wantSlot)
			}
		})
	}
}

// statusMove builds a minimal always-hits status move carrying just the
// condition field(s) under test, so transformMove runs without tripping on
// accuracy/target parsing.
func statusMove(id, name string, conds upstreamMove) upstreamMove {
	conds.ID = id
	conds.Name = name
	conds.Type = "Normal"
	conds.Category = "Status"
	conds.Target = "self"
	conds.Accuracy = json.RawMessage("true")
	return conds
}
