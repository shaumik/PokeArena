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
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: fmt.Sprintf("%s braced itself!", p.Name)})
	} else {
		p.Volatiles.Protect = true
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: fmt.Sprintf("%s protected itself!", p.Name)})
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

// protectBlocksFoeMove reports whether def's Protect volatile should stop
// m from connecting. Self-targeting moves bypass (Protect doesn't sit
// between the user and its own buffs/heals); bypass-protect-flagged moves
// (Feint, Hyperspace Hole, Phantom Force) ignore the shield.
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
	if m.HasFlag("bypass-protect") {
		return false
	}
	return true
}

// isProtectionMove reports whether m is in the stall family — used by
// executeMove to decide whether to reset ProtectCounter after a move
// resolves. Quick Guard / Wide Guard / King's Shield / Spiky Shield /
// Baneful Bunker / Obstruct aren't modeled yet; when they land, add
// their IDs here so they share the stall counter.
func isProtectionMove(m domain.Move) bool {
	switch m.ID {
	case "protect", "detect", "endure":
		return true
	}
	return false
}
