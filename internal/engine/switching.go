package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// doSwitch brings in a teammate. Stat stages and volatiles reset on both the
// outgoing and incoming Pokémon. The Sleep counter on the outgoing Pokémon
// resets too (Gen 5+ semantics — see docs/battle-state.md).
func doSwitch(s *BattleState, side, idx int, rng *RNG, log *[]LogLine) {
	doSwitchWithCarry(s, side, idx, nil, rng, log)
}

// batonCarry is the subset of the outgoing's state that Baton Pass copies
// onto the incoming. Stages always transfer; among volatiles, Confusion and
// Substitute do (Leech Seed / Encore aren't modeled yet). Flinch /
// MovedLast / Charging / MustRecharge are turn-scheduling state and never
// pass under canonical Showdown.
type batonCarry struct {
	Stages     Stages
	Confusion  *ConfusionState
	Substitute *SubstituteState
}

// doSwitchWithCarry performs a switch, optionally transferring the outgoing
// Pokémon's stat stages and select volatiles to the incoming (Baton Pass).
// carry == nil is the plain reset-on-switch path doSwitch uses.
func doSwitchWithCarry(s *BattleState, side, idx int, carry *batonCarry, rng *RNG, log *[]LogLine) {
	sd := &s.Sides[side]
	if idx < 0 || idx >= len(sd.Team) || idx == sd.Active || sd.Team[idx].Fainted {
		return
	}
	out := &sd.Team[sd.Active]
	// Switch-out ability hook (Natural Cure, Regenerator) runs before the
	// outgoing's status / stages / volatiles are reset, so the hook can
	// observe what it's clearing.
	applyOnSwitchOut(out, side, log)
	out.Stages = Stages{}
	out.Volatiles = Volatiles{}
	if out.Status == StatusSleep {
		out.SleepTurns = 0
	}
	if !out.Fainted {
		*log = append(*log, LogLine{Type: "switch", Side: side, Text: fmt.Sprintf("%s, come back!", out.Name)})
	}
	sd.Active = idx
	in := &sd.Team[idx]
	in.Stages = Stages{}
	in.Volatiles = Volatiles{}
	if carry != nil {
		in.Stages = carry.Stages
		if carry.Confusion != nil {
			cc := *carry.Confusion
			in.Volatiles.Confusion = &cc
		}
		if carry.Substitute != nil {
			ss := *carry.Substitute
			in.Volatiles.Substitute = &ss
		}
	}
	*log = append(*log, LogLine{Type: "switch", Side: side, Text: fmt.Sprintf("Go, %s!", in.Name)})
	// Entry hazards fire before the ability switch-in hook: canon order is
	// Stealth Rock → Spikes → Toxic Spikes → Intimidate/Drizzle/etc. A
	// hazard KO short-circuits the rest (applyOnSwitchIn no-ops on a
	// fainted active).
	applyHazardsOnSwitchIn(s, side, log)
	applyOnSwitchIn(s, side, log)
	// Slot-condition switch-in consumer: Healing Wish fully restores
	// the incoming. Runs after hazards/abilities so it undoes any
	// chip and lands the incoming at full HP regardless of what fired
	// during entry.
	applySlotConditionsOnSwitchIn(s, side, log)
	// Hazard chip on entry can put the incoming Pokémon straight into its
	// berry's range. Checked after Healing Wish so a full restore isn't
	// immediately followed by a pointless Sitrus.
	applyItemHPTrigger(s, side, rng, log)
}

// applySelfSwitch handles U-turn / Volt Switch / Flip Turn / Teleport (plain
// "normal") and Baton Pass ("copyvolatile"). Called at the tail of
// executeMove: if the user is alive and has a live bench member, the switch
// fires immediately so a same-turn slower foe sees (and can target) the
// replacement. The bench member is the lowest-indexed live teammate —
// deterministic across replays, matching how the AI / picker controllers
// already resolve faint replacements today.
func applySelfSwitch(s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) {
	if m.SelfSwitch == "" {
		return
	}
	atk := s.Active(side)
	if atk.Fainted || atk.HP <= 0 {
		return
	}
	sd := &s.Sides[side]
	target := -1
	for i := range sd.Team {
		if i != sd.Active && !sd.Team[i].Fainted {
			target = i
			break
		}
	}
	if target == -1 {
		return
	}
	var carry *batonCarry
	if m.SelfSwitch == "copyvolatile" {
		c := batonCarry{Stages: atk.Stages}
		if atk.Volatiles.Confusion != nil {
			cc := *atk.Volatiles.Confusion
			c.Confusion = &cc
		}
		if atk.Volatiles.Substitute != nil {
			ss := *atk.Volatiles.Substitute
			c.Substitute = &ss
		}
		carry = &c
	}
	doSwitchWithCarry(s, side, target, carry, rng, log)
}
