package engine

import (
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/specs"
)

func init() {
	specs.RegisterWeather("rain")
	specs.RegisterWeather("sun")
	specs.RegisterWeather("sandstorm")
	specs.RegisterWeather("snow")
}

// WeatherKind identifies a field condition. The empty string means no active
// weather; concrete values are the slugs the domain layer's Move.Weather field
// uses (set by the data-sync transform).
type WeatherKind string

const (
	WeatherClear     WeatherKind = ""
	WeatherRain      WeatherKind = "rain"
	WeatherSun       WeatherKind = "sun"
	WeatherSandstorm WeatherKind = "sandstorm"
	WeatherSnow      WeatherKind = "snow"
)

// WeatherState is the active battle-level weather. TurnsLeft counts down at
// end of turn (after residuals); when it hits zero the engine clears the
// weather. Persistent setters (weather-rock items, ability auto-setters) will
// extend the default duration once items / abilities ship; for now every
// setter move uses defaultWeatherTurns.
type WeatherState struct {
	Kind      WeatherKind `json:"kind"`
	TurnsLeft int         `json:"turns_left"`
}

// defaultWeatherTurns is how long a player-set weather lasts. Items and
// abilities will override this when they land.
const defaultWeatherTurns = 5

// damageMultByType returns the damage multiplier the active weather applies
// to a move of the given type. 1.0 when no weather is active or no rule
// matches; 0.5 / 1.5 for the canonical sun-rain matchups.
func damageMultByType(w *WeatherState, moveType domain.Type) float64 {
	if w == nil {
		return 1.0
	}
	switch w.Kind {
	case WeatherRain:
		switch moveType {
		case "water":
			return 1.5
		case "fire":
			return 0.5
		}
	case WeatherSun:
		switch moveType {
		case "fire":
			return 1.5
		case "water":
			return 0.5
		}
	}
	return 1.0
}

// defenseMult returns the defensive stat multiplier the active weather
// applies for the given defender. Sandstorm: Rock-types get +50% SpD. Snow:
// Ice-types get +50% Def. The category determines which stat the boost
// targets when we apply it in computeDamage.
//
// Returns (mult, applies-to-physical?). The boolean isolates which call site
// in computeDamage takes the boost: snow boosts Def (physical formula) while
// sandstorm boosts SpD (special formula).
func defenseMult(w *WeatherState, def *Pokemon, cat domain.Category) float64 {
	if w == nil {
		return 1.0
	}
	switch w.Kind {
	case WeatherSandstorm:
		if cat == domain.CatSpecial && isType(def, "rock") {
			return 1.5
		}
	case WeatherSnow:
		if cat == domain.CatPhysical && isType(def, "ice") {
			return 1.5
		}
	}
	return 1.0
}

// residualDamage returns the chip damage the active weather inflicts on p at
// end of turn, or 0 if p is immune. Sandstorm chips 1/16 max HP to anything
// that is not Rock / Ground / Steel; snow (modern Gen 9) does no residual
// damage; rain and sun never chip. Returns 0 for clear weather too.
func weatherResidual(w *WeatherState, p *Pokemon) int {
	if w == nil {
		return 0
	}
	if w.Kind == WeatherSandstorm {
		if isType(p, "rock") || isType(p, "ground") || isType(p, "steel") {
			return 0
		}
		if abilityImmuneToSandstorm(p) || itemImmuneToSandstorm(p) {
			return 0
		}
		dmg := p.MaxHP / 16
		if dmg < 1 {
			dmg = 1
		}
		return dmg
	}
	return 0
}

// weatherStartedText is the "<weather> began!" log line at setter time.
func weatherStartedText(kind WeatherKind) string {
	switch kind {
	case WeatherRain:
		return "It started to rain!"
	case WeatherSun:
		return "The sunlight turned harsh!"
	case WeatherSandstorm:
		return "A sandstorm kicked up!"
	case WeatherSnow:
		return "It started to snow!"
	}
	return ""
}

// weatherContinuesText is the "<weather> continues!" line emitted each turn the
// weather is still active. Sandstorm has its own residual-damage log line; the
// continues message fires every turn regardless.
func weatherContinuesText(kind WeatherKind) string {
	switch kind {
	case WeatherRain:
		return "Rain continues to fall."
	case WeatherSun:
		return "The sunlight is strong."
	case WeatherSandstorm:
		return "The sandstorm rages."
	case WeatherSnow:
		return "It keeps snowing."
	}
	return ""
}

// weatherClearedText is the line when TurnsLeft reaches zero.
func weatherClearedText(kind WeatherKind) string {
	switch kind {
	case WeatherRain:
		return "The rain stopped."
	case WeatherSun:
		return "The sunlight faded."
	case WeatherSandstorm:
		return "The sandstorm subsided."
	case WeatherSnow:
		return "The snow stopped."
	}
	return ""
}

// applyWeatherSetter spawns or refreshes the battle-level weather. A setter
// that names the currently-active weather fails (canon — Rain Dance in
// rain is a wasted PP); otherwise the new weather replaces any active
// condition for its default-turn duration. Called from applyStatusMove's
// weather-field dispatch.
func applyWeatherSetter(s *BattleState, side int, kind WeatherKind, log *[]LogLine) {
	if s.Weather != nil && s.Weather.Kind == kind {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	s.Weather = &WeatherState{Kind: kind, TurnsLeft: weatherTurnsFor(s.Active(side), defaultWeatherTurns, kind)}
	*log = append(*log, LogLine{Type: "weather", Side: -1, Text: weatherStartedText(kind)})
}
