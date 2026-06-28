package engine

import (
	"fmt"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

// statusvols.go owns the status-adjacent volatiles — each has its own
// gameplay shape but they share a "harass the target every turn" feel:
//
//   Attract       — 50% immobilize per turn (gender check degraded;
//                   gender isn't modeled, so the volatile always applies
//                   and the per-turn roll runs unconditionally)
//   Yawn          — 1-turn delayed Sleep
//   Nightmare     — 1/4 MaxHP chip per end-of-turn while target is asleep
//   Curse         — Ghost variant chips foe 1/4 MaxHP/turn for 50% user
//                   HP; non-Ghost variant boosts +1 Atk / +1 Def / -1 Spe
//                   on the user. Routed by user type in applyStatusMove.
//   Destiny Bond  — KO the attacker if the holder faints to a direct
//                   attack this turn. Cleared at end-of-turn either way.

func init() {
	specs.RegisterVolatile("attract")
	specs.RegisterVolatile("yawn")
	specs.RegisterVolatile("nightmare")
	specs.RegisterVolatile("curse")
	specs.RegisterVolatile("destinybond")
	registerVolatile("attract", applyAttractVolatile)
	registerVolatile("yawn", applyYawnVolatile)
	registerVolatile("nightmare", applyNightmareVolatile)
	registerVolatile("curse", applyCurseFoeVolatile)
	registerVolatile("destinybond", applyDestinyBondVolatile)
}

// YawnState is a 2-tick delayed-Sleep counter. Apply sets TurnsLeft=2;
// end-of-turn decrements; sleep is inflicted when TurnsLeft would drop
// below 1 (so the canonical 1-turn delay holds: apply on turn N, sleep
// at end of turn N+1).
type YawnState struct {
	TurnsLeft int `json:"turns_left"`
}

// applyAttractVolatile sets the infatuation flag on the target. Gender
// isn't modeled, so the gender-match guard is skipped — Attract always
// succeeds. Re-applying while already attracted is a no-op (canon).
func applyAttractVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Attract {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Attract = true
	*log = append(*log, LogLine{
		Type: "attract", Side: side,
		Text: fmt.Sprintf("%s fell in love!", p.Name),
	})
}

// applyYawnVolatile sets a 2-tick countdown that lands a Sleep at the
// next end-of-turn. Fails if the target already has a non-volatile
// status or is already yawning.
func applyYawnVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Yawn != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if p.Status != StatusNone {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Yawn = &YawnState{TurnsLeft: 2}
	*log = append(*log, LogLine{
		Type: "yawn", Side: side,
		Text: fmt.Sprintf("%s grew drowsy!", p.Name),
	})
}

// applyNightmareVolatile flags a sleeping target for 1/4-MaxHP chip
// per end-of-turn. Fails if the target isn't asleep (canon — Nightmare
// requires its victim be already snoring).
func applyNightmareVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Nightmare {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if p.Status != StatusSleep {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Nightmare = true
	*log = append(*log, LogLine{
		Type: "nightmare", Side: side,
		Text: fmt.Sprintf("%s began having a nightmare!", p.Name),
	})
}

// applyCurseFoeVolatile sets the curse-residual flag on the target.
// Called from the Ghost branch of applyCurse; the non-Ghost branch
// never routes through volatileHandlers because it just emits boosts.
func applyCurseFoeVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Curse {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Curse = true
	*log = append(*log, LogLine{
		Type: "curse", Side: side,
		Text: fmt.Sprintf("%s was cursed!", p.Name),
	})
}

// applyDestinyBondVolatile arms the bond on the user. The end-of-turn
// sweep clears it; in between, if the user faints to a direct attack,
// the executeMove tail KOs the attacker.
func applyDestinyBondVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.DestinyBond {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.DestinyBond = true
	*log = append(*log, LogLine{
		Type: "destinybond", Side: side,
		Text: fmt.Sprintf("%s is trying to take its foe down with it!", p.Name),
	})
}

// applyCurse routes the Curse status move by user type. Ghost users
// pay 50% MaxHP and inflict the foe-side residual; non-Ghost users
// boost themselves +1 Atk / +1 Def / -1 Spe (canon). The move's
// target field in the dataset is "foe" (the Ghost path's target), so
// we ignore it and dispatch by type here.
func applyCurse(s *BattleState, side int, _ domain.Move, rng *RNG, log *[]LogLine) {
	atk := s.Active(side)
	if isType(atk, "ghost") {
		// Ghost variant: pay 50% MaxHP, inflict curse on foe.
		cost := atk.MaxHP / 2
		if cost > atk.HP {
			cost = atk.HP
		}
		applySelfDamage(atk, side, cost, log)
		def := s.Active(1 - side)
		if !def.Fainted {
			applyCurseFoeVolatile(def, 1-side, domain.Move{}, s, rng, log)
		}
		return
	}
	// Non-Ghost variant: +1 Atk, +1 Def, -1 Spe on self.
	applyStages(atk, side, "attack", 1, log)
	applyStages(atk, side, "defense", 1, log)
	applyStages(atk, side, "speed", -1, log)
}

// tickStatusVols runs the end-of-turn ticks for the status-adjacent
// volatiles on side's active: Yawn (delayed sleep), Nightmare (chip),
// Curse (chip), Destiny Bond (clear). Side 0 first, then side 1, for
// log determinism.
//
// Order inside one side: Yawn → Nightmare → Curse → DestinyBond.
// Yawn first so a yawn-induced sleep can be observed by Nightmare —
// not currently exploitable (Nightmare requires the apply target to
// already be asleep), but the order is canonical.
func tickStatusVols(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted {
		return
	}
	if y := p.Volatiles.Yawn; y != nil {
		y.TurnsLeft--
		if y.TurnsLeft <= 0 {
			p.Volatiles.Yawn = nil
			// Defer to inflictStatus so terrain / safeguard / type guards
			// still apply. rng is nil-safe (Sleep duration uses a fresh
			// fixed source if rng is nil; we pass a synthetic).
			inflictStatus(p, side, StatusSleep, s, NewRNG(uint64(p.MaxHP)+uint64(s.Turn)), log)
		}
	}
	if p.Volatiles.Nightmare {
		if p.Status != StatusSleep {
			p.Volatiles.Nightmare = false
			*log = append(*log, LogLine{
				Type: "nightmare", Side: side,
				Text: fmt.Sprintf("%s's nightmare ended.", p.Name),
			})
		} else {
			chip := p.MaxHP / 4
			if chip < 1 {
				chip = 1
			}
			applySelfDamage(p, side, chip, log)
		}
	}
	if p.Volatiles.Curse {
		chip := p.MaxHP / 4
		if chip < 1 {
			chip = 1
		}
		applySelfDamage(p, side, chip, log)
	}
	// DestinyBond is one-shot — clears at end-of-turn regardless of
	// whether the user fainted (the KO-attacker hook fired in
	// executeMove if it was going to). Same lifecycle shape as
	// Protect / Endure: cleared in the transient sweep, not here.
}

// attractImmobilizesThisTurn rolls the 50% miss-turn for an attracted
// holder. Called from canAct. Logs the in-love line on the miss; the
// caller treats it as "can't act" (return false).
func attractImmobilizesThisTurn(p *Pokemon, side int, rng *RNG, log *[]LogLine) bool {
	if !p.Volatiles.Attract {
		return false
	}
	*log = append(*log, LogLine{
		Type: "attract", Side: side,
		Text: fmt.Sprintf("%s is in love...", p.Name),
	})
	if rng.Chance(50) {
		*log = append(*log, LogLine{
			Type: "attract", Side: side,
			Text: fmt.Sprintf("%s is immobilized by love!", p.Name),
		})
		return true
	}
	return false
}

// destinyBondClaimsAttacker reports whether def's DestinyBond should
// KO atk after a fatal direct attack. Called from executeMove's faint-
// resolution tail. Direct-attack means category != status; status-move
// chip (residuals, Curse, Leech Seed) does NOT trigger the bond. The
// flag is consumed on a successful claim — Showdown clears Destiny
// Bond after it fires; we mirror that.
func destinyBondClaimsAttacker(def *Pokemon, m domain.Move) bool {
	if !def.Volatiles.DestinyBond {
		return false
	}
	if m.Category == domain.CatStatus {
		return false
	}
	return true
}
