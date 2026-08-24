package engine

import (
	"fmt"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

func init() {
	specs.RegisterVolatile("protect")
	specs.RegisterVolatile("endure")
	registerVolatile("protect", func(p *Pokemon, side int, _ domain.Move, _ *BattleState, rng *RNG, log *[]LogLine) {
		applyProtectMove(p, side, false, rng, log)
	})
	registerVolatile("endure", func(p *Pokemon, side int, _ domain.Move, _ *BattleState, rng *RNG, log *[]LogLine) {
		applyProtectMove(p, side, true, rng, log)
	})
}

// applyProtectMove handles the volatileStatus="protect" / "endure" slug —
// both moves share Showdown's "stall" counter, which divides the success
// chance by 3 for every consecutive successful use of any stall move
// (Protect, Detect, Endure share the chain). On success, sets the
// matching one-turn volatile and increments ProtectCounter. On a failed
// roll, the volatile doesn't stick and the counter resets to 0 (canon:
// a broken chain takes the user back to a 100% chance next time).
//
// Caller is responsible for resetting ProtectCounter when the user takes
// any non-stall action this turn — handled by the defer in executeMove.
func applyProtectMove(p *Pokemon, side int, endure bool, rng *RNG, log *[]LogLine) {
	chance := protectChance(p.Volatiles.ProtectCounter)
	if !rng.Chance(chance) {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		p.Volatiles.ProtectCounter = 0
		return
	}
	if endure {
		p.Volatiles.Endure = true
		*log = append(*log, LogLine{
			Type: "status", Side: side,
			Text: fmt.Sprintf("%s braced itself!", p.Name),
		})
	} else {
		// Deliberately silent. "X protected itself!" is the block-time
		// announcement — executeMove emits it (Type "protect") at the moment
		// the shield actually turns something away. Announcing here as well
		// printed the identical sentence twice for a single block, once from
		// each site. Raising the shield is already visible as "X used
		// Protect!", so a Protect that nothing attacks into still reads.
		p.Volatiles.Protect = true
	}
	p.Volatiles.ProtectCounter++
}

// protectChance returns the integer percent chance that a Protect / Endure
// attempt lands at the given consecutive-use count. Canon Gen 4+: 1/(3^n).
// We collapse the long tail to a 1% floor (Showdown's int math bottoms out
// near 0.1% by the 6th consecutive use; one-in-100 is close enough and
// keeps the function's range honest).
func protectChance(count int) int {
	switch count {
	case 0:
		return 100
	case 1:
		return 33
	case 2:
		return 11
	case 3:
		return 4
	}
	return 1
}

// protectBlocksFoeMove reports whether def's Protect volatile should stop m
// from connecting.
//
// Canon blocks a move if and only if it carries Showdown's `protect` flag —
// Battle#checkMoveBypassesProtect, sim/battle.ts. The default is *allow* and
// the flag is the list of exceptions, which is the opposite of what this
// predicate used to do: it blocked everything that was not self-targeted and
// listed the escapes. That inversion, plus a data pipeline that marked the
// entry hazards as foe-targeting, made "press Protect" the answer to a hazard
// lead — Stealth Rock, Spikes, Toxic Spikes and Defog all bounced off a shield
// they never touch in a real game.
//
// What the flag deliberately does not cover, and what therefore goes through:
// the entry hazards (upstream target foeSide), the field moves (target all —
// weather, terrain, the rooms, Haze, Perish Song), the side-support moves
// (Reflect, Light Screen, Safeguard, Tailwind), and the flagless foe-facing
// status moves — Roar, Whirlwind, Mean Look, Curse, Psych Up. Damaging moves
// are unaffected: every one in this dataset carries the flag except Feint and
// Phantom Force, which carry bypass-protect instead.
//
// One divergence left standing and worth naming: canon's breakProtect does not
// merely pass through, it *removes* the volatile and resets the stall chain for
// the rest of the turn (battle-actions.ts#hitStepBreakProtect). This only skips
// the check. Unobservable today — neither of our two bypass moves carries
// `protect`, so nothing else in the turn can be blocked anyway.
//
// Endure is NOT involved here — Endure doesn't intercept the move, it
// clamps the damage figure in dealDamage so the user survives at 1 HP.
func protectBlocksFoeMove(def *Pokemon, m domain.Move) bool {
	if def == nil || !def.Volatiles.Protect {
		return false
	}
	if m.Target == domain.TargetSelf {
		return false
	}
	if !m.HasFlag("protect") {
		return false
	}
	if m.HasFlag("bypass-protect") {
		return false
	}
	return true
}
