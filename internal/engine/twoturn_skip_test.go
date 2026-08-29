package engine

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// twoTurnBattle sets up Venusaur vs Snorlax with one move on each side, so a
// single ResolveTurn is unambiguous about whose charge is being tested.
func twoTurnBattle(t *testing.T, d *domain.Dex, moveID string) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "b", "P1", []int{3}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 10, MaxPP: 10}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	return s
}

func bothMove() [2]Action {
	return [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}}
}

// TestSolarBeamSkipsChargeInSun: the whole point of the Drought/Chlorophyll
// sun archetype is that its payoff move fires the turn it is picked. Without
// this Solar Beam always spent a turn charging and the archetype had nothing
// to build toward.
func TestSolarBeamSkipsChargeInSun(t *testing.T) {
	d := loadDex(t)
	s := twoTurnBattle(t, d, "solar-beam")
	s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 5}
	foeHP := s.Active(1).HP

	log := ResolveTurn(d, s, bothMove())
	if s.Active(0).Volatiles.Charging != nil {
		t.Error("Solar Beam should not charge under the sun")
	}
	if s.Active(1).HP >= foeHP {
		t.Errorf("Solar Beam should have connected on the turn it was picked (%d → %d); log %v",
			foeHP, s.Active(1).HP, logTexts(log))
	}
	if logHas(log, "began charging") {
		t.Errorf("no charge line expected; got %v", logTexts(log))
	}
}

// TestSolarBeamStillChargesInOtherWeather: only the sun does this. Rain, sand,
// snow and clear skies all leave the charge turn in place.
func TestSolarBeamStillChargesInOtherWeather(t *testing.T) {
	d := loadDex(t)
	for _, w := range []*WeatherState{
		nil,
		{Kind: WeatherRain, TurnsLeft: 5},
		{Kind: WeatherSandstorm, TurnsLeft: 5},
		{Kind: WeatherSnow, TurnsLeft: 5},
	} {
		kind := "clear"
		if w != nil {
			kind = string(w.Kind)
		}
		s := twoTurnBattle(t, d, "solar-beam")
		s.Weather = w
		log := ResolveTurn(d, s, bothMove())
		// Sand and snow chip at end of turn, so the foe's HP is not a clean
		// signal here — the charge volatile and the log are.
		if s.Active(0).Volatiles.Charging == nil {
			t.Errorf("weather %s: Solar Beam should still charge", kind)
		}
		if !logHas(log, "began charging") {
			t.Errorf("weather %s: expected a charge line, got %v", kind, logTexts(log))
		}
	}
}

// TestUtilityUmbrellaStillCharges: the umbrella holder is standing out of the
// sun, so it gets no free Solar Beam. Read through weatherFor, like every
// other sun-keyed effect.
func TestUtilityUmbrellaStillCharges(t *testing.T) {
	d := loadDex(t)
	s := twoTurnBattle(t, d, "solar-beam")
	s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 5}
	s.Active(0).Item = ItemUtilityUmbrella

	ResolveTurn(d, s, bothMove())
	if s.Active(0).Volatiles.Charging == nil {
		t.Error("a Utility Umbrella holder is out of the sun and should still charge Solar Beam")
	}
}

// TestPowerHerbSkipsChargeAndIsConsumed: the herb works on any two-turn move,
// once.
func TestPowerHerbSkipsChargeAndIsConsumed(t *testing.T) {
	d := loadDex(t)
	for _, id := range []string{"solar-beam", "sky-attack", "razor-wind", "skull-bash"} {
		if _, ok := d.Moves[id]; !ok {
			continue
		}
		t.Run(id, func(t *testing.T) {
			s := twoTurnBattle(t, d, id)
			s.Active(0).Item = ItemPowerHerb
			log := ResolveTurn(d, s, bothMove())

			if s.Active(0).Volatiles.Charging != nil {
				t.Errorf("%s should skip its charge with a Power Herb", id)
			}
			if s.Active(0).Item != ItemNone {
				t.Errorf("the Power Herb should be consumed, still holding %q", s.Active(0).Item)
			}
			if !logHas(log, "Power Herb") {
				t.Errorf("the consume should be logged; got %v", logTexts(log))
			}
		})
	}
}

// TestPowerHerbIsOneShot: a second two-turn move charges normally.
func TestPowerHerbIsOneShot(t *testing.T) {
	d := loadDex(t)
	s := twoTurnBattle(t, d, "razor-wind")
	s.Active(0).Item = ItemPowerHerb

	ResolveTurn(d, s, bothMove())
	if s.Active(0).Volatiles.Charging != nil {
		t.Fatal("first use should skip the charge")
	}
	ResolveTurn(d, s, bothMove())
	if s.Active(0).Volatiles.Charging == nil {
		t.Error("with the herb spent, the second use should charge normally")
	}
}

// TestSunDoesNotSpendThePowerHerb: the sun check runs first, so a Solar Beam
// that was already free doesn't burn a herb it didn't need — the herb is
// still there for the next charge move.
func TestSunDoesNotSpendThePowerHerb(t *testing.T) {
	d := loadDex(t)
	s := twoTurnBattle(t, d, "solar-beam")
	s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 5}
	s.Active(0).Item = ItemPowerHerb

	ResolveTurn(d, s, bothMove())
	if s.Active(0).Volatiles.Charging != nil {
		t.Fatal("Solar Beam should be free under the sun")
	}
	if s.Active(0).Item != ItemPowerHerb {
		t.Error("the sun made the move free; the Power Herb should not have been spent")
	}
}

// TestPowerHerbLeavesNonChargeMovesAlone: nothing about an ordinary move
// should touch the herb.
func TestPowerHerbLeavesNonChargeMovesAlone(t *testing.T) {
	d := loadDex(t)
	s := twoTurnBattle(t, d, "tackle")
	s.Active(0).Item = ItemPowerHerb
	ResolveTurn(d, s, bothMove())
	if s.Active(0).Item != ItemPowerHerb {
		t.Error("a one-turn move should not consume the Power Herb")
	}
}

// TestSuppressedPowerHerbStillCharges: Embargo, Magic Room and Klutz all make
// a held item do nothing. skipChargeTurn reads the item through itemOf, so it
// inherits that for free — worth pinning, since reading the raw slot instead
// is the easy mistake and would let a suppressed herb still fire.
func TestSuppressedPowerHerbStillCharges(t *testing.T) {
	d := loadDex(t)
	s := twoTurnBattle(t, d, "razor-wind")
	s.Active(0).Item = ItemPowerHerb
	s.Active(0).Volatiles.Embargo = &EmbargoState{Turns: 5}

	ResolveTurn(d, s, bothMove())
	if s.Active(0).Volatiles.Charging == nil {
		t.Error("a Power Herb under Embargo does nothing, so the move should charge")
	}
	if s.Active(0).Item != ItemPowerHerb {
		t.Error("a suppressed herb should not be consumed")
	}
}
