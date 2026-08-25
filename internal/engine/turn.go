package engine

import (
	"fmt"
	"math"

	"pokearena/internal/domain"
)

// maxTurns caps a battle so two defensive teams cannot loop forever; at the
// cap the winner is decided on remaining team HP.
const maxTurns = 300

// struggleMove is the typeless fallback used when a Pokémon has no PP left.
//
// It carries no self-effect. Its recoil used to ride the standard
// Effect.Recoil block, and the reuse was the defect twice over: that block
// takes its fraction of the *damage dealt*, where Struggle since Gen 4 costs a
// quarter of the user's **maximum HP** whatever it dealt — so a Struggle into a
// resist cost almost nothing here and a quarter of the bar in canon, which is
// the difference between Struggle being a last resort and being free. And that
// block is gated on !abilityBlocksRecoil, so a Rock Head user Struggled for
// nothing; canon exempts Struggle from Rock Head specifically, because
// struggleRecoil is not recoil in the sense the ability cares about (it is a
// bare directDamage in battle-actions.ts, not a move's recoil field).
//
// The Magic Guard half is a different question and stays where it is: the
// engine treats Struggle's chip as indirect damage, which is the reading the
// wider references take.
//
// See applyStruggleRecoil for the rule as it is now applied.
var struggleMove = domain.Move{
	Name: "Struggle", Type: "", Category: domain.CatPhysical, Power: 50, Accuracy: 100,
}

// isStruggle reports whether m is the Struggle fallback. Struggle has no dex
// entry and therefore no ID, which is what distinguishes it — the same test
// tickMetronome makes for the same reason.
func isStruggle(m domain.Move) bool {
	return m.ID == "" && m.Name == "Struggle"
}

