package engine

import (
	"fmt"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

// slotconditions.go owns the per-side "slot conditions" — state that
// rides on a side's active position rather than the Pokémon itself, so
// it persists across switches. Two are modeled:
//
//   Wish         — delayed heal. Cast on turn N, restores ½ of the
//                  caster's MaxHP to whoever holds the slot at end of
//                  turn N+1.
//   Healing Wish — sacrificial. The user faints immediately; the next
//                  Pokémon switched in is restored to full HP and has
//                  any non-volatile status cleared.
//
// SlotConditions lives on Side (declared inline below). Setter moves
// dispatch through slotConditionSetters (same registry pattern as
// side conditions / weather). Wish ticks down in ResolveTurn; Healing
// Wish is consumed in doSwitch's switch-in hook.

func init() {
	specs.RegisterSlotCondition("wish")
	specs.RegisterSlotCondition("healingwish")
	// Registered so the vocabulary tells the truth, though no move in the
	// dataset names it: upstream's Future Sight installs the condition from a
	// JS onTry rather than declaring `slotCondition`, so the dump carries
	// nothing and the transform has nothing to map. The engine models it all
	// the same, which is what this entry says.
	specs.RegisterSlotCondition("futuremove")
	registerSlotCondition("wish", applyWishSetter)
	registerSlotCondition("healingwish", applyHealingWishSetter)
}

// SlotConditions is the per-side, per-slot state bag. Currently holds
// Wish (timer + payload) and HealingWish (one-shot flag). Future slot
// conditions (Lunar Dance) would land here too.
type SlotConditions struct {
	Wish        *WishState `json:"wish,omitempty"`
	HealingWish bool       `json:"healing_wish,omitempty"`
	// FutureMove is a delayed attack aimed at this slot. Unlike the other two
	// it belongs to the *other* side: canon's Future Sight calls
	// addSlotCondition on the target's side, which is what makes it hit
	// whoever is standing there when it lands rather than whoever was there
	// when it was cast.
	FutureMove *FutureMoveState `json:"future_move,omitempty"`
}

// FutureMoveState is a Future Sight in flight.
//
// The attacker is a (side, team index) pair and not a *Pokemon, because
// BattleState is deep-copied by Clone and round-tripped through JSON, and a
// pointer survives neither: after one clone it would name a Pokemon on nobody's
// team. Name is carried alongside for the log line, since the attacker may have
// fainted by the time the hit lands and its slot may since have been reused.
//
// TurnsLeft is 3 at cast and ticks at end of turn, so the hit lands at the end
// of the second turn after the one it was cast on. Wish's is 2 and fires a turn
// earlier; the two are deliberately not shared.
type FutureMoveState struct {
	MoveID     string `json:"move_id"`
	SourceSide int    `json:"source_side"`
	SourceTeam int    `json:"source_team"`
	SourceName string `json:"source_name"`
	TurnsLeft  int    `json:"turns_left"`
}

// WishState encodes the delayed heal: Amount is the HP figure to
// restore (snapshotted at cast time so swapping in a max-HP variant
// doesn't change the heal value), Healer is the caster's name for
// the canonical log line, and TurnsLeft is the end-of-turn countdown
// (apply sets 2 → tick to 1 → next tick fires the heal at 0).
type WishState struct {
	Healer    string `json:"healer"`
	Amount    int    `json:"amount"`
	TurnsLeft int    `json:"turns_left"`
}

// slotConditionSetter is the contract a mechanic fulfills to claim a
// SlotCondition slug. Same shape as sideConditionSetter — the slug
// comes off Move.SlotCondition and the dispatcher routes through
// slotConditionSetters.
type slotConditionSetter func(s *BattleState, side int, log *[]LogLine)

var slotConditionSetters = map[string]slotConditionSetter{}

func registerSlotCondition(slug string, h slotConditionSetter) {
	slotConditionSetters[slug] = h
}

// applyWishSetter arms a Wish on the user's side. Fails if a Wish is
// already pending on the slot (canon — overlap loses to first-come).
func applyWishSetter(s *BattleState, side int, log *[]LogLine) {
	sc := &s.Sides[side].SlotConditions
	if sc.Wish != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	atk := s.Active(side)
	sc.Wish = &WishState{
		Healer:    atk.Name,
		Amount:    atk.MaxHP / 2,
		TurnsLeft: 2,
	}
	*log = append(*log, LogLine{
		Type: "wish", Side: side,
		Text: fmt.Sprintf("%s made a wish!", atk.Name),
	})
}

// applyHealingWishSetter is the sacrificial path. The user faints on
// the spot and the slot flag is set so the next switch-in is fully
// restored. Fails if the side has no live bench to receive the
// healing (no point fainting the user for a heal that can't trigger).
//
// A slot that is *already* wishing is the one case where the move reports
// failure and the user dies anyway. That looks like a bug and is canon:
// upstream's moveHit checks `selfdestruct === 'ifHit'` against damage[i]
// *before* folding in whether addSlotCondition succeeded, so the faint is
// unconditional on the move having reached its target and only the
// wish-setting half can fail. It matters because the wish is not consumed by
// an arrival that needed nothing (see applySlotConditionsOnSwitchIn), so a
// side that sacrifices twice in a row genuinely reaches this — upstream's
// Intimidate double-KO case does exactly that, and would deadlock if the
// second sacrifice refused.
func applyHealingWishSetter(s *BattleState, side int, log *[]LogLine) {
	sd := &s.Sides[side]
	alreadyWishing := sd.SlotConditions.HealingWish
	hasBench := false
	for i := range sd.Team {
		if i != sd.Active && !sd.Team[i].Fainted {
			hasBench = true
			break
		}
	}
	if !hasBench {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	atk := s.Active(side)
	if alreadyWishing {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
	} else {
		sd.SlotConditions.HealingWish = true
		*log = append(*log, LogLine{
			Type: "healingwish", Side: side,
			Text: fmt.Sprintf("%s is calling on the spirit of the past!", atk.Name),
		})
	}
	atk.HP = 0
	faint(atk, side, log)
}

// tickSlotConditions ticks Wish down and fires the heal when it
// expires. Side 0 first, then side 1, for log determinism. Called
// from ResolveTurn's end-of-turn block. HealingWish has no tick — it
// consumes on switch-in.
func tickSlotConditions(s *BattleState, side int, log *[]LogLine) {
	sc := &s.Sides[side].SlotConditions
	if sc.Wish == nil {
		return
	}
	sc.Wish.TurnsLeft--
	if sc.Wish.TurnsLeft > 0 {
		return
	}
	payload := sc.Wish
	sc.Wish = nil
	active := s.Active(side)
	if active.Fainted {
		return
	}
	*log = append(*log, LogLine{
		Type: "wish", Side: side,
		Text: fmt.Sprintf("%s's Wish came true!", payload.Healer),
	})
	healPokemon(active, side, payload.Amount, log)
}

// applySlotConditionsOnSwitchIn fires when a new Pokémon enters the
// slot. Healing Wish is consumed here — the incoming is fully
// restored and any non-volatile status cleared. Called from doSwitch
// *before* the entry-hazard and ability-on-switch-in passes, which is
// canon's ordering: Showdown runs one SwitchIn field event and sorts
// its handlers by subOrder, where a slot condition is 3 and a side
// condition (the hazards) is 4. So the wish tops the arrival up and
// Stealth Rock then takes its cut of the restored total.
func applySlotConditionsOnSwitchIn(s *BattleState, side int, log *[]LogLine) {
	sd := &s.Sides[side]
	if !sd.SlotConditions.HealingWish {
		return
	}
	in := &sd.Team[sd.Active]
	if in.Fainted {
		// Can't heal a fainted slot — leave the flag for whatever
		// comes in next (matches Showdown).
		return
	}
	// A switch-in that needs nothing does not spend the wish. Canon's
	// healingwish onSwap gates the whole body on
	// `!target.fainted && (target.hp < target.maxhp || target.status)`
	// (ps/data/moves.ts), so a Pokémon that arrives at full HP with no
	// status walks past it and the condition stays armed for whoever
	// comes in after — the sacrifice is not wasted on a body that had
	// nothing to heal. Without this the flag cleared unconditionally and
	// the wish evaporated on the first switch, healing nobody.
	//
	// Note the guard reads HP *before* the entry hazards, which is only
	// correct because this now runs ahead of them (see doSwitchWithCarry).
	// Run after the hazard pass and every arrival under Stealth Rock is
	// below full, so the guard would never refuse and this case would
	// stay red.
	if in.HP >= in.MaxHP && in.Status == StatusNone {
		return
	}
	sd.SlotConditions.HealingWish = false
	in.HP = in.MaxHP
	if in.Status != StatusNone {
		in.Status = StatusNone
		in.SleepTurns = 0
		in.ToxicCounter = 0
	}
	*log = append(*log, LogLine{
		Type: "healingwish", Side: side,
		Text: fmt.Sprintf("The healing wish came true for %s!", in.Name),
	})
}

// CloneSlotConditions deep-copies the slot-condition bag so BattleState.
// Clone produces independent state. Pointer-or-bool members are deep-
// copied with the same shape used for Volatiles / SideConditions /
// PseudoWeather.
func CloneSlotConditions(src SlotConditions) SlotConditions {
	out := SlotConditions{HealingWish: src.HealingWish}
	if src.Wish != nil {
		w := *src.Wish
		out.Wish = &w
	}
	if src.FutureMove != nil {
		f := *src.FutureMove
		out.FutureMove = &f
	}
	return out
}

// --- future-impact damage ---

// futureMoveTurns is how many end-of-turn ticks a fresh Future Sight has left.
// Canon computes an absolute `endingTurn = (turn - 1) + 2` and compares against
// it each residual, which lands the hit at the end of the *second* turn after
// the one it was cast on. Counting ticks makes that three: one at the end of the
// cast turn, one at the end of the turn after, and the third is the landing.
const futureMoveTurns = 3

// armFutureMove installs a pending hit on the foe's slot, reporting whether
// there was room for it.
//
// The slot is the foe's, deliberately. Canon's Future Sight is a slot condition
// on the target's side, which is the whole of two of its rules: a target that
// switches out hands the hit to its replacement, and a second Future Sight aimed
// at the same slot fails while the first is still in flight.
func armFutureMove(s *BattleState, side int, m domain.Move) bool {
	sc := &s.Sides[1-side].SlotConditions
	if sc.FutureMove != nil {
		return false
	}
	atk := s.Active(side)
	sc.FutureMove = &FutureMoveState{
		MoveID:     m.ID,
		SourceSide: side,
		SourceTeam: s.Sides[side].Active,
		SourceName: atk.Name,
		TurnsLeft:  futureMoveTurns,
	}
	return true
}

// tickFutureMoves counts one end-of-turn off a pending hit on side's slot and
// delivers it at zero. Called from ResolveTurn's residual block.
//
// Two orderings matter and both are canon's. The condition is removed *before*
// the hit resolves, so a hit that KOs its target cannot leave a stale pending
// entry behind, and a fresh Future Sight can be cast the same turn. And the
// clock is ticked before the occupant is checked, so a hit aimed at a slot whose
// occupant has fainted expires instead of waiting forever — canon exempts slot
// conditions from the "skip handlers on fainted holders" rule for exactly that.
func tickFutureMoves(dex *domain.Dex, s *BattleState, side int, rng *RNG, log *[]LogLine) {
	sc := &s.Sides[side].SlotConditions
	fm := sc.FutureMove
	if fm == nil {
		return
	}
	fm.TurnsLeft--
	if fm.TurnsLeft > 0 {
		return
	}
	sc.FutureMove = nil

	target := s.Active(side)
	if target == nil || target.Fainted || target.HP <= 0 {
		// Canon hints and returns: the attack arrives at an empty slot and
		// simply does not happen.
		return
	}
	*log = append(*log, LogLine{
		Type: "futuremove", Side: side,
		Text: fmt.Sprintf("%s took the %s attack!", target.Name, dex.Moves[fm.MoveID].Name),
	})
	// Protect and Endure are stripped before the hit rather than consulted:
	// canon's onEnd calls removeVolatile on both, which is why a Future Sight
	// cannot be blocked by a shield put up on the turn it lands.
	target.Volatiles.Protect = false
	target.Volatiles.Endure = false
	deliverFutureMove(dex, s, side, fm, rng, log)
}

// deliverFutureMove resolves the pending hit. The attacker is read out of the
// stored team index, which is the point of storing one: it may have switched
// out, and it may have fainted.
//
// A benched attacker is not the one it was. Canon's ignoringItem and
// ignoringAbility both return true for anything off the field, and its stat
// stages went with it when it left — so the figure is computed from a copy with
// the item, the ability and the stages stripped. The text upstream ships with
// the move says exactly this: "if the user is no longer active at the time,
// damage is calculated based on the user's natural Special Attack stat, types,
// and level, with no boosts from its held item or Ability."
func deliverFutureMove(dex *domain.Dex, s *BattleState, side int, fm *FutureMoveState, rng *RNG, log *[]LogLine) {
	src := &s.Sides[fm.SourceSide].Team[fm.SourceTeam]
	atk := *src
	onField := s.Sides[fm.SourceSide].Active == fm.SourceTeam && !src.Fainted
	if !onField {
		atk.Item, atk.Ability = ItemNone, AbilityNone
		atk.Stages = Stages{}
		atk.Volatiles = Volatiles{}
	}
	def := s.Active(side)
	m := dex.Moves[fm.MoveID]
	// The cast's `ignore-immunity` flag is the outer move's, and its only job
	// upstream is to keep the *cast* from being refused by the type chart. The
	// hit canon synthesizes carries the opposite value, so a Dark type walls
	// the Psychic attack. Stripped here rather than at the cast, because the
	// cast is the half that needs it.
	m.Flags = withoutFlag(m.Flags, "ignore-immunity")

	res := computeDamage(dex, &atk, def, m, effectiveWeather(s), s.Terrain,
		&s.Sides[side].Conditions, &s.PseudoWeather, rng)
	if res.Effectiveness == 0 {
		*log = append(*log, LogLine{
			Type: "immune", Side: fm.SourceSide,
			Text: fmt.Sprintf("It doesn't affect %s...", def.Name),
		})
		return
	}
	dmg := res.Damage
	if dmg > def.HP {
		dmg = def.HP
	}
	if hasSubstitute(def) && !bypassesSubstitute(m, &atk) {
		applyDamageToSubstitute(def, side, dmg, log)
		return
	}
	hurt(def, dmg)
	if dmg > 0 {
		def.Volatiles.DamagedThisTurn = true
		def.TimesAttacked++
		switch m.Category {
		case domain.CatPhysical:
			def.Volatiles.ReactivePhysical, def.Volatiles.TookPhysicalHit = 2*dmg, true
		case domain.CatSpecial:
			def.Volatiles.ReactiveSpecial, def.Volatiles.TookSpecialHit = 2*dmg, true
		}
	}
	*log = append(*log, LogLine{
		Type: "damage", Side: side,
		Text: fmt.Sprintf("%s took %d damage.", def.Name, dmg),
	})
	if res.Effectiveness > 1 {
		*log = append(*log, LogLine{Type: "effective", Side: fm.SourceSide, Text: "It's super effective!"})
	} else if res.Effectiveness < 1 {
		*log = append(*log, LogLine{Type: "resisted", Side: fm.SourceSide, Text: "It's not very effective..."})
	}
	if def.HP <= 0 {
		faint(def, side, log)
	}
	// Life Orb, and only Life Orb. Canon's onEnd re-fires exactly one item hook
	// by name, and only when the caster is still on the field — which is what
	// keeps a replacement that happens to be holding one from paying for a hit
	// it did not throw. Everything else the attacker's side would normally get
	// (Shell Bell's drain, Throat Spray) is skipped for the same reason: canon
	// suppresses the whole AfterMoveSecondarySelf event on a future move and
	// then re-runs this one line of it.
	if onField && dmg > 0 && lifeOrbRecoilApplies(src, m) {
		applyLifeOrbRecoil(src, fm.SourceSide, log)
		if src.HP <= 0 {
			faint(src, fm.SourceSide, log)
		}
	}
}

// withoutFlag returns flags with one entry removed. Copies rather than
// filtering in place: the slice belongs to the shared dex entry, and every
// other move of that id would see the edit.
func withoutFlag(flags []string, drop string) []string {
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		if f != drop {
			out = append(out, f)
		}
	}
	return out
}
