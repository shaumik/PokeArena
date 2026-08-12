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
// Substitute do. Flinch / MovedLast / Charging / MustRecharge are
// turn-scheduling state and never pass under canonical Showdown.
//
// The carry set is narrower than canon's: Leech Seed is modeled (see
// LeechSeedState) and canon passes it along with the rest of the receiver's
// inherited baggage, but it is not carried here yet. Widening the set is a
// behavior change, so it is left to its own commit rather than folded into a
// comment fix.
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
	// A Pokémon that switched in does not act this turn. Zoom Lens asks "will
	// the target still move after me?", so from its holder's point of view a
	// fresh switch-in is settled exactly like one that has already moved.
	in.Volatiles.MovedThisTurn = true
	// The incoming's volatiles were just zeroed, so re-mirror the field's Magic
	// Room state onto it. Items are suppressed by the room, not by the mon.
	syncMagicRoomFlags(s)
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
// replacement.
//
// want is the bench slot the controller asked for (Action.SwitchTarget), or
// nil for "you pick". Choosing the pivot target is the whole point of a pivot
// move — Volt Switch into the counter you actually want, Baton Pass +2 onto
// the sweeper that can use it — and without it party order silently overrode
// the tactical decision: the engine always took the lowest-indexed live
// teammate, so a Baton Pass was a boost handed to whoever happened to sit
// earliest at team-build time.
//
// nil still means lowest-indexed live teammate. That keeps replays and
// unaware controllers byte-identical, and it stays deterministic — the fix
// here is to let the caller aim, not to randomize.
func applySelfSwitch(s *BattleState, side int, m domain.Move, want *int, rng *RNG, log *[]LogLine) {
	if m.SelfSwitch == "" {
		return
	}
	atk := s.Active(side)
	if atk.Fainted || atk.HP <= 0 {
		return
	}
	sd := &s.Sides[side]
	target := selfSwitchTarget(sd, want)
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

// selfSwitchTarget resolves the bench slot a self-switch brings in. want is
// the controller's choice or nil; -1 comes back when the side has nobody left
// to send, which is how a U-turn on a last Pokémon quietly does nothing.
//
// An out-of-range, fainted or already-active choice falls back to the default
// rather than failing the move. LegalActions only ever offers legal targets,
// so this path means a controller went around it — and canon has no "your
// pivot fizzled because you aimed badly" outcome to imitate.
func selfSwitchTarget(sd *Side, want *int) int {
	if want != nil {
		i := *want
		if i >= 0 && i < len(sd.Team) && i != sd.Active && !sd.Team[i].Fainted {
			return i
		}
	}
	for i := range sd.Team {
		if i != sd.Active && !sd.Team[i].Fainted {
			return i
		}
	}
	return -1
}
