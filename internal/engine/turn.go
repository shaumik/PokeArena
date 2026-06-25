package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// maxTurns caps a battle so two defensive teams cannot loop forever; at the
// cap the winner is decided on remaining team HP.
const maxTurns = 300

// struggleMove is the typeless fallback used when a Pokémon has no PP left.
// 25% recoil rides on the user via the standard self-effect block.
var struggleMove = domain.Move{
	Name: "Struggle", Type: "", Category: domain.CatPhysical, Power: 50, Accuracy: 100,
	Self: &domain.Effect{Recoil: 0.25},
}

// ResolveTurn advances the battle by one turn given both sides' actions, and
// returns the turn log. It mutates s in place; callers that need the prior
// state should Clone first. The RNG state travels inside s, so resolving the
// same turn from the same state always produces the identical result — which
// is what makes turn resolution safely idempotent under message redelivery.
func ResolveTurn(dex *domain.Dex, s *BattleState, actions [2]Action) []LogLine {
	var log []LogLine
	if s.Phase != PhaseChoosing {
		return log
	}
	rng := NewRNG(s.RNGState)
	defer func() { s.RNGState = rng.State() }()

	s.Turn++
	log = append(log, LogLine{Type: "turn", Side: -1, Text: fmt.Sprintf("— Turn %d —", s.Turn)})

	// Lead on-switch-in: triggers like Intimidate that fire when a Pokémon
	// "enters the field" should also fire for the starting leads, who never
	// went through doSwitch. We piggyback on turn 1 rather than burdening
	// NewBattle/NewBattleFromPicks with a log channel.
	if s.Turn == 1 {
		applyOnSwitchIn(s, 0, &log)
		applyOnSwitchIn(s, 1, &log)
	}

	// Switches always resolve before moves.
	for i := 0; i < 2; i++ {
		if actions[i].Kind == ActionSwitch {
			doSwitch(s, i, actions[i].Index, &log)
		}
	}

	// Movers act in priority-then-speed order.
	var movers []int
	for i := 0; i < 2; i++ {
		if actions[i].Kind == ActionMove {
			movers = append(movers, i)
		}
	}
	ordered := orderMovers(dex, s, movers, actions, rng)
	for i, side := range ordered {
		if s.Active(side).Fainted {
			continue
		}
		// Mark the last scheduled mover so Analytic can read it from inside
		// computeDamage. Set before executeMove so the hook sees true on the
		// same move it modifies.
		if i == len(ordered)-1 {
			s.Active(side).Volatiles.MovedLast = true
		}
		executeMove(dex, s, side, actions[side].Index, rng, &log)
	}

	// End-of-turn residual damage (burn, poison, toxic).
	for i := 0; i < 2; i++ {
		applyResidual(s, i, &log)
	}

	// Leech Seed drains the seeded side, healing the seeder's active.
	// Runs after status residuals so a burn-then-seed combo still
	// chips before the drain heals — canon ordering. Side 0 first
	// for log determinism.
	applyLeechSeedResidual(s, 0, &log)
	applyLeechSeedResidual(s, 1, &log)

	// Aqua Ring + Ingrain heals. Independent of Leech Seed; the
	// heal-not-chip ticks come after the chip-not-heal ticks.
	applyRingHeals(s, 0, &log)
	applyRingHeals(s, 1, &log)

	// Weather residual chip + counter tick. Sandstorm chips Side 0 then
	// Side 1 (stable order; speed ordering doesn't matter for a
	// non-interactive residual).
	applyWeatherResidual(s, &log)
	tickWeather(s, &log)

	// Terrain residual (Grassy heal) then counter tick. Same stable
	// side-0-then-side-1 order as weather. Cloud Nine does NOT suppress
	// terrain in Gen 8+, so we read s.Terrain directly without an
	// "effective" filter.
	applyTerrainResidual(s, &log)
	tickTerrain(s, &log)

	// Per-side screens (Reflect / Light Screen / Aurora Veil): no residual,
	// just count down and clear at zero. Side 0 then Side 1 for log
	// determinism. tickBuffs handles Tailwind / Safeguard / Mist with
	// the same shape.
	tickScreens(s, 0, &log)
	tickScreens(s, 1, &log)
	tickBuffs(s, 0, &log)
	tickBuffs(s, 1, &log)

	// Lock/restrict timer volatiles (Disable / Encore / Taunt / Embargo).
	// Per-active, side 0 then side 1 for log determinism. Torment and
	// Imprison are indefinite — not ticked.
	tickLockRestrict(s, 0, &log)
	tickLockRestrict(s, 1, &log)

	// Status-adjacent volatiles: Yawn → Nightmare chip → Curse chip.
	// Side 0 first for log determinism. Destiny Bond clears in the
	// transient sweep below (same lifecycle as Protect/Endure).
	tickStatusVols(s, 0, &log)
	tickStatusVols(s, 1, &log)

	// Gimmick timers (Magnet Rise, Telekinesis). Snatch / Magic Coat
	// are one-turn flags cleared in the transient sweep below.
	tickGimmicks(s, 0, &log)
	tickGimmicks(s, 1, &log)

	// Pseudo-weather is field-scoped (not per-side); one tick covers
	// all active timers. Order inside tickPseudoWeather is stable.
	tickPseudoWeather(s, &log)

	// Slot conditions: Wish heal lands here on its scheduled tick.
	// Side 0 first for log determinism. HealingWish has no tick — it
	// consumes on switch-in via applySlotConditionsOnSwitchIn.
	tickSlotConditions(s, 0, &log)
	tickSlotConditions(s, 1, &log)

	// Ability end-of-turn ticks (Speed Boost, Rain Dish, Ice Body, Dry Skin,
	// Solar Power). Side 0 then Side 1 — stable order matches weather.
	applyAbilityEndOfTurn(s, 0, rng, &log)
	applyAbilityEndOfTurn(s, 1, rng, &log)

	// Held-item end-of-turn ticks (Leftovers +1/16 heal). After abilities,
	// same stable side-0-then-side-1 order.
	applyItemEndOfTurn(s, 0, &log)
	applyItemEndOfTurn(s, 1, &log)

	// Clear transient volatiles. Flinch is one-shot — if it wasn't consumed
	// this turn (e.g. because the flincher was slower, or the target fainted
	// before they could try to move), it must not leak into next turn.
	// MovedLast is per-turn scheduling state, also cleared here. Protect /
	// Endure are one-shot shields that expire at end of turn even if no
	// foe move tested them; ProtectCounter persists (the stall chain runs
	// across turns until broken by a non-stall action).
	for i := 0; i < 2; i++ {
		s.Active(i).Volatiles.Flinch = false
		s.Active(i).Volatiles.MovedLast = false
		s.Active(i).Volatiles.Protect = false
		s.Active(i).Volatiles.Endure = false
		s.Active(i).Volatiles.DestinyBond = false
		s.Active(i).Volatiles.Snatch = false
		s.Active(i).Volatiles.MagicCoat = false
		s.Active(i).Volatiles.Roost = false
	}

	updatePhase(s, &log)
	return log
}

