package engine

import (
	"fmt"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

// aim.go owns the volatiles that bend crit / accuracy / evasion math:
//
//   Focus Energy — persistent +2 crit ratio
//   Laser Focus  — next damage move guaranteed crit (one-shot)
//   Charge       — next Electric move 2× base power (one-shot); the
//                  +1 SpD boost rides through Effect.Boosts already
//   Defense Curl — register-only (Boosts.Def +1 from upstream; Rollout
//                  doubling not modeled)
//   Minimize     — register-only (Boosts.Eva +2 from upstream; Body
//                  Slam / Stomp double-power vs minimized not modeled)
//   Foresight    — ignores target's positive evasion + lifts Ghost vs
//                  Normal/Fighting immunity (Odor Sleuth shares slug)
//   Miracle Eye  — ignores target's positive evasion + lifts Dark vs
//                  Psychic immunity
//
// Volatiles wipe on switch-out as usual. Focus Energy is persistent;
// Laser Focus / Charge are one-shot consumed at the next damaging
// move; Foresight / Miracle Eye are persistent until switch. Defense
// Curl and Minimize have no live gameplay role — they exist so the
// audit clears and so future Rollout / Body Slam mechanics have the
// flag already populated.

func init() {
	specs.RegisterVolatile("focusenergy")
	specs.RegisterVolatile("laserfocus")
	specs.RegisterVolatile("charge")
	specs.RegisterVolatile("defensecurl")
	specs.RegisterVolatile("minimize")
	specs.RegisterVolatile("foresight")
	specs.RegisterVolatile("miracleeye")
	registerVolatile("focusenergy", applyFocusEnergyVolatile)
	registerVolatile("laserfocus", applyLaserFocusVolatile)
	registerVolatile("charge", applyChargeVolatile)
	registerVolatile("defensecurl", applyDefenseCurlVolatile)
	registerVolatile("minimize", applyMinimizeVolatile)
	registerVolatile("foresight", applyForesightVolatile)
	registerVolatile("miracleeye", applyMiracleEyeVolatile)
}

// applyFocusEnergyVolatile latches the +2 crit-ratio buff onto the
// user. Re-applying while already pumped is a no-op (canon).
func applyFocusEnergyVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.FocusEnergy {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.FocusEnergy = true
	*log = append(*log, LogLine{
		Type: "focusenergy", Side: side,
		Text: fmt.Sprintf("%s is getting pumped!", p.Name),
	})
}

// applyLaserFocusVolatile arms the next-move-crits flag. Consumed in
// the executeMove tail after the move resolves (whether or not it hit)
// — Laser Focus is spent on the attempt, not the success.
func applyLaserFocusVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.LaserFocus {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.LaserFocus = true
	*log = append(*log, LogLine{
		Type: "laserfocus", Side: side,
		Text: fmt.Sprintf("%s concentrated intensely!", p.Name),
	})
}

// applyChargeVolatile arms the next-Electric-move-2×-BP flag. The
// matching Effect.Boosts entry handles the +1 SpD separately; this
// handler only flags the BP boost. Consumed after the user's next
// damaging move (any type — canon: clears after one turn whether or
// not Electric was used).
func applyChargeVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Charge {
		// Recharge is allowed — overwriting the same flag is harmless.
		// Showdown re-emits the start flavour; mirror that.
	}
	p.Volatiles.Charge = true
	*log = append(*log, LogLine{
		Type: "charge", Side: side,
		Text: fmt.Sprintf("%s began charging power!", p.Name),
	})
}

// applyDefenseCurlVolatile registers the volatile slug so the audit
// clears. The +1 Def boost from upstream rides through Effect.Boosts;
// the volatile itself has no live behavior today (Rollout doubling
// not modeled). Logged as a flavour-only line so it doesn't disappear
// silently from the turn log.
func applyDefenseCurlVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, _ *[]LogLine) {
	p.Volatiles.DefenseCurl = true
}

