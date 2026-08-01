package engine

import "fmt"

// lockedmove.go implements the "rampage" moves — Outrage, Thrash, Petal Dance.
// The user is locked into the move for 2-3 turns, can't switch out, and
// collapses into confusion ("fatigue") when the rampage ends. Showdown drives
// this from a self-applied `lockedmove` volatile whose existence the move-
// coverage audit can't see (it lives in JS callbacks), so the behavior is
// lifted here and gated by move ID, with lockedmove_test.go as its guardrail.
//
// The mechanic mirrors the two-turn Charging volatile: LegalActions pins the
// user to the locked slot (see battle.go) and executeMove resolves the move
// from the lock rather than the submitted index. The differences are that the
// rampage strikes every turn (not charge-then-strike) and that PP is paid only
// on the first turn.
//
// Degradations from canon, consistent with the rest of the engine:
//   - The rampage continues through a miss or a type-immune target (Gen-6+
//     Outrage into a Fairy still locks and still fatigues); it ends early only
//     when the user is prevented from acting (sleep / paralysis / flinch /
//     confusion self-hit), handled by clearing the lock on the canAct gate.
//   - Fatigue confusion ignores Misty Terrain (which would otherwise block
//     confusion on a grounded user).

// lockedMoveIDs are the moves that lock the user into a multi-turn rampage.
// Only outrage and thrash ship in the Gen-1 learnset today; petal-dance is
// listed so it works the moment it lands.
var lockedMoveIDs = map[string]bool{
	"outrage":     true,
	"thrash":      true,
	"petal-dance": true,
}

func isLockedMove(id string) bool { return lockedMoveIDs[id] }

// lockedMoveDuration rolls how many turns the rampage lasts: 2 or 3,
// uniformly. Held-item modifiers (none are modeled) would extend this.
func lockedMoveDuration(rng *RNG) int { return rng.Range(2, 3) }

// tickLockedMove counts down an active rampage at the end of a turn the user
// acted. When the counter hits zero the lock clears and the user becomes
// confused from fatigue. A no-op when no rampage is active, so it's safe to
// arm as a deferred call on every move resolution.
func tickLockedMove(p *Pokemon, side int, rng *RNG, log *[]LogLine) {
	lm := p.Volatiles.LockedMove
	if lm == nil {
		return
	}
	lm.Turns--
	if lm.Turns > 0 {
		return
	}
	p.Volatiles.LockedMove = nil
	if p.Volatiles.Confusion == nil {
		p.Volatiles.Confusion = &ConfusionState{Turns: rng.Range(2, 5)}
	}
	*log = append(*log, LogLine{
		Type: "status", Side: side,
		Text: fmt.Sprintf("%s became confused due to fatigue!", p.Name),
	})
	// Fatigue sets the volatile directly rather than going through
	// applyConfusionVolatile, so the held-item cure has to be invoked here too
	// — otherwise a Lum/Persim holder finishes an Outrage and sits confused for
	// 2-5 turns with an unused berry in hand.
	applyItemStatusCure(p, side, log)
}