// ResolveReplace applies forced switches after faints. sw[i] is the switch
// chosen for side i (nil if that side does not need to replace).
func ResolveReplace(s *BattleState, sw [2]*Action) []LogLine {
	var log []LogLine
	if s.Phase != PhaseReplace {
		return log
	}
	for i := 0; i < 2; i++ {
		if s.Replace[i] && sw[i] != nil && sw[i].Kind == ActionSwitch {
			doSwitch(s, i, sw[i].Index, &log)
			s.Replace[i] = false
		}
	}
	if !s.Replace[0] && !s.Replace[1] {
		s.Phase = PhaseChoosing
	}
	return log
}

// orderMovers returns the (at most two) move-takers in execution order.
func orderMovers(dex *domain.Dex, s *BattleState, movers []int, actions [2]Action, rng *RNG) []int {
	if len(movers) < 2 {
		return movers
	}
	a, b := movers[0], movers[1]
	if goesFirst(dex, s, b, a, actions, rng) {
		return []int{b, a}
	}
	return []int{a, b}
}

// goesFirst reports whether side x acts before side y.
func goesFirst(dex *domain.Dex, s *BattleState, x, y int, actions [2]Action, rng *RNG) bool {
	px, py := movePriority(dex, s, x, actions[x].Index), movePriority(dex, s, y, actions[y].Index)
	if px != py {
		return px > py
	}
	w := effectiveWeather(s)
	sx := int(float64(effectiveSpeed(s.Active(x), w)) * sideSpeedMult(s, x))
	sy := int(float64(effectiveSpeed(s.Active(y), w)) * sideSpeedMult(s, y))
	if sx != sy {
		// Trick Room inverts the speed comparison: the slower side
		// goes first. Speed ties (sx == sy) still break by RNG below
		// — Trick Room doesn't change that.
		if trickRoomActive(s) {
			return sx < sy
		}
		return sx > sy
	}
	return rng.IntN(2) == 0 // speed tie broken by the seeded RNG
}

func movePriority(dex *domain.Dex, s *BattleState, side, idx int) int {
	act := s.Active(side)
	if idx < 0 || idx >= len(act.Moves) {
		return 0
	}
	return dex.Moves[act.Moves[idx].MoveID].Priority
}

