package engine

import (
	"fmt"

	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/specs"
)

// statusvols.go owns the status-adjacent volatiles — each has its own
// gameplay shape but they share a "harass the target every turn" feel:
//
//   Attract       — 50% immobilize per turn, and only between opposite
//                   genders (see gendersAttract)
//   Yawn          — 1-turn delayed Sleep
//   Nightmare     — 1/4 MaxHP chip per end-of-turn while target is asleep
//   Curse         — Ghost variant chips foe 1/4 MaxHP/turn for 50% user
//                   HP; non-Ghost variant boosts +1 Atk / +1 Def / -1 Spe
//                   on the user. Routed by user type in applyStatusMove.
//   Destiny Bond  — KO the attacker if the holder faints to a direct
//                   attack. Lasts until the holder's next move rather
//                   than to the end of the turn, which is what makes a
//                   back-to-back use fail.

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

// applyAttractVolatile sets the infatuation flag on the target. Infatuation
// needs opposite genders: it fails against a genderless target, against a
// genderless user, and between two of the same gender. Re-applying while
// already attracted is a no-op (canon).
//
// The user is the foe of the target's side. Attract is foe-targeting — no
// path applies it to its own user — so that holds wherever this is reached.
func applyAttractVolatile(p *Pokemon, side int, _ domain.Move, s *BattleState, _ *RNG, log *[]LogLine) {
	var user *Pokemon
	if s != nil {
		user = s.Active(1 - side)
	}
	if !gendersAttract(user, p) {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if abilityBlocksInfatuation(p) {
		revealAbility(p)
		*log = append(*log, LogLine{
			Type: "ability", Side: side,
			Text: fmt.Sprintf("%s's Oblivious keeps it from being infatuated!", p.Name),
		})
		return
	}
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

// gendersAttract reports whether infatuation can pass from user to target.
// Canon needs one of each: a genderless Pokémon on either side of the
// exchange can't be involved, and two of the same gender don't work either.
//
// A missing gender on either side is read as genderless, which refuses. That
// is the safe direction — Attract without this check landed on everything,
// including legendaries and same-sex targets, at 100% accuracy through
// Substitute, on 73 of the 80 curated species. A test fixture that forgot to
// set a gender gets a failed Attract, which is loud; the alternative default
// silently restores the bug.
func gendersAttract(user, target *Pokemon) bool {
	if user == nil || target == nil {
		return false
	}
	if user.Gender == "" || target.Gender == "" {
		return false
	}
	if user.Gender == domain.GenderGenderless || target.Gender == domain.GenderGenderless {
		return false
	}
	return user.Gender != target.Gender
}

// applyYawnVolatile sets a 2-tick countdown that lands a Sleep at the
// next end-of-turn. It fails wherever canon's yawn.onTryHit fails —
// `if (target.status || !target.runStatusImmunity('slp')) return false` —
// plus at the one field effect whose condition refuses the drowsiness
// itself rather than the eventual Sleep.
//
// The two-stage shape is the thing to keep straight. Yawn asks up front
// whether the target could be put to sleep *now*, and if the answer is no
// the move fails outright rather than arming a countdown that would only
// fizzle; the countdown then asks again, through inflictStatus, when it
// expires (tickStatusVols). So every guard inflictStatus already makes has
// to be re-made here, and — the part that is easy to get wrong — a guard
// that belongs only to the eventual Sleep must *not* be:
//
//   - Already drowsy, or already carrying a non-volatile status. The
//     second is target.status in onTryHit; the first is Showdown declining
//     to add a volatile that is already present.
//   - Insomnia and Vital Spirit (abilityBlocksStatus), and Leaf Guard in
//     sun (abilityBlocksStatusState, which is weather-aware). All three
//     carry an onTryAddVolatile in data/abilities.ts that names yawn, so
//     they refuse the drowsiness and not merely the sleep.
//   - Electric Terrain, and only Electric Terrain. Its condition has an
//     onTryAddVolatile for yawn; Misty Terrain's onTryAddVolatile names
//     confusion only, so a Yawn cast under Misty Terrain *does* land and
//     is then stopped two turns later by the onSetStatus half. That
//     asymmetry is why this reads s.Terrain directly instead of calling
//     terrainBlocksStatus, which quite correctly answers for both terrains
//     and is therefore the wrong question to ask at this stage.
//   - Uproar is deliberately absent. It refuses sleep through
//     onAnySetStatus and has no onTryAddVolatile, so Yawn lands during an
//     Uproar and simply never collects.
//
// Type immunity to Sleep needs no check because no type has one — which is
// also why inflictStatus's type switch has no Sleep case. Substitute and
// Safeguard are handled a level up in applyEffectFields, which refuses
// every foe-induced volatile behind a doll and consults
// safeguardBlocksFoeVolatile before dispatching here.
func applyYawnVolatile(p *Pokemon, side int, _ domain.Move, s *BattleState, _ *RNG, log *[]LogLine) {
	// Electric Terrain's own yawn gate. Grounded-only, like every other
	// terrain effect; the semi-invulnerability half of upstream's check has
	// no counterpart here because this engine models no two-turn move that
	// leaves the field.
	terrainRefuses := s != nil && s.Terrain != nil &&
		s.Terrain.Kind == TerrainElectric && isGrounded(p, &s.PseudoWeather)
	// Every reason above fails the move the same way — one "But it failed!"
	// and no countdown — so they share the exit rather than repeating it.
	if p.Volatiles.Yawn != nil ||
		p.Status != StatusNone ||
		abilityBlocksStatus(p, StatusSleep) ||
		(s != nil && abilityBlocksStatusState(s, p, StatusSleep)) ||
		terrainRefuses {
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

// applyDestinyBondVolatile arms the bond on the user. It lasts until the user's
// next move — executeMove spends it, and an aborted move drops it — and in
// between, if the user faints to a direct attack, the executeMove tail KOs the
// attacker.
//
// A back-to-back use fails, and it also *takes the bond down*: canon's
// onPrepareHit is the single line `return !pokemon.removeVolatile('destinybond')`,
// so the removal and the failure are the same statement. Refusing without
// removing would leave the threat standing on a turn the user spent failing to
// renew it, which is the opposite of the rule.
//
// This guard used to be unreachable: the end-of-turn transient sweep cleared
// the volatile next to Protect and Endure, so it was never still up when the
// next turn asked — and Destiny Bond was re-armable indefinitely.
func applyDestinyBondVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.DestinyBond {
		p.Volatiles.DestinyBond = false
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
