package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// SubstituteState is the doll's live durability. HP starts at MaxHP/4 (the
// HP the owner spent at setup) and ticks down as foe damage absorbs into
// the doll. When HP reaches 0 the doll breaks: the volatile clears and any
// overflow damage from the breaking hit is discarded (Gen 5+ semantics).
// MaxHP is kept for UI/log use and never changes after setup.
type SubstituteState struct {
	HP    int `json:"hp"`
	MaxHP int `json:"max_hp"`
}

// substituteCostDenom is the canonical fraction the user pays at setup:
// 1/4 of MaxHP. Integer division — fractional remainders are kept by the
// user (a 21 MaxHP fixture pays 5 HP and the doll has 5 HP, not 5.25).
const substituteCostDenom = 4

// applySubstituteSetup is the volatileStatus="substitute" handler — Target
// is "self" so the move's Primary carries a Volatile field that routes
// here. Costs MaxHP/4 from the user and stands up a doll with that many
// HP. Fails (loud "But it failed!") if the user already has a sub up,
// can't afford the cost without fainting, or has a MaxHP so low that
// integer division zeroes the cost.
func applySubstituteSetup(p *Pokemon, side int, log *[]LogLine) {
	if p.Volatiles.Substitute != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	cost := p.MaxHP / substituteCostDenom
	if cost < 1 || p.HP <= cost {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.HP -= cost
	p.Volatiles.Substitute = &SubstituteState{HP: cost, MaxHP: cost}
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s put up a substitute! (-%d)", p.Name, cost)})
}

// hasSubstitute is a nil-safe predicate used by the dispatch in turn.go and
// the AI heuristic.
func hasSubstitute(p *Pokemon) bool {
	return p != nil && p.Volatiles.Substitute != nil
}

// bypassesSubstitute reports whether a move from atk ignores a substitute
// on the defender. Canon (Gen 6+):
//
//   - sound-flagged moves bypass the doll
//   - bypass-sub-flagged moves (Encore, Perish Song, Roar / Whirlwind,
//     Memento, Disable, ...) bypass the doll
//
// Infiltrator isn't modeled yet; when it lands, OR it in here so the
// holder's moves treat foe subs as transparent.
func bypassesSubstitute(m domain.Move, atk *Pokemon) bool {
	if m.HasFlag("sound") || m.HasFlag("bypass-sub") {
		return true
	}
	return false
}

// applyDamageToSubstitute soaks dmg into the defender's doll. Returns the
// amount the doll actually absorbed — capped at the doll's HP, so a
// breaking hit's overflow does NOT pass through to the holder (canon
// Gen 5+; older gens leaked the overflow). Callers use the returned
// value as "damage dealt" for drain heal and recoil accounting — both
// canonically scale off the damage figure even when the doll ate the hit.
//
// Emits the canonical "X's substitute took the damage!" line and, on a
// break, the "X's substitute faded!" line. The volatile is cleared
// on break here, so the very next attack that turn lands on the holder.
func applyDamageToSubstitute(def *Pokemon, defSide int, dmg int, log *[]LogLine) int {
	sub := def.Volatiles.Substitute
	if sub == nil {
		return 0
	}
	absorbed := dmg
	if absorbed > sub.HP {
		absorbed = sub.HP
	}
	sub.HP -= absorbed
	*log = append(*log, LogLine{Type: "damage", Side: defSide,
		Text: fmt.Sprintf("%s's substitute took the damage! (-%d)", def.Name, absorbed)})
	if sub.HP <= 0 {
		def.Volatiles.Substitute = nil
		*log = append(*log, LogLine{Type: "status", Side: defSide,
			Text: fmt.Sprintf("%s's substitute faded!", def.Name)})
	}
	return absorbed
}
