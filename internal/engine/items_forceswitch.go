package engine

import "pokearena/internal/domain"

// items_forceswitch.go is the three items that take somebody off the field:
// Eject Button, Red Card and Eject Pack.
//
// They read as one feature and are not. The distinction that matters is the
// one canon draws between a *switch* and a *drag*, and these three land on
// both sides of it:
//
//	Eject Button   the holder switches out          switchFlag  — a switch
//	Eject Pack     the holder switches out          switchFlag  — a switch
//	Red Card       the attacker is thrown out    forceSwitchFlag — a drag
//
// A switch is the holder's own departure: its side picks the replacement, and
// nothing that blocks a *drag* blocks it. A drag is done to somebody: the
// replacement is random, and Ingrain, Suction Cups and a Sky Drop in progress
// all refuse it. forceswitch.go's header spells out why the two are separate
// events upstream and why Shed Shell sits on only one of them; the same line
// runs through here.
//
// So Eject Button and Eject Pack route to the self-switch machinery — with
// Pursuit's interception, which a departing holder is just as exposed to as a
// U-turn user — and Red Card routes to applyForceSwitch, which already knows
// every refusal a drag has to respect.
//
// Red Card's consumption is deliberately not conditional on the drag working.
// Upstream says so in a comment of its own ("The item is used up even against
// a pokemon with Ingrain or that otherwise can't be forced out"), and the
// ordering in its handler is explicit: useItem first, then the DragOut event,
// then the flag.

const (
	ItemEjectButton ItemKind = "eject-button"
	ItemRedCard     ItemKind = "red-card"
	ItemEjectPack   ItemKind = "eject-pack"
)

func init() {
	registerItem(&Item{
		Kind: ItemEjectButton,
		Name: "Eject Button",
		Desc: "Switches the holder out when it is hit by a damaging move.",
	})
	registerItem(&Item{
		Kind: ItemRedCard,
		Name: "Red Card",
		Desc: "Forces the attacker out when the holder is hit by a damaging move.",
	})
	registerItem(&Item{
		Kind: ItemEjectPack,
		Name: "Eject Pack",
		Desc: "Switches the holder out when any of its stats are lowered.",
	})
}

// --- Eject Button and Red Card ---

// applyHitReactiveSwitchItems fires the two items that answer a damaging move
// connecting: the defender's Eject Button and its Red Card.
//
// Reports whether the attacker's own self-switch has been canceled. Eject
// Button clears `source.switchFlag` upstream, so a U-turn that pops a button
// leaves the attacker standing — the button's holder is the one that leaves,
// and canon will not run two switches out of one move.
//
// Called from the damaging-move tail *before* applySelfSwitch, which is
// canon's order: onAfterMoveSecondary runs inside the move, and the switchFlag
// the pivot set is not acted on until runAction returns.
func applyHitReactiveSwitchItems(s *BattleState, atkSide int, m domain.Move, connected bool, rng *RNG, log *[]LogLine) (cancelSelfSwitch bool) {
	if !connected || m.Category == domain.CatStatus {
		return false
	}
	// Canon also guards on `!move.flags['futuremove']`, and there is nothing
	// to write for it: this engine's Future Sight lands from
	// deliverFutureMove, which applies its damage directly and never reaches
	// the damaging-move tail this is called from. The ported case that says a
	// Future Sight must not pop an Eject Button therefore holds structurally.
	// If a future move is ever routed through dealDamage, it needs the guard.
	defSide := 1 - atkSide
	atk, def := s.Active(atkSide), s.Active(defSide)
	if atk == nil || def == nil || atk == def || def.Fainted || def.HP <= 0 {
		return false
	}

	// The attacking move is over as far as anything that happens below is
	// concerned. Upstream reaches that state structurally — switchFlag is not
	// acted on until runAction returns and battle.activeMove is cleared — but
	// this engine fires the items from inside executeMove, where s.moldBreaker
	// is still pointing at the attacker.
	//
	// Leaving it set is observable and wrong: the replacement that walks in
	// would have its own ability suppressed on entry, so a Levitate holder
	// brought in by an Eject Button took Spikes damage it is immune to. That is
	// the ported case named for it. Cleared and restored rather than left to the
	// caller's defer, because the switch-in happens inside this call.
	defer func(prev *Pokemon) { s.moldBreaker = prev }(s.moldBreaker)
	s.moldBreaker = nil

	// Eject Button first, matching canon's handler order only in the sense
	// that both are onAfterMoveSecondary — but the button is the defender's
	// own departure and the Red Card throws the attacker, so if the button
	// fires the holder is gone and its card never gets a turn.
	if it := itemOf(def); it != nil && it.Kind == ItemEjectButton {
		if ejectHolder(s, defSide, rng, log) {
			return true
		}
	}
	if it := itemOf(def); it != nil && it.Kind == ItemRedCard {
		redCardThrowsAttacker(s, defSide, rng, log)
	}
	return false
}

