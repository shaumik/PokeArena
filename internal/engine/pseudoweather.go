package engine

import (
	"fmt"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

// pseudoweather.go owns the field-wide non-weather conditions: Trick
// Room (speed inversion), Wonder Room (Def/SpD swap), Magic Room
// (every held item suppressed field-wide — see itemSuppressed and the
// MagicRoomHere mirror), and Gravity
// (5/3 accuracy boost, everything grounded, and the airborne moves banned).
// Unlike Weather and
// Terrain, multiple pseudo-weathers can coexist; each is an
// independent 5-turn timer. The aggregate bag lives on BattleState
// as a value-typed struct (PseudoWeather) — fields mirror the
// SideConditions / screens.go pattern.

func init() {
	specs.RegisterPseudoWeather("trickroom")
	specs.RegisterPseudoWeather("wonderroom")
	specs.RegisterPseudoWeather("magicroom")
	specs.RegisterPseudoWeather("gravity")
	registerPseudoWeather("trickroom", applyTrickRoomSetter)
	registerPseudoWeather("wonderroom", applyWonderRoomSetter)
	registerPseudoWeather("magicroom", applyMagicRoomSetter)
	registerPseudoWeather("gravity", applyGravitySetter)
}

// pseudoWeatherSetter is the contract a mechanic fulfills to claim a
// `Move.PseudoWeather` slug. Same shape as sideConditionSetter, just
// at battle scope (no per-side context — pseudo-weathers are global).
type pseudoWeatherSetter func(s *BattleState, side int, log *[]LogLine)

var pseudoWeatherSetters = map[string]pseudoWeatherSetter{}

func registerPseudoWeather(slug string, h pseudoWeatherSetter) {
	pseudoWeatherSetters[slug] = h
}

// PWTimer is the per-pseudo-weather countdown. TurnsLeft starts at
// defaultPseudoWeatherTurns and decrements at end of turn after
// residuals.
type PWTimer struct {
	TurnsLeft int `json:"turns_left"`
}

// PseudoWeather is the bag of active field-wide conditions on the
// battle. Multiple can be up at once (Trick Room + Gravity is canon
// and common). Nil fields mean "not active." Value-typed because all
// state is owned by the timers themselves.
type PseudoWeather struct {
	TrickRoom  *PWTimer `json:"trick_room,omitempty"`
	WonderRoom *PWTimer `json:"wonder_room,omitempty"`
	MagicRoom  *PWTimer `json:"magic_room,omitempty"`
	Gravity    *PWTimer `json:"gravity,omitempty"`
}

const defaultPseudoWeatherTurns = 5

// ClonePseudoWeather returns a deep copy of pw. Used by
// BattleState.Clone so AI rollouts mutate timers without aliasing.
func ClonePseudoWeather(pw PseudoWeather) PseudoWeather {
	out := pw
	if pw.TrickRoom != nil {
		t := *pw.TrickRoom
		out.TrickRoom = &t
	}
	if pw.WonderRoom != nil {
		w := *pw.WonderRoom
		out.WonderRoom = &w
	}
	if pw.MagicRoom != nil {
		m := *pw.MagicRoom
		out.MagicRoom = &m
	}
	if pw.Gravity != nil {
		g := *pw.Gravity
		out.Gravity = &g
	}
	return out
}

// applyTrickRoomSetter / applyWonderRoomSetter / applyMagicRoomSetter
// / applyGravitySetter spawn or toggle their respective pseudo-weather.
// Canonical Showdown: re-using a setter while the pseudo-weather is
// already up CLEARS it early (Trick Room into Trick Room undoes the
// effect). We mirror that — no "But it failed" on re-set.
func applyTrickRoomSetter(s *BattleState, side int, log *[]LogLine) {
	if s.PseudoWeather.TrickRoom != nil {
		s.PseudoWeather.TrickRoom = nil
		*log = append(*log, LogLine{
			Type: "pseudoweather", Side: -1,
			Text: "The twisted dimensions returned to normal!",
		})
		return
	}
	s.PseudoWeather.TrickRoom = &PWTimer{TurnsLeft: defaultPseudoWeatherTurns}
	*log = append(*log, LogLine{
		Type: "pseudoweather", Side: side,
		Text: fmt.Sprintf("%s twisted the dimensions!", s.Active(side).Name),
	})
	// Canon's onAnyPseudoWeatherChange, which is Room Service's second firing
	// point — and only fires on the way up. Un-setting Trick Room above returns
	// before this, which is right: the room going away pays nobody.
	applyFieldReactiveItems(s, log)
}

func applyWonderRoomSetter(s *BattleState, side int, log *[]LogLine) {
	if s.PseudoWeather.WonderRoom != nil {
		s.PseudoWeather.WonderRoom = nil
		*log = append(*log, LogLine{
			Type: "pseudoweather", Side: -1,
			Text: "Wonder Room wore off, and Def and Sp. Def stats returned to normal!",
		})
		return
	}
	s.PseudoWeather.WonderRoom = &PWTimer{TurnsLeft: defaultPseudoWeatherTurns}
	*log = append(*log, LogLine{
		Type: "pseudoweather", Side: side,
		Text: "It created a bizarre area in which Def and Sp. Def stats are swapped!",
	})
}

// applyMagicRoomSetter raises or dismisses Magic Room, which suppresses every
// held item on the field. syncMagicRoomFlags pushes the change onto both
// actives — see Volatiles.MagicRoomHere for why the field state is mirrored.
func applyMagicRoomSetter(s *BattleState, side int, log *[]LogLine) {
	defer syncMagicRoomFlags(s)
	if s.PseudoWeather.MagicRoom != nil {
		s.PseudoWeather.MagicRoom = nil
		*log = append(*log, LogLine{
			Type: "pseudoweather", Side: -1,
			Text: "Magic Room wore off, and held items resumed their effects!",
		})
		return
	}
	s.PseudoWeather.MagicRoom = &PWTimer{TurnsLeft: defaultPseudoWeatherTurns}
	*log = append(*log, LogLine{
		Type: "pseudoweather", Side: side,
		Text: "It created a bizarre area in which Pokémon's held items lose their effects!",
	})
}

func applyGravitySetter(s *BattleState, side int, log *[]LogLine) {
	if s.PseudoWeather.Gravity != nil {
		// Gravity is one of the few pseudo-weathers that doesn't clear
		// on re-set; the canonical Showdown behavior is "But it failed."
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	s.PseudoWeather.Gravity = &PWTimer{TurnsLeft: defaultPseudoWeatherTurns}
	*log = append(*log, LogLine{
		Type: "pseudoweather", Side: -1,
		Text: "Gravity intensified!",
	})
	// Everything already in the air comes down. Canon's onFieldStart walks
	// every active and strips the fly/bounce charge, Magnet Rise and
	// Telekinesis, announcing once per Pokémon that had any of them. Side 0
	// then side 1, the same log-determinism convention tickPseudoWeather uses.
	//
	// The divergence on the charge is the one already documented at
	// applySmackDownVolatile: canon pairs the removal with
	// queue.cancelMove(pokemon), and this engine has no queued action to
	// cancel, so a Pokémon that had not yet acted still takes its turn and is
	// refused by gravityBlocksMove with a "can't" line where canon silently
	// eats the action.
	for i := 0; i < 2; i++ {
		p := s.Active(i)
		if p == nil || p.Fainted {
			continue
		}
		applies := cancelAirborneCharge(p)
		if p.Volatiles.MagnetRise != nil {
			p.Volatiles.MagnetRise = nil
			applies = true
		}
		if p.Volatiles.Telekinesis != nil {
			p.Volatiles.Telekinesis = nil
			applies = true
		}
		if applies {
			*log = append(*log, LogLine{
				Type: "pseudoweather", Side: i,
				Text: fmt.Sprintf("%s fell from the sky due to the gravity!", p.Name),
			})
		}
	}
}

// tickPseudoWeather decrements each active pseudo-weather and clears
// any whose TurnsLeft hits zero. Called once per turn after side
// residuals. Order: Trick Room, Wonder Room, Magic Room, Gravity for
// log determinism.
func tickPseudoWeather(s *BattleState, log *[]LogLine) {
	pw := &s.PseudoWeather
	if pw.TrickRoom != nil {
		pw.TrickRoom.TurnsLeft--
		if pw.TrickRoom.TurnsLeft <= 0 {
			pw.TrickRoom = nil
			*log = append(*log, LogLine{
				Type: "pseudoweather", Side: -1,
				Text: "The twisted dimensions returned to normal!",
			})
		}
	}
	if pw.WonderRoom != nil {
		pw.WonderRoom.TurnsLeft--
		if pw.WonderRoom.TurnsLeft <= 0 {
			pw.WonderRoom = nil
			*log = append(*log, LogLine{
				Type: "pseudoweather", Side: -1,
				Text: "Wonder Room wore off, and Def and Sp. Def stats returned to normal!",
			})
		}
	}
	if pw.MagicRoom != nil {
		pw.MagicRoom.TurnsLeft--
		if pw.MagicRoom.TurnsLeft <= 0 {
			pw.MagicRoom = nil
			defer syncMagicRoomFlags(s)
			*log = append(*log, LogLine{
				Type: "pseudoweather", Side: -1,
				Text: "Magic Room wore off, and held items resumed their effects!",
			})
		}
	}
	if pw.Gravity != nil {
		pw.Gravity.TurnsLeft--
		if pw.Gravity.TurnsLeft <= 0 {
			pw.Gravity = nil
			*log = append(*log, LogLine{
				Type: "pseudoweather", Side: -1,
				Text: "Gravity returned to normal.",
			})
		}
	}
}

// trickRoomActive reports whether Trick Room is up. Called from
// goesFirst to flip the speed comparison.
func trickRoomActive(s *BattleState) bool {
	return s != nil && s.PseudoWeather.TrickRoom != nil
}

// gravityActive reports whether Gravity is up. Called from resolveAccuracy for
// the ×5/3 accuracy boost and from gravityBlocksMove for the move ban. The
// grounding half of Gravity lives in groundedness (terrain.go), which reads the
// timer directly rather than through here.
func gravityActive(s *BattleState) bool {
	return s != nil && s.PseudoWeather.Gravity != nil
}

// gravityBlocksMove reports whether Gravity refuses m outright. The rule is the
// move's `gravity` flag and nothing else — upstream's onDisableMove,
// onBeforeMove and onModifyMove all read exactly that, which is why the flag
// had to be carried through data-sync before any of this could work.
//
// Three consumers rather than one, and they are not redundant. The selection
// filter in LegalActionsDex is the polite half: it stops a controller offering
// a move that cannot be used. The refusal in executeMove is the load-bearing
// half, because the dex-less LegalActions cannot run the filter and the AI
// calls that one — without the refusal an AI-driven side would keep choosing
// Fly under Gravity and keep having it work.
func gravityBlocksMove(s *BattleState, m domain.Move) bool {
	return gravityActive(s) && m.HasFlag("gravity")
}