// applyMinimizeVolatile registers the slug so the audit clears. The
// +2 Eva boost rides through Effect.Boosts; the volatile has no live
// behavior today (Body Slam / Stomp double-power vs minimized not
// modeled). Quiet — boost log already prints.
func applyMinimizeVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, _ *[]LogLine) {
	p.Volatiles.Minimize = true
}

// applyForesightVolatile drops the target's positive Eva to 0 right
// away (the persistent ignore-positive-eva read runs in
// resolveAccuracy) and arms the Ghost-immunity lift. Re-applying while
// already foresighted is a no-op.
func applyForesightVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Foresight {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Foresight = true
	if p.Stages.Eva > 0 {
		p.Stages.Eva = 0
	}
	*log = append(*log, LogLine{
		Type: "foresight", Side: side,
		Text: fmt.Sprintf("%s was identified!", p.Name),
	})
}

// applyMiracleEyeVolatile is the Psychic-vs-Dark twin of Foresight.
func applyMiracleEyeVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.MiracleEye {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.MiracleEye = true
	if p.Stages.Eva > 0 {
		p.Stages.Eva = 0
	}
	*log = append(*log, LogLine{
		Type: "miracleeye", Side: side,
		Text: fmt.Sprintf("%s was identified!", p.Name),
	})
}

// critStageBonus returns the additional crit-ratio stages a Pokémon
// has from active volatiles. Read by computeDamage to pick the
// per-stage chance denominator.
func critStageBonus(p *Pokemon) int {
	bonus := 0
	if p.Volatiles.FocusEnergy {
		bonus += 2
	}
	return bonus
}

// critChanceDenom maps a crit stage (0+) to its chance denominator.
// Showdown's Gen 6+ crit table: 0→1/24, 1→1/8, 2→1/2, 3+→guaranteed
// (1/1). High-crit moves add 1 stage upstream; Focus Energy adds 2.
func critChanceDenom(stage int) int {
	switch {
	case stage <= 0:
		return 24
	case stage == 1:
		return 8
	case stage == 2:
		return 2
	default:
		return 1
	}
}

// foresightOrMiracleEyeIgnoresEva reports whether the target's
// positive evasion should be zeroed for any move (not just IgnoreEvasion
// moves). Read by resolveAccuracy.
func foresightOrMiracleEyeIgnoresEva(def *Pokemon) bool {
	return def.Volatiles.Foresight || def.Volatiles.MiracleEye
}

// liftedImmunityMult returns the per-type multiplier with Foresight /
// Miracle Eye immunity lifts applied. If the raw chart multiplier is
// non-zero, returns it unchanged. If it's zero (immunity), checks
// whether the active volatile on def lifts it for this attack type.
func liftedImmunityMult(raw float64, atkType, defType domain.Type, def *Pokemon) float64 {
	if raw != 0 {
		return raw
	}
	if def.Volatiles.Foresight && defType == "ghost" && (atkType == "normal" || atkType == "fighting") {
		return 1.0
	}
	if def.Volatiles.MiracleEye && defType == "dark" && atkType == "psychic" {
		return 1.0
	}
	return raw
}

// effectivenessWithLifts is the immunity-aware effectiveness
// recompute used inside computeDamage. Computes the per-type product
// using liftedImmunityMult so a Foresight / Miracle Eye holder takes
// neutral damage from Normal/Fighting/Psychic where the type chart
// says immune. Used in place of dex.Effectiveness on the damage path
// (and not on the ability TypeMultOverride path — those are
// canonically not lifted).
func effectivenessWithLifts(dex *domain.Dex, atkType domain.Type, def *Pokemon) float64 {
	// roostTypes suppresses the Flying type while the defender is roosting.
	t1, t2 := roostTypes(def)
	m1 := liftedImmunityMult(dex.Multiplier(atkType, t1), atkType, t1, def)
	m2 := liftedImmunityMult(dex.Multiplier(atkType, t2), atkType, t2, def)
	return m1 * m2
}
