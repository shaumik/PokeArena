package engine

import (
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/specs"
)

// ScreenKind identifies a per-side damage-reducing condition. The empty
// string means no screen; concrete values match the slugs the domain
// layer's Move.SideCondition field uses (set by the data-sync transform).
type ScreenKind string

const (
	ScreenNone        ScreenKind = ""
	ScreenReflect     ScreenKind = "reflect"     // halves incoming physical
	ScreenLightScreen ScreenKind = "lightscreen" // halves incoming special
	ScreenAuroraVeil  ScreenKind = "auroraveil"  // halves both; requires hail/snow at setup
)

// ScreenState is one active screen on a side. TurnsLeft counts down at
// end of turn; the screen clears at zero. A setter reads the caster's
// held Light Clay through screenTurnsFor, which pushes the duration from
// defaultScreenTurns to 8.
type ScreenState struct {
	TurnsLeft int `json:"turns_left"`
}

// SideConditions is the bag of per-side conditions a Side carries. The
// three screens are independent: Reflect + Light Screen can coexist
// (each set by its own move), and Aurora Veil layers on top of either
// — the multiplier picks the relevant one and doesn't stack. Hazards
// (Stealth Rock, Spikes, Toxic Spikes) sit alongside the screens and
// fire on switch-in; see hazards.go. Tailwind / Safeguard / Mist are
// the buff-shaped conditions — speed mult, status shield, drop shield;
// see buffs.go.
type SideConditions struct {
	Reflect     *ScreenState     `json:"reflect,omitempty"`
	LightScreen *ScreenState     `json:"light_screen,omitempty"`
	AuroraVeil  *ScreenState     `json:"aurora_veil,omitempty"`
	Hazards     Hazards          `json:"hazards"`
	Tailwind    *TailwindState   `json:"tailwind,omitempty"`
	Safeguard   *SafeguardState  `json:"safeguard,omitempty"`
	Mist        *MistState       `json:"mist,omitempty"`
	QuickGuard  *QuickGuardState `json:"quick_guard,omitempty"`
	WideGuard   *WideGuardState  `json:"wide_guard,omitempty"`
}

// defaultScreenTurns is how long a screen lasts when set without an
// extender item. Light Clay pushes it to 8; see screenTurnsFor.
const defaultScreenTurns = 5

// screenDamageMult is the multiplier the defender's screens apply to an
// incoming move. 0.5× for the matching category; 1.0× if no screen
// applies, the move is a crit (screens don't reduce crit damage), or
// the move is a status move. Aurora Veil halves both categories.
//
// In doubles the multiplier would be ~2/3 (the canon "0.667×"); we are
// singles, so it's the cleaner 0.5×.
func screenDamageMult(sc *SideConditions, m domain.Move, crit bool) float64 {
	if sc == nil || crit {
		return 1.0
	}
	if m.Category == domain.CatStatus {
		return 1.0
	}
	if sc.AuroraVeil != nil {
		return 0.5
	}
	switch m.Category {
	case domain.CatPhysical:
		if sc.Reflect != nil {
			return 0.5
		}
	case domain.CatSpecial:
		if sc.LightScreen != nil {
			return 0.5
		}
	}
	return 1.0
}

// screenSlot returns a pointer to the slot for kind k inside sc, or
// nil if k isn't a recognized screen. Used by the setter to detect a
// re-set (canonical "But it failed!") and by tickScreens to count down.
func screenSlot(sc *SideConditions, k ScreenKind) **ScreenState {
	switch k {
	case ScreenReflect:
		return &sc.Reflect
	case ScreenLightScreen:
		return &sc.LightScreen
	case ScreenAuroraVeil:
		return &sc.AuroraVeil
	}
	return nil
}

// screenStartedText / screenClearedText are the log-line flavor strings
// for setter / expiry events. Mirrors weatherStartedText etc. There's
// no per-turn "continues" line for screens — Showdown doesn't emit one
// and the noise would crowd the log on a Reflect+Light Screen team.
func screenStartedText(k ScreenKind) string {
	switch k {
	case ScreenReflect:
		return "Reflect raised the team's Defense!"
	case ScreenLightScreen:
		return "Light Screen raised the team's Special Defense!"
	case ScreenAuroraVeil:
		return "Aurora Veil shielded the team from damage!"
	}
	return ""
}

func screenClearedText(k ScreenKind) string {
	switch k {
	case ScreenReflect:
		return "Reflect wore off."
	case ScreenLightScreen:
		return "Light Screen wore off."
	case ScreenAuroraVeil:
		return "Aurora Veil wore off."
	}
	return ""
}

func init() {
	specs.RegisterSideCondition("reflect")
	specs.RegisterSideCondition("lightscreen")
	specs.RegisterSideCondition("auroraveil")
	registerSideCondition("reflect", func(s *BattleState, side int, log *[]LogLine) {
		applyScreenSetter(s, side, ScreenReflect, log)
	})
	registerSideCondition("lightscreen", func(s *BattleState, side int, log *[]LogLine) {
		applyScreenSetter(s, side, ScreenLightScreen, log)
	})
	registerSideCondition("auroraveil", func(s *BattleState, side int, log *[]LogLine) {
		applyScreenSetter(s, side, ScreenAuroraVeil, log)
	})
}

// applyScreenSetter spawns a screen on the user's side. Re-setting an
// already-active screen fails (canonical Showdown — Reflect into Reflect
// is a wasted PP). Aurora Veil additionally fails unless hail/snow is
// active when used; once up, it persists even if the weather changes.
// Called from applyStatusMove's side-condition dispatch.
func applyScreenSetter(s *BattleState, side int, kind ScreenKind, log *[]LogLine) {
	if kind == ScreenAuroraVeil {
		w := effectiveWeather(s)
		if w == nil || w.Kind != WeatherSnow {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
	}
	slot := screenSlot(&s.Sides[side].Conditions, kind)
	if slot == nil {
		return
	}
	if *slot != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	*slot = &ScreenState{TurnsLeft: screenTurnsFor(s.Active(side), defaultScreenTurns)}
	*log = append(*log, LogLine{Type: "screen", Side: side, Text: screenStartedText(kind)})
}

// CloneSideConditions returns a deep copy of sc. Used by BattleState.Clone
// and by the ai package's View construction so AI search rollouts can
// mutate side conditions without aliasing the real battle's pointers.
// Exported because ai.MakeView and ai.cloneSide need it.
func CloneSideConditions(sc SideConditions) SideConditions {
	out := sc
	if sc.Reflect != nil {
		r := *sc.Reflect
		out.Reflect = &r
	}
	if sc.LightScreen != nil {
		l := *sc.LightScreen
		out.LightScreen = &l
	}
	if sc.AuroraVeil != nil {
		a := *sc.AuroraVeil
		out.AuroraVeil = &a
	}
	if sc.Tailwind != nil {
		t := *sc.Tailwind
		out.Tailwind = &t
	}
	if sc.Safeguard != nil {
		sg := *sc.Safeguard
		out.Safeguard = &sg
	}
	if sc.Mist != nil {
		m := *sc.Mist
		out.Mist = &m
	}
	if sc.QuickGuard != nil {
		q := *sc.QuickGuard
		out.QuickGuard = &q
	}
	if sc.WideGuard != nil {
		w := *sc.WideGuard
		out.WideGuard = &w
	}
	return out
}
