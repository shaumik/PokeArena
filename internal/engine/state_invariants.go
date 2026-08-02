package engine

import "fmt"

// OnInvariantViolation, when non-nil, is called with any breach
// ValidateStateInvariants finds after each resolved turn and each resolved
// replacement. Nil by default, and the check is skipped entirely when it is nil
// — production pays nothing, and no new failure mode is introduced into a live
// battle.
//
// It exists because "test-only" meant "only the dozen tests that remember to
// call it", out of several hundred that resolve turns. Setting it in a TestMain
// makes every one of them an invariant test.
//
// Be honest about the size of that win. Both corruptions found in this engine so
// far — a Magic Room mirror desynced by faint(), and a replace phase dropping to
// PhaseChoosing with a fainted active — were measured against this hook after
// the fact, and *neither* would have been caught by it, because no test outside
// the dedicated ones happens to produce those states. What it did catch, on its
// first run, was a fixture in TestMoxieBoostsOnKO that set a Snorlax to 999 HP
// against a MaxHP of 235 — a state the engine cannot produce, quietly exercised
// for as long as that test has existed.
//
// So this is cheap prospective insurance and a fixture-quality check, not a net
// that would have caught the last two bugs. It is worth having on those terms.
//
// Tests should set it to something that fails loudly. It is deliberately a plain
// package var rather than a parameter: threading a callback through ResolveTurn
// would change a signature that a lot of callers depend on, for a facility only
// tests use.
var OnInvariantViolation func(error)

// checkInvariants runs the invariant check if anyone is listening. The nil test
// is the fast path and the common one.
func checkInvariants(s *BattleState) {
	if OnInvariantViolation == nil {
		return
	}
	if err := ValidateStateInvariants(s); err != nil {
		OnInvariantViolation(err)
	}
}

// ValidateStateInvariants checks the structural invariants every BattleState
// must satisfy between turns. Used by tests as a guardrail: any path that
// leaves the state inconsistent (corrupted phase, negative HP, volatiles
// out of sync with status, etc.) trips a loud error here instead of silently
// breaking gameplay several turns later.
//
// Returns the first violation as an error; nil means the state is well-formed.
// Phase==Choosing implies neither active is Fainted; Phase==Replace implies
// the Replace flags match each active's Fainted flag; Phase==Ended implies
// Winner is set and no further turns make sense.
//
// See OnInvariantViolation for how to have this run automatically after every
// resolved turn, rather than only where a test remembers to call it.
func ValidateStateInvariants(s *BattleState) error {
	for i := 0; i < 2; i++ {
		sd := &s.Sides[i]
		if sd.Active < 0 || sd.Active >= len(sd.Team) {
			return fmt.Errorf("side %d: Active index %d out of range [0,%d)", i, sd.Active, len(sd.Team))
		}
		// Magic Room's per-Pokémon mirror must agree with the field. Checked
		// *after* the bounds check above, not before: s.Active indexes the team
		// slice unguarded, so reading it first turned an out-of-range Active —
		// the very corruption the next line reports — into a panic.
		if got := s.Active(i).Volatiles.MagicRoomHere; got != (s.PseudoWeather.MagicRoom != nil) {
			return fmt.Errorf("side %d active %s: MagicRoomHere=%v but the field says %v — "+
				"a syncMagicRoomFlags call site is missing",
				i, s.Active(i).Name, got, s.PseudoWeather.MagicRoom != nil)
		}
		for j := range sd.Team {
			p := &sd.Team[j]
			if p.HP < 0 {
				return fmt.Errorf("side %d team[%d] %s: HP=%d is negative", i, j, p.Name, p.HP)
			}
			if p.HP > p.MaxHP {
				return fmt.Errorf("side %d team[%d] %s: HP=%d exceeds MaxHP=%d", i, j, p.Name, p.HP, p.MaxHP)
			}
			if p.Fainted && p.HP != 0 {
				return fmt.Errorf("side %d team[%d] %s: Fainted but HP=%d", i, j, p.Name, p.HP)
			}
			if !p.Fainted && p.HP == 0 {
				return fmt.Errorf("side %d team[%d] %s: HP=0 but Fainted=false", i, j, p.Name)
			}
			if p.Volatiles.Nightmare && p.Status != StatusSleep {
				return fmt.Errorf("side %d team[%d] %s: Nightmare set but Status=%q (must be sleep)", i, j, p.Name, p.Status)
			}
		}
	}
	switch s.Phase {
	case PhaseChoosing:
		for i := 0; i < 2; i++ {
			if s.Active(i).Fainted {
				return fmt.Errorf("PhaseChoosing but side %d active %q is fainted", i, s.Active(i).Name)
			}
			if s.Replace[i] {
				return fmt.Errorf("PhaseChoosing but Replace[%d]=true", i)
			}
		}
	case PhaseReplace:
		anyReplace := false
		for i := 0; i < 2; i++ {
			if s.Replace[i] != s.Active(i).Fainted {
				return fmt.Errorf("PhaseReplace: Replace[%d]=%v but Active.Fainted=%v",
					i, s.Replace[i], s.Active(i).Fainted)
			}
			if s.Replace[i] {
				anyReplace = true
			}
		}
		if !anyReplace {
			return fmt.Errorf("PhaseReplace but neither side needs to replace")
		}
	case PhaseEnded:
		if s.Winner < 0 || s.Winner > 2 {
			return fmt.Errorf("PhaseEnded but Winner=%d (must be 0, 1, or 2)", s.Winner)
		}
	default:
		return fmt.Errorf("unknown Phase %q", s.Phase)
	}
	return nil
}
