package engine

import (
	"fmt"

	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/specs"
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
	// Re-charging while already charged is allowed — overwriting the same flag
	// is harmless, and Showdown re-emits the start flavor, which we mirror.
	p.Volatiles.Charge = true
	*log = append(*log, LogLine{
		Type: "charge", Side: side,
		Text: fmt.Sprintf("%s began charging power!", p.Name),
	})
}

// applyDefenseCurlVolatile registers the volatile slug so the audit
// clears. The +1 Def boost from upstream rides through Effect.Boosts;
// the volatile itself has no live behavior today (Rollout doubling
// not modeled). Logged as a flavor-only line so it doesn't disappear
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

// liftedImmunityMult returns the per-type multiplier with the type-chart
// immunity lifts applied. If the raw chart multiplier is non-zero, returns it
// unchanged. If it's zero (an immunity), checks whether anything on def lifts
// it: Foresight and Miracle Eye for their specific pairings, Scrappy from the
// attacker's side, Ring Target for the lot.
func liftedImmunityMult(raw float64, atkType, defType domain.Type, def *Pokemon, atkScrappy bool) float64 {
	if raw != 0 {
		return raw
	}
	if itemLiftsOwnImmunities(def) {
		// Ring Target gives up every type-chart immunity the holder has, and
		// gives up only those: canon adds effectiveness in steps and settles
		// immunity separately (Pokemon#runEffectiveness vs #runImmunity), so
		// an immune pairing contributes neutral rather than zeroing the
		// product and the other half of a dual typing still decides. Fighting
		// on Ghost/Poison is 0.5x, not 1x. Ability and volatile immunities are
		// untouched — those are not the chart.
		return 1.0
	}
	if defType == "ghost" && (atkType == "normal" || atkType == "fighting") &&
		(def.Volatiles.Foresight || atkScrappy) {
		// Foresight (defender-side volatile) and Scrappy (attacker-side
		// ability) both let Normal / Fighting land on Ghost for neutral.
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
func effectivenessWithLifts(dex *domain.Dex, atkType domain.Type, def *Pokemon, atkScrappy bool) float64 {
	// roostTypes suppresses the Flying type while the defender is roosting.
	t1, t2 := roostTypes(def)
	m1 := liftedImmunityMult(dex.Multiplier(atkType, t1), atkType, t1, def, atkScrappy)
	m2 := liftedImmunityMult(dex.Multiplier(atkType, t2), atkType, t2, def, atkScrappy)
	return m1 * m2
}

// --- guaranteed hit ---

// lockOnTurns is how many end-of-turn ticks a fresh aim survives. Canon gives
// the condition duration 2, and it ticks at the Residual event: the turn it was
// taken counts as the first, so the aim covers the next turn's move and then
// lapses. The move that spends it does not clear it — there is no spend.
const lockOnTurns = 2

// applyLockOn is the onHit for lock-on and mind-reader, which are the same move
// twice: identical accuracy, identical PP, identical `onTryHit`, and the same
// `lockon` volatile. Upstream keeps them apart only for their announcement and
// their Z-move, so they share a handler here for the same reason the engine's
// Mean Look and Block do.
//
// The refusal is keyed on the *user's* own volatile, not the target's — taking
// aim twice fails, and so does following a Lock-On with a Mind Reader.
func applyLockOn(s *BattleState, side int, m domain.Move, log *[]LogLine) {
	user, foe := s.Active(side), s.Active(1-side)
	if user.Volatiles.LockOn != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if foe == nil || foe.Fainted {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	user.Volatiles.LockOn = &LockOnState{
		TurnsLeft:  lockOnTurns,
		TargetSide: 1 - side,
		TargetTeam: s.Sides[1-side].Active,
	}
	*log = append(*log, LogLine{
		Type: "lockon", Side: side,
		Text: fmt.Sprintf("%s took aim at %s!", user.Name, foe.Name),
	})
}

// lockedOn reports whether atk has an aim covering def specifically. Consulted
// from resolveAccuracy.
//
// The identity check is the whole point: the aim is at a Pokemon, so a foe that
// pivots out leaves behind a lock that names somebody who is no longer there,
// and the replacement is missable again.
func lockedOn(s *BattleState, atk, def *Pokemon) bool {
	lo := atk.Volatiles.LockOn
	if lo == nil || def == nil {
		return false
	}
	if lo.TargetSide < 0 || lo.TargetSide > 1 {
		return false
	}
	team := s.Sides[lo.TargetSide].Team
	return lo.TargetTeam >= 0 && lo.TargetTeam < len(team) && &team[lo.TargetTeam] == def
}

// tickLockOn counts one end-of-turn off an aim. Silent on expiry: canon emits
// no line when the condition lapses, and a "the aim wore off" message would
// tell the opponent something the game does not.
func tickLockOn(p *Pokemon) {
	lo := p.Volatiles.LockOn
	if lo == nil {
		return
	}
	lo.TurnsLeft--
	if lo.TurnsLeft <= 0 {
		p.Volatiles.LockOn = nil
	}
}
