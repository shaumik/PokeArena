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
// but unpredictable per battle.
//
// What blocks a drag is a different question from what blocks a switch, and
// canon draws the line with two separate events. onTrapPokemon blockers —
// Arena Trap, Magnet Pull, Mean Look, the partial traps — stop a Pokémon
// *choosing* to flee and do nothing against a phazer, which is why they gate
// LegalActions and not this file. onDragOut blockers — Ingrain, and Suction
// Cups and Guard Dog if a species carrying them is ever synced in — stop the
// drag itself, and belong here.
//
// The distinction is load-bearing for Shed Shell, which is onTrapPokemon only
// (ps/data/items.ts: it sets pokemon.trapped = false and has no onDragOut). It
// beats every trap and the holder's own Ingrain when the holder chooses to
// leave, and does nothing at all against a Roar. Consulting
// itemAllowsSwitchOut here would be wrong.

// applyForceSwitch picks a random live bench Pokémon on the foe's
// side and switches them in. atkSide is the attacker; the foe is
// 1-atkSide.
//
// Returns whether the move resolved rather than whether anybody actually
// moved: the caller uses it to decide on a "But it failed!" line, and a drag
// refused by Ingrain is not a failure in canon. Upstream's forceSwitch prints
// -fail only when the DragOut event returns exactly false; Ingrain's handler
// returns null, which announces the roots and stops there
// (ps/sim/battle-actions.ts:1353, ps/data/moves.ts ingrain).
//
// A fainted foe (Circle Throw KO'd them) or a foe with no live
// bench is the false case. The caller decides whether to emit a fail log:
// status variants want it, damaging variants don't (the damage was
// the visible effect).
func applyForceSwitch(s *BattleState, atkSide int, rng *RNG, log *[]LogLine) bool {
	foeSide := 1 - atkSide
	foe := s.Active(foeSide)
	if foe.Fainted {
		return false
	}
	// Rooted: the drag is refused and announced, and no RNG is drawn. The early
	// return matters beyond tidiness — the phazing path runs in the golden
	// replay corpus, so a refusal that still consumed a draw would shift every
	// subsequent roll in games where nothing was rooted to begin with.
	if foe.Volatiles.Ingrain {
		*log = append(*log, LogLine{
			Type: "force-switch", Side: foeSide,
			Text: fmt.Sprintf("%s is anchored in place with its roots!", foe.Name),
		})
		return true
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
	doSwitch(s, foeSide, pick, rng, log)
	return true
}
