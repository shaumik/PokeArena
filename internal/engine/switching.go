package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// doSwitch brings in a teammate. Stat stages and volatiles reset on both the
// outgoing and incoming Pokémon. The Sleep counter on the outgoing Pokémon
// resets too (Gen 5+ semantics — see docs/battle-state.md).
func doSwitch(s *BattleState, side, idx int, rng *RNG, log *[]LogLine) {
	doSwitchWithCarry(s, side, idx, nil, rng, log)
}

// batonCarry is the outgoing Pokémon's state as Baton Pass hands it to the
// incoming: the stat stages plus the volatile bag, already scrubbed of
// everything that must not travel.
//
// The shape here is deliberately the *inverse* of an allow-list. Canon does
// not enumerate what Baton Pass passes — Pokemon#copyVolatileFrom
// (ps/sim/pokemon.ts) assigns `boosts` wholesale and then loops every volatile
// the passer holds, skipping only the ones whose condition carries
// `noCopy: true`. An allow-list gets the answer wrong every time a volatile is
// added and nobody remembers to widen it, which is exactly how this engine
// ended up passing three things out of a canon set of twenty-odd: Focus
// Energy, Leech Seed and Ingrain were all silently dropped, and the last of
// those is what the upstream Ingrain case measures.
//
// So newBatonCarry copies the whole struct and nulls a deny-list. See its
// comment for the field-by-field reasoning.
type batonCarry struct {
	Stages    Stages
	Volatiles Volatiles
}

