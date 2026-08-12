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

// TestTransformMoveStatOverrides: Showdown names the stat the damage formula
// should read on overrideOffensiveStat / overrideDefensiveStat. Dropping
// those silently is what left Psystrike an ordinary special Psychic move and
// Body Press swinging off Attack (issue #130 §6), so an unrecognized value is
// an error rather than a quiet passthrough.
func TestTransformMoveStatOverrides(t *testing.T) {
	t.Run("mapped to our slugs", func(t *testing.T) {
		m := statusMove("psystrike", "Psystrike", upstreamMove{OverrideDefensiveStat: "def"})
		m.Category = "Special"
		m.BasePower = 100
		m.Target = "normal"
		out, err := transformMove(m)
		if err != nil {
			t.Fatalf("transformMove: %v", err)
		}
		if out.OverrideDefensiveStat != "defense" {
			t.Errorf("OverrideDefensiveStat = %q, want %q", out.OverrideDefensiveStat, "defense")
		}
		if out.OverrideOffensiveStat != "" {
			t.Errorf("OverrideOffensiveStat = %q, want empty", out.OverrideOffensiveStat)
		}
	})

	t.Run("offensive side", func(t *testing.T) {
		m := statusMove("body-press", "Body Press", upstreamMove{OverrideOffensiveStat: "def"})
		m.Category = "Physical"
		m.BasePower = 80
		m.Target = "normal"
		out, err := transformMove(m)
		if err != nil {
			t.Fatalf("transformMove: %v", err)
		}
		if out.OverrideOffensiveStat != "defense" {
			t.Errorf("OverrideOffensiveStat = %q, want %q", out.OverrideOffensiveStat, "defense")
		}
	})

	t.Run("unset stays unset", func(t *testing.T) {
		out, err := transformMove(statusMove("tackle", "Tackle", upstreamMove{}))
		if err != nil {
			t.Fatalf("transformMove: %v", err)
		}
		if out.OverrideOffensiveStat != "" || out.OverrideDefensiveStat != "" {
			t.Errorf("overrides should be empty, got %q / %q", out.OverrideOffensiveStat, out.OverrideDefensiveStat)
		}
	})

	t.Run("non-formula stats are rejected", func(t *testing.T) {
		for _, stat := range []string{"spe", "evasion", "hp", "nonsense"} {
			m := statusMove("x", "X", upstreamMove{OverrideDefensiveStat: stat})
			if _, err := transformMove(m); err == nil {
				t.Errorf("overrideDefensiveStat %q should fail the transform, not ship silently", stat)
			}
		}
	})
}

// TestTransformSecondarySelfPayload: a secondary's `self` block aims its
// payload at the user. Dropping it shipped eleven curated moves with a bare
// {"chance": N} that rolled and then did nothing — Rapid Spin's +1 Speed,
// Power-Up Punch's +1 Atk, Ancient Power's omniboost (issue #130 §7).
func TestTransformSecondarySelfPayload(t *testing.T) {
	damaging := func(id, name string, secs []secondaryRaw) upstreamMove {
		m := statusMove(id, name, upstreamMove{})
		m.Category = "Physical"
		m.BasePower = 50
		m.Target = "normal"
		m.Secondaries = secs
		return m
	}

	t.Run("self boosts land with the self flag set", func(t *testing.T) {
		out, err := transformMove(damaging("rapid-spin", "Rapid Spin", []secondaryRaw{
			{Chance: 100, Self: &selfRaw{Boosts: map[string]int{"spe": 1}}},
		}))
		if err != nil {
			t.Fatalf("transformMove: %v", err)
		}
		if len(out.Secondaries) != 1 {
			t.Fatalf("want 1 secondary, got %d", len(out.Secondaries))
		}
		sec := out.Secondaries[0]
		if !sec.Self {
			t.Error("Self should be true")
		}
		if sec.Boosts["speed"] != 1 {
			t.Errorf("Boosts = %v, want speed:1", sec.Boosts)
		}
	})

	t.Run("target-side secondaries keep Self false", func(t *testing.T) {
		out, err := transformMove(damaging("flamethrower", "Flamethrower", []secondaryRaw{
			{Chance: 10, Status: "brn"},
		}))
		if err != nil {
			t.Fatalf("transformMove: %v", err)
		}
		if out.Secondaries[0].Self {
			t.Error("a foe-targeted secondary should not be flagged Self")
		}
	})

	t.Run("a secondary carrying both sides is rejected", func(t *testing.T) {
		_, err := transformMove(damaging("x", "X", []secondaryRaw{
			{Chance: 30, Status: "brn", Self: &selfRaw{Boosts: map[string]int{"atk": 1}}},
		}))
		if err == nil {
			t.Error("a secondary with both a self and a target payload should fail the transform")
		}
	})
}