// executeMove runs one Pokémon's move. The phases are split into named helpers
// so future ability/item hooks can slot between them without rewriting the
// function: canAct → choosePP → announceMove → resolveAccuracy → dealDamage
// → applyDamageEffects, with applyResidual called separately at end-of-turn.
//
// Two extra gates wrap the normal flow:
//   - MustRecharge: the user spent its last move on Hyper Beam (or similar).
//     This turn is consumed recovering; no move resolves.
//   - Charging (two-turn): if the user isn't already charging, the first
//     hit of a two-turn move sets the Charging volatile and skips strike;
//     if it is, the strike resolves against the charged move regardless of
//     the submitted moveIdx.
func executeMove(dex *domain.Dex, s *BattleState, side, moveIdx int, rng *RNG, log *[]LogLine) {
	atk := s.Active(side)

	// Stall counter reset: any path through this function that does NOT
	// end in a successful Protect/Endure (which increments the counter
	// itself) means the user took a non-stall action — recharge, can't-
	// act, miss, damage, status, you name it — and the chain breaks.
	// applyProtectMove on success bumps the counter past counterBefore;
	// on a failed roll it explicitly zeroes it. So a defer that resets
	// only when the counter is unchanged covers every reset case without
	// special-casing each early return.
	counterBefore := atk.Volatiles.ProtectCounter
	defer func() {
		if atk.Volatiles.ProtectCounter == counterBefore {
			atk.Volatiles.ProtectCounter = 0
		}
	}()

	if atk.Volatiles.MustRecharge {
		atk.Volatiles.MustRecharge = false
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: fmt.Sprintf("%s must recharge!", atk.Name)})
		return
	}

	if !canAct(atk, side, rng, log) {
		// A locked-move rampage (Outrage / Thrash / Petal Dance) ends without
		// fatigue confusion if the user is prevented from acting this turn
		// (sleep / paralysis / flinch / confusion self-hit). Gen-5+ behavior.
		atk.Volatiles.LockedMove = nil
		return
	}

	// A rampage ticks down at the end of every turn the user acts — whether
	// the move hits, misses, or is immune — and fatigues the user when it runs
	// out. Armed here, before the resolution paths that return early, so it
	// fires on all of them; a no-op until the lock below is set.
	defer tickLockedMove(atk, side, rng, log)

	// Choice lock: a held Choice item forces the holder to repeat its locked
	// move. Redirect the submitted index to the locked slot so the normal
	// selection path (PP, two-turn charge) runs against the right move. Skipped
	// mid-charge / mid-rampage — those carry their own forced move below.
	if atk.Volatiles.ChoiceLockMoveID != "" && atk.Volatiles.Charging == nil && atk.Volatiles.LockedMove == nil {
		if idx := choiceLockedSlot(atk); idx >= 0 && atk.Moves[idx].PP > 0 {
			moveIdx = idx
		}
	}

	var m domain.Move
	switch {
	case atk.Volatiles.Charging != nil:
		// Strike turn of a two-turn move. PP was paid on the charge turn;
		// the moveIdx the controller submitted is ignored.
		ch := atk.Volatiles.Charging
		atk.Volatiles.Charging = nil
		m = dex.Moves[atk.Moves[ch.MoveIdx].MoveID]
	case atk.Volatiles.LockedMove != nil:
		// Forced rampage continuation. PP was paid on the first turn; the
		// submitted index is ignored (LegalActions already pins it).
		m = dex.Moves[atk.Moves[atk.Volatiles.LockedMove.MoveIdx].MoveID]
	default:
		m = choosePP(dex, atk, moveIdx)
		// First move under a Choice item commits the holder to it until it
		// switches out. Set on the real chosen slot (not Struggle), regardless
		// of whether the move goes on to hit — canon locks on use.
		if atk.Volatiles.ChoiceLockMoveID == "" && moveIdx >= 0 && moveIdx < len(atk.Moves) && m.ID != "" && isChoiceLockItem(atk) {
			atk.Volatiles.ChoiceLockMoveID = m.ID
		}
		if m.HasFlag("two-turn") && moveIdx >= 0 && moveIdx < len(atk.Moves) {
			atk.Volatiles.Charging = &ChargingState{MoveIdx: moveIdx}
			*log = append(*log, LogLine{Type: "move", Side: side,
				Text: fmt.Sprintf("%s began charging %s!", atk.Name, m.Name)})
			return
		}
	}

	// Lock/restrict gate: Disable / Encore / Taunt / Torment / Imprison
	// can refuse the chosen move at resolve time. PP has already been
	// spent above (canon — the attempt still costs a use). announceMove
	// is suppressed; we log a "cant" line instead. LastMoveID is set so
	// Torment's "twice in a row" still counts a refused attempt as the
	// last action.
	if reason, blocked := lockRestrictBlocksMove(s, side, m); blocked {
		*log = append(*log, LogLine{Type: "cant", Side: side, Text: reason})
		atk.Volatiles.LastMoveID = m.ID
		atk.Volatiles.LastMoveName = m.Name
		return
	}

	announceMove(atk, side, m, log)
	// Record the move as the user's "last move" right after announce.
	// Disable / Encore inflicted by the foe later in the same turn read
	// this — canonical "your last move" semantics. Cleared on switch-out
	// with the rest of Volatiles.
	if m.ID != "" {
		atk.Volatiles.LastMoveID = m.ID
		atk.Volatiles.LastMoveName = m.Name
	}

	// First turn of a rampage move: commit to a 2-3 turn lock. Forced
	// continuations already carry the lock and never re-roll. The deferred
	// tickLockedMove above counts this turn as one of the locked turns.
	if atk.Volatiles.LockedMove == nil && isLockedMove(m.ID) && moveIdx >= 0 && moveIdx < len(atk.Moves) {
		atk.Volatiles.LockedMove = &LockedMoveState{MoveIdx: moveIdx, Turns: lockedMoveDuration(rng)}
	}

	// Spit Up: dynamic base power = 100 × stockpile count, and the stockpile
	// empties when the move fires (regardless of how it lands). With no
	// stockpile the move fails outright. The stat-stage removal that comes with
	// emptying the stockpile is the user's own and doesn't feed Spit Up's
	// damage, so doing it up front keeps every miss/immune/blocked path
	// consuming the stockpile the way canon does.
	if m.ID == "spit-up" {
		n := stockpileCount(atk)
		if n == 0 {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
		m.Power = 100 * n
		releaseStockpile(atk, side, log)
	}

	// Psychic Terrain blocks priority moves aimed at a grounded foe. The
	// move announces but doesn't connect — Showdown emits a "protected"
	// flavour line; we lean on the generic terrain log type so the UI can
	// style it consistently with other terrain events.
	if m.Target != domain.TargetSelf {
		def := s.Active(1 - side)
		if terrainBlocksPriorityAgainst(s.Terrain, def, m.Priority) {
			*log = append(*log, LogLine{Type: "terrain", Side: side,
				Text: fmt.Sprintf("%s surrounds itself with Psychic Terrain!", def.Name)})
			return
		}
		// Protect / Detect: the foe's one-turn shield blocks every
		// foe-targeted move (damaging or status) unless the move carries
		// bypass-protect (Feint, Hyperspace Hole, ...). Returning here
		// suppresses the damage step, applyDamageEffects, contact riders,
		// and the m.Self / Primary / Secondary cascade — canonical
		// behavior for a fully absorbed attempt.
		if protectBlocksFoeMove(def, m) {
			*log = append(*log, LogLine{Type: "protect", Side: 1 - side,
				Text: fmt.Sprintf("%s protected itself!", def.Name)})
			return
		}
	}

	// OHKO immunity short-circuits fire before the accuracy roll: the
	// canonical log for Sheer Cold vs Ice or any OHKO vs Sturdy is
	// "doesn't affect" / "is unaffected", not "missed". (Normal type
	// immunity still happens inside computeDamage post-accuracy.)
	if m.OHKO != "" && resolveOHKOImmunity(s, side, m, log) {
		return
	}

	if !resolveAccuracy(s, side, m, rng, log) {
		applyMissOrEndEffects(s, side, m, log)
		return
	}

	if m.Category == domain.CatStatus {
		applyStatusMove(s, side, m, rng, log)
		applySelfSwitch(s, side, m, log)
		return
	}

	// Multi-strike moves (Bullet Seed, Bone Rush, Bonemerang, Triple Axel, ...)
	// loop the strike phase. The accuracy roll above gates the whole sequence;
	// each hit then rolls its own damage spread, crit, and secondary effects.
	// The loop stops early if either side faints (so a multi-hit move can't
	// continue against a 0-HP target, and Rough Skin can cut it short).
	planned := 1
	if m.IsMultihit() {
		planned = multihitCount(m, rng)
	}
	hits := 0
	for i := 0; i < planned; i++ {
		if s.Active(1-side).HP <= 0 || atk.HP <= 0 {
			break
		}
		dmg, ok := dealDamage(dex, s, side, m, rng, log)
		if !ok {
			// Type immunity also fires the post-move tail: a Ghost on the
			// receiving end of Explosion still takes no damage, but the
			// user still detonates (matches the canonical Gen-1 behavior).
			// For multi-hit moves immunity is type-based and deterministic,
			// so if the first strike is immune the rest would be too —
			// short-circuit the whole sequence.
			if i == 0 {
				applyMissOrEndEffects(s, side, m, log)
				return
			}
			break
		}
		applyDamageEffects(s, side, m, dmg, rng, log)
		hits++
	}
	if m.IsMultihit() && hits > 0 {
		*log = append(*log, LogLine{Type: "info", Side: side,
			Text: fmt.Sprintf("Hit %d time(s)!", hits)})
	}

	// Rapid Spin clears the user's side hazards on a successful hit. The
	// Speed +1 self-boost is already wired via the upstream secondary; only
	// the hazard sweep needs the hand-coded hook (Showdown encodes it in
	// JS). Triggered before faint resolution so a contact-faint counter
	// (Rough Skin) doesn't suppress the spin sweep — the move connected,
	// and that's what gates the clear.
	if hits > 0 && m.ID == "rapid-spin" {
		applyRapidSpin(s, side, log)
	}

	// Consume one-shot aim buffs: Laser Focus arms the next attempt's
	// guaranteed crit and Charge arms the next damaging move's ×2 BP
	// for Electric. Both clear here after the move resolves whether
	// or not it hit (canonical: spent on the attempt, not the success).
	atk.Volatiles.LaserFocus = false
	atk.Volatiles.Charge = false

	if m.HasFlag("recharge") {
		atk.Volatiles.MustRecharge = true
	}
	if m.HasFlag("selfdestruct") {
		applySelfDestruct(atk, side, log)
	}

	def := s.Active(1 - side)
	if def.HP <= 0 {
		// Destiny Bond check captures the flag BEFORE faint() wipes
		// volatiles. Direct-attack only — status-move chip can't
		// trigger. Fires before the user's own faint check so an
		// attacker that would also self-faint (e.g. recoil) still
		// reports the bond-KO first.
		bondClaims := destinyBondClaimsAttacker(def, m)
		faint(def, 1-side, log)
		if bondClaims {
			*log = append(*log, LogLine{Type: "destinybond", Side: 1 - side,
				Text: fmt.Sprintf("%s took its attacker down with it!", def.Name)})
			atk.HP = 0
		}
	}
	// Life Orb recoil: the holder chips itself after a damaging move connects
	// (hits > 0). Applied after the foe's faint resolves so the hit lands
	// first; the atk-faint check below catches a recoil KO. Suppressed for
	// Sheer-Force-boosted moves and by Magic Guard (see lifeOrbRecoilApplies).
	if hits > 0 && lifeOrbRecoilApplies(atk, m) {
		applyLifeOrbRecoil(atk, side, log)
	}
	if atk.HP <= 0 {
		faint(atk, side, log)
	}

	// Damage-variant self-switch (U-turn, Volt Switch, Flip Turn) runs after
	// faint resolution so a contact-hit-reactive faint (Rocky Helmet, Rough
	// Skin) suppresses the switch the way it does in canon.
	applySelfSwitch(s, side, m, log)

	// forceSwitch damage variants (Circle Throw, Dragon Tail): after
	// damage and faint resolution, drag the foe to a random live bench
	// teammate. A KO'd foe is a silent no-op; a foe with no live bench
	// is also silent (damage was the visible effect — no "But it
	// failed" line for damage variants).
	if hits > 0 && m.ForceSwitch {
		applyForceSwitch(s, side, rng, log)
	}
}

