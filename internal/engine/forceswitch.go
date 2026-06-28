package engine

import (
	"fmt"
)

// forceswitch.go implements Move.ForceSwitch: the target is forced
// to switch to a random live bench teammate after the move resolves.
// Roar / Whirlwind are status variants (no damage; the switch is the
// whole point); Circle Throw / Dragon Tail are damaging variants
// (damage then switch). All four are priority -6 in the dataset, so
// they reliably go last — no engine work for that, the priority
// field carries it.
//
// Showdown semantics: a forced switch is the foe's choice in
// canon — the foe controller picks who comes in. We pick randomly
// via the seeded RNG so the choice is deterministic per replay
// but unpredictable per battle. Trapping abilities (Shadow Tag,
// Arena Trap, Magnet Pull) and abilities that block phazing
// (Suction Cups) aren't modeled; if they ever ship, gate them
// here.

// applyForceSwitch picks a random live bench Pokémon on the foe's
// side and switches them in. atkSide is the attacker; the foe is
// 1-atkSide. Returns true if a switch happened.
//
// A fainted foe (Circle Throw KO'd them) or a foe with no live
// bench is a no-op. The caller decides whether to emit a fail log:
// status variants want it, damaging variants don't (the damage was
// the visible effect).
func applyForceSwitch(s *BattleState, atkSide int, rng *RNG, log *[]LogLine) bool {
	foeSide := 1 - atkSide
	foe := s.Active(foeSide)
	if foe.Fainted {
		return false
	}
	sd := &s.Sides[foeSide]
	candidates := make([]int, 0, len(sd.Team)-1)
	for i := range sd.Team {
		if i != sd.Active && !sd.Team[i].Fainted {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return false
	}
	pick := candidates[rng.IntN(len(candidates))]
	*log = append(*log, LogLine{
		Type: "force-switch", Side: foeSide,
		Text: fmt.Sprintf("%s was dragged out!", foe.Name),
	})
	doSwitch(s, foeSide, pick, log)
	return true
}