// newBatonCarry snapshots out's stages and volatiles for a Baton Pass,
// dropping the state that does not travel and deep-copying the pointers that
// do. It reads out but never writes it — doSwitchWithCarry wipes the passer
// separately, the same way canon calls clearVolatile on it after the copy.
//
// Two categories come out. The first is canon's `noCopy` set, which for the
// volatiles this engine models is: Attract, Defense Curl, Destiny Bond,
// Disable, Encore, Flash Fire, Foresight, Imprison, Minimize, Miracle Eye,
// Nightmare, Smack Down, Stockpile, Torment, Trapped, Yawn and the Choice
// lock. Each of those is either a debuff the target earned (Disable, Torment,
// Trapped) or a marker whose meaning is tied to the body that carries it
// (Flash Fire's charge, Minimize's size), and canon flags them one by one in
// data/moves.ts, data/conditions.ts and data/abilities.ts.
//
// The second is this engine's turn-scheduling state, which has no canon
// counterpart because canon does not keep it in the volatile bag at all —
// Showdown reads it off Pokemon fields (`moveThisTurn`, `hurtThisTurn`,
// `activeMoveActions`, `lastMove`) that clearVolatile resets on both sides of
// a switch. Passing any of it would let a Baton Pass launder the turn: a
// receiver that arrives with DamagedThisTurn set hands the foe a doubled
// Avalanche it never earned, one that arrives with MoveActions set loses Fake
// Out, and one that arrives with MovedThisTurn set would fight the flag
// doSwitchWithCarry sets immediately afterwards.
//
// Everything else travels, because canon says so: Confusion, Substitute,
// Leech Seed, Aqua Ring, Ingrain, Perish Song, Curse, Focus Energy, Laser
// Focus, Charge, Taunt, Embargo, Magnet Rise, Telekinesis, Gastro Acid, the
// partial trap, Unburden's item-loss flag, a primed Micle Berry and the
// Metronome streak. The two mirrors (MagicRoomHere, AbilitySuppressed) are
// dropped rather than copied because doSwitchWithCarry re-derives both from
// the field a few lines later; copying them would just be a stale read that
// the sync then overwrites.
func newBatonCarry(out *Pokemon) *batonCarry {
	c := &batonCarry{Stages: out.Stages, Volatiles: out.Volatiles}
	v := &c.Volatiles

	// --- canon's noCopy set -------------------------------------------------
	v.Attract = false
	v.DefenseCurl = false
	v.DestinyBond = false
	v.Disable = nil
	v.Encore = nil
	v.FlashFireCharged = false
	v.Foresight = false
	v.Imprison = nil
	v.Minimize = false
	v.MiracleEye = false
	v.Nightmare = false
	v.SmackDown = false
	v.Stockpile = nil
	v.Torment = false
	v.Trapped = false
	v.Yawn = nil
	v.ChoiceLockMoveID = ""

	// --- turn-scheduling state ----------------------------------------------
	// Flinch, Protect, Endure, Roost, Snatch and Magic Coat are all cleared by
	// the end-of-turn transient sweep, and none of them can coexist with a
	// Baton Pass in the same turn anyway — the passer used Baton Pass, not
	// Protect. Charging, LockedMove and MustRecharge are move locks: a
	// Pokémon under any of them cannot select Baton Pass at all, so carrying
	// them would only ever be a way to teleport a lock onto a body that has no
	// business honoring it.
	v.Flinch = false
	v.Protect = false
	v.Endure = false
	v.Roost = false
	v.Snatch = false
	v.MagicCoat = false
	v.Charging = nil
	v.LockedMove = nil
	v.MustRecharge = false
	// ProtectCounter is the passer's stall chain. Canon does not flag `stall`
	// noCopy, but this engine zeroes the counter in executeMove's own defer
	// the moment a non-stall action resolves — and Baton Pass is that action,
	// so the value read here is one the passer is about to lose. Copying it
	// would hand the receiver a 1/3-odds Protect the passer never gets to use.
	v.ProtectCounter = 0
	// LastMoveID / LastMoveName are canon's `lastMove`, a Pokemon field that
	// clearVolatile nulls on both sides of a switch rather than a volatile
	// copyVolatileFrom loops over. They live in this struct only because the
	// switch wipe is the reset this engine wanted; the receiver's "last move"
	// is genuinely nothing, and leaving the passer's would let Disable or
	// Encore land on a move the receiver has never used and may not know.
	v.LastMoveID = ""
	v.LastMoveName = ""
	// MoveActions is canon's activeMoveActions, which switchIn resets to zero
	// (ps/sim/battle-actions.ts). Its whole reason for living in Volatiles is
	// that a switch zeroes it, and Fake Out's "first turn out" gate reads it —
	// a receiver that inherited the passer's count would arrive unable to use
	// the move it just switched in for.
	v.MoveActions = 0
	v.MovedLast = false
	v.MovedThisTurn = false
	v.DamagedThisTurn = false
	v.HurtThisTurn = false
	v.CustapBoost = false
	// The reactive register is this turn's record of hits the *passer* took, so
	// handing it on would let a receiver Counter damage it never suffered.
	v.ReactivePhysical, v.TookPhysicalHit = 0, false
	v.ReactiveSpecial, v.TookSpecialHit = 0, false
	// A Bide cannot be passed either: canon's condition is noCopy in all but
	// name, since the volatile locks the move slot it was started from and the
	// receiver has no such slot.
	v.Bide = nil
	// The per-turn stat-direction flags and the move-failure record are all
	// Pokemon fields upstream, not volatiles: switchIn clears
	// statsRaisedThisTurn / statsLoweredThisTurn outright
	// (ps/sim/battle-actions.ts) and clearVolatile nulls moveLastTurnResult.
	// They sit in this struct for the same reason LastMoveID does — the switch
	// wipe is the reset the engine wanted — so the carry must not hand them on.
	// A receiver arriving on a Baton Pass has not had its stats touched and has
	// not failed a move, and inheriting either would arm Lash Out, Burning
	// Jealousy or Stomping Tantrum off something it never experienced.
	v.Unnerve = false
	v.StatsRaisedThisTurn = false
	v.StatsLoweredThisTurn = false
	v.MoveThisTurnFailed = false
	v.MoveLastTurnFailed = false
	// The two field mirrors. syncMagicRoomFlags and syncAbilitySuppression
	// both run inside doSwitchWithCarry after the carry lands, so these are
	// re-derived from the field for the receiver either way; zeroing them
	// keeps the carry honest about owning only the passer's own state.
	v.MagicRoomHere = false
	v.AbilitySuppressed = false

	// --- deep-copy the pointers that travel ---------------------------------
	// Assigning the struct copied the pointers, not the pointees, so without
	// this the passer and the receiver would share one ConfusionState / one
	// Substitute / one LeechSeedState. The passer's bag is wiped on the way
	// out so nothing would *read* the alias today, but a shared Substitute is
	// the kind of bug that only shows up once some later mechanic looks at a
	// benched Pokémon — battle.go's Clone does the same thing field by field
	// for exactly this reason.
	//
	// LeechSeed needs no more than this: SourceSide is an int, so copying the
	// state by value copies the seeder's identity with it, and the seed
	// continues to feed the same side it always did.
	if x := v.Confusion; x != nil {
		y := *x
		v.Confusion = &y
	}
	if x := v.Substitute; x != nil {
		y := *x
		v.Substitute = &y
	}
	if x := v.PartialTrap; x != nil {
		y := *x
		v.PartialTrap = &y
	}
	if x := v.LeechSeed; x != nil {
		y := *x
		v.LeechSeed = &y
	}
	if x := v.PerishSong; x != nil {
		y := *x
		v.PerishSong = &y
	}
	if x := v.Taunt; x != nil {
		y := *x
		v.Taunt = &y
	}
	if x := v.Embargo; x != nil {
		y := *x
		v.Embargo = &y
	}
	if x := v.MagnetRise; x != nil {
		y := *x
		v.MagnetRise = &y
	}
	if x := v.Telekinesis; x != nil {
		y := *x
		v.Telekinesis = &y
	}
	// Fury Cutter's chain does travel — canon's condition carries no noCopy, so
	// copyVolatileFrom hands it over like any other volatile. It is only worth
	// anything to a receiver that also carries the move, which is the sort of
	// thing Baton Pass teams are built to arrange.
	if x := v.FuryCutter; x != nil {
		y := *x
		v.FuryCutter = &y
	}
	// Rollout's chain does not travel: canon's condition carries onLockMove,
	// and a Baton Pass out is one of the things that ends the lock. The
	// receiver starts any chain of its own from the bottom.
	v.Rollout = nil
	return c
}

