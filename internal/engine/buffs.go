package engine

import (
	"fmt"

	"pokearena/internal/specs"
)

// buffs.go owns the per-side helper conditions that aren't damage screens
// and aren't entry hazards: Tailwind (speed mult), Safeguard (status
// shield), Mist (stat-drop shield). Each is a small TurnsLeft counter
// with no extra payload. State structs are colocated here; the bag they
// live in (SideConditions) is declared in screens.go alongside the
// damage-screen state, since that file grew the aggregate first.

func init() {
	specs.RegisterSideCondition("tailwind")
	specs.RegisterSideCondition("safeguard")
	specs.RegisterSideCondition("mist")
	registerSideCondition("tailwind", applyTailwindSetter)
	registerSideCondition("safeguard", applySafeguardSetter)
	registerSideCondition("mist", applyMistSetter)
}

// TailwindState is a per-side speed-doubling buff. Gen 5+: 4 turns,
// ticked down at end of turn. While active, every member of the user's
// side gets a ×2 multiplier on effectiveSpeed for turn-ordering only;
// move damage is not affected.
type TailwindState struct {
	TurnsLeft int `json:"turns_left"`
}

// SafeguardState is a per-side non-volatile-status shield. 5 turns,
// ticked down at end of turn. While active, foe-induced status moves
// (burn, poison, toxic, paralysis, sleep, freeze) and the Confusion
// volatile are blocked outright. Self-inflicted statuses (Rest) bypass
// — Safeguard is checked only when the effect comes from the other
// side. Yawn bypasses Safeguard in canon; not modeled because Yawn
// itself isn't modeled yet.
type SafeguardState struct {
	TurnsLeft int `json:"turns_left"`
}

// MistState is a per-side stat-drop shield. 5 turns, ticked down at end
// of turn. While active, foe-induced stat drops are blocked silently
// — the foe's move still hits, but the drop never lands. Self-induced
// drops (Overheat's -2 SpA on the user, etc.) bypass since applyStages
// is the un-foe path; Mist sits only on applyStagesFromFoe.
type MistState struct {
	TurnsLeft int `json:"turns_left"`
}

const (
	defaultTailwindTurns  = 4
	defaultSafeguardTurns = 5
	defaultMistTurns      = 5
)

// applyTailwindSetter spawns Tailwind on the user's side. Re-setting
// while already active fails (canon — Tailwind into Tailwind is a
// wasted PP). Called from applyStatusMove's side-condition dispatch.
func applyTailwindSetter(s *BattleState, side int, log *[]LogLine) {
	sc := &s.Sides[side].Conditions
	if sc.Tailwind != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	sc.Tailwind = &TailwindState{TurnsLeft: defaultTailwindTurns}
	*log = append(*log, LogLine{
		Type: "tailwind", Side: side,
		Text: fmt.Sprintf("The Tailwind blew from behind %s's team!", s.Sides[side].Trainer),
	})
}

func applySafeguardSetter(s *BattleState, side int, log *[]LogLine) {
	sc := &s.Sides[side].Conditions
	if sc.Safeguard != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	sc.Safeguard = &SafeguardState{TurnsLeft: defaultSafeguardTurns}
	*log = append(*log, LogLine{
		Type: "safeguard", Side: side,
		Text: fmt.Sprintf("%s's team became cloaked in a mystical veil!", s.Sides[side].Trainer),
	})
}

func applyMistSetter(s *BattleState, side int, log *[]LogLine) {
	sc := &s.Sides[side].Conditions
	if sc.Mist != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	sc.Mist = &MistState{TurnsLeft: defaultMistTurns}
	*log = append(*log, LogLine{
		Type: "mist", Side: side,
		Text: fmt.Sprintf("%s's team became shrouded in mist!", s.Sides[side].Trainer),
	})
}

// tickBuffs decrements each active buff on side and clears any whose
// TurnsLeft hits zero. Same shape as tickScreens — called after it in
// ResolveTurn's end-of-turn block. No per-turn flavor lines; only the
// expiry message lands.
func tickBuffs(s *BattleState, side int, log *[]LogLine) {
	sc := &s.Sides[side].Conditions
	if sc.Tailwind != nil {
		sc.Tailwind.TurnsLeft--
		if sc.Tailwind.TurnsLeft <= 0 {
			sc.Tailwind = nil
			*log = append(*log, LogLine{
				Type: "tailwind", Side: side,
				Text: fmt.Sprintf("%s's Tailwind petered out.", s.Sides[side].Trainer),
			})
		}
	}
	if sc.Safeguard != nil {
		sc.Safeguard.TurnsLeft--
		if sc.Safeguard.TurnsLeft <= 0 {
			sc.Safeguard = nil
			*log = append(*log, LogLine{
				Type: "safeguard", Side: side,
				Text: fmt.Sprintf("%s's team is no longer protected by Safeguard.", s.Sides[side].Trainer),
			})
		}
	}
	if sc.Mist != nil {
		sc.Mist.TurnsLeft--
		if sc.Mist.TurnsLeft <= 0 {
			sc.Mist = nil
			*log = append(*log, LogLine{
				Type: "mist", Side: side,
				Text: fmt.Sprintf("%s's team is no longer shrouded in mist.", s.Sides[side].Trainer),
			})
		}
	}
	// Quick Guard / Wide Guard tick down too — one-turn flags with
	// no live effect in singles (no allies / spread moves) but the
	// timer still needs to clear so a second-turn re-use can land.
	tickGuards(s, side)
}

// sideSpeedMult returns the speed multiplier the side's buffs apply
// to its active for turn-ordering. Tailwind doubles speed; anything
// else is 1.0. Called from goesFirst.
func sideSpeedMult(s *BattleState, side int) float64 {
	if s.Sides[side].Conditions.Tailwind != nil {
		return 2.0
	}
	return 1.0
}

// safeguardBlocksFoeStatus reports whether the target's side has an
// active Safeguard that should refuse a foe-induced non-volatile
// status. Caller is responsible for emitting the "Safeguard protected
// X!" log line on a block (so a silent secondary-status block reads
// the same as a normal foe-move miss in the log).
func safeguardBlocksFoeStatus(s *BattleState, tgtSide int) bool {
	if s == nil {
		return false
	}
	return s.Sides[tgtSide].Conditions.Safeguard != nil
}

// safeguardBlocksFoeVolatile reports whether the target's side has an
// active Safeguard that should refuse a foe-induced volatile. Today
// only Confusion is gated; Yawn bypasses Safeguard in canon and the
// rest of our volatiles aren't status-shaped.
func safeguardBlocksFoeVolatile(s *BattleState, tgtSide int, slug string) bool {
	if s == nil {
		return false
	}
	if s.Sides[tgtSide].Conditions.Safeguard == nil {
		return false
	}
	return slug == "confusion"
}

// mistBlocksFoeDrop reports whether the target's side has an active
// Mist that should refuse a foe-induced stat drop. Called from
// applyStagesFromFoe before the ability gate so the "Mist!" log line
// takes precedence over Clear Body's.
func mistBlocksFoeDrop(s *BattleState, tgtSide int) bool {
	if s == nil {
		return false
	}
	return s.Sides[tgtSide].Conditions.Mist != nil
}