// ejectHolder switches the holder out and consumes the item, reporting whether
// it fired.
//
// Refuses, without consuming, when there is nobody to bring in — canon's
// onAfterMoveSecondary checks canSwitch before it sets the flag and puts the
// flag back if useItem fails, which is a long way of saying the button does
// nothing on an empty bench and is still there afterwards.
//
// A holder already leaving is refused too (`target.forceSwitchFlag ||
// target.beingCalledBack`), and so is one held by a Sky Drop. The engine has
// no mid-turn switch queue for the first two to observe, so the Sky Drop hold
// is the one that has a subject here.
func ejectHolder(s *BattleState, side int, rng *RNG, log *[]LogLine) bool {
	p := s.Active(side)
	if p.Volatiles.SkyDrop != nil || heldBySkyDrop(s, side) {
		return false
	}
	target := selfSwitchTarget(&s.Sides[side], nil)
	if target == -1 {
		return false
	}
	consumeItemAnnounced(p, side, itemOf(p), log)
	// A queued Pursuit strikes the holder on its way out, exactly as it does a
	// U-turn user: canon's BeforeSwitchOut fires off switchFlag regardless of
	// what set it. Passing no move is right — the departure is the item's, not
	// a move's, so nothing here can be a Baton Pass or a Shed Tail that would
	// skip the event.
	if runPursuitBeforeSwitchOut(s, side, domain.Move{}, log) && (p.Fainted || p.HP <= 0) {
		return true
	}
	doSwitchWithCarry(s, side, target, nil, rng, log)
	return true
}

// redCardThrowsAttacker drags the attacker out and consumes the card.
//
// applyForceSwitch does the whole job: it already refuses for Ingrain and for
// a Sky Drop in progress, announces those refusals, and draws no RNG when it
// refuses. Its argument is the side whose *foe* gets dragged, so the holder's
// own side is what goes in.
//
// The card is spent before the drag is attempted and regardless of whether it
// lands, which is upstream's stated behavior and not an accident of ordering.
func redCardThrowsAttacker(s *BattleState, holderSide int, rng *RNG, log *[]LogLine) {
	atk := s.Active(1 - holderSide)
	if atk == nil || atk.Fainted || atk.HP <= 0 {
		return
	}
	// Nobody to drag in: canon's `!this.canSwitch(source.side)` returns before
	// useItem, so the card is not spent either.
	if selfSwitchTarget(&s.Sides[1-holderSide], nil) == -1 {
		return
	}
	holder := s.Active(holderSide)
	consumeItemAnnounced(holder, holderSide, itemOf(holder), log)
	applyForceSwitch(s, holderSide, rng, log)
}

// --- Eject Pack ---

// armEjectPack records that the holder's stats were lowered, so the switch can
// happen later. Called from applyStages, which is the single write point every
// stage change goes through — self-inflicted and foe-induced alike, which is
// what canon's onAfterBoost sees too. Two of the ported cases turn on that: a
// holder that drops its own stats with Swallow, and one dropped by its own
// Moody, both eject.
//
// The delay is canon's and it is not incidental. Upstream sets a flag in
// onAfterBoost and spends it from four other hooks — onAnySwitchIn,
// onAnyAfterMega, onAnyAfterMove and a residual at order 29 — because the drop
// usually lands in the middle of somebody else's move and the field is in no
// state to run a switch there. This engine has the same problem and takes the
// same answer: arm here, act at the two points that exist, the end of the move
// and the end of the turn.
//
// Upstream additionally excludes Parting Shot by name
// (`this.activeMove?.id === 'partingshot'`), because that move is already a
// self-switch for its *user* and the two would collide. It is not written here
// and could not be: applyStages does not know the move, the move is not in this
// dataset, and the row that wants it is filed as needing Parting Shot rather
// than needing the pack.
func armEjectPack(p *Pokemon) {
	if p == nil || p.Fainted {
		return
	}
	if it := itemOf(p); it == nil || it.Kind != ItemEjectPack {
		return
	}
	p.Volatiles.EjectPackArmed = true
}

// fireEjectPack spends an armed pack: the holder switches out and the item goes
// with it.
//
// Disarms whether or not the switch happens. Canon's onUseItem returns false
// with an empty bench, which leaves the flag set and the item held — but that
// only matters across a window this engine does not have, since the two points
// it checks are the end of the move and the end of the turn and the bench does
// not change in between. Clearing is the simpler invariant and the observable
// behavior is the same.
func fireEjectPack(s *BattleState, side int, rng *RNG, log *[]LogLine) {
	p := s.Active(side)
	if p == nil || !p.Volatiles.EjectPackArmed {
		return
	}
	p.Volatiles.EjectPackArmed = false
	if p.Fainted || p.HP <= 0 {
		return
	}
	if it := itemOf(p); it == nil || it.Kind != ItemEjectPack {
		return
	}
	ejectHolder(s, side, rng, log)
}

// fireEjectPacks spends both sides' packs, holder-first on the side that armed
// earliest is not a distinction this engine can draw, so it goes in side order.
// Singles means at most one is ever armed in practice: the two ways to arm the
// far side's pack in the same window — a spread move and an ally's Intimidate —
// are both doubles-only.
func fireEjectPacks(s *BattleState, rng *RNG, log *[]LogLine) {
	// Same reasoning as applyHitReactiveSwitchItems: one of the two call sites
	// is inside executeMove, where the attacker's mold-breaking is still on the
	// state, and the Pokémon that walks in must not be standing in it.
	defer func(prev *Pokemon) { s.moldBreaker = prev }(s.moldBreaker)
	s.moldBreaker = nil
	for side := 0; side < 2; side++ {
		fireEjectPack(s, side, rng, log)
	}
}
