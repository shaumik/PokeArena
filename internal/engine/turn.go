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
	log := make([]LogLine, 0, 1)
	if s.Phase != PhaseChoosing {
		return log
	}
	rng := NewRNG(s.RNGState)
	defer func() { s.RNGState = rng.State() }()

	s.Turn++
	log = append(log, LogLine{Type: "turn", Side: -1, Text: fmt.Sprintf("— Turn %d —", s.Turn)})

	// MovedThisTurn is established fresh here rather than relying solely on the
	// end-of-turn sweep. A switch that happens in the *replace* phase sets the
	// flag (a switched-in Pokémon doesn't act), but ResolveReplace runs outside
	// this function, so that sweep never sees it — the flag would still be set
	// when the next turn began and Zoom Lens would pay out against a Pokémon
	// that is very much about to move. Clearing at the top makes the flag
	// unambiguously "this turn's" no matter which path installed the active;
	// the switch phase below re-sets it for a mid-turn switch-in.
	for i := 0; i < 2; i++ {
		s.Active(i).Volatiles.MovedThisTurn = false
	}

	// Lead on-switch-in: triggers like Intimidate that fire when a Pokémon
	// "enters the field" should also fire for the starting leads, who never
	// went through doSwitch. We piggyback on turn 1 rather than burdening
	// NewBattle/NewBattleFromPicks with a log channel.
	if s.Turn == 1 {
		applyOnSwitchIn(s, 0, &log)
		applyOnSwitchIn(s, 1, &log)
	}

	// "Tightening its focus": Focus Punch announces its intent at the top of
	// the turn, before anyone acts. If the user is hit before it fires, the
	// loss-of-focus check in executeMove cancels the move.
	for i := 0; i < 2; i++ {
		if actions[i].Kind == ActionMove && !s.Active(i).Fainted &&
			foeSelectedMove(dex, s.Active(i), actions[i].Index).ID == "focus-punch" {
			log = append(log, LogLine{
				Type: "move", Side: i,
				Text: fmt.Sprintf("%s is tightening its focus!", s.Active(i).Name),
			})
		}
	}

	// Custap Berry arms before anything resolves: a holder in its last quarter
	// of HP jumps to the front of its priority bracket, which is the whole
	// point of the item (a slower Pokémon getting one move off first). The
	// berry is spent here whether or not the jump changes the order. Side 0
	// first for log determinism.
	for i := 0; i < 2; i++ {
		applyCustapBerry(s, i, actions[i], &log)
		applyQuickClaw(s, i, actions[i], rng, &log)
	}

	// Pursuit interception: a Pursuit user strikes a fleeing target before it
	// leaves, out of normal speed order and at doubled power (the doubling is
	// keyed on the switch action inside executeMove). The pursuer is flagged
	// done so it doesn't also act in the mover loop below.
	var pursued [2]bool
	for i := 0; i < 2; i++ {
		if actions[i].Kind != ActionSwitch {
			continue
		}
		foe := 1 - i
		if actions[foe].Kind != ActionMove || s.Active(foe).Fainted {
			continue
		}
		if foeSelectedMove(dex, s.Active(foe), actions[foe].Index).ID != "pursuit" {
			continue
		}
		executeMove(dex, s, foe, actions[foe], actions[i], false, rng, &log)
		pursued[foe] = true
	}

	// Switches always resolve before moves. A target KO'd by Pursuit above
	// stays put — its faint routes into the replace phase instead.
	for i := 0; i < 2; i++ {
		if actions[i].Kind == ActionSwitch && !s.Active(i).Fainted {
			doSwitch(s, i, actions[i].Index, rng, &log)
		}
	}

	// Movers act in priority-then-speed order (Pursuit chasers already acted).
	var movers []int
	for i := 0; i < 2; i++ {
		if actions[i].Kind == ActionMove && !pursued[i] {
			movers = append(movers, i)
		}
	}
	ordered := orderMovers(dex, s, movers, actions, rng)
	// Track which side has already resolved its move this turn so Sucker
	// Punch can tell whether its target has yet to act.
	var moved [2]bool
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
		mover := s.Active(side)
		executeMove(dex, s, side, actions[side], actions[1-side], moved[1-side], rng, &log)
		moved[side] = true
		// Stamp the Pokémon that actually acted, captured before the move: a
		// U-turn user has already been replaced by the time this line runs, and
		// stamping the replacement would credit it with a move it never made.
		// A mover that left the field gets nothing — doSwitchWithCarry wiped its
		// volatiles on the way out, the end-of-turn sweep only reaches the two
		// actives, and so a flag re-dirtied here would ride the bench forever.
		// The replacement already carries MovedThisTurn from the switch itself.
		if mover == s.Active(side) {
			mover.Volatiles.MovedThisTurn = true
		}
	}

	// End-of-turn residuals, in canon's order. Showdown assigns each an
	// onResidualOrder and runs them ascending; the numbers below are those, and
	// the whole block is arranged to match rather than grouped by kind:
	//
	//	1  weather chip (sandstorm, hail)
	//	5  held-item heals (Leftovers, Black Sludge)
	//	6  Aqua Ring
	//	7  Ingrain
	//	8  Leech Seed
	//	9  status chip (poison, toxic, burn)
	//
	// Weather first is the part that matters and the part this engine had
	// backwards: a 1-HP Leftovers holder in sand used to survive here and dies
	// in canon, because the chip is supposed to land before the heal. Leftovers
	// ahead of poison was already right and is preserved.
	//
	// applyItemHPTriggers runs after each step that moves HP, because any of
	// them can push a holder into berry range. It no-ops for a holder with
	// nothing to trigger, so the repetition is cheap.
	applyWeatherResidual(s, &log)
	applyItemHPTriggers(s, rng, &log)
	tickWeather(s, &log)

	applyItemEndOfTurn(s, 0, &log)
	applyItemEndOfTurn(s, 1, &log)

	// Aqua Ring + Ingrain heals, then Leech Seed's drain. Side 0 first
	// throughout for log determinism.
	applyRingHeals(s, 0, &log)
	applyRingHeals(s, 1, &log)
	applyLeechSeedResidual(s, 0, &log)
	applyLeechSeedResidual(s, 1, &log)
	applyItemHPTriggers(s, rng, &log)

	// Status chip (burn, poison, toxic) is last of the HP movers.
	for i := 0; i < 2; i++ {
		applyResidual(s, i, &log)
	}
	applyItemHPTriggers(s, rng, &log)

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

	// Late held-item residuals: the orbs and Sticky Barb. Canon puts these at
	// the very end of the residual order, so the turn an orb fires costs the
	// holder no status damage — that free turn is the whole reason to run one.
	applyItemEndOfTurnLate(s, 0, rng, &log)
	applyItemEndOfTurnLate(s, 1, rng, &log)

	// Final pinch sweep: the timer ticks and volatile residuals above (Leech
	// Seed, Nightmare, Curse, partial trap) can also drop a holder into range,
	// as can a Sticky Barb tick. The herbs are checked in the same sweep — a
	// Taunt or a stat drop inflicted this turn is theirs to answer.
	applyItemHPTriggers(s, rng, &log)
	applyItemStatChecks(s, &log)

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
		s.Active(i).Volatiles.MovedThisTurn = false
		s.Active(i).Volatiles.DamagedThisTurn = false
		s.Active(i).Volatiles.Protect = false
		s.Active(i).Volatiles.Endure = false
		s.Active(i).Volatiles.DestinyBond = false
		s.Active(i).Volatiles.Snatch = false
		s.Active(i).Volatiles.MagicCoat = false
		s.Active(i).Volatiles.Roost = false
		// CustapBoost is this turn's ordering decision, not a lasting buff.
		// A Micle prime is not cleared here — it has to survive into the next
		// turn to be spendable at all — but it does tick down, so it lapses
		// rather than banking indefinitely.
		s.Active(i).Volatiles.CustapBoost = false
		if s.Active(i).Volatiles.MicleTurns > 0 {
			s.Active(i).Volatiles.MicleTurns--
		}
	}

	updatePhase(s, &log)
	checkInvariants(s)
	return log
}

