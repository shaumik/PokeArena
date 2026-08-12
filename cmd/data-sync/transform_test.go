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

// TestTransformMoveEmitsHighCrit: Showdown carries the boosted crit rate as
// the numeric critRatio static, which the engine reads as its "high-crit"
// flag. Before this was wired, damage.go read a flag no move in data/ carried
// and Stone Edge was strictly worse than Rock Slide (issue #130 §4).
func TestTransformMoveEmitsHighCrit(t *testing.T) {
	cases := []struct {
		name  string
		ratio int
		want  bool
	}{
		{"unset ratio is not high-crit", 0, false},
		{"ratio 1 is the normal rate", 1, false},
		{"ratio 2 is high-crit", 2, true},
		{"ratio 3+ still reads as high-crit", 3, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := statusMove("stone-edge", "Stone Edge", upstreamMove{CritRatio: c.ratio})
			m.Category = "Physical"
			m.BasePower = 100
			m.Target = "normal"
			out, err := transformMove(m)
			if err != nil {
				t.Fatalf("transformMove: %v", err)
			}
			got := false
			for _, f := range out.Flags {
				if f == "high-crit" {
					got = true
				}
			}
			if got != c.want {
				t.Errorf("critRatio %d: high-crit = %v, want %v (flags %v)", c.ratio, got, c.want, out.Flags)
			}
		})
	}
}