// multihitCount returns the number of strikes for one use of a multi-hit
// move. Fixed counts (Bonemerang [2,2], Triple Axel [3,3]) return MinHits;
// the canonical [2,5] range follows the Gen-5+ weighted distribution
// (35% 2 hits, 35% 3 hits, 15% 4 hits, 15% 5 hits). Other ranges fall back
// to a uniform roll — no curated move uses one today, the branch exists for
// forward compatibility.
func multihitCount(m domain.Move, rng *RNG) int {
	if m.MinHits == m.MaxHits {
		return m.MinHits
	}
	if m.MinHits == 2 && m.MaxHits == 5 {
		roll := rng.IntN(100)
		switch {
		case roll < 35:
			return 2
		case roll < 70:
			return 3
		case roll < 85:
			return 4
		default:
			return 5
		}
	}
	return rng.Range(m.MinHits, m.MaxHits)
}

// applyMissOrEndEffects handles the post-resolution tail for cases where the
// damage step didn't fire (miss or type-immune): currently just selfdestruct,
// which detonates the user regardless of whether the move connected.
func applyMissOrEndEffects(s *BattleState, side int, m domain.Move, log *[]LogLine) {
	if !m.HasFlag("selfdestruct") {
		return
	}
	atk := s.Active(side)
	applySelfDestruct(atk, side, log)
	if atk.HP <= 0 {
		faint(atk, side, log)
	}
}