// applyStruggleRecoil charges the user a quarter of its maximum HP, rounded to
// nearest and floored at 1 — canon's clampIntRange(round(baseMaxhp / 4), 1).
// Independent of what the move dealt, and not blocked by Rock Head; Magic Guard
// still refuses it as indirect damage.
func applyStruggleRecoil(atk *Pokemon, side int, log *[]LogLine) {
	if abilityBlocksIndirectDamage(atk) {
		return
	}
	amt := int(math.Round(float64(atk.MaxHP) / 4))
	if amt < 1 {
		amt = 1
	}
	applySelfDamage(atk, side, amt, log)
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
	// The stat-direction flags need the same treatment for the same reason, and
	// the case is not hypothetical: an Intimidate on a replacement that comes in
	// after a KO drops the attacker's Attack during ResolveReplace, which the
	// end-of-turn sweep has already run past. Left standing, that drop would
	// double the attacker's Lash Out on the following turn — a turn on which
	// nothing lowered its stats at all.
	for i := 0; i < 2; i++ {
		s.Active(i).Volatiles.MovedThisTurn = false
		s.Active(i).Volatiles.StatsRaisedThisTurn = false
		s.Active(i).Volatiles.StatsLoweredThisTurn = false
	}

	// Ability suppression is established before anything reads an ability this
	// turn. On turn 1 that ordering is the mechanic: a lead Weezing has to be
	// filling the field already when the opposing lead's Drought would fire,
	// and the lead hooks below are the first thing that would ask. Canon gets
	// there by giving Neutralizing Gas an onPreStart that runs ahead of every
	// onStart; this is the same ordering with the state made explicit.
	syncAbilitySuppression(s, &log)

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
			// The volatile is what makes the announcement mean something.
			// Canon's focuspunch condition carries an onTryAddVolatile that
			// refuses a flinch outright, so a Fake Out cannot stop a Pokemon
			// that has already braced — the announce used to be the only trace
			// of the intent and applyFlinchVolatile had nothing to consult.
			s.Active(i).Volatiles.FocusPunch = true
			log = append(log, LogLine{
				Type: "move", Side: i,
				Text: fmt.Sprintf("%s is tightening its focus!", s.Active(i).Name),
			})
		}
	}

	// Assault Vest, as of *choice*. The vest is a selection-time gate in canon —
	// Showdown implements it as an onDisableMove, so it decides what appears on
	// the menu and nothing else. executeMove re-checks it as belt-and-braces
	// against a controller that ignores the legal set, and that copy read the
	// item as of *execution*: a vest handed over mid-turn by Trick retroactively
	// canceled a status move its new holder had already legally chosen.
	//
	// Snapshotting here, before anything resolves, is what "the state the choice
	// was validated against" means — and it is a local, not state on the
	// Pokemon, because it is true for exactly the length of this turn.
	var vestedAtChoice [2]bool
	for i := 0; i < 2; i++ {
		vestedAtChoice[i] = itemBlocksStatusMoves(s.Active(i))
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

	// Pursuit interception, site one of two: a Pursuit user strikes a target
	// that *chose* to switch, before it leaves, out of normal speed order and
	// at doubled power (the doubling is keyed on the switch action inside
	// executeMove). The pursuer is flagged done so it doesn't also act in the
	// mover loop below.
	//
	// Canon has a second site for a target leaving under its own power — a
	// pivot move — which cannot be handled here because the pivot has not
	// resolved yet. That one fires from inside applySelfSwitch; see
	// runPursuitBeforeSwitchOut and the arming in the mover loop below.
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
		executeMove(dex, s, foe, actions[foe], actions[i], false, vestedAtChoice[foe], rng, &log)
		pursued[foe] = true
	}

	// Switches always resolve before moves. A target KO'd by Pursuit above
	// stays put — its faint routes into the replace phase instead.
	//
	// A chosen switch is an *action*, and actions go in Speed order — of the
	// Pokemon that is leaving, because that is the one whose action it is. Each
	// one resolves completely, install and entry effects together, before the
	// other side's begins. That is what upstream's "should happen in order of
	// switch-out's Speed stat" is about: the faster side's replacement arrives
	// while the slower side's outgoing Pokemon is still on the field, so an
	// Intimidate on the way in cuts the Attack of a Pokemon that is about to
	// leave, and the slower side's arrival walks in untouched.
	//
	// Deliberately NOT the batched treatment the replace phase gets. Two
	// replacements after a double KO are simultaneous — one fieldEvent over both
	// — where two chosen switches are two actions in a queue. Batching these
	// would swap the case above for its opposite.
	//
	// The order is read before anything is installed, since installing changes
	// the actives it is read from — and only when both sides are actually
	// switching, because speedOrder draws from the RNG on a Speed tie and a
	// draw nobody needed would shift the stream on every turn of a mirror
	// match.
	switchOrder := [2]int{0, 1}
	if actions[0].Kind == ActionSwitch && actions[1].Kind == ActionSwitch {
		switchOrder = speedOrder(s, rng)
	}
	for _, i := range switchOrder {
		if actions[i].Kind == ActionSwitch && !s.Active(i).Fainted {
			doSwitch(s, i, actions[i].Index, rng, &log)
		}
	}

	// Movers act in priority-then-speed order (a Pursuit chaser that intercepted
	// a *chosen* switch above already acted; one that intercepts a pivot is
	// consumed from inside the loop below).
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
	// consumed marks a side whose action was spent as a Pursuit chase from
	// inside the other side's pivot move, so the loop does not let it act twice.
	var consumed [2]bool
	for i, side := range ordered {
		if s.Active(side).Fainted || consumed[side] {
			continue
		}
		// Arm a Pursuit against a foe that is still to move. This is the
		// engine's answer to canon's `!this.queue.cancelMove(source)` test: the
		// chase is possible only while the pursuer's own action is unspent, so
		// arming it here — before the foe's move runs, and only when the foe
		// has not yet acted — reproduces the rule that a faster Pursuit user
		// gets no interception because it already moved.
		foe := 1 - side
		if !moved[foe] && !consumed[foe] && actions[foe].Kind == ActionMove &&
			!s.Active(foe).Fainted &&
			foeSelectedMove(dex, s.Active(foe), actions[foe].Index).ID == "pursuit" {
			s.pursuit = &pursuitChase{
				dex: dex, side: foe, action: actions[foe],
				vested: vestedAtChoice[foe], rng: rng,
			}
		}
		// Mark the last scheduled mover so Analytic can read it from inside
		// computeDamage. Set before executeMove so the hook sees true on the
		// same move it modifies.
		if i == len(ordered)-1 {
			s.Active(side).Volatiles.MovedLast = true
		}
		mover := s.Active(side)
		executeMove(dex, s, side, actions[side], actions[1-side], moved[1-side], vestedAtChoice[side], rng, &log)
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
		// If the move above triggered the armed chase, the pursuer has now had
		// its action and must not take a second one.
		if s.pursuit != nil {
			if s.pursuit.spent {
				moved[s.pursuit.side] = true
				consumed[s.pursuit.side] = true
			}
			s.pursuit = nil
		}
		// A move can end the gas mid-turn — by KOing the Weezing holding it,
		// by dousing it with Gastro Acid, or by pivoting it out. Re-derived
		// here rather than only at end of turn so the second mover, and every
		// residual after it, sees the ability it should.
		syncAbilitySuppression(s, &log)
	}

	// End-of-turn residuals, in canon's order. Showdown runs the whole phase as
	// a single fieldEvent('Residual') whose handlers are speed-sorted by
	// Battle#comparePriority: onResidualOrder first, then priority, then
	// **Speed**, then onResidualSubOrder. So the phase is a sequence of ordered
	// blocks, and within a block the faster Pokémon goes first. The upstream
	// numbers, restricted to what this engine models:
	//
	//	 1  the weather's own handler — the countdown, then its chip
	//	    (sandstorm, hail) and every onWeather rider that hangs off it
	//	 4  Wish
	//	 5  Grassy Terrain heal (sub 2), Hydration / Shed Skin (sub 3),
	//	    Leftovers / Black Sludge (sub 4)
	//	 6  Aqua Ring
	//	 7  Ingrain
	//	 8  Leech Seed
	//	 9  poison / toxic chip
	//	10  burn chip, the end-of-turn berries
	//	11  Nightmare      12  Curse       13  partial trap
	//	15  Taunt   16  Encore   17  Disable   18  Magnet Rise
	//	19  Telekinesis   21  Embargo   23  Yawn   24  Perish Song
	//	27  the field timers: terrain, Trick Room, Wonder Room, Gravity
	//	28  Speed Boost / Moody / Harvest (sub 2), the orbs and Sticky
	//	    Barb (sub 3)
	//	29  White Herb, Eject Pack, Mirror Herb
	//
	// Two things about order 1 are load-bearing and were both wrong here.
	//
	// The countdown comes *first*, inside the weather's own handler, and canon
	// skips the rest of that handler when the timer hits zero — fieldEvent's
	// `handler.state.duration--; if (!duration) { end(); continue }`. So on the
	// weather's final turn the weather is already gone when residuals run:
	// neither the chip nor the weather-keyed abilities fire. This engine ran the
	// chip first and ticked at the very end, which chipped on a turn canon does
	// not. docs/engine-findings.md records the earlier decision to move the
	// countdown *after* the ability ticks, so that "one residual phase gives one
	// answer about whether the weather is up". That diagnosis was right and the
	// resolution went the wrong way: canon's single answer is "already over",
	// and moving the countdown to the top gives the same consistency while
	// matching it.
	//
	// And the phase is ordered by Speed, not by side index — see speedOrder.
	//
	// applyItemHPTriggers runs after each step that moves HP, because any of
	// them can push a holder into berry range. It no-ops for a holder with
	// nothing to trigger, so the repetition is cheap.
	order := speedOrder(s, rng)

	tickWeather(s, &log)
	applyWeatherResidual(s, order, &log)
	applyItemHPTriggers(s, rng, &log)

	// Order 3: a Future Sight cast two turns ago lands here. Above the order-5
	// block and below the weather countdown, which is canon's own position and
	// is observable in both directions — a sandstorm in its last turn is
	// already gone when the hit arrives, and a Psychic Terrain in its last turn
	// still boosts it, because the terrain clock does not run until much
	// further down.
	for _, i := range order {
		tickFutureMoves(dex, s, i, rng, &log)
	}
	applyItemHPTriggers(s, rng, &log)

	// Order 5, one Speed-ordered block: each Pokémon's Grassy Terrain heal
	// (sub 2) comes before its own Leftovers tick (sub 4), and the faster
	// Pokémon's pair comes before the slower one's.
	for _, i := range order {
		applyTerrainResidual(s, i, &log)
		applyItemEndOfTurn(s, i, &log)
	}

	// Aqua Ring + Ingrain heals (6, 7), then Leech Seed's drain (8).
	for _, i := range order {
		applyRingHeals(s, i, &log)
	}
	for _, i := range order {
		applyLeechSeedResidual(s, i, &log)
	}
	applyItemHPTriggers(s, rng, &log)

	// Status chip (poison and toxic at 9, burn at 10) is last of the HP movers.
	for _, i := range order {
		applyResidual(s, i, &log)
	}
	applyItemHPTriggers(s, rng, &log)

	// Per-side screens (Reflect / Light Screen / Aurora Veil): no residual,
	// just count down and clear at zero. Side 0 then Side 1 for log
	// determinism. tickBuffs handles Tailwind / Safeguard / Mist with
	// the same shape.
	for _, i := range order {
		tickScreens(s, i, &log)
		tickBuffs(s, i, &log)
	}

	// Lock/restrict timer volatiles (Disable / Encore / Taunt / Embargo).
	// Per-active, side 0 then side 1 for log determinism. Torment and
	// Imprison are indefinite — not ticked.
	for _, i := range order {
		tickLockRestrict(s, i, &log)
	}

	// Status-adjacent volatiles: Yawn → Nightmare chip → Curse chip.
	// Side 0 first for log determinism. Destiny Bond clears in the
	// transient sweep below (same lifecycle as Protect/Endure).
	for _, i := range order {
		tickStatusVols(s, i, &log)
	}

	// Gimmick timers (Magnet Rise, Telekinesis). Snatch / Magic Coat
	// are one-turn flags cleared in the transient sweep below.
	for _, i := range order {
		tickGimmicks(s, i, &log)
	}

	// Order 27, the field timers: the terrain's counter and the rooms', which
	// upstream gives the same onFieldResidualOrder. Cloud Nine does NOT
	// suppress terrain in Gen 8+, so s.Terrain is read directly without an
	// "effective" filter. Pseudo-weather is field-scoped (not per-side); one
	// tick covers all active timers.
	tickTerrain(s, &log)
	tickPseudoWeather(s, &log)

	// Slot conditions: Wish heal lands here on its scheduled tick.
	// Side 0 first for log determinism. HealingWish has no tick — it
	// consumes on switch-in via applySlotConditionsOnSwitchIn.
	for _, i := range order {
		tickSlotConditions(s, i, &log)
	}

	// The residual chip above can KO the gas holder, so suppression is
	// re-derived before the abilities that would be freed by that get their
	// tick. A Speed Boost holder whose Weezing just died to poison is owed
	// this turn's boost.
	syncAbilitySuppression(s, &log)

	// Order 28: the ability end-of-turn ticks (Speed Boost, Rain Dish, Ice Body,
	// Dry Skin, Solar Power). The weather-keyed ones read the weather that is
	// left after the countdown at the top of the phase, which is the whole
	// point of moving it there: on the weather's final turn the sky is already
	// gone for the abilities *and* for the chip, so the phase gives one answer
	// rather than two. Upstream puts the onWeather riders inside the weather's
	// own handler at order 1 rather than out here at 28; the difference is
	// unobservable in a singles engine with no other order-1..27 effect that
	// reads them, and the answer to "is the weather up?" is the same either way.
	for _, i := range order {
		applyAbilityEndOfTurn(s, i, rng, &log)
	}

	// Late held-item residuals: the orbs and Sticky Barb. Canon puts these at
	// the very end of the residual order, so the turn an orb fires costs the
	// holder no status damage — that free turn is the whole reason to run one.
	for _, i := range order {
		applyItemEndOfTurnLate(s, i, rng, &log)
	}

	// Perish Song counts at the very end of the residual order, after every
	// heal and chip has had its say. A Pokémon the song takes this turn was
	// going to be taken regardless — but anything that would have fainted to
	// poison or a Life Orb tick does so first, and the log reads in that
	// order.
	for _, i := range order {
		tickPerishSong(s, i, &log)
	}

	// Final pinch sweep: the timer ticks and volatile residuals above (Leech
	// Seed, Nightmare, Curse, partial trap) can also drop a holder into range,
	// as can a Sticky Barb tick. The herbs are checked in the same sweep — a
	// Taunt or a stat drop inflicted this turn is theirs to answer.
	applyItemHPTriggers(s, rng, &log)
	applyItemStatChecks(s, &log)

	// Eject Pack's backstop, canon's onResidualOrder 29 — the last of the four
	// places upstream drains the flag, and the one that catches a drop nothing
	// else did. It runs after the herbs on purpose: a White Herb that undoes
	// the drop does not disarm the pack (the drop still happened, and canon
	// arms from onAfterBoost regardless of what restores it afterwards), but
	// the holder should have the herb's answer before it decides to leave.
	fireEjectPacks(s, rng, &log)

	// Destiny Bond is deliberately NOT in the sweep below. It used to be, next
	// to Protect and Endure, which made the consecutive-use guard in
	// applyDestinyBondVolatile unreachable — the volatile was gone before the
	// next turn could refuse it, so the threat was re-armable indefinitely and a
	// Pokemon could hold it up every turn it was alive. Canon keeps it until the
	// user's next move (the condition's onBeforeMove removes it for anything
	// that is not Destiny Bond itself), which is what executeMove does now.
	//
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
		s.Active(i).Volatiles.HurtThisTurn = false
		// Counter and Mirror Coat pay back this turn's hits and no other:
		// canon gives their volatile duration 1. Bide's store is not cleared
		// here — it is the one accumulator meant to outlive the turn.
		s.Active(i).Volatiles.ReactivePhysical, s.Active(i).Volatiles.TookPhysicalHit = 0, false
		s.Active(i).Volatiles.ReactiveSpecial, s.Active(i).Volatiles.TookSpecialHit = 0, false
		tickBide(s.Active(i))
		s.Active(i).Volatiles.StatsRaisedThisTurn = false
		s.Active(i).Volatiles.StatsLoweredThisTurn = false
		// The failure record shifts a turn rather than clearing: Stomping
		// Tantrum asks about the turn before this one. A Pokémon that did not
		// act at all (it switched in, or was asleep and never reached
		// executeMove) leaves MoveThisTurnFailed false, which is right — canon's
		// moveThisTurnResult is undefined in that case and Stomping Tantrum
		// compares strictly against false.
		s.Active(i).Volatiles.MoveLastTurnFailed = s.Active(i).Volatiles.MoveThisTurnFailed
		s.Active(i).Volatiles.MoveThisTurnFailed = false
		tickFuryCutter(s.Active(i))
		tickRollout(s.Active(i))
		tickLockOn(s.Active(i))
		s.Active(i).Volatiles.Protect = false
		s.Active(i).Volatiles.Endure = false
		s.Active(i).Volatiles.Snatch = false
		s.Active(i).Volatiles.MagicCoat = false
		s.Active(i).Volatiles.Roost = false
		s.Active(i).Volatiles.FocusPunch = false
		// CustapBoost is this turn's ordering decision, not a lasting buff.
		// A Micle prime is not cleared here — it has to survive into the next
		// turn to be spendable at all — but it does tick down, so it lapses
		// rather than banking indefinitely.
		s.Active(i).Volatiles.CustapBoost = false
		if s.Active(i).Volatiles.MicleTurns > 0 {
			s.Active(i).Volatiles.MicleTurns--
		}
	}

	// Final re-derive: the late residuals (Perish Song, the orbs, Sticky Barb)
	// can still take the gas holder down after the block above. Leaves the
	// mirror agreeing with the field at the turn boundary, which is where
	// ValidateStateInvariants checks it.
	syncAbilitySuppression(s, &log)

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
	// Both replacements are installed before either one's entry effects run, and
	// then the entry effects go in Speed order. Canon collects every switch-in,
	// speed-sorts them, and fires one fieldEvent('SwitchIn'); this loop used to
	// install-and-resolve each side in index order, which after a double KO gave
	// p1's replacement an Intimidate aimed at a slot that still held p2's corpse
	// — swallowed by the hook's own fainted check — and then let p2's
	// replacement intimidate normally. Which side got the drop for free depended
	// only on side index. See applySwitchInEffects.
	//
	// This is also what puts Trick Room into the entry phase: speedOrder reads
	// it, so a Drought/Drizzle race after a double KO resolves the way the turn
	// order would.
	var entering []int
	for i := 0; i < 2; i++ {
		if s.Replace[i] && sw[i] != nil && sw[i].Kind == ActionSwitch {
			installSwitchIn(s, i, sw[i].Index, nil, &log)
			entering = append(entering, i)
		}
	}
	// Only both-sides-entering needs the order, and only then is the RNG draw a
	// Speed tie can cost worth spending.
	if len(entering) == 2 {
		o := speedOrder(s, rng)
		entering = []int{o[0], o[1]}
	}
	for _, i := range entering {
		applySwitchInEffects(s, i, rng, &log)
	}
	// A side with nothing left to send never reaches doSwitch, so the gas can
	// end here without any switch having happened.
	syncAbilitySuppression(s, &log)

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
func executeMove(dex *domain.Dex, s *BattleState, side int, action Action, foeAction Action,
	foeMoved, vestedAtChoice bool, rng *RNG, log *[]LogLine,
) {
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
	// This is a move action, whatever becomes of it. Counted before every
	// early return below, because canon counts the action and not the
	// outcome: a recharge turn, a flinch and a full paralysis all burn the
	// user's "first turn out".
	atk.Volatiles.MoveActions++

	counterBefore := atk.Volatiles.ProtectCounter
	defer func() {
		if atk.Volatiles.ProtectCounter == counterBefore {
			atk.Volatiles.ProtectCounter = 0
		}
	}()

	// Held in the air by the foe's Sky Drop: the turn is gone. Canon expresses
	// it as an onFoeBeforeMove that returns null, and the null rather than false
	// is load-bearing — it means the move did not *fail*, so nothing that reads
	// a failed move (Stomping Tantrum) arms off a turn spent as somebody else's
	// cargo. Returning here, above the block that records a failure, is that
	// distinction.
	//
	// Canon also undoes its own move-action count for the same reason Fake Out
	// would otherwise be spent by a turn the user never got.
	if heldBySkyDrop(s, side) {
		atk.Volatiles.MoveActions--
		*log = append(*log, LogLine{
			Type: "cant", Side: side,
			Text: fmt.Sprintf("%s can't move while it is in the air!", atk.Name),
		})
		return
	}

	if atk.Volatiles.MustRecharge {
		atk.Volatiles.MustRecharge = false
		// A turn spent recharging is a turn the holder's last move did not
		// succeed, so a Metronome streak dies here — see the defer below for
		// why every non-success has to say so.
		breakMetronomeStreak(atk)
		// And an aborted move drops a Destiny Bond, whatever the move was:
		// canon's onMoveAborted is unconditional where onBeforeMove exempts
		// Destiny Bond itself.
		atk.Volatiles.DestinyBond = false
		*log = append(*log, LogLine{
			Type: "status", Side: side,
			Text: fmt.Sprintf("%s must recharge!", atk.Name),
		})
		return
	}

	// canAct needs to know what is being attempted, because two moves are
	// usable while asleep and the sleep gate lives inside it. The move is read
	// without paying for it — a forced continuation if one is armed, otherwise
	// the submitted slot — for the same reason the Gravity and Focus Punch
	// gates below use foeSelectedMove.
	if !canAct(atk, side, pendingMove(dex, atk, moveIdx), rng, log) {
		breakMetronomeStreak(atk)
		// Aborted, so the bond goes — including a Destiny Bond the user was
		// trying to renew. This is the case upstream names "should be removed
		// the next turn if a fast user is asleep": the threat cannot outlive a
		// turn the user could not act on.
		atk.Volatiles.DestinyBond = false
		// A locked-move rampage (Outrage / Thrash / Petal Dance) ends without
		// fatigue confusion if the user is prevented from acting this turn
		// (sleep / paralysis / flinch / confusion self-hit). Gen-5+ behavior.
		atk.Volatiles.LockedMove = nil
		// Bide dies the same way, and canon is explicit about it: the condition
		// carries onMoveAborted, which fires on every route that stops the user
		// acting. A store the user cannot release is a store it loses.
		atk.Volatiles.Bide = nil
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
	defer tickLockedMove(s, atk, side, rng, log)

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
	// calledFrom is the move the user actually chose, on the turns when what
	// resolves is something else that move called. Empty for every ordinary
	// move, and read once — at the point the user's own last-move register is
	// written, which must name the caller rather than the callee.
	var calledFrom domain.Move
	// twoTurnStrike records that this is the strike leg of a two-turn move,
	// which is the one thing Metronome's counter needs to know about the shape
	// of the turn — see tickMetronome.
	twoTurnStrike := atk.Volatiles.Charging != nil
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
	case atk.Volatiles.Bide != nil:
		// A Bide already in flight. Same shape as the rampage above: PP was
		// paid on the first turn and the submitted index is ignored.
		m = dex.Moves[atk.Moves[atk.Volatiles.Bide.MoveIdx].MoveID]
	default:
		// Focus Punch loses its focus if the user was hit by a damaging move
		// before it fired this turn (it sits at -3 priority). Checked here,
		// above choosePP, because canon's beforeMoveCallback runs *before*
		// deductPP in battle-actions.ts — a Focus Punch that never went off
		// costs the user nothing. Unlike Sucker Punch it also fails before the
		// announce: canon shows only the "lost its focus" line, never
		// "used Focus Punch!".
		//
		// Deliberately not generalized. Fake Out's onTry and Damp's
		// onAnyTryMove both live *inside* useMove, i.e. after deductPP, so
		// their "PP is already spent" behavior is correct canon; only
		// beforeMoveCallback moves are pre-PP, and Focus Punch is the only one
		// in this dataset.
		// Gravity refuses the airborne moves before PP is paid, for the same
		// reason: upstream's onBeforeMove runs inside runMove ahead of
		// deductPP, so a Fly refused by Gravity costs the user nothing. Read
		// through foeSelectedMove, which looks the move up without paying for
		// it. This is the authoritative refusal — the LegalActionsDex filter
		// only keeps the option off a menu, and the dex-less LegalActions the
		// AI calls cannot run that filter at all.
		if sel := foeSelectedMove(dex, atk, moveIdx); gravityBlocksMove(s, sel) {
			breakMetronomeStreak(atk)
			*log = append(*log, LogLine{
				Type: "cant", Side: side,
				Text: fmt.Sprintf("%s can't use %s because of gravity!", atk.Name, sel.Name),
			})
			return
		}
		if sel := foeSelectedMove(dex, atk, moveIdx); sel.ID == "focus-punch" && atk.Volatiles.DamagedThisTurn {
			breakMetronomeStreak(atk)
			*log = append(*log, LogLine{
				Type: "fail", Side: side,
				Text: fmt.Sprintf("%s lost its focus and couldn't move!", atk.Name),
			})
			return
		}
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
		// Nature Power is not a move so much as a name for whichever move the
		// terrain says: Tri Attack on bare ground, and Thunderbolt / Energy
		// Ball / Moonblast / Psychic under the four terrains.
		//
		// Substituted here, and the position is the whole of the design. It is
		// after choosePP, so the user pays for Nature Power and the called move
		// costs nothing — canon's useMove is not a second action. It is after
		// the Choice lock, so a Choice Specs holder is locked into Nature Power
		// rather than into whatever the terrain happened to be. And it is
		// before everything else, so the substituted move simply *is* the move
		// from here on: it announces itself, rolls its own accuracy, carries
		// its own type and category, and sets DamagedThisTurn for a Focus Punch
		// to lose its focus on.
		//
		// This note used to end by arguing that the general form would be the
		// wrong trade — that Metronome, Sleep Talk, Copycat, Assist and Mirror
		// Move "all need to call a move the *user* did not choose,
		// mid-resolution and re-entrantly", where Nature Power only needs a
		// substitution. Half of that held up. Re-entering executeMove really is
		// the wrong way to get re-entrancy, for the reasons calledmoves.go sets
		// out at length; but what is left once you stop trying is this same
		// substitution, so the seam generalized after all and the family lives
		// on the two lines below.
		if m.ID == "nature-power" {
			m = naturePowerMove(dex, s)
		}
		if callsAnotherMove(m) {
			// The caller announces itself before it picks, because canon logs
			// both lines and because a refusal that never named the move doing
			// the refusing reads as a mystery.
			announceMove(atk, side, m, log)
			atk.Volatiles.LastMoveID, atk.Volatiles.LastMoveName = m.ID, m.Name
			// Sleep Talk's onTry. The sleep-usable flag says a move may be
			// *selected* while asleep; this says it may be used only then. The
			// two are separate rules on the same two moves, and shipping the
			// first without the second gives an always-on random move.
			called, ok := domain.Move{}, false
			if !m.HasFlag("sleep-usable") || atk.Status == StatusSleep {
				called, ok = chooseCalledMove(dex, s, side, m, foeAction, foeMoved, rng)
			}
			if !ok {
				breakMetronomeStreak(atk)
				atk.Volatiles.MoveThisTurnFailed = true
				*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
				return
			}
			calledFrom, m = m, called
			// Gravity gets a second say, on the move that actually resolves.
			// The gate above ran against the slot the controller picked, which
			// is the caller — and canon carries both an onBeforeMove and an
			// onModifyMove for exactly this reason, so that a Gravity refuses
			// the airborne move a Sleep Talk rolled as well as one chosen
			// outright. Upstream ships a case that measures it.
			if gravityBlocksMove(s, m) {
				breakMetronomeStreak(atk)
				atk.Volatiles.MoveThisTurnFailed = true
				*log = append(*log, LogLine{
					Type: "cant", Side: side,
					Text: fmt.Sprintf("%s can't use %s because of gravity!", atk.Name, m.Name),
				})
				return
			}
		}
		if m.HasFlag("two-turn") && moveIdx >= 0 && moveIdx < len(atk.Moves) && !skipChargeTurn(s, side, m, log) {
			// Sky Drop's lift is the one charge turn that reaches out and takes
			// hold of something, so it has refusals of its own and they run
			// before anything is armed. A Power Herb cannot skip it either —
			// canon excludes the move by name — which skipChargeTurn already
			// gets right, since Sky Drop is not one of the moves it names.
			if m.ID == "sky-drop" {
				if skyDropLiftRefused(s, side, m, log) {
					return
				}
				atk.Volatiles.Charging = &ChargingState{MoveIdx: moveIdx}
				startSkyDrop(s, side, moveIdx, log)
				return
			}
			atk.Volatiles.Charging = &ChargingState{MoveIdx: moveIdx}
			*log = append(*log, LogLine{
				Type: "move", Side: side,
				Text: fmt.Sprintf("%s began charging %s!", atk.Name, m.Name),
			})
			return
		}
	}

	// Sky Drop's release. The hold is cleared through a defer for the same
	// reason Bide's is: every way the drop can end early still has to put the
	// target down, and a hold left standing would freeze the other side's turns
	// for the rest of the battle.
	if atk.Volatiles.SkyDrop != nil {
		defer func() { atk.Volatiles.SkyDrop = nil }()
		if !skyDropRelease(s, side, log) {
			return
		}
	}

	// Bide's later turns. Canon expresses the storing turn as a
	// beforeMoveCallback that aborts runMove outright — no PP, no announce, no
	// hit — and the release as an onBeforeMove that fires a synthesized move and
	// then returns false. Both sit above deductPP, which is why neither costs
	// anything: the whole move was paid for on the first turn.
	//
	// The volatile is cleared through a defer rather than inline because every
	// way the release can end early — a Protect, a type immunity, the target
	// fainting to something else first — still has to end the lock. Leaving it
	// set would pin the user's slot for the rest of the battle.
	if atk.Volatiles.Bide != nil {
		release, stored := bideAction(atk)
		if !release {
			*log = append(*log, LogLine{
				Type: "bide", Side: side,
				Text: fmt.Sprintf("%s is storing energy!", atk.Name),
			})
			return
		}
		defer func() { atk.Volatiles.Bide = nil }()
		if stored == 0 {
			// Canon fails loudly on an empty store rather than dealing the
			// one-point floor its damage field would otherwise produce.
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
		*log = append(*log, LogLine{
			Type: "bide", Side: side,
			Text: fmt.Sprintf("%s unleashed its energy!", atk.Name),
		})
	}

	// Mold Breaker: from here until the move is done resolving, the *other*
	// Pokemon's ability is switched off — Showdown's Battle#activeMove
	// .ignoreAbility plus #suppressingAbility, which is a fact about the field
	// rather than a check at each gate. Recording it here rather than testing
	// abilityBreaksMold at every defender-ability site is what gives it the
	// reach it should have had: Shield Dust, Clear Body, Sticky Hold, Damp, and
	// a Levitate holder dragged onto Spikes by the same move all read it now.
	//
	// Saved and restored rather than cleared, because a move can resolve inside
	// another one — Dancer's copy, a Magic Coat bounce, Metronome's roll — and
	// the outer move's suppression has to come back when the inner one ends.
	prevMoldBreaker := s.moldBreaker
	if abilityBreaksMold(atk) {
		s.moldBreaker = atk
	}
	defer func() { s.moldBreaker = prevMoldBreaker }()

	// Assault Vest bars status moves outright. Checked here as well as in
	// LegalActions because a controller that ignores the legal set must not be
	// able to sneak one through — the same belt-and-braces the lock/restrict
	// gate below uses. PP is already spent, matching how a refused move works
	// everywhere else in this function.
	//
	// vestedAtChoice, not the item as it stands now: canon's Assault Vest is a
	// selection-time gate (an onDisableMove), so a vest that arrived after the
	// choice was made cannot retroactively cancel it. ResolveTurn snapshots the
	// answer before anything resolves — see the note there. Reading the live
	// item here is what let a Klutz holder Trick its vest onto a slower foe and
	// cancel the Calm Mind that foe had already committed to.
	if m.Category == domain.CatStatus && vestedAtChoice {
		name := "Assault Vest"
		if it := itemOf(atk); it != nil {
			name = it.Name
		}
		*log = append(*log, LogLine{
			Type: "cant", Side: side,
			Text: fmt.Sprintf("%s cannot use status moves! (%s)", atk.Name, name),
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

	// Fake Out only works as the user's first action after entering the
	// field. The dataset carries the move as priority +3 with a 100% flinch
	// secondary and no restriction whatsoever, and no Go file mentioned it at
	// all — so it was a guaranteed flinch, every turn, for as long as the user
	// stayed in. Both finalists of the agent tournament found it independently
	// on turn 13 of the final, and it is why that final was 3–0: priority
	// ignores Trick Room, so an unrestricted Fake Out is not a nuisance to a
	// speed-inversion team, it is a hard lock. PP is already spent, matching
	// every other refusal in this function.
	if m.ID == "fake-out" && atk.Volatiles.MoveActions > 1 {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		atk.Volatiles.LastMoveID = m.ID
		atk.Volatiles.LastMoveName = m.Name
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
	tickMetronome(atk, m, twoTurnStrike)

	// Destiny Bond lasts until the user's next move and then goes, whatever that
	// move is — canon's onBeforeMove at priority -1. Any move but Destiny Bond
	// itself spends it, which is also what makes a back-to-back use fail:
	// applyDestinyBondVolatile finds the volatile still up.
	if m.ID != "destiny-bond" {
		atk.Volatiles.DestinyBond = false
	}

	// Charge is spent by any Electric move that is not Charge, and by nothing
	// else. Both halves of that were inverted: the clear lived at the tail of
	// the damaging-move path and fired regardless of type, so Air Slash ate the
	// charge, and it sat below the status-move early return, so Thunder Wave
	// could never spend it. The comment at damage.go:213 stated the Gen 8 rule
	// — "cleared after the next damaging move regardless of type" — as if it
	// were current; Gen 9 is upstream's onAfterMove, keyed on the move's type
	// and its id, with no mention of category.
	//
	// Deferred rather than written inline: canon spends it from onAfterMove
	// *and* onMoveAborted, so every way out of this function spends it — and
	// deferring is also what keeps the read in computeDamage on the same move
	// that is about to spend it.
	defer func() {
		if m.Type == "electric" && m.ID != "charge" {
			atk.Volatiles.Charge = false
		}
	}()

	// And it survives only a move that *succeeded*. Canon keys the next tick on
	// pokemon.moveLastTurnResult, so a refusal resets the count exactly the way
	// a miss does — a move stopped by Protect, by an immunity, by Psychic
	// Terrain, or a Fling with nothing to throw all put the holder back to
	// x1.0. There are a dozen ways to leave this function without resolving, so
	// the break is deferred and the two success paths cancel it, rather than
	// each failure having to remember.
	metronomeSucceeded := false
	// notFail records canon's NOT_FAIL: an outcome that stopped the move
	// without being a failure. Protect is the one this engine reaches — its
	// onTryHit returns NOT_FAIL, which sets moveThisTurnResult to *null*, and
	// Stomping Tantrum compares strictly against false.
	//
	// This is where the move-failure record and the Metronome streak part
	// company, and they part company because they are asking different
	// questions. Metronome wants "did this move connect" and a Protect breaks
	// its chain; Stomping Tantrum wants "did this move fail" and a Protect is
	// not a failure — the attack was answered, not botched.
	notFail := false
	defer func() {
		if !metronomeSucceeded {
			breakMetronomeStreak(atk)
		}
		atk.Volatiles.MoveThisTurnFailed = !metronomeSucceeded && !notFail
	}()

	announceMove(atk, side, m, log)
	// Record the move as the user's "last move" right after announce.
	// Disable / Encore inflicted by the foe later in the same turn read
	// this — canonical "your last move" semantics. Cleared on switch-out
	// with the rest of Volatiles.
	// A called move does not overwrite this: canon writes the register from
	// runMove for the move the user chose and never for one that move called,
	// which is why a Disable landed on a Sleep Talk user names Sleep Talk.
	if m.ID != "" && calledFrom.ID == "" {
		atk.Volatiles.LastMoveID = m.ID
		atk.Volatiles.LastMoveName = m.Name
	}
	// The battle's own register is the other question — what did anyone last
	// resolve — and it answers with the called move rather than the caller,
	// because canon writes it from inside useMove where every route arrives.
	// Copycat and Conversion 2 are the readers that need the difference.
	s.LastMoveUsedID = m.ID
	// The type is recorded unconditionally, Struggle included — see
	// Volatiles.LastMoveType for why the two writes disagree about that. Read
	// here rather than after the dynamic-power adjustments below, so a Weather
	// Ball records the type it was declared with rather than the one the sky
	// gave it; nothing in this dataset reads the difference.
	atk.Volatiles.LastMoveType = m.Type

	// Sucker Punch and Upper Hand only land if the target still has a
	// damaging move queued this turn. Both fail against a target that
	// already moved (the user was outsped), one that is switching, one
	// using a status move, or one locked into a recharge turn; Upper Hand
	// additionally requires that queued move to carry positive priority.
	// Placed after announce so the "used X! / But it failed!" pair matches
	// canon; the PP is already spent by choosePP above.
	switch m.ID {
	case "belch":
		// Canon's onTry, which runs inside useMove after deductPP — so the
		// attempt costs the PP, like every other refusal in this function.
		if !atk.AteBerry {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
	case "snore":
		// The other half of sleep-usable, and the same onTry Sleep Talk carries:
		// the flag lets the move be selected while asleep, this makes sleep the
		// only time it can be used. Without it Snore is an unconditional 50-power
		// sound move with a flinch rider on 78 of the 80 species.
		if atk.Status != StatusSleep {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
	case "future-sight":
		// Canon installs the pending hit from an onTry, which short-circuits
		// above every one of the hit steps: no invulnerability check, no
		// Protect, no type immunity and — the one with teeth — no accuracy
		// roll, so a Future Sight can never miss and never draws from the RNG.
		// Placed here, after the announce and after the PP, for the same
		// reason: the attempt costs a use.
		//
		// It reports NOT_FAIL, which is a *success* rather than a failure, so
		// metronomeSucceeded is set. That is not cosmetic — the deferred block
		// above turns it into MoveThisTurnFailed, which Stomping Tantrum reads
		// on the following turn, and upstream ships a case saying a Future
		// Sight must not arm it.
		if !armFutureMove(s, side, m) {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
		*log = append(*log, LogLine{
			Type: "futuremove", Side: side,
			Text: fmt.Sprintf("%s foresaw an attack!", atk.Name),
		})
		metronomeSucceeded = true
		return
	case "bide":
		// First turn of the store. Everything above has had its say — the PP is
		// paid, Disable and Taunt have been consulted, the move has announced
		// itself — and nothing below should run, because upstream ships Bide as
		// a Physical move with basePower 0 and `target: "self"`. This engine
		// aims every damaging move at the foe, so a Bide allowed to fall through
		// would chip the opponent for the formula's one-point floor on the very
		// turn it is meant to be standing still.
		//
		// The later turns never reach here: they are intercepted above, before
		// the announce, because canon aborts them in beforeMoveCallback.
		if atk.Volatiles.Bide == nil {
			startBide(atk, side, moveIdx, log)
			metronomeSucceeded = true
			return
		}
	case "counter", "mirror-coat":
		// Canon's onTry, which tests whether a qualifying attack landed at all
		// — not whether it dealt anything. A hit clamped to zero by Endure
		// still arms the move, which then pays back its floor of one; only a
		// turn with no hit of the right category is a failure. Like Belch above
		// this sits after deductPP, so the attempt costs the PP.
		took := atk.Volatiles.TookPhysicalHit
		if m.ID == "mirror-coat" {
			took = atk.Volatiles.TookSpecialHit
		}
		if !took {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
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

	// Hex / Venoshock double against a statused target; Weather Ball changes
	// type and doubles in weather. All three are basePowerCallback moves
	// upstream, so the dataset ships them flat — see callbackmoves.go.
	m = applyCallbackPower(s, atk, s.Active(1-side), m, calledFrom.ID)

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

	// Fling and Natural Gift take their power (and Natural Gift its type) from
	// the user's item, and fail outright with nothing to throw or a suppressed
	// slot. Canon's onPrepareHit runs before the accuracy roll, so a Fling with
	// an empty slot never rolls and therefore cannot "miss" — and the item is
	// spent right here, before the throw resolves, so nothing downstream can
	// still read it as held. thrown carries the slug to the delivery below.
	//
	// It also runs before the Protect and Psychic-Terrain gates below, which is
	// the ordering canon has and this engine did not: battle-actions.ts fires
	// singleEvent('Try') and runEvent('PrepareHit') above the moveSteps loop,
	// and hitStepTryHitEvent — where Protect lives — is inside it. So the item
	// is thrown, and lost, even when the throw is fully absorbed. Getting that
	// backwards made Fling into a shield-check: press Protect and the Iron Ball
	// stays on the belt.
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

	// Psychic Terrain blocks priority moves aimed at a grounded foe. The
	// move announces but doesn't connect — Showdown emits a "protected"
	// flavor line; we lean on the generic terrain log type so the UI can
	// style it consistently with other terrain events.
	if m.Target != domain.TargetSelf {
		def := s.Active(1 - side)
		if terrainBlocksPriorityAgainst(s.Terrain, &s.PseudoWeather, def, m.Priority) {
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
			notFail = true
			*log = append(*log, LogLine{
				Type: "protect", Side: 1 - side,
				Text: fmt.Sprintf("%s protected itself!", def.Name),
			})
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
	if resolveStatusMoveTypeImmunity(dex, s, side, m, log) {
		return
	}

	// Endeavor refuses a target that is not strictly above the user's HP —
	// there is nothing to drag down to. Canon states it as an onTryImmunity
	// (`return pokemon.hp < target.hp`), so it belongs here beside
	// Synchronoise's rather than with the "But it failed!" refusals: it is
	// announced as an immunity and it happens *above* the accuracy roll, so an
	// Endeavor that had nothing to do never even rolls to hit. The comparison
	// is strict — equal HP is a refusal.
	if m.ID == "endeavor" && atk.HP >= s.Active(1-side).HP {
		*log = append(*log, LogLine{
			Type: "immune", Side: side,
			Text: fmt.Sprintf("It doesn't affect %s...", s.Active(1-side).Name),
		})
		applyMissOrEndEffects(s, side, m, log)
		return
	}

	// Synchronoise only touches a target that shares a type with the user
	// (canon's onTryImmunity). Sits with the other immunity gates, above the
	// accuracy roll, because upstream's hitStepTryImmunity does.
	if m.ID == "synchronoise" && !sharesAType(atk, s.Active(1-side)) {
		*log = append(*log, LogLine{
			Type: "immune", Side: side,
			Text: fmt.Sprintf("It doesn't affect %s...", s.Active(1-side).Name),
		})
		applyMissOrEndEffects(s, side, m, log)
		return
	}

	if landed, missed := resolveAccuracy(s, side, m, rng, log); !landed {
		// A whiff breaks the Metronome streak through the defer above, along
		// with every other way this move can fail to resolve.
		// Blunder Policy answers a genuine miss only — a move refused by
		// Soundproof or Safety Goggles never rolled, so there was no blunder.
		if missed {
			applyItemOnMoveMissed(s, side, m, log)
		}
		applyMissOrEndEffects(s, side, m, log)
		return
	}

	if m.Category == domain.CatStatus {
		resolved := applyStatusMove(dex, s, side, m, rng, log)
		metronomeSucceeded = resolved
		// Memento. Canon tests `damage[i] !== false` — did the hit step reach
		// the target — and it tests it *before* folding in whether the effect
		// accomplished anything, so the sacrifice is paid for connecting and
		// not for succeeding. `resolved` is that question here: the miss, the
		// Protect and the type immunity have all returned above, and the one
		// remaining way a status move reaches this line without connecting is
		// applyEffectFields refusing it through a Substitute, which reports
		// itself by returning false. So a Memento walled by a doll costs
		// nothing and a Memento into Clear Body still kills its user.
		applySelfDestructIfHit(s, side, m, resolved, log)
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
		// A status move is the commonest way to drop a foe's stats — Growl,
		// Charm, Tickle — so the pack has to be drained on this path as well.
		// Eject Button and Red Card are not: both are gated on a damaging move
		// upstream (`move.category !== 'Status'`).
		fireEjectPacks(s, rng, log)
		return
	}

	// Multi-strike moves (Bullet Seed, Bone Rush, Bonemerang, Triple Axel, ...)
	// loop the strike phase. The accuracy roll above gates the whole sequence;
	// each hit then rolls its own damage spread, crit, and secondary effects.
	// The loop stops early if either side faints (so a multi-hit move can't
	// continue against a 0-HP target, and Rough Skin can cut it short).
	// Brick Break shatters the target side's screens before it hits: canon puts
	// the removal in the move's own onTryHit, which spreadMoveHit fires above
	// the substitute redirect and above getSpreadDamage. So the screens come
	// down through a Substitute, and before the damage is computed — the hit is
	// not halved by the Reflect it is breaking. Not on a miss, not through
	// Protect and not against a type the move cannot touch: all three stop the
	// move upstream of that callback, which is why this asks the type chart
	// rather than waiting for the strike to land.
	if m.ID == "brick-break" {
		if eff, _ := typeEffectiveness(dex, atk, s.Active(1-side), m, &s.PseudoWeather); eff != 0 {
			if clearScreensOnSide(s, 1-side) {
				*log = append(*log, LogLine{
					Type: "hazard", Side: 1 - side,
					Text: fmt.Sprintf("%s shattered the screens!", atk.Name),
				})
			}
		}
	}

	planned := 1
	if m.IsMultihit() {
		planned = multihitCount(m, atk, rng)
	}
	// subAte stays true only while every strike so far has been eaten by a
	// Substitute. A multi-hit move that breaks the doll and then connects has
	// reached the target, which is what the item-theft moves gate on.
	hits, totalDmg, subAte := 0, 0, true
	sawCrit := false
	// Snapshot before the first strike: Burning Jealousy burns a target whose
	// stats went up *before* the attack, and a Weakness Policy that fires off
	// this very hit is too late to be burned for. Canon gets that ordering for
	// free — the secondary runs in runMoveEffects, ahead of the DamagingHit
	// event the policy hangs off — where this engine applies the defender's
	// reactive items inside dealDamage, before the post-hit block below.
	targetWasRaised := s.Active(1 - side).Volatiles.StatsRaisedThisTurn
	for i := 0; i < planned; i++ {
		if s.Active(1-side).HP <= 0 || atk.HP <= 0 {
			break
		}
		dmg, ok, absorbedBySub, wasCrit := dealDamage(dex, s, side, m, rng, log)
		sawCrit = sawCrit || wasCrit
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
				applyMissOrEndEffects(s, side, m, log)
				return
			}
			break
		}
		applyDamageEffects(s, side, m, dmg, rng, log)
		hits++
	}
	metronomeSucceeded = hits > 0
	// Struggle's recoil, which is its own rule rather than the shared
	// self-effect block — see struggleMove. Canon charges it on a connecting
	// hit, from battle-actions.ts's moveHit tail.
	if hits > 0 && isStruggle(m) {
		applyStruggleRecoil(atk, side, log)
	}
	if m.IsMultihit() && hits > 0 {
		*log = append(*log, LogLine{
			Type: "info", Side: side,
			Text: fmt.Sprintf("Hit %d time(s)!", hits),
		})
	}

	// Rapid Spin clears the user's side hazards on a successful hit. The
	// Speed +1 self-boost is already wired via the upstream secondary; only
	// the hazard sweep needs the hand-coded hook (Showdown encodes it in JS).
	//
	// Two gates beyond "the move connected", and the comment here used to argue
	// against the first of them: it said the sweep runs before faint resolution
	// so a contact-faint counter like Rough Skin "doesn't suppress the spin
	// sweep". That is precisely backwards. Canon gates every removeSideCondition
	// in rapidspin's callbacks on `pokemon.hp`, so a spinner killed by a Rocky
	// Helmet on the way in clears nothing — which is the difference between a
	// suicide spin being a real cost and being free. The faint window is still
	// open here (see the note further down), so the test is on HP rather than on
	// the flag.
	//
	// And Sheer Force takes the sweep with the secondary. Upstream's Rapid Spin
	// carries its Speed boost as `secondary: {chance: 100, self: {...}}`, and
	// Sheer Force's onModifyMove deletes move.secondaries and sets
	// move.hasSheerForce — which is exactly what both of rapidspin's callbacks
	// check before touching anything. So the ability trades the whole rider
	// away, hazard clear included. Note canon only sets the flag when the move
	// *has* secondaries, so a secondary-less move still does its thing; that is
	// why this asks about m.Secondaries rather than about the ability alone.
	sheerForced := abilityBlocksOwnSecondaries(atk) && len(m.Secondaries) > 0
	if hits > 0 && m.ID == "rapid-spin" && atk.HP > 0 && !sheerForced {
		applyRapidSpin(s, side, log)
	}

	// Ice Spinner sweeps the terrain on a connecting hit, with the same "the
	// user is still standing" gate Rapid Spin has: canon runs the wipe from
	// onAfterHit, which spreadMoveHit fires only `if (pokemon.hp)` and only
	// after runEvent('DamagingHit') — so a Rocky Helmet that kills the spinner
	// on the way in leaves the terrain up.
	if hits > 0 && m.ID == "ice-spinner" && atk.HP > 0 {
		clearTerrain(s, log)
	}

	// Clear Smog wipes the target's stat changes on a connecting hit, and Tri
	// Attack rolls its 20% burn / freeze / paralysis. Both are onHit callbacks
	// upstream and neither survives the static dump — see callbackmoves.go.
	//
	// Clear Smog is gated on the doll not having eaten the strike as well as on
	// the move connecting. A hit the Substitute absorbed never reached the
	// holder, so it cannot wipe the holder's boosts — canon's clearsmog onHit
	// is reached through moveHit on the target, which a doll short-circuits.
	// The flag is computed two statements above and the item-theft moves
	// already consult it; only this branch did not.
	if hits > 0 {
		switch m.ID {
		case "clear-smog":
			if !subAte {
				applyClearSmog(s, side, log)
			}
		case "tri-attack":
			applyTriAttack(s, side, rng, log)
		case "wake-up-slap":
			if !subAte {
				cureStatusIf(s.Active(1-side), 1-side, StatusSleep, log)
			}
		case "smelling-salts":
			// Upstream's onHit, not a secondary: Shield Dust and a Covert Cloak
			// do not stop it. The doll does, because a hit it ate never reached
			// the holder.
			if !subAte {
				cureStatusIf(s.Active(1-side), 1-side, StatusParalysis, log)
			}
		case "sparkling-aria":
			// Upstream hangs this off onAfterMove and bails when the user
			// fainted, so the user has to still be standing.
			if !subAte && atk.HP > 0 {
				cureStatusIf(s.Active(1-side), 1-side, StatusBurn, log)
			}
		case "burning-jealousy":
			// Burns only a target whose stats went *up* this turn — the
			// punish-the-setup move. Upstream shapes it as a 100%-chance
			// secondary whose onHit tests the flag, so it is refused by the
			// same things that refuse any secondary and is gated on the doll
			// like Clear Smog: a hit the Substitute ate never reached the
			// holder to burn it.
			if !subAte {
				applyBurningJealousy(s, side, targetWasRaised, rng, log)
			}
		}
	}

	// Anger Point, and any other reaction to taking a critical hit, fires here
	// rather than inside dealDamage. Canon's order is the move's own
	// singleEvent('Hit') and *then* runEvent('Hit') — the ability — so on a
	// Clear Smog crit the wipe happens first and the +6 Attack survives it.
	// Firing it from dealDamage put the boost before the wipe and produced the
	// two lines back to back with nothing to show for them:
	//
	//	Primeape's Anger Point maxed its Attack!
	//	Primeape's stat changes were removed!
	//
	// Gated on the doll for the same reason Clear Smog is: a crit the
	// Substitute ate never reached the holder.
	if sawCrit && !subAte {
		applyOnCrit(s, 1-side, log)
	}

	// Knock Off / Thief / Covet take the target's item once the hit has landed,
	// and a thrown berry reaches its target. Both gated on hits > 0 and on a
	// doll not having eaten the strike: canon runs these off the move connecting
	// with the target itself, and a berry thrown into a Substitute is wasted.
	if hits > 0 {
		applyItemMoveAfterHit(s, side, m, subAte, rng, log)
	}
	applyItemMoveDelivery(s, side, m, thrown, hits > 0 && !subAte, rng, log)
	// The pinch check dealDamage held back for this move's target (see the skip
	// there). It lands here, after the item-taking hooks and still inside the
	// faint window, which is the closest structural match to canon's Update
	// following singleEvent('AfterHit'). Harmless for every other move:
	// applyItemHPTrigger no-ops when the holder is above its threshold or the
	// slot is already empty.
	if deferDefenderPinchCheck(m) {
		applyItemHPTriggers(s, rng, log)
	}

	// Consume the one-shot aim buff: Laser Focus arms the next attempt's
	// guaranteed crit and clears here after the move resolves whether or not it
	// hit (canonical: spent on the attempt, not the success).
	atk.Volatiles.LaserFocus = false

	if m.HasFlag("recharge") {
		atk.Volatiles.MustRecharge = true
	}
	if m.HasFlag("selfdestruct") {
		applySelfDestruct(atk, side, log)
	}
	// Final Gambit, whose damage was the user's whole HP bar and whose cost is
	// the same number. `hits > 0` is the damaging-move spelling of canon's
	// `damage[i] !== false`: a Ghost walls the Fighting-type hit and the user
	// walks away, while a Substitute eats it and the user still dies.
	//
	// Above the faint check below rather than calling faint() here, so the KO
	// resolves in the ordinary place and Life Orb, Shell Bell and Throat Spray
	// all see the user at zero — which is where canon leaves it too, since the
	// damageCallback zeroes the user before the target is ever touched.
	applySelfDestructIfHit(s, side, m, hits > 0, log)

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

	// Eject Button and Red Card answer the hit, and they go before the
	// attacker's own pivot because a button that fires cancels it: upstream
	// clears `source.switchFlag` from inside the button's handler, so a U-turn
	// that pops one leaves the attacker standing and the holder is the one
	// that leaves.
	ejected := applyHitReactiveSwitchItems(s, side, m, hits > 0, rng, log)

	// Damage-variant self-switch (U-turn, Volt Switch, Flip Turn) runs after
	// faint resolution so a contact-hit-reactive faint (Rocky Helmet, Rough
	// Skin) suppresses the switch the way it does in canon.
	if !ejected {
		applySelfSwitch(s, side, m, action.SwitchTarget, rng, log)
	}

	// An Eject Pack armed by anything this move did — a secondary's drop, a
	// self-inflicted one, a Defiant-style reactor — spends itself here. This is
	// canon's onAnyAfterMove, one of the four places upstream drains the flag.
	fireEjectPacks(s, rng, log)

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
	// The jump kicks charge their user for missing. Canon fires this from
	// onMoveFail, which covers every way a hit step can filter the target:
	// semi-invulnerability, Protect, a type immunity, a powder refusal, and an
	// ordinary miss. It does *not* fire when there was no target at all.
	if hasCrashDamage(m) {
		applyCrashDamage(s.Active(side), side, log)
	}
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
// Normal type immunity for a *damaging* move (Ghost vs Horn Drill, Flying vs
// Fissure, Levitate vs Fissure) still runs inside computeDamage post-accuracy.
// Status moves are gated separately and pre-accuracy, by
// resolveStatusMoveTypeImmunity below.
func resolveOHKOImmunity(s *BattleState, side int, m domain.Move, log *[]LogLine) bool {
	def := s.Active(1 - side)
	if m.OHKO != "any" && isType(def, domain.Type(m.OHKO)) {
		*log = append(*log, LogLine{
			Type: "immune", Side: side,
			Text: fmt.Sprintf("It doesn't affect %s...", def.Name),
		})
		return true
	}
	if a := abilityOf(def); a != nil && a.Kind == AbilitySturdy && !abilityBreaksMoldAgainst(s.Active(side), def) {
		revealAbility(def)
		*log = append(*log, LogLine{
			Type: "ability", Side: 1 - side,
			Text: fmt.Sprintf("%s is unaffected by the one-hit KO! (Sturdy)", def.Name),
		})
		return true
	}
	return false
}

// fieldWideSoundMoves are the sound moves whose effect is the whole field
// rather than one target, so a single Soundproof holder must not cancel them.
// Upstream expresses this through the move's target (`all`) and a per-target
// onTryHit; this engine collapses every target to foe or self, so the
// distinction has to be named. One member today.
var fieldWideSoundMoves = map[string]bool{"perish-song": true}

// sunSkipsChargeMoves are the two-turn moves that gather their energy from
// sunlight and therefore need no charge turn while the sun is up. Keyed by
// move ID because the condition is the move's flavor, not anything the
// dataset carries — Showdown encodes it as a JS callback, so there is nothing
// to sync.
var sunSkipsChargeMoves = map[string]bool{
	"solar-beam":  true,
	"solar-blade": true,
}

// skipChargeTurn reports whether a two-turn move resolves immediately instead
// of arming its charge, and consumes anything that was spent to make that
// happen. Called at the one point the charge is about to be set.
//
// Two escape hatches, matching canon:
//
//   - Sunlight, for Solar Beam and Solar Blade. Read through weatherFor, so a
//     Utility Umbrella holder is standing out of the sun and still charges.
//     Free — nothing is consumed.
//   - Power Herb, for any two-turn move. Consumed, and only when it is what
//     did the work: the sun check runs first so a Solar Beam under the sun
//     doesn't spend a herb it didn't need.
//
// Without these, the Drought-Ninetales sun archetype has no payoff move and
// Power Herb has nothing to do.
func skipChargeTurn(s *BattleState, side int, m domain.Move, log *[]LogLine) bool {
	atk := s.Active(side)
	if sunSkipsChargeMoves[m.ID] {
		if w := weatherFor(atk, effectiveWeather(s)); w != nil && w.Kind == WeatherSun {
			*log = append(*log, LogLine{
				Type: "move", Side: side,
				Text: fmt.Sprintf("%s took in sunlight!", atk.Name),
			})
			return true
		}
	}
	if it := itemOf(atk); it != nil && it.Kind == ItemPowerHerb {
		consumeItemAnnounced(atk, side, it, log)
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
//
// Order matters and is canon's: the refusals come first, then the auto-hit
// paths, then the roll. An immunity is not an accuracy problem, so nothing
// that makes a move unmissable — the bypass-acc flag, a status move's zero
// accuracy, No Guard, Telekinesis — may reach past one. Running the auto-hit
// returns first is how Roar, Confide and Disarming Voice were landing on
// Soundproof holders: all three are sound moves that skip the roll.
func resolveAccuracy(s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) (landed, missed bool) {
	atk := s.Active(side)
	def := s.Active(1 - side)

	// Powder moves bounce off Grass-types, Overcoat and Safety Goggles.
	if reason, refused := powderRefusedBy(atk, def, m); refused {
		text := fmt.Sprintf("It doesn't affect %s...", def.Name)
		if reason != "" {
			text = fmt.Sprintf("It doesn't affect %s... (%s)", def.Name, reason)
		}
		*log = append(*log, LogLine{Type: "immune", Side: side, Text: text})
		return false, false // refused, not missed
	}
	// Soundproof: sound-flagged moves don't affect the holder at all. We
	// log "doesn't affect" rather than "missed" to match canon.
	//
	// Refusing here refuses the whole *move*, which is right for a move aimed
	// at one target and wrong for a field-wide one. Soundproof is a per-target
	// immunity in canon (an onTryHit on the holder), not a veto: a field-wide
	// sound move still starts on everything that heard it, including the user's
	// own side. Perish Song is the one such move in this dataset, and it is
	// exempted here so its per-target check can live where the effect does, in
	// applyPerishSong. Getting this wrong meant a Soundproof foe canceled the
	// song for both sides, user included.
	if m.HasFlag("sound") && !fieldWideSoundMoves[m.ID] {
		if a := abilityOf(def); a != nil && a.Kind == "soundproof" && !abilityBreaksMoldAgainst(atk, def) {
			*log = append(*log, LogLine{
				Type: "immune", Side: side,
				Text: fmt.Sprintf("It doesn't affect %s... (Soundproof)", def.Name),
			})
			return false, false // refused, not missed
		}
	}

	if m.HasFlag("bypass-acc") || m.Accuracy == 0 {
		return true, false
	}
	// Toxic never misses when a Poison-type uses it. Canon hard-codes the move
	// id in the same place, right beside move.alwaysHit
	// (battle-actions.ts: `move.id === 'toxic' && this.battle.gen >= 8 &&
	// pokemon.hasType('Poison')`), and the placement is the point: landing here,
	// above the modifier chain, is what makes it dodge Wonder Skin's x0.5 rather
	// than merely improving the odds. Gen 8+, so current for this engine.
	if m.ID == "toxic" && isType(atk, "poison") {
		return true, false
	}
	// No Guard on either combatant makes the move land unconditionally —
	// the holder's own moves never miss and moves aimed at it always hit.
	if abilityNoGuard(atk) || abilityNoGuard(def) {
		return true, false
	}
	// Lock-On / Mind Reader: the user took aim last turn and this move cannot
	// miss the thing it aimed at.
	//
	// Placed last in the auto-hit block, which puts it exactly where canon's
	// Accuracy event sits — after every modifier and after the OHKO branch's
	// own 30, so it beats evasion stages, Bright Powder, Sand Veil and an OHKO
	// move's base accuracy alike; canon returns `true` from onSourceAccuracy
	// and that overwrites the number rather than improving it.
	//
	// And placed *below* the two refusals above, because the rule those two
	// share is that an immunity is not an accuracy problem: a locked-on powder
	// move still bounces off Safety Goggles, a locked-on Boomburst is still
	// refused by Soundproof. Canon agrees — its immunity step runs above its
	// accuracy step.
	//
	// Canon's other half, onSourceInvulnerability, has no counterpart here
	// because this engine has no semi-invulnerability to beat: a Pokemon
	// mid-Fly is hittable by everything (see cancelAirborneCharge, which says
	// so). If that ever changes, the invulnerability gate has to go *above*
	// this one for Lock-On to beat it, mirroring canon's step 0 / step 4 split.
	if lockedOn(s, atk, def) {
		return true, false
	}
	// Telekinesis on the target makes every move land — the lifted
	// holder is too easy a target to miss. Canceled by Smack Down
	// (which clears the Telekinesis volatile on apply).
	if telekinesisAutoHits(def) {
		return true, false
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
	// The other half of Gravity — grounding Flying-types and Levitate
	// holders for the duration — is in groundedness (terrain.go).
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
func dealDamage(dex *domain.Dex, s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) (dmg int, ok, hitSub, crit bool) {
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
		return 0, false, false, false
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
		return absorbed, true, true, res.Crit
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
	// Sturdy: a full-HP holder survives an otherwise-lethal hit at 1 HP. It
	// sits between Endure and the sash because that is the priority order canon
	// resolves the three in — one onDamage event, Endure at -10, Sturdy at -30,
	// Focus Sash at -40 — and because Endure clamping first is what makes
	// Sturdy's own `dmg >= HP` test false, so the announcement is Endure's.
	//
	// This used to be clamped at the end of computeDamage, upstream of the whole
	// chain, which meant Endure never saw a lethal figure to clamp: the Pokemon
	// survived either way and reported Sturdy where canon reports Endure.
	sturdySaved := false
	// abilitySuppressed rather than abilityBreaksMold: this is a defender-side
	// question, and the file that owns it says so — "predicates that decide a
	// defender-side question take the state now and ask here". Reading the
	// attacker's ability directly missed the two things the state-aware form
	// knows. Ability Shield is one (canon's suppressingAbility ends in
	// `&& !target?.hasItem('Ability Shield')`, and a shielded Sturdy holder
	// must still survive a Mold Breaker hit). A mold breaker attacking itself
	// is the other: canon exempts `this.activePokemon !== target` from gen 8
	// on, so a Mold Breaker holder does not break its own Sturdy.
	if !enduredHit && !abilitySuppressed(s, def) {
		if capped, fired := abilitySurviveOHKO(def, dmg); fired {
			dmg = capped
			sturdySaved = true
		}
	}
	// Focus Sash: a full-HP holder survives an otherwise-lethal hit at 1 HP,
	// then the sash is consumed. Endure and Sturdy both take precedence — each
	// has already clamped and neither is consumed, so there is no reason to burn
	// the sash. Saves from OHKO moves too — their dmg was set to def.HP above,
	// so the clamp still fires.
	sashSaved := false
	if !enduredHit && !sturdySaved {
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
		revealItem(def)
		*log = append(*log, LogLine{
			Type: "item", Side: 1 - side,
			Text: fmt.Sprintf("The %s weakened the damage to %s!", berry.Name, def.Name),
		})
		consumeItem(def)
	}
	hurt(def, dmg)
	// The reactive register, written here and not a line earlier because here is
	// below the Substitute redirect: a doll-eaten hit never reaches this
	// statement, and canon likewise never fires DamagingHit for one. Counter off
	// a Substitute is the classic wrong answer and this placement is the whole
	// of the fix.
	//
	// Recorded on a connecting hit whether or not it dealt anything, and
	// assigned rather than accumulated — see ReactiveDamage for both reasons.
	// Every other route into HP loss (recoil, residuals, hazards, a confusion
	// self-hit) goes through hurt() directly and is invisible here, which is
	// also canon: DamagingHit fires from the move path alone.
	switch m.Category {
	case domain.CatPhysical:
		def.Volatiles.ReactivePhysical, def.Volatiles.TookPhysicalHit = 2*dmg, true
	case domain.CatSpecial:
		def.Volatiles.ReactiveSpecial, def.Volatiles.TookSpecialHit = 2*dmg, true
	}
	// Bide soaks up everything instead, of either category, and adds rather than
	// replaces. Canon's handler sits at onDamagePriority -101 — dead last — so
	// it counts the figure that actually landed.
	if bd := def.Volatiles.Bide; bd != nil {
		bd.Damage += dmg
	}
	if dmg > 0 {
		// Flag the hit for the counter-punch moves that resolve later this
		// turn: Revenge / Avalanche read it for their ×2 BP, Assurance for
		// the same, and Focus Punch for its loss-of-focus fail. Only direct
		// move damage counts (confusion self-hits and recoil take other paths).
		def.Volatiles.DamagedThisTurn = true
		// Rage Fist's counter, which is the same event measured over the whole
		// battle rather than the turn — and so lives on the Pokémon, not on its
		// volatiles. Counted per connecting strike, so a multi-hit move raises
		// it once per hit exactly as canon does.
		def.TimesAttacked++
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
		revealItem(def)
		*log = append(*log, LogLine{
			Type: "item", Side: 1 - side,
			Text: fmt.Sprintf("%s hung on with its %s!", def.Name, saver.Name),
		})
		if saver.SurviveOHKO != nil {
			consumeItem(def)
		}
	}
	if m.OHKO != "" && !enduredHit && !sturdySaved && !sashSaved {
		*log = append(*log, LogLine{Type: "info", Side: side, Text: "It's a one-hit KO!"})
	} else if m.OHKO == "" {
		if res.Crit {
			*log = append(*log, LogLine{Type: "crit", Side: side, Text: "A critical hit!"})
			// Anger Point is deliberately NOT fired here. It is the defender's
			// reaction to the crit — canon's runEvent('Hit') — and canon runs
			// the *move's* own singleEvent('Hit') first, so a Clear Smog crit
			// wipes the stat changes and Anger Point's +6 lands after rather
			// than being wiped by it. executeMove fires it past the move
			// callbacks for that reason; see the applyOnCrit call there.
		}
		if res.Effectiveness > 1 {
			*log = append(*log, LogLine{Type: "effective", Side: side, Text: "It's super effective!"})
		} else if res.Effectiveness < 1 {
			*log = append(*log, LogLine{Type: "resisted", Side: side, Text: "It's not very effective..."})
		}
		if sturdySaved {
			revealAbility(def)
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
	// exactly as it does in canon — upstream's eachEvent('Update') sits inside
	// hitStepMoveHitLoop (battle-actions.ts:967), not after it.
	//
	// The defender is held back for the moves that can still take its item.
	// Canon's onAfterHit runs before that Update, so a Knock Off must find the
	// belt it is about to empty; see deferDefenderPinchCheck. The deferred
	// check runs from executeMove once applyItemMoveAfterHit has had its turn.
	skip := -1
	if deferDefenderPinchCheck(m) {
		skip = 1 - side
	}
	applyItemHPTriggersExcept(s, skip, rng, log)
	return dmg, true, false, res.Crit
}

// canAct applies pre-move status and volatile checks and reports whether the
// Pokémon moves. Order: flinch (one-shot, consumed) → confusion (may self-hit
// and preempt) → attract (50% immobilize) → non-volatile status (freeze /
// sleep / para).
func canAct(p *Pokemon, side int, m domain.Move, rng *RNG, log *[]LogLine) bool {
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
		// Snore and Sleep Talk are the two moves upstream marks sleepUsable,
		// and the flag is the whole of their rule. Note the order: the counter
		// has already ticked and the wake check has already run, and the "fast
		// asleep" line is emitted either way — canon emits its own `cant` line
		// unconditionally too, and only then lets a sleepUsable move through.
		return m.HasFlag("sleep-usable")
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
	hurt(p, dmg)
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
		endBattle(s, winnerOfAMutualWipe(*log), log)
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

// winnerOfAMutualWipe decides a battle in which both sides ran out of Pokemon
// on the same turn. Gen 5 onward settles it by faint order — the side whose
// last Pokemon faints *first* loses — and this engine used to score it an
// unconditional draw.
//
// The order is read out of this turn's log rather than kept as state, because
// the log already has it: faint() appends one line per faint in the order the
// faints happen, and a mutual wipe is necessarily same-turn (a side emptying on
// its own ends the battle there and then). The side whose last faint line comes
// earlier emptied earlier and loses.
//
// Where that order comes from matters as much as the rule. The residual phase
// walks by Speed (see speedOrder), so a Perish Song mirror kills the faster
// Pokemon first and the *slower* side wins — and on a genuine Speed tie the
// order is a coin flip from the battle's own RNG, which is upstream's answer
// too.
//
// A draw is still reachable, and still has to be handled downstream: the turn
// cap settles an exact HP tie that way. What is no longer reachable is a
// mutual wipe scoring 0.5 — see docs/battle-state.md.
func winnerOfAMutualWipe(log []LogLine) int {
	last := [2]int{-1, -1}
	for i, l := range log {
		if l.Type == "faint" && l.Side >= 0 && l.Side < 2 {
			last[l.Side] = i
		}
	}
	switch {
	case last[0] < 0 || last[1] < 0:
		// One side's wipe is not in this turn's log at all, which should not
		// happen — but guessing a winner from nothing would be worse than
		// admitting there is no order to appeal to.
		return 2
	case last[0] < last[1]:
		return 1
	default:
		return 0
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

// resolveStatusMoveTypeImmunity refuses a foe-target status move the defender's
// type is immune to, and reports whether it did. Sits between the OHKO gate and
// the accuracy roll, which is canon's step order: invulnerability → TryHit →
// **type immunity** → move-specific immunities → accuracy
// (ps/sim/battle-actions.ts:550-570). Landing above the roll is also what makes
// Blunder Policy behave — an immunity is not a miss.
//
// The rule reads backwards from the intuition, so it is worth stating plainly:
// **status moves ignore type immunity by default.** Upstream resolves
// Move#ignoreImmunity to `category === 'Status'` when the move does not say
// otherwise (ps/sim/dex-moves.ts:501), and runImmunity returns true immediately
// on that flag (ps/sim/pokemon.ts:2243). That is why Glare paralyzes a Ghost,
// Confuse Ray confuses a Normal-type and Sand Attack drops a Levitate holder's
// accuracy — all of which this engine already got right, and all of which a
// blanket "status moves respect the type chart" gate would have broken.
//
// Thunder Wave is the single move in gen 9 that opts back in
// (`ignoreImmunity: false`, ps/data/moves.ts:19595), and correspondingly it is
// the one status move of the 167 in this dataset without the ignore-immunity
// flag. So this gate is written for exactly one move today. It is written
// against the flag rather than against the move ID because the flag is what
// canon consults, and because that is the difference between a rule and a
// special case.
//
// Reusing typeEffectiveness rather than reaching for the raw chart is
// deliberate: it already carries Foresight / Miracle Eye / Ring Target lifts,
// Roost, Mold Breaker and the ability and item type overrides. That last one
// matters — Volt Absorb, Lightning Rod and Motor Drive refuse Thunder Wave in
// canon, since their onTryHit keys on the move's type and not on its category.
func resolveStatusMoveTypeImmunity(dex *domain.Dex, s *BattleState, side int, m domain.Move, log *[]LogLine) bool {
	if m.Category != domain.CatStatus || m.Target == domain.TargetSelf || m.HasFlag("ignore-immunity") {
		return false
	}
	def := s.Active(1 - side)
	if def == nil || def.Fainted {
		return false
	}
	if eff, _ := typeEffectiveness(dex, s.Active(side), def, m, &s.PseudoWeather); eff != 0 {
		return false
	}
	*log = append(*log, LogLine{
		Type: "immune", Side: 1 - side,
		Text: fmt.Sprintf("It doesn't affect %s...", def.Name),
	})
	return true
}

// applySelfDestructIfHit is Showdown's `selfdestruct: 'ifHit'`: the user pays
// its life for a move that reached its target, whether or not the move then
// accomplished anything. Memento and Final Gambit are the two, and connected
// carries each caller's answer to canon's `damage[i] !== false`.
//
// The difference from applySelfDestruct — the `always` sibling — is entirely in
// when it is asked. Explosion faints its user above the hit steps, so a miss and
// a type immunity still detonate it and Damp gets to refuse it by name; these
// two are checked from inside the hit, so neither happens on a move that never
// landed. Sharing the "selfdestruct" flag between them would have made Final
// Gambit suicide into a Ghost and made Memento free to Damp.
//
// HP is zeroed rather than fainted so the caller's own faint check runs, keeping
// both moves inside the faint window the rest of executeMove is written against.
func applySelfDestructIfHit(s *BattleState, side int, m domain.Move, connected bool, log *[]LogLine) {
	if !connected || !m.HasFlag("selfdestruct-if-hit") {
		return
	}
	atk := s.Active(side)
	if atk == nil || atk.HP <= 0 {
		return
	}
	atk.HP = 0
	if m.Category == domain.CatStatus {
		// The status path has no faint check of its own — applyHealingWish's
		// sacrifice is the precedent — so this one faints inline.
		faint(atk, side, log)
	}
}

// pendingMove reports the move a Pokémon is about to attempt, without spending
// any PP on the question. A charge, a rampage or a Bide in flight overrides the
// submitted slot, exactly as the resolution switch does further down; anything
// else is the slot the controller picked.
//
// It exists because canAct has to run before that switch — a Pokémon that
// cannot act never reaches it — and yet needs to know what the move is, since
// Snore and Sleep Talk are usable while asleep and nothing else is.
func pendingMove(dex *domain.Dex, p *Pokemon, moveIdx int) domain.Move {
	forced := -1
	switch {
	case p.Volatiles.Charging != nil:
		forced = p.Volatiles.Charging.MoveIdx
	case p.Volatiles.LockedMove != nil:
		forced = p.Volatiles.LockedMove.MoveIdx
	case p.Volatiles.Bide != nil:
		forced = p.Volatiles.Bide.MoveIdx
	}
	if forced >= 0 && forced < len(p.Moves) {
		return dex.Moves[p.Moves[forced].MoveID]
	}
	return foeSelectedMove(dex, p, moveIdx)
}