// doSwitchWithCarry performs a switch, optionally transferring the outgoing
// Pokémon's stat stages and select volatiles to the incoming (Baton Pass).
// carry == nil is the plain reset-on-switch path doSwitch uses.
// doSwitchWithCarry installs the incoming Pokemon and immediately resolves its
// entry effects — the right shape for a switch that happens on its own. Where
// two arrivals are simultaneous, the caller installs both with
// installSwitchIn and then runs applySwitchInEffects for each in Speed order;
// see the note on applySwitchInEffects for why that is not the same thing.
func doSwitchWithCarry(s *BattleState, side, idx int, carry *batonCarry, rng *RNG, log *[]LogLine) {
	installSwitchIn(s, side, idx, carry, log)
	applySwitchInEffects(s, side, rng, log)
}

// installSwitchIn brings a Pokemon in: the outgoing's book-keeping, the slot
// change, the volatile wipe or Baton Pass carry, the field mirrors, and the
// "Go, X!" line. It stops short of every entry effect.
func installSwitchIn(s *BattleState, side, idx int, carry *batonCarry, log *[]LogLine) {
	sd := &s.Sides[side]
	if idx < 0 || idx >= len(sd.Team) || idx == sd.Active || sd.Team[idx].Fainted {
		return
	}
	out := &sd.Team[sd.Active]
	// Switch-out ability hook (Natural Cure, Regenerator) runs before the
	// outgoing's status / stages / volatiles are reset, so the hook can
	// observe what it's clearing.
	applyOnSwitchOut(out, side, log)
	// An ability written onto the outgoing Pokémon lasts only while it is on
	// the field — Trace's copy, and anything the ability-setting moves handed
	// it (abilitysetting.go). Restored before the hooks below so nothing further
	// down this function observes the borrowed ability. AbilityRevealed is
	// deliberately left alone: the copy announced itself, and knowledge does
	// not un-happen.
	if out.BaseAbility != "" {
		out.Ability = out.BaseAbility
		out.BaseAbility = ""
	}
	// Stats rewritten on the field (Speed Swap, Power Split) revert with it —
	// canon discards the edit by re-running setSpecies from clearVolatile.
	if out.BaseStats != nil {
		out.Stats = *out.BaseStats
		out.BaseStats = nil
	}
	// Typing rewritten on the field (Soak, Reflect Type, the two Conversions)
	// reverts with it, by the same argument: canon discards the change when
	// clearVolatile re-runs setSpecies.
	if out.BaseTypes != nil {
		out.Type1, out.Type2 = out.BaseTypes[0], out.BaseTypes[1]
		out.BaseTypes = nil
	}
	out.Stages = Stages{}
	out.Volatiles = Volatiles{}
	// A move-based trap dies with its trapper's departure, not with the
	// victim's. Every way off the field funnels through here — a chosen switch,
	// a replacement, a pivot move, a Roar-drag — so the victim is freed
	// whichever way the trapper went.
	releaseTrapsSetBy(s, 1-side)
	// Toxic's escalating clock resets when the badly-poisoned Pokémon leaves
	// the field (Gen 3+). The status itself persists — only the multiplier
	// goes back to the bottom, so a Pokémon that returns starts the ladder at
	// 1/16 again instead of resuming the count it left on. Switching out is
	// canon's standard answer to Toxic, and without this reset a benched
	// Pokémon carried a clock it had no way to clear: one tournament Machamp
	// came back on a 5/16 tick after 24 turns off the field.
	//
	// ToxicCounter is the *next* tick's numerator (applyStatusResidual reads
	// it, then increments), so 1 is the reset value. Zero would hand the
	// returning Pokémon a free damage-less turn.
	if out.Status == StatusToxic {
		out.ToxicCounter = 1
	}
	// The sleep counter deliberately survives a switch. Zeroing it here used to
	// look like a Gen-5 rule and was not one: canAct wakes anything sitting at
	// SleepTurns <= 0, so resetting on the way out meant a sleeper woke the
	// instant it came back — pivoting cured sleep outright. No generation works
	// that way, and it deleted the sleep axis from the format.
	if !out.Fainted {
		*log = append(*log, LogLine{Type: "switch", Side: side, Text: fmt.Sprintf("%s, come back!", out.Name)})
	}
	sd.Active = idx
	in := &sd.Team[idx]
	in.Stages = Stages{}
	in.Volatiles = Volatiles{}
	if carry != nil {
		in.Stages = carry.Stages
		in.Volatiles = carry.Volatiles
	}
	// A Pokémon that switched in does not act this turn. Zoom Lens asks "will
	// the target still move after me?", so from its holder's point of view a
	// fresh switch-in is settled exactly like one that has already moved.
	in.Volatiles.MovedThisTurn = true
	// The incoming's volatiles were just zeroed, so re-mirror the field's Magic
	// Room state onto it. Items are suppressed by the room, not by the mon.
	syncMagicRoomFlags(s)
	// Ability suppression is re-derived here for both sides, because a switch
	// can change it in either direction: the incoming may be walking into a
	// Neutralizing Gas that is already up, or the Pokémon that just left may
	// have been the gas itself — in which case this call is what lifts the
	// suppression off the foe and re-runs its switch-in ability.
	//
	// Placed before the entry hooks below so the incoming's own ability is
	// already correctly gated by the time applyOnSwitchIn reaches it, and so
	// the foe's resume reads as part of the switch-out rather than trailing
	// after the arrival.
	syncAbilitySuppression(s, log)
	*log = append(*log, LogLine{Type: "switch", Side: side, Text: fmt.Sprintf("Go, %s!", in.Name)})
	// The gap between the leaver and the arrival's entry hooks. Canon's switch
	// and runSwitch are two separate queue actions with an eachEvent('Update')
	// between them, so there is a moment where the old Unnerve holder is gone
	// and the new one's onStart has not run — and a berry that was being held
	// back gets eaten in it. This is that Update, narrowed to the berries.
	//
	// Deliberately here rather than in doSwitchWithCarry: it has to run before
	// applySwitchInEffects arms the arrival's own latch, and putting it inside
	// the install covers the simultaneous-arrival path too.
	for i := 0; i < 2; i++ {
		applyItemStatusCure(s, s.Active(i), i, log)
	}
}