// applySelfDestruct drops the user to 0 HP. Used by Explosion / Self-Destruct
// after the move resolves (hit or miss); the caller is responsible for the
// subsequent faint() call.
func applySelfDestruct(atk *Pokemon, side int, log *[]LogLine) {
	if atk.HP <= 0 {
		return
	}
	atk.HP = 0
	*log = append(*log, LogLine{Type: "recoil", Side: side,
		Text: fmt.Sprintf("%s exploded!", atk.Name)})
}

// choosePP picks the move to use this turn and decrements its PP. If the
// requested slot is empty or out of range, Struggle is used instead.
func choosePP(dex *domain.Dex, atk *Pokemon, moveIdx int) domain.Move {
	if moveIdx >= 0 && moveIdx < len(atk.Moves) && atk.Moves[moveIdx].PP > 0 {
		atk.Moves[moveIdx].PP--
		return dex.Moves[atk.Moves[moveIdx].MoveID]
	}
	return struggleMove
}

// announceMove logs the "X used Y!" line that opens every move execution.
func announceMove(atk *Pokemon, side int, m domain.Move, log *[]LogLine) {
	*log = append(*log, LogLine{Type: "move", Side: side, Text: fmt.Sprintf("%s used %s!", atk.Name, m.Name)})
}

// resolveOHKOImmunity handles the two pre-accuracy immunity gates specific
// to OHKO moves and emits the canonical log line for each. Returns true if
// the move was absorbed (caller should stop processing the move).
//
//	(1) Sheer Cold's ohko="ice" makes Ice-types immune even though the type
//	    chart says Ice vs Ice is a normal 0.5× matchup.
//	(2) Sturdy (Gen 5+) blocks OHKO moves outright — distinct from the
//	    "leave at 1 HP" clamp the SurviveOHKO hook applies to normal hits.
//
// Normal type immunity (Ghost vs Horn Drill, Flying vs Fissure, Levitate
// vs Fissure) still runs inside computeDamage post-accuracy.
func resolveOHKOImmunity(s *BattleState, side int, m domain.Move, log *[]LogLine) bool {
	def := s.Active(1 - side)
	if m.OHKO != "any" && isType(def, domain.Type(m.OHKO)) {
		*log = append(*log, LogLine{Type: "immune", Side: side,
			Text: fmt.Sprintf("It doesn't affect %s...", def.Name)})
		return true
	}
	if a := abilityOf(def); a != nil && a.Kind == AbilitySturdy {
		*log = append(*log, LogLine{Type: "ability", Side: 1 - side,
			Text: fmt.Sprintf("%s is unaffected by the one-hit KO! (Sturdy)", def.Name)})
		return true
	}
	return false
}