// ResolveReplace applies forced switches after faints. sw[i] is the switch
// chosen for side i (nil if that side does not need to replace).
func ResolveReplace(s *BattleState, sw [2]*Action) []LogLine {
	var log []LogLine
	if s.Phase != PhaseReplace {
		return log
	}
	// The switch path can draw from the RNG (a pinch item that fires off entry
	// hazard chip), so replacement resolution carries the battle's stream the
	// same way ResolveTurn does. Nothing draws unless such an item actually
	// fires, so the common case leaves RNGState untouched and replays identically.
	rng := NewRNG(s.RNGState)
	defer func() { s.RNGState = rng.State() }()
	for i := 0; i < 2; i++ {
		if s.Replace[i] && sw[i] != nil && sw[i].Kind == ActionSwitch {
			doSwitch(s, i, sw[i].Index, rng, &log)
		}
	}
	// Re-derive the phase rather than assuming the switch worked. A replacement
	// can die on the way in — Stealth Rock and Spikes both call faint() from
	// applyHazardsOnSwitchIn — and clearing the flag unconditionally left the
	// battle in PhaseChoosing with a fainted active, which is a state
	// ValidateStateInvariants rejects and LegalActions would read volatiles
	// from. updatePhase reads each side's active and also ends the battle for a
	// side that has just run out, which the hand-rolled version could not do.
	updatePhase(s, &log)
	checkInvariants(s)
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
	// Custap Berry grants precedence *within* the bracket, not across it: it
	// only breaks a tie in priority, and loses to any genuinely higher-priority
	// move. Both sides holding one cancels out and the speed check decides.
	// Lagging Tail is checked before the jump-the-queue items: canon resolves
	// "moves last" ahead of "moves first", so a holder of both is still last.
	lx, ly := itemMovesLast(s.Active(x)), itemMovesLast(s.Active(y))
	if lx != ly {
		return ly // x goes first exactly when y is the one lagging
	}
	cx, cy := s.Active(x).Volatiles.CustapBoost, s.Active(y).Volatiles.CustapBoost
	if cx != cy {
		return cx
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

// foeQueuedAttack reports the damaging move the target still has pending
// this turn, if any. ok is false when the target has nothing to intercept:
// it already acted (foeMoved), is switching rather than attacking, is
// locked into a recharge turn, or selected a status move. This is the
// shared gate behind Sucker Punch and Upper Hand — Showdown's onTry check.
func foeQueuedAttack(dex *domain.Dex, s *BattleState, side int, foeAction Action, foeMoved bool) (domain.Move, bool) {
	if foeMoved || foeAction.Kind != ActionMove {
		return domain.Move{}, false
	}
	foe := s.Active(1 - side)
	if foe.Volatiles.MustRecharge {
		return domain.Move{}, false
	}
	m := foeSelectedMove(dex, foe, foeAction.Index)
	if m.Category == domain.CatStatus {
		return domain.Move{}, false
	}
	return m, true
}

// foeSelectedMove resolves the move a side chose from its action index,
// falling back to Struggle for an empty/out-of-range slot (the same
// convention choosePP uses).
func foeSelectedMove(dex *domain.Dex, foe *Pokemon, idx int) domain.Move {
	if idx >= 0 && idx < len(foe.Moves) {
		return dex.Moves[foe.Moves[idx].MoveID]
	}
	return struggleMove
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
func executeMove(dex *domain.Dex, s *BattleState, side int, action Action, foeAction Action, foeMoved bool, rng *RNG, log *[]LogLine) {
	moveIdx := action.Index
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
		*log = append(*log, LogLine{
			Type: "status", Side: side,
			Text: fmt.Sprintf("%s must recharge!", atk.Name),
		})
		return
	}

	if !canAct(atk, side, rng, log) {
		// A locked-move rampage (Outrage / Thrash / Petal Dance) ends without
		// fatigue confusion if the user is prevented from acting this turn
		// (sleep / paralysis / flinch / confusion self-hit). Gen-5+ behavior.
		atk.Volatiles.LockedMove = nil
		// A confusion self-hit lands here, and it lowers HP like any other
		// damage. Without this the holder waits until the end-of-turn sweep to
		// eat — after the foe has already had its move.
		applyItemHPTrigger(s, side, rng, log)
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
		// Pressure: a foe move aimed at the holder costs an extra PP. Charged on
		// the same slot choosePP just paid, on the initiating turn only.
		applyPressurePP(s, side, atk, moveIdx, m)
		// Leppa Berry refills a move that just hit zero PP. Checked after both
		// charges above, since Pressure can be what empties the slot.
		applyItemPPRestore(atk, side, log)
		// First move under a Choice item commits the holder to it until it
		// switches out. Set on the real chosen slot (not Struggle), regardless
		// of whether the move goes on to hit — canon locks on use.
		if atk.Volatiles.ChoiceLockMoveID == "" && moveIdx >= 0 && moveIdx < len(atk.Moves) && m.ID != "" && isChoiceLockItem(atk) {
			atk.Volatiles.ChoiceLockMoveID = m.ID
		}
		if m.HasFlag("two-turn") && moveIdx >= 0 && moveIdx < len(atk.Moves) {
			atk.Volatiles.Charging = &ChargingState{MoveIdx: moveIdx}
			*log = append(*log, LogLine{
				Type: "move", Side: side,
				Text: fmt.Sprintf("%s began charging %s!", atk.Name, m.Name),
			})
			return
		}
	}

	// Assault Vest bars status moves outright. Checked here as well as in
	// LegalActions because a controller that ignores the legal set must not be
	// able to sneak one through — the same belt-and-braces the lock/restrict
	// gate below uses. PP is already spent, matching how a refused move works
	// everywhere else in this function.
	if m.Category == domain.CatStatus && itemBlocksStatusMoves(atk) {
		*log = append(*log, LogLine{
			Type: "cant", Side: side,
			Text: fmt.Sprintf("%s cannot use status moves! (%s)", atk.Name, itemOf(atk).Name),
		})
		return
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

	// Focus Punch loses its focus if the user was hit by a damaging move
	// before it fired this turn (it sits at −3 priority). Unlike Sucker
	// Punch this fails *before* the announce — canon shows only the
	// "lost its focus" line, never "used Focus Punch!". PP is already spent.
	if m.ID == "focus-punch" && atk.Volatiles.DamagedThisTurn {
		*log = append(*log, LogLine{
			Type: "fail", Side: side,
			Text: fmt.Sprintf("%s lost its focus and couldn't move!", atk.Name),
		})
		return
	}

	// Damp: an Explosion / Self-Destruct move fizzles if any active Pokémon
	// has Damp. The user doesn't blow up — the attempt just fails (PP was
	// already spent in choosePP above, canon).
	if m.HasFlag("selfdestruct") && dampActive(s) {
		*log = append(*log, LogLine{
			Type: "cant", Side: side,
			Text: fmt.Sprintf("%s cannot use %s! (Damp)", atk.Name, m.Name),
		})
		atk.Volatiles.LastMoveID = m.ID
		atk.Volatiles.LastMoveName = m.Name
		return
	}

	// Metronome's consecutive-use streak is keyed on the move that actually
	// resolves, so it ticks once the move is settled (past the charge / rampage
	// redirects) and before any damage is computed off it.
	tickMetronome(atk, m)

	announceMove(atk, side, m, log)
	// Record the move as the user's "last move" right after announce.
	// Disable / Encore inflicted by the foe later in the same turn read
	// this — canonical "your last move" semantics. Cleared on switch-out
	// with the rest of Volatiles.
	if m.ID != "" {
		atk.Volatiles.LastMoveID = m.ID
		atk.Volatiles.LastMoveName = m.Name
	}

	// Sucker Punch and Upper Hand only land if the target still has a
	// damaging move queued this turn. Both fail against a target that
	// already moved (the user was outsped), one that is switching, one
	// using a status move, or one locked into a recharge turn; Upper Hand
	// additionally requires that queued move to carry positive priority.
	// Placed after announce so the "used X! / But it failed!" pair matches
	// canon; the PP is already spent by choosePP above.
	switch m.ID {
	case "sucker-punch":
		if _, ok := foeQueuedAttack(dex, s, side, foeAction, foeMoved); !ok {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
	case "upper-hand":
		if fm, ok := foeQueuedAttack(dex, s, side, foeAction, foeMoved); !ok || fm.Priority <= 0 {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
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

	// Water Spout / Eruption / Dragon Energy: dynamic base power scales with
	// the user's remaining HP — power = floor(150 × curHP ÷ maxHP), with a
	// floor of 1 so a near-fainted user still does chip damage rather than a
	// zero-power (and thus damageless) hit. Mirrors Showdown's basePowerCallback.
	// Of the three only Water Spout is in the current (Gen-1-scoped) dataset;
	// Eruption and Dragon Energy are keyed here so the mechanic is correct the
	// day they're ever synced in, but nothing drives them today.
	if isHPRatioPowerMove(m.ID) && atk.MaxHP > 0 {
		p := m.Power * atk.HP / atk.MaxHP
		if p < 1 {
			p = 1
		}
		m.Power = p
	}

	// Acrobatics doubles when the user is holding nothing — the one move in the
	// dataset whose base power reads the item slot, and the reason it is keyed
	// here rather than left to the broader item-manipulation family (Knock Off,
	// Trick, Fling, ...) that this engine does not model. Note the canonical
	// interaction with Unburden: a holder that just ate its berry is now bare,
	// so the doubling applies from that point on.
	if m.ID == "acrobatics" && atk.Item == ItemNone {
		m.Power *= 2
	}

	// Knock Off hits 50% harder when there is something to knock off. Canon keys
	// the boost on the target holding an item, not on the removal succeeding, so
	// a Sticky Hold holder keeps its item and still takes the bigger hit.
	if knockOffBoosts(m, s.Active(1-side)) {
		m.Power = m.Power * 3 / 2
	}

	// Counter-tempo power doublings, all keyed on this turn's action order:
	//   - Payback: ×2 if the target already moved (Gen 5+ drops the old
	//     switch-out boost, so only foeMoved counts).
	//   - Revenge / Avalanche: ×2 if the user was damaged earlier this turn
	//     (they sit at −4 priority, so the hit has usually already landed).
	//   - Pursuit: ×2 when it intercepts a fleeing target — ResolveTurn runs
	//     the chase before the switch and passes the switch as foeAction.
	switch m.ID {
	case "payback":
		if foeMoved {
			m.Power *= 2
		}
	case "revenge", "avalanche":
		if atk.Volatiles.DamagedThisTurn {
			m.Power *= 2
		}
	case "pursuit":
		if foeAction.Kind == ActionSwitch {
			m.Power *= 2
		}
	}

	// Psychic Terrain blocks priority moves aimed at a grounded foe. The
	// move announces but doesn't connect — Showdown emits a "protected"
	// flavor line; we lean on the generic terrain log type so the UI can
	// style it consistently with other terrain events.
	if m.Target != domain.TargetSelf {
		def := s.Active(1 - side)
		if terrainBlocksPriorityAgainst(s.Terrain, def, m.Priority) {
			*log = append(*log, LogLine{
				Type: "terrain", Side: side,
				Text: fmt.Sprintf("%s surrounds itself with Psychic Terrain!", def.Name),
			})
			return
		}
		// Protect / Detect: the foe's one-turn shield blocks every
		// foe-targeted move (damaging or status) unless the move carries
		// bypass-protect (Feint, Hyperspace Hole, ...). Returning here
		// suppresses the damage step, applyDamageEffects, contact riders,
		// and the m.Self / Primary / Secondary cascade — canonical
		// behavior for a fully absorbed attempt.
		if protectBlocksFoeMove(def, m) {
			*log = append(*log, LogLine{
				Type: "protect", Side: 1 - side,
				Text: fmt.Sprintf("%s protected itself!", def.Name),
			})
			return
		}
	}

	// Fling and Natural Gift take their power (and Natural Gift its type) from
	// the user's item, and fail outright with nothing to throw or a suppressed
	// slot. Canon's onPrepareHit runs before the accuracy roll, so a Fling with
	// an empty slot never rolls and therefore cannot "miss" — and the item is
	// spent right here, before the throw resolves, so nothing downstream can
	// still read it as held. thrown carries the slug to the delivery below.
	thrown, itemMoveFailed := applyItemMovePrepare(s, side, &m, log)
	if itemMoveFailed {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}

	// Poltergeist has nothing to throw at an empty-handed target. Canon's onTry
	// fires before the accuracy roll, so a whiff never even gets rolled.
	if poltergeistFails(m, s.Active(1-side)) {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}

	// OHKO immunity short-circuits fire before the accuracy roll: the
	// canonical log for Sheer Cold vs Ice or any OHKO vs Sturdy is
	// "doesn't affect" / "is unaffected", not "missed". (Normal type
	// immunity still happens inside computeDamage post-accuracy.)
	if m.OHKO != "" && resolveOHKOImmunity(s, side, m, log) {
		return
	}

	if landed, missed := resolveAccuracy(s, side, m, rng, log); !landed {
		// A whiff breaks a Metronome streak: canon keys the count on the last
		// move having succeeded, so a shaky move can't ramp on misses alone.
		breakMetronomeStreak(atk)
		// Blunder Policy answers a genuine miss only — a move refused by
		// Soundproof or Safety Goggles never rolled, so there was no blunder.
		if missed {
			applyItemOnMoveMissed(s, side, m, log)
		}
		applyMissOrEndEffects(s, side, m, log)
		return
	}

	if m.Category == domain.CatStatus {
		resolved := applyStatusMove(s, side, m, rng, log)
		// Throat Spray: canon hangs it off onAfterMoveSecondarySelf, which runs
		// at the tail of the hit loop — so it pays out for a move that reached
		// its target, and not for one stopped short of that. Protect and the
		// immunities are refused above; Snatch and Magic Coat intercept from
		// inside applyStatusMove, which is what `resolved` reports. Fired before
		// applySelfSwitch so a sound self-switch move (Parting Shot, once the
		// dataset has it) boosts the mon that swung and then loses the boost on
		// its way out — also canon.
		if resolved {
			applyItemOnMoveUsed(s, side, m, log)
		}
		applyItemStatChecks(s, log)
		// Substitute (1/4 max HP) and Ghost Curse (1/2) pay HP here, which can
		// drop the user straight past a berry threshold. Checked before the
		// self-switch so the berry belongs to the Pokémon that paid for it.
		applyItemHPTrigger(s, side, rng, log)
		// Gated on `resolved` for the same reason the item hook is: a Snatched
		// Baton Pass belongs to the thief, so the user must not switch out on the
		// back of a move the log just said was taken from it. The thief doesn't
		// switch either — self-switch lives here in the caller rather than in
		// applyStatusMove, so the re-dispatch can't reach it — which leaves the
		// move fizzling. That is a degradation, but a self-consistent one; the
		// alternative contradicts its own log line.
		if resolved {
			applySelfSwitch(s, side, m, action.SwitchTarget, rng, log)
		}
		return
	}

	// Multi-strike moves (Bullet Seed, Bone Rush, Bonemerang, Triple Axel, ...)
	// loop the strike phase. The accuracy roll above gates the whole sequence;
	// each hit then rolls its own damage spread, crit, and secondary effects.
	// The loop stops early if either side faints (so a multi-hit move can't
	// continue against a 0-HP target, and Rough Skin can cut it short).
	planned := 1
	if m.IsMultihit() {
		planned = multihitCount(m, atk, rng)
	}
	// subAte stays true only while every strike so far has been eaten by a
	// Substitute. A multi-hit move that breaks the doll and then connects has
	// reached the target, which is what the item-theft moves gate on.
	hits, totalDmg, subAte := 0, 0, true
	for i := 0; i < planned; i++ {
		if s.Active(1-side).HP <= 0 || atk.HP <= 0 {
			break
		}
		dmg, ok, absorbedBySub := dealDamage(dex, s, side, m, rng, log)
		// A doll eating the hit is not damage dealt to the target, so it must
		// not feed Shell Bell's drain — canon's move.totalDamage skips it too.
		if !absorbedBySub {
			totalDmg += dmg
			subAte = false
		}
		if !ok {
			// Type immunity also fires the post-move tail: a Ghost on the
			// receiving end of Explosion still takes no damage, but the
			// user still detonates (matches the canonical Gen-1 behavior).
			// For multi-hit moves immunity is type-based and deterministic,
			// so if the first strike is immune the rest would be too —
			// short-circuit the whole sequence.
			if i == 0 {
				breakMetronomeStreak(atk)
				applyMissOrEndEffects(s, side, m, log)
				return
			}
			break
		}
		applyDamageEffects(s, side, m, dmg, rng, log)
		hits++
	}
	if m.IsMultihit() && hits > 0 {
		*log = append(*log, LogLine{
			Type: "info", Side: side,
			Text: fmt.Sprintf("Hit %d time(s)!", hits),
		})
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

	// Knock Off / Thief / Covet take the target's item once the hit has landed,
	// and a thrown berry reaches its target. Both gated on hits > 0 and on a
	// doll not having eaten the strike: canon runs these off the move connecting
	// with the target itself, and a berry thrown into a Substitute is wasted.
	if hits > 0 {
		applyItemMoveAfterHit(s, side, m, subAte, rng, log)
	}
	applyItemMoveDelivery(s, side, m, thrown, hits > 0 && !subAte, rng, log)

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

	// --- the faint window closes here ---
	//
	// Everything above this point, from the damage loop down, runs while a
	// killed Pokémon still has Fainted == false and HP == 0. Anything added in
	// that stretch that asks "is this Pokémon out of the fight?" must test the
	// HP, not the flag — isDown() in items_moves.go is that predicate, and three
	// separate bugs came from sites that checked Fainted alone (Thief looting a
	// corpse, a thrown heal berry resurrecting the target and canceling its own
	// KO, Knock Off the same).
	//
	// The window is deliberate and canon-shaped: Showdown batches faints in
	// faintMessages() and guards each site with `if (!target.hp)` for exactly
	// this reason — its own Knock Off onAfterHit opens with `if (source.hp)`.
	// Fainting inline instead would diverge from that and reorder Destiny Bond,
	// Life Orb recoil, applyOnFaint/applyOnKO, Shell Bell and the self-switch
	// suppression all at once. Guard at the site; do not move the faint.
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
			*log = append(*log, LogLine{
				Type: "destinybond", Side: 1 - side,
				Text: fmt.Sprintf("%s took its attacker down with it!", def.Name),
			})
			atk.HP = 0
		}
		// On-faint / on-KO ability reactions, gated on a connecting strike so a
		// move that whiffed every hit can't claim a KO. Aftermath runs first:
		// if it chips the attacker to 0, the attacker's own Moxie (checked by
		// applyOnKO via atk.HP) no longer fires — you don't bank a KO boost
		// while fainting to your victim.
		if hits > 0 {
			applyOnFaint(s, 1-side, side, m, log)
			applyOnKO(s, side, log)
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
	// Throat Spray sits on the same canon event as the Life Orb recoil above
	// (onAfterMoveSecondarySelf) and carries the same "the move connected" gate.
	//
	// It sits below the faint check because Destiny Bond zeroes atk.HP directly
	// without fainting, and canon leaves the item on a Pokémon on its way out.
	// Note that this placement is NOT what enforces that — applyItemOnMoveUsed's
	// own `p.Fainted || p.HP <= 0` guard is, and it holds from either side of
	// the faint check. The two are deliberately redundant: the guard covers
	// every call site, the placement keeps this one readable. Moving the call
	// back above the faint check would not break a test, so don't read the
	// silence as permission — drop the guard and the Destiny Bond hole reopens.
	if hits > 0 {
		applyItemOnMoveUsed(s, side, m, log)
	}

	// Shell Bell drains once, off the move's *total* damage — a multi-hit move
	// heals on the sum rather than truncating each strike independently, and
	// the drain lands after recoil so a heal clipped at max HP matches canon.
	if hits > 0 {
		applyItemDrainOnDamageDealt(s, side, totalDmg, log)
	}
	// Pinch items after the user's own self-damage (recoil, Life Orb, Struggle)
	// has resolved — dealDamage's in-loop check ran before any of it. The herb
	// check rides along: a stat drop from a secondary or a self-effect lands in
	// the same window.
	applyItemHPTriggers(s, rng, log)
	applyItemStatChecks(s, log)

	// Damage-variant self-switch (U-turn, Volt Switch, Flip Turn) runs after
	// faint resolution so a contact-hit-reactive faint (Rocky Helmet, Rough
	// Skin) suppresses the switch the way it does in canon.
	applySelfSwitch(s, side, m, action.SwitchTarget, rng, log)

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
// forward compatibility. Skill Link (MaxesMultihit) forces the top of the
// range regardless of the distribution.
func multihitCount(m domain.Move, atk *Pokemon, rng *RNG) int {
	if m.MinHits == m.MaxHits {
		return m.MinHits
	}
	if abilityMaxesMultihit(atk) {
		return m.MaxHits
	}
	// Loaded Dice puts a floor under the roll rather than maxing it: a [2,5]
	// move becomes 4-or-5. Skill Link above already forces the maximum, so the
	// dice never get consulted for a Skill Link holder.
	if floor := itemMinMultihit(atk); floor > m.MinHits {
		if floor >= m.MaxHits {
			return m.MaxHits
		}
		return floor + rng.IntN(m.MaxHits-floor+1)
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
	*log = append(*log, LogLine{
		Type: "recoil", Side: side,
		Text: fmt.Sprintf("%s exploded!", atk.Name),
	})
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
		*log = append(*log, LogLine{
			Type: "immune", Side: side,
			Text: fmt.Sprintf("It doesn't affect %s...", def.Name),
		})
		return true
	}
	if a := abilityOf(def); a != nil && a.Kind == AbilitySturdy && !abilityBreaksMold(s.Active(side)) {
		*log = append(*log, LogLine{
			Type: "ability", Side: 1 - side,
			Text: fmt.Sprintf("%s is unaffected by the one-hit KO! (Sturdy)", def.Name),
		})
		return true
	}
	return false
}

// resolveAccuracy rolls the accuracy check and reports whether the move lands.
// Effective accuracy is move.Accuracy * accMult(clamp(atk.Acc - def.Eva, -6,
// +6)). The bypass-acc flag (Aerial Ace, Swift, Aura Sphere) skips the roll.
// Moves with Accuracy==0 are also unmissable (status-move convention).
// The second result distinguishes a genuine accuracy-roll failure from a move
// that was refused outright (Soundproof, Safety Goggles). Both stop the move,
// but only the first is a *miss* — Blunder Policy answers a whiff, not an
// immunity, and treating the two alike had it firing off a powder move bouncing
// harmlessly off a pair of goggles.
func resolveAccuracy(s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) (landed, missed bool) {
	atk := s.Active(side)
	if m.HasFlag("bypass-acc") || m.Accuracy == 0 {
		return true, false
	}
	def := s.Active(1 - side)
	// No Guard on either combatant makes the move land unconditionally —
	// the holder's own moves never miss and moves aimed at it always hit.
	if abilityNoGuard(atk) || abilityNoGuard(def) {
		return true, false
	}
	// Telekinesis on the target makes every move land — the lifted
	// holder is too easy a target to miss. Canceled by Smack Down
	// (which clears the Telekinesis volatile on apply).
	if telekinesisAutoHits(def) {
		return true, false
	}
	// Safety Goggles: powder-flagged moves don't affect the holder. Same
	// "doesn't affect" shape as Soundproof below — the move is refused, not
	// missed.
	if itemBlocksPowderMove(def, m) {
		*log = append(*log, LogLine{
			Type: "immune", Side: side,
			Text: fmt.Sprintf("It doesn't affect %s... (%s)", def.Name, itemOf(def).Name),
		})
		return false, false // refused, not missed
	}
	// Soundproof: sound-flagged moves don't affect the holder at all. We
	// log "doesn't affect" rather than "missed" to match canon.
	if m.HasFlag("sound") {
		if a := abilityOf(def); a != nil && a.Kind == "soundproof" && !abilityBreaksMold(atk) {
			*log = append(*log, LogLine{
				Type: "immune", Side: side,
				Text: fmt.Sprintf("It doesn't affect %s... (Soundproof)", def.Name),
			})
			return false, false // refused, not missed
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
	acc := float64(m.Accuracy) * accStageMultiplier(combined) * abilityAccuracyMult(atk) * abilityAccuracyMultVs(s, def, m)
	// Held-item accuracy: the attacker's own lenses, then the defender's
	// evasion items. Both sit beside their ability equivalents in the chain.
	// OHKO moves bypass the accuracy modifiers entirely in canon, the same
	// exclusion the Micle Berry check below applies.
	if m.OHKO == "" {
		acc *= itemAccuracyMult(s, side) * itemAccuracyMultVs(def)
	}
	chance := int(acc)
	// A primed Micle Berry is spent here, at the point a real accuracy roll
	// happens — not on an unmissable move (those returned above) and not on an
	// OHKO move, whose accuracy canon explicitly refuses to boost. Integer math
	// so the ×1.2 lands where canon lands it.
	if atk.Volatiles.MicleTurns > 0 && m.OHKO == "" {
		atk.Volatiles.MicleTurns = 0
		chance = chance * micleAccuracyNum / micleAccuracyDen
	}
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
		return false, true
	}
	return true, false
}

// dealDamage computes and applies damage for a non-status move, logging the
// damage/crit/effectiveness lines. Returns (dmg, true) on a normal hit, or
// (0, false) if the move was immune-blocked. A frozen target hit by a
// Fire-type damaging move thaws before damage applies; the move still lands.
// hitSub reports that a Substitute absorbed the blow. The caller needs it to
// keep sub-absorbed damage out of any total that feeds the holder's own items:
// Shell Bell drains off the damage its move did to the *target*, and canon's
// move.totalDamage does not accumulate a substitute hit.
func dealDamage(dex *domain.Dex, s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) (dmg int, ok, hitSub bool) {
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
				*log = append(*log, LogLine{
					Type: "immune", Side: side,
					Text: fmt.Sprintf("It doesn't affect %s...", def.Name),
				})
			}
		} else {
			*log = append(*log, LogLine{
				Type: "immune", Side: side,
				Text: fmt.Sprintf("It doesn't affect %s...", def.Name),
			})
		}
		return 0, false, false
	}
	if def.Status == StatusFreeze && (m.Type == "fire" || m.ThawsTarget) {
		def.Status = StatusNone
		*log = append(*log, LogLine{
			Type: "status", Side: 1 - side,
			Text: fmt.Sprintf("%s was thawed by the heat!", def.Name),
		})
	}
	dmg = res.Damage
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
		// No applyOnHit here: a hit the doll absorbed never reached the holder,
		// so no contact rider fires. The call used to be made with hitSub=true
		// and the dispatcher now returns immediately on that, so it was dead —
		// removed rather than left to read as if it did something.
		return absorbed, true, true
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
		if capped, fired := itemSurviveOHKO(def, dmg, rng); fired {
			dmg = capped
			sashSaved = true
		}
	}
	// Resist berry: the ×0.5 is already baked into res.Damage (computeDamage
	// consulted the same predicate), so all that's left is to announce it and
	// spend the berry. Announced before the damage line to match canon order.
	if itemResistBerryApplies(atk, def, m, res.Effectiveness) {
		berry := itemOf(def)
		*log = append(*log, LogLine{
			Type: "item", Side: 1 - side,
			Text: fmt.Sprintf("The %s weakened the damage to %s!", berry.Name, def.Name),
		})
		consumeItem(def)
	}
	def.HP -= dmg
	if dmg > 0 {
		// Flag the hit for the counter-punch moves that resolve later this
		// turn: Revenge / Avalanche read it for their ×2 BP, Focus Punch for
		// its loss-of-focus fail. Only direct move damage counts (confusion
		// self-hits and recoil take other paths).
		def.Volatiles.DamagedThisTurn = true
	}
	*log = append(*log, LogLine{Type: "damage", Side: 1 - side, Text: fmt.Sprintf("%s took %d damage.", def.Name, dmg)})
	if enduredHit {
		*log = append(*log, LogLine{
			Type: "endure", Side: 1 - side,
			Text: fmt.Sprintf("%s endured the hit!", def.Name),
		})
	}
	if sashSaved {
		// Focus Sash is spent on the save; Focus Band rolls again next time. The
		// registry entry decides which, so the log names the item that actually
		// fired rather than assuming a sash.
		saver := itemOf(def)
		*log = append(*log, LogLine{
			Type: "item", Side: 1 - side,
			Text: fmt.Sprintf("%s hung on with its %s!", def.Name, saver.Name),
		})
		if saver.SurviveOHKO != nil {
			consumeItem(def)
		}
	}
	if m.OHKO != "" && !enduredHit && !sashSaved {
		*log = append(*log, LogLine{Type: "info", Side: side, Text: "It's a one-hit KO!"})
	} else if m.OHKO == "" {
		if res.Crit {
			*log = append(*log, LogLine{Type: "crit", Side: side, Text: "A critical hit!"})
			// Anger Point: a surviving defender maxes its Attack off the crit.
			applyOnCrit(s, 1-side, log)
		}
		if res.Effectiveness > 1 {
			*log = append(*log, LogLine{Type: "effective", Side: side, Text: "It's super effective!"})
		} else if res.Effectiveness < 1 {
			*log = append(*log, LogLine{Type: "resisted", Side: side, Text: "It's not very effective..."})
		}
		if res.Sturdy {
			*log = append(*log, LogLine{
				Type: "ability", Side: 1 - side,
				Text: fmt.Sprintf("%s hung on with Sturdy!", def.Name),
			})
		}
	}
	// On-hit hook for contact riders (Static, Flame Body, Poison Point,
	// Effect Spore). The hook itself checks the contact flag — keeping that
	// inside the ability avoids spreading move-flag inspection across
	// integration sites.
	applyOnHit(s, 1-side, m, false, rng, log)
	// Reactive held items on the defender (Enigma / Jaboca / Rowap / Kee /
	// Maranga). Same "the hit actually connected on the holder" gate as the
	// ability riders — a Substitute-absorbed hit returned above.
	applyItemOnHitTaken(s, 1-side, m, res, log)
	applyItemOnHitTakenPassive(s, 1-side, m, res, log)
	// Shell Bell drains on the attacker's side of the same connecting hit.
	applyItemOnDealtDamage(s, side, dmg, m, rng, log)
	// Pinch items, checked for both sides: the defender may have just dropped
	// past its threshold, and a Jaboca/Rowap chip may have pushed the attacker
	// past its own. Inside the multi-hit loop, so a berry fires between strikes
	// exactly as it does in canon.
	applyItemHPTriggers(s, rng, log)
	return dmg, true, false
}

// canAct applies pre-move status and volatile checks and reports whether the
// Pokémon moves. Order: flinch (one-shot, consumed) → confusion (may self-hit
// and preempt) → attract (50% immobilize) → non-volatile status (freeze /
// sleep / para).
func canAct(p *Pokemon, side int, rng *RNG, log *[]LogLine) bool {
	if p.Volatiles.Flinch {
		p.Volatiles.Flinch = false
		*log = append(*log, LogLine{
			Type: "status", Side: side,
			Text: p.Name + " flinched and couldn't move!",
		})
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
			// Early Bird burns sleep twice as fast: a second tick each turn.
			if p.SleepTurns > 0 && abilityIsEarlyBird(p) {
				p.SleepTurns--
			}
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
	*log = append(*log, LogLine{
		Type: "status", Side: side,
		Text: fmt.Sprintf("%s hurt itself in its confusion! (-%d)", p.Name, dmg),
	})
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
		*log = append(*log, LogLine{
			Type: "win", Side: winner,
			Text: fmt.Sprintf("%s won the battle!", s.Sides[winner].Trainer),
		})
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