// applySwitchInEffects runs the entry effects for a Pokemon that is already
// installed and announced. Split out from the install so simultaneous
// switch-ins can be brought in *first* and then have their entry effects
// resolved as one Speed-ordered block, which is what canon does: Showdown
// collects every switch-in, speed-sorts them, and fires one
// fieldEvent('SwitchIn') (sim/battle-actions.ts). Interleaving install and
// entry per side — this engine's old shape — makes the result depend on side
// index, and after a double KO that is visible: p1's replacement enters, its
// Intimidate fires against a slot that still holds p2's corpse and is swallowed
// by the hook's own fainted check, and then p2's replacement enters and
// intimidates normally. One side gets the drop for free.
//
// The lead path in ResolveTurn already had the right shape — install both, then
// run the hooks — so this is the replace path catching up to it.
func applySwitchInEffects(s *BattleState, side int, rng *RNG, log *[]LogLine) {
	// Switch-in effects run in canon's subOrder: slot conditions (3), then
	// side conditions — the entry hazards (4) — then abilities (7). Showdown
	// derives those numbers in Battle#resolvePriority (ps/sim/battle.ts,
	// `isSlotCondition` → subOrder 3, side condition → 4, Ability → 7) and
	// sorts one `SwitchIn` field event by them.
	//
	// Slot-condition switch-in consumer: Healing Wish fully restores the
	// incoming. It used to run *last*, after hazards and abilities, on the
	// reasoning that a full restore should undo any entry chip. That reads
	// well and is the wrong game: the gen-4 case upstream is titled "should
	// heal a switch-in for full after hazards mid-turn" and asserts the
	// arrival *faints*, which only makes sense if the modern rule it is
	// contrasted with is heal-then-chip. So the receiver is topped up first
	// and Stealth Rock then takes its cut of the restored total — a Pokémon
	// sent in on a Healing Wish under rocks arrives at 15/16, not at full.
	applySlotConditionsOnSwitchIn(s, side, log)
	// Entry hazards fire before the ability switch-in hook — hazards, then
	// Intimidate/Drizzle/etc. Among themselves the hazards run in the order
	// they were laid rather than in a fixed one; see applyHazardsOnSwitchIn. A
	// hazard KO short-circuits the rest (applyOnSwitchIn no-ops on a
	// fainted active).
	applyHazardsOnSwitchIn(s, side, log)
	applyOnSwitchIn(s, side, log)
	// Hazard chip on entry can put the incoming Pokémon straight into its
	// berry's range, and with the Healing Wish consumer moved above the
	// hazards that is now reachable off a wish as well: heal to full, eat the
	// rocks, and a Sitrus holder can legitimately be in range. Checked last so
	// the berry sees the HP the Pokémon actually finished entry on.
	applyItemHPTrigger(s, side, rng, log)
}