// resolveAccuracy rolls the accuracy check and reports whether the move lands.
// Effective accuracy is move.Accuracy * accMult(clamp(atk.Acc - def.Eva, -6,
// +6)). The bypass-acc flag (Aerial Ace, Swift, Aura Sphere) skips the roll.
// Moves with Accuracy==0 are also unmissable (status-move convention).
func resolveAccuracy(s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) bool {
	if m.HasFlag("bypass-acc") || m.Accuracy == 0 {
		return true
	}
	atk := s.Active(side)
	def := s.Active(1 - side)
	// Telekinesis on the target makes every move land — the lifted
	// holder is too easy a target to miss. Cancelled by Smack Down
	// (which clears the Telekinesis volatile on apply).
	if telekinesisAutoHits(def) {
		return true
	}
	// Soundproof: sound-flagged moves don't affect the holder at all. We
	// log "doesn't affect" rather than "missed" to match canon.
	if m.HasFlag("sound") {
		if a := abilityOf(def); a != nil && a.Kind == "soundproof" {
			*log = append(*log, LogLine{Type: "immune", Side: side,
				Text: fmt.Sprintf("It doesn't affect %s... (Soundproof)", def.Name)})
			return false
		}
	}
	// ignoreEvasion (Chip Away, Darkest Lariat): only positive evasion
	// boosts get zeroed; drops still help the attacker. Mirrors
	// canonical Showdown behavior — "ignore the buff, not the debuff".
	// Foresight / Miracle Eye on the defender broadens the same gate
	// to every move: any positive eva is ignored while either volatile
	// is active.
	defEva := def.Stages.Eva
	if (m.IgnoreEvasion || foresightOrMiracleEyeIgnoresEva(def)) && defEva > 0 {
		defEva = 0
	}
	combined := atk.Stages.Acc - defEva
	if combined > 6 {
		combined = 6
	}
	if combined < -6 {
		combined = -6
	}
	chance := int(float64(m.Accuracy) * accStageMultiplier(combined) * abilityAccuracyMult(atk))
	// Gravity boosts every move's accuracy by 5/3. Stacks
	// multiplicatively with stages and ability mods; clamp follows.
	// Gravity also grounds Flying-types for the duration, but that
	// interaction (Earthquake hits Gyarados) is not modeled in this
	// pass — only the accuracy boost lands here.
	if gravityActive(s) {
		chance = chance * 5 / 3
	}
	if chance > 100 {
		chance = 100
	}
	if chance < 100 && rng.IntN(100) >= chance {
		*log = append(*log, LogLine{Type: "miss", Side: side, Text: fmt.Sprintf("%s's attack missed!", atk.Name)})
		return false
	}
	return true
}

