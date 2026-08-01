package engine

import "math"

// weatherheal.go lifts the weather-scaled recovery of Moonlight, Synthesis,
// and Morning Sun. Showdown encodes the heal amount in an onModifyMove JS
// callback, so it never reaches us through the declarative dataset — the
// curated moves carry only the "heal" flag with no Effect block. We gate the
// behavior by move ID in applyStatusMove, the same way Defog and Curse are
// lifted (see effects.go). The move-coverage audit is blind to this gap by
// design; the dedicated tests in weatherheal_test.go are its guardrail.

// weatherHealMoveIDs are the self-target status moves whose heal amount scales
// with the active weather. Only moonlight and synthesis ship in the Gen-1
// learnset today; morning-sun is listed so it works the moment it lands.
var weatherHealMoveIDs = map[string]bool{
	"moonlight":   true,
	"synthesis":   true,
	"morning-sun": true,
}

func isWeatherHealMove(id string) bool { return weatherHealMoveIDs[id] }

// weatherHealFraction is the fraction of max HP Moonlight/Synthesis/Morning Sun
// restore under the active weather: 2/3 in harsh sun, 1/4 in any other weather
// (rain, sandstorm, snow), and 1/2 under clear skies — the canonical Gen-5+
// amounts.
func weatherHealFraction(w *WeatherState) float64 {
	if w == nil {
		return 0.5
	}
	switch w.Kind {
	case WeatherSun:
		return 2.0 / 3.0
	case WeatherRain, WeatherSandstorm, WeatherSnow:
		return 0.25
	default:
		return 0.5
	}
}

// applyWeatherHeal restores the user's HP by the weather-scaled fraction. The
// round-then-cap arithmetic mirrors the declarative Effect.Heal path so a
// fixed-amount Recover and a weather heal of the same fraction land on the
// same HP.
func applyWeatherHeal(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	// effectiveWeather, not s.Weather: Cloud Nine / Air Lock suppress the
	// bonus. weatherFor on top of it: a Utility Umbrella holder is not standing
	// in the sun as far as its own Synthesis is concerned, so it heals the
	// no-weather 1/2 rather than the sunny 2/3.
	amt := int(math.Round(float64(p.MaxHP) * weatherHealFraction(weatherFor(p, effectiveWeather(s)))))
	healPokemon(p, side, amt, log)
}