// applySelfSwitch handles U-turn / Volt Switch / Flip Turn / Teleport (plain
// "normal") and Baton Pass ("copyvolatile"). Called at the tail of
// executeMove: if the user is alive and has a live bench member, the switch
// fires immediately so a same-turn slower foe sees (and can target) the
// replacement.
//
// One foe does not see the replacement: a slower Pursuit user strikes the
// departing Pokémon instead, from inside this function. See
// runPursuitBeforeSwitchOut.
//
// With no live bench member the two status pivots fail loudly and the damaging
// ones stay silent — selfSwitchFailsWithoutTarget below holds that distinction
// and the reasoning for it.
//
// want is the bench slot the controller asked for (Action.SwitchTarget), or
// nil for "you pick". Choosing the pivot target is the whole point of a pivot
// move — Volt Switch into the counter you actually want, Baton Pass +2 onto
// the sweeper that can use it — and without it party order silently overrode
// the tactical decision: the engine always took the lowest-indexed live
// teammate, so a Baton Pass was a boost handed to whoever happened to sit
// earliest at team-build time.
//
// nil still means lowest-indexed live teammate. That keeps replays and
// unaware controllers byte-identical, and it stays deterministic — the fix
// here is to let the caller aim, not to randomize.
func applySelfSwitch(s *BattleState, side int, m domain.Move, want *int, rng *RNG, log *[]LogLine) {
	if m.SelfSwitch == "" {
		return
	}
	atk := s.Active(side)
	if atk.Fainted || atk.HP <= 0 {
		return
	}
	sd := &s.Sides[side]
	target := selfSwitchTarget(sd, want)
	if target == -1 {
		// Nobody to bring in. For the damaging pivots that is the end of it —
		// see selfSwitchFailsWithoutTarget for why the two status pivots are
		// louder about it.
		if selfSwitchFailsWithoutTarget(m) {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		}
		return
	}
	// A queued Pursuit strikes here, before the user actually leaves. This is
	// canon's second BeforeSwitchOut site: the first is inside switchIn and
	// serves a *chosen* switch (this engine runs it as the interception loop at
	// the top of ResolveTurn), and this one is the tail of runAction, which
	// fires once a move has set switchFlag (ps/sim/battle.ts:2892). Upstream
	// reaches it before makeRequest('switch') on the next line, which is
	// literally what the ported case is named after: the damage lands before
	// the replacement is even asked for.
	//
	// Placed after the target == -1 return on purpose, and that matches canon
	// rather than merely being convenient: with no live bench the pivot never
	// sets switchFlag, so the BeforeSwitchOut loop never runs and no Pursuit
	// fires.
	if runPursuitBeforeSwitchOut(s, side, m, log) && (atk.Fainted || atk.HP <= 0) {
		// Canon's 'pursuitfaint': the chase KO'd the pivot user, so there is
		// nothing left to switch out and the replacement comes through the
		// replace phase instead.
		return
	}
	var carry *batonCarry
	if m.SelfSwitch == "copyvolatile" {
		carry = newBatonCarry(atk)
	}
	doSwitchWithCarry(s, side, target, carry, rng, log)
}