// dealDamage computes and applies damage for a non-status move, logging the
// damage/crit/effectiveness lines. Returns (dmg, true) on a normal hit, or
// (0, false) if the move was immune-blocked. A frozen target hit by a
// Fire-type damaging move thaws before damage applies; the move still lands.
func dealDamage(dex *domain.Dex, s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) (int, bool) {
	atk := s.Active(side)
	def := s.Active(1 - side)
	res := computeDamage(dex, atk, def, m, effectiveWeather(s), s.Terrain, &s.Sides[1-side].Conditions, &s.PseudoWeather, rng)
	if res.Effectiveness == 0 {
		if res.AbilityImmune {
			// The ability's TypeMultOverride blocked it — let the ability's
			// own bonus hook describe what happened (Volt Absorb "absorbed
			// the electricity!", Flash Fire "warmed up!"). For pure
			// immunities (Levitate) the bonus hook is nil and we fall back
			// to the generic "doesn't affect" message.
			before := len(*log)
			abilityImmunityBonus(s, 1-side, m.Type, log)
			if len(*log) == before {
				*log = append(*log, LogLine{Type: "immune", Side: side,
					Text: fmt.Sprintf("It doesn't affect %s...", def.Name)})
			}
		} else {
			*log = append(*log, LogLine{Type: "immune", Side: side,
				Text: fmt.Sprintf("It doesn't affect %s...", def.Name)})
		}
		return 0, false
	}
	if def.Status == StatusFreeze && (m.Type == "fire" || m.ThawsTarget) {
		def.Status = StatusNone
		*log = append(*log, LogLine{Type: "status", Side: 1 - side,
			Text: fmt.Sprintf("%s was thawed by the heat!", def.Name)})
	}
	dmg := res.Damage
	// OHKO override: damage = full target HP. Sturdy was already filtered
	// upstream, so any OHKO that reaches here connects for a clean KO. The
	// formula's BP-0 crit/effectiveness lines are noise on a one-hit KO and
	// Showdown suppresses them — we follow suit and emit a single
	// "one-hit KO!" line instead.
	if m.OHKO != "" {
		dmg = def.HP
	}
	if dmg > def.HP {
		dmg = def.HP
	}
	// Substitute redirection: a doll on the defender soaks the hit before
	// def.HP changes. Sound and bypass-sub moves treat the doll as
	// transparent and damage the holder directly. Type effectiveness is
	// still computed against the holder (not the sub), so SE / resist
	// lines print either way. An OHKO move whose target has a sub simply
	// breaks the sub — it does NOT one-hit KO the holder, and the
	// "one-hit KO!" line is suppressed (Showdown's behavior).
	if hasSubstitute(def) && !bypassesSubstitute(m, atk) {
		if m.OHKO != "" {
			dmg = def.Volatiles.Substitute.HP
		}
		absorbed := applyDamageToSubstitute(def, 1-side, dmg, log)
		if res.Crit {
			*log = append(*log, LogLine{Type: "crit", Side: side, Text: "A critical hit!"})
		}
		if res.Effectiveness > 1 {
			*log = append(*log, LogLine{Type: "effective", Side: side, Text: "It's super effective!"})
		} else if res.Effectiveness < 1 {
			*log = append(*log, LogLine{Type: "resisted", Side: side, Text: "It's not very effective..."})
		}
		// Contact riders (Rough Skin, Static, Flame Body, Poison Point,
		// Effect Spore) still fire when a contact move hits the doll —
		// the attacker did touch the holder's body, the doll just stood
		// between them. Canonical.
		applyOnHit(s, 1-side, m, rng, log)
		return absorbed, true
	}
	// Endure: a lethal hit clamps to leave the target at 1 HP. Endure does
	// NOT block sub-routed damage (the doll already absorbed it) and does
	// NOT block status moves (those bypass dealDamage entirely). OHKO
	// moves are clamped too — Endure pays for that one-HP survival.
	enduredHit := false
	if def.Volatiles.Endure && dmg >= def.HP {
		dmg = def.HP - 1
		if dmg < 0 {
			dmg = 0
		}
		enduredHit = true
	}
	// Focus Sash: a full-HP holder survives an otherwise-lethal hit at 1 HP,
	// then the sash is consumed. Endure takes precedence (it already clamped
	// and is reusable, so there's no reason to burn the sash). Saves from OHKO
	// moves too — their dmg was set to def.HP above, so the clamp still fires.
	sashSaved := false
	if !enduredHit {
		if capped, fired := itemSurviveOHKO(def, dmg); fired {
			dmg = capped
			sashSaved = true
		}
	}
	def.HP -= dmg
	*log = append(*log, LogLine{Type: "damage", Side: 1 - side, Text: fmt.Sprintf("%s took %d damage.", def.Name, dmg)})
	if enduredHit {
		*log = append(*log, LogLine{Type: "endure", Side: 1 - side,
			Text: fmt.Sprintf("%s endured the hit!", def.Name)})
	}
	if sashSaved {
		consumeItem(def)
		*log = append(*log, LogLine{Type: "item", Side: 1 - side,
			Text: fmt.Sprintf("%s hung on with its Focus Sash!", def.Name)})
	}
	if m.OHKO != "" && !enduredHit && !sashSaved {
		*log = append(*log, LogLine{Type: "info", Side: side, Text: "It's a one-hit KO!"})
	} else if m.OHKO == "" {
		if res.Crit {
			*log = append(*log, LogLine{Type: "crit", Side: side, Text: "A critical hit!"})
		}
		if res.Effectiveness > 1 {
			*log = append(*log, LogLine{Type: "effective", Side: side, Text: "It's super effective!"})
		} else if res.Effectiveness < 1 {
			*log = append(*log, LogLine{Type: "resisted", Side: side, Text: "It's not very effective..."})
		}
		if res.Sturdy {
			*log = append(*log, LogLine{Type: "ability", Side: 1 - side,
				Text: fmt.Sprintf("%s hung on with Sturdy!", def.Name)})
		}
	}
	// On-hit hook for contact riders (Static, Flame Body, Poison Point,
	// Effect Spore). The hook itself checks the contact flag — keeping that
	// inside the ability avoids spreading move-flag inspection across
	// integration sites.
	applyOnHit(s, 1-side, m, rng, log)
	return dmg, true
}

