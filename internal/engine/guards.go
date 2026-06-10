package engine

import (
	"fmt"

	"pokearena/internal/specs"
)

// guards.go owns Quick Guard and Wide Guard — the doubles-flavored
// side-shield moves. Both are functionally no-ops in our singles
// engine (Quick Guard blocks priority moves on allies, Wide Guard
// blocks spread moves on allies, and singles has no allies or spread
// moves) but the slugs register so the audit clears and so a future
// doubles layer has the bag already populated.
//
// Each is implemented as a one-turn side flag that clears in the
// end-of-turn buff tick (tickBuffs already handles the SideCondition
// timer pattern for Tailwind / Safeguard / Mist). The "consume on
// apply" semantic — failure on re-apply within the same turn — is
// canonical but unobservable in singles, so we apply it cleanly
// rather than try to model the "consecutive uses lose value" curve.

func init() {
	specs.RegisterSideCondition("quickguard")
	specs.RegisterSideCondition("wideguard")
	registerSideCondition("quickguard", applyQuickGuardSetter)
	registerSideCondition("wideguard", applyWideGuardSetter)
}

// QuickGuardState is a one-turn priority-block flag. Has no live
// effect in singles (priority moves still resolve normally because
// there are no allies to shield) — present so the slug is registered.
type QuickGuardState struct {
	TurnsLeft int `json:"turns_left"`
}

// WideGuardState is a one-turn spread-block flag. Has no live effect
// in singles (no spread moves are routed through the engine) —
// present so the slug is registered.
type WideGuardState struct {
	TurnsLeft int `json:"turns_left"`
}

func applyQuickGuardSetter(s *BattleState, side int, log *[]LogLine) {
	sc := &s.Sides[side].Conditions
	if sc.QuickGuard != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	sc.QuickGuard = &QuickGuardState{TurnsLeft: 1}
	*log = append(*log, LogLine{Type: "quickguard", Side: side,
		Text: fmt.Sprintf("Quick Guard protected %s's team!", s.Sides[side].Trainer)})
}

func applyWideGuardSetter(s *BattleState, side int, log *[]LogLine) {
	sc := &s.Sides[side].Conditions
	if sc.WideGuard != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	sc.WideGuard = &WideGuardState{TurnsLeft: 1}
	*log = append(*log, LogLine{Type: "wideguard", Side: side,
		Text: fmt.Sprintf("Wide Guard protected %s's team!", s.Sides[side].Trainer)})
}

// tickGuards ticks down the one-turn Quick/Wide Guard flags. Called
// from tickBuffs (which handles the same shape for Tailwind /
// Safeguard / Mist).
func tickGuards(s *BattleState, side int) {
	sc := &s.Sides[side].Conditions
	if sc.QuickGuard != nil {
		sc.QuickGuard.TurnsLeft--
		if sc.QuickGuard.TurnsLeft <= 0 {
			sc.QuickGuard = nil
		}
	}
	if sc.WideGuard != nil {
		sc.WideGuard.TurnsLeft--
		if sc.WideGuard.TurnsLeft <= 0 {
			sc.WideGuard = nil
		}
	}
}
