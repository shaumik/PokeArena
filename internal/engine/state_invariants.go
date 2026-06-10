package engine

import "fmt"

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
func ValidateStateInvariants(s *BattleState) error {
	for i := 0; i < 2; i++ {
		sd := &s.Sides[i]
		if sd.Active < 0 || sd.Active >= len(sd.Team) {
			return fmt.Errorf("side %d: Active index %d out of range [0,%d)", i, sd.Active, len(sd.Team))
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