// canAct applies pre-move status and volatile checks and reports whether the
// Pokémon moves. Order: flinch (one-shot, consumed) → confusion (may self-hit
// and preempt) → attract (50% immobilize) → non-volatile status (freeze /
// sleep / para).
func canAct(p *Pokemon, side int, rng *RNG, log *[]LogLine) bool {
	if p.Volatiles.Flinch {
		p.Volatiles.Flinch = false
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: p.Name + " flinched and couldn't move!"})
		return false
	}
	if attractImmobilizesThisTurn(p, side, rng, log) {
		return false
	}
	if p.Volatiles.Confusion != nil {
		p.Volatiles.Confusion.Turns--
		if p.Volatiles.Confusion.Turns <= 0 {
			p.Volatiles.Confusion = nil
			*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " snapped out of confusion!"})
		} else {
			*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " is confused..."})
			if rng.Chance(33) {
				confusionSelfHit(p, side, rng, log)
				return false
			}
		}
	}
	switch p.Status {
	case StatusFreeze:
		if rng.Chance(20) {
			p.Status = StatusNone
			*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " thawed out!"})
			return true
		}
		*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " is frozen solid!"})
		return false
	case StatusSleep:
		if p.SleepTurns > 0 {
			p.SleepTurns--
		}
		if p.SleepTurns <= 0 {
			p.Status = StatusNone
			*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " woke up!"})
			return true
		}
		*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " is fast asleep."})
		return false
	case StatusParalysis:
		if rng.Chance(25) {
			*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " is paralyzed! It can't move!"})
			return false
		}
	}
	return true
}

// confusionSelfHit deals self-damage when a confused Pokémon hits itself. The
// virtual move is 40-power typeless physical, using the user's own Atk/Def
// stages, no STAB, no crit, no type effectiveness. Burn does NOT halve this
// damage (Showdown convention).
func confusionSelfHit(p *Pokemon, side int, rng *RNG, log *[]LogLine) {
	a := float64(p.Stats.Atk) * stageMultiplier(p.Stages.Atk)
	d := float64(p.Stats.Def) * stageMultiplier(p.Stages.Def)
	base := (float64(2*Level)/5.0+2.0)*40.0*a/d/50.0 + 2.0
	randMult := float64(rng.Range(85, 100)) / 100.0
	dmg := int(base * randMult)
	if dmg < 1 {
		dmg = 1
	}
	if dmg > p.HP {
		dmg = p.HP
	}
	p.HP -= dmg
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s hurt itself in its confusion! (-%d)", p.Name, dmg)})
	if p.HP <= 0 {
		faint(p, side, log)
	}
}

// updatePhase recomputes the battle phase and winner after a turn.
func updatePhase(s *BattleState, log *[]LogLine) {
	l0, l1 := s.LiveCount(0), s.LiveCount(1)
	switch {
	case l0 == 0 && l1 == 0:
		endBattle(s, 2, log)
		return
	case l0 == 0:
		endBattle(s, 1, log)
		return
	case l1 == 0:
		endBattle(s, 0, log)
		return
	}
	if s.Turn >= maxTurns {
		f0, f1 := hpFraction(s, 0), hpFraction(s, 1)
		switch {
		case f0 > f1:
			endBattle(s, 0, log)
		case f1 > f0:
			endBattle(s, 1, log)
		default:
			endBattle(s, 2, log)
		}
		return
	}
	s.Replace[0] = s.Active(0).Fainted
	s.Replace[1] = s.Active(1).Fainted
	if s.Replace[0] || s.Replace[1] {
		s.Phase = PhaseReplace
	} else {
		s.Phase = PhaseChoosing
	}
}

func endBattle(s *BattleState, winner int, log *[]LogLine) {
	s.Phase = PhaseEnded
	s.Winner = winner
	s.Replace = [2]bool{}
	switch winner {
	case 2:
		*log = append(*log, LogLine{Type: "win", Side: -1, Text: "The battle ended in a draw!"})
	default:
		*log = append(*log, LogLine{Type: "win", Side: winner,
			Text: fmt.Sprintf("%s won the battle!", s.Sides[winner].Trainer)})
	}
}

func hpFraction(s *BattleState, side int) float64 {
	var cur, max float64
	for i := range s.Sides[side].Team {
		cur += float64(s.Sides[side].Team[i].HP)
		max += float64(s.Sides[side].Team[i].MaxHP)
	}
	if max == 0 {
		return 0
	}
	return cur / max
}
