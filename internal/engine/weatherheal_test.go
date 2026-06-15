package engine

import (
	"math"
	"testing"
)

// newHealBattle builds a 1v1 where the user (Snorlax — high MaxHP so the heal
// never caps out) is chipped to 1 HP and holds only moveID; the foe Splashes.
// Snorlax is Normal, so no weather residual chips it on the turn we measure —
// every weather we test (clear / sun / rain / snow) leaves end-of-turn HP equal
// to the heal alone. (Sandstorm is the lone chipping weather and is covered via
// the shared "any other weather" branch by the rain and snow cases.)
func newHealBattle(t *testing.T, moveID string) *BattleState {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1) // Snorlax vs Snorlax
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 5, MaxPP: 5}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(0).HP = 1
	return s
}

// wantHealedHP is the HP a 1-HP user should sit at after a frac-of-max heal,
// mirroring the engine's round-then-cap arithmetic.
func wantHealedHP(max int, frac float64) int {
	want := 1 + int(math.Round(float64(max)*frac))
	if want > max {
		want = max
	}
	return want
}

func TestWeatherHealClearSkies(t *testing.T) {
	d := loadDex(t)
	s := newHealBattle(t, "moonlight")
	max := s.Active(0).MaxHP
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if got, want := s.Active(0).HP, wantHealedHP(max, 0.5); got != want {
		t.Errorf("Moonlight in clear weather healed to %d, want %d (max %d)", got, want, max)
	}
}

func TestWeatherHealSun(t *testing.T) {
	d := loadDex(t)
	s := newHealBattle(t, "moonlight")
	s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 5}
	max := s.Active(0).MaxHP
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if got, want := s.Active(0).HP, wantHealedHP(max, 2.0/3.0); got != want {
		t.Errorf("Moonlight in sun healed to %d, want %d (max %d)", got, want, max)
	}
}

func TestWeatherHealRain(t *testing.T) {
	d := loadDex(t)
	s := newHealBattle(t, "moonlight")
	s.Weather = &WeatherState{Kind: WeatherRain, TurnsLeft: 5}
	max := s.Active(0).MaxHP
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if got, want := s.Active(0).HP, wantHealedHP(max, 0.25); got != want {
		t.Errorf("Moonlight in rain healed to %d, want %d (max %d)", got, want, max)
	}
}

func TestWeatherHealSnow(t *testing.T) {
	d := loadDex(t)
	s := newHealBattle(t, "moonlight")
	s.Weather = &WeatherState{Kind: WeatherSnow, TurnsLeft: 5}
	max := s.Active(0).MaxHP
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if got, want := s.Active(0).HP, wantHealedHP(max, 0.25); got != want {
		t.Errorf("Moonlight in snow healed to %d, want %d (max %d)", got, want, max)
	}
}

// TestWeatherHealSynthesis confirms the gate covers every weather-heal move, not
// just Moonlight — Synthesis uses the identical fraction table.
func TestWeatherHealSynthesis(t *testing.T) {
	d := loadDex(t)
	s := newHealBattle(t, "synthesis")
	s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 5}
	max := s.Active(0).MaxHP
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if got, want := s.Active(0).HP, wantHealedHP(max, 2.0/3.0); got != want {
		t.Errorf("Synthesis in sun healed to %d, want %d (max %d)", got, want, max)
	}
}