// pursuitSkipsBeforeSwitchOut reports whether m's user leaves without giving a
// queued Pursuit its chance. Baton Pass and Shed Tail set
// skipBeforeSwitchOutEventFlag from their own onHit (ps/data/moves.ts); U-turn,
// Volt Switch, Flip Turn, Parting Shot and Teleport do not.
//
// Keyed on the move ID rather than on SelfSwitch == "copyvolatile", even though
// the two coincide exactly in today's dataset. The coincidence is an accident
// of which moves are curated — Shed Tail is not in this dataset and would
// arrive as a plain self-switch — and canon draws the line move by move, the
// same argument selfSwitchFailsWithoutTarget makes above.
func pursuitSkipsBeforeSwitchOut(m domain.Move) bool {
	return m.ID == "baton-pass" || m.ID == "shed-tail"
}

// runPursuitBeforeSwitchOut fires a Pursuit armed against the pivot user on
// side, reporting whether it fired.
//
// The arming lives in ResolveTurn's mover loop, which is where the "has the
// pursuer acted yet?" question can be answered. Canon asks it as
// `!this.queue.cancelMove(source)`: a chase is only possible while the
// pursuer's own action is still in the queue, which is why a *faster* Pursuit
// user gets no interception — it has already moved.
func runPursuitBeforeSwitchOut(s *BattleState, side int, m domain.Move, log *[]LogLine) bool {
	ch := s.pursuit
	// ch.side == side would be the pursuer pivoting out itself, which is not an
	// interception — a Pokémon does not chase itself off the field.
	if ch == nil || ch.spent || ch.side == side {
		return false
	}
	if pursuitSkipsBeforeSwitchOut(m) {
		return false
	}
	pursuer := s.Active(ch.side)
	if pursuer == nil || pursuer.Fainted || pursuer.HP <= 0 {
		return false
	}
	// Spend it before the call, not after: executeMove can re-enter this file,
	// and a chase that could fire twice would be worse than one that never
	// fires at all.
	ch.spent = true
	// The synthetic ActionSwitch is what trips the ×2 in executeMove's
	// base-power block — canon's basePowerCallback reads target.switchFlag,
	// which is exactly the state the pivot user is in right now.
	executeMove(ch.dex, s, ch.side, ch.action, Action{Kind: ActionSwitch}, true, ch.vested, ch.rng, log)
	return true
}

// selfSwitchFailsWithoutTarget reports whether a self-switch move fails
// outright when its side has nobody left to send in, rather than resolving and
// then quietly not switching.
//
// Canon draws this line move by move, not by category. Baton Pass carries an
// `onHit` that emits `-fail` when `!this.canSwitch(side)`, and Teleport an
// `onTry` returning `!!this.canSwitch(source.side)` (ps/data/moves.ts) — both
// exist because the move has no other effect, so "announce and do nothing" is
// indistinguishable from a move that worked. U-turn, Volt Switch and Flip Turn
// carry neither hook: they are `selfSwitch: true` and nothing else, they deal
// their damage, and the switch half simply does not happen. That asymmetry is
// deliberate upstream and the comment on selfSwitchTarget below describes the
// engine end of it, so this predicate is keyed on the move and must stay that
// way — widening it to `m.Category == CatStatus` or to every self-switch move
// would make a last-Pokémon U-turn print "But it failed!" after it had already
// knocked something out.
//
// Healing Wish is the same shape one file over and takes the same route
// (applyHealingWishSetter in slotconditions.go emits this exact line on an
// empty bench); it is not listed here because it is a slot condition rather
// than a self-switch, so it never reaches applySelfSwitch.
func selfSwitchFailsWithoutTarget(m domain.Move) bool {
	switch m.ID {
	case "baton-pass", "teleport":
		return true
	}
	return false
}

// selfSwitchTarget resolves the bench slot a self-switch brings in. want is
// the controller's choice or nil; -1 comes back when the side has nobody left
// to send, which is how a U-turn on a last Pokémon quietly does nothing.
//
// An out-of-range, fainted or already-active choice falls back to the default
// rather than failing the move. LegalActions only ever offers legal targets,
// so this path means a controller went around it — and canon has no "your
// pivot fizzled because you aimed badly" outcome to imitate.
func selfSwitchTarget(sd *Side, want *int) int {
	if want != nil {
		i := *want
		if i >= 0 && i < len(sd.Team) && i != sd.Active && !sd.Team[i].Fainted {
			return i
		}
	}
	for i := range sd.Team {
		if i != sd.Active && !sd.Team[i].Fainted {
			return i
		}
	}
	return -1
}
