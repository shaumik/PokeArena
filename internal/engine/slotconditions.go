package engine

import (
	"fmt"

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
	registerSlotCondition("wish", applyWishSetter)
	registerSlotCondition("healingwish", applyHealingWishSetter)
}

// SlotConditions is the per-side, per-slot state bag. Currently holds
// Wish (timer + payload) and HealingWish (one-shot flag). Future slot
// conditions (Lunar Dance) would land here too.
type SlotConditions struct {
	Wish        *WishState `json:"wish,omitempty"`
	HealingWish bool       `json:"healing_wish,omitempty"`
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

// slotConditionSetter is the contract a mechanic fulfils to claim a
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
	*log = append(*log, LogLine{Type: "wish", Side: side,
		Text: fmt.Sprintf("%s made a wish!", atk.Name)})
}

// applyHealingWishSetter is the sacrificial path. The user faints on
// the spot and the slot flag is set so the next switch-in is fully
// restored. Fails if the side has no live bench to receive the
// healing (no point fainting the user for a heal that can't trigger).
func applyHealingWishSetter(s *BattleState, side int, log *[]LogLine) {
	sd := &s.Sides[side]
	if sd.SlotConditions.HealingWish {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
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
	sd.SlotConditions.HealingWish = true
	*log = append(*log, LogLine{Type: "healingwish", Side: side,
		Text: fmt.Sprintf("%s is calling on the spirit of the past!", atk.Name)})
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
	*log = append(*log, LogLine{Type: "wish", Side: side,
		Text: fmt.Sprintf("%s's Wish came true!", payload.Healer)})
	healPokemon(active, side, payload.Amount, log)
}

// applySlotConditionsOnSwitchIn fires when a new Pokémon enters the
// slot. Healing Wish is consumed here — the incoming is fully
// restored and any non-volatile status cleared. Called from doSwitch
// after the entry-hazard / ability-on-switch-in passes so the heal
// undoes any switch-in chip and doesn't leave the new active in a
// half-finished state.
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
	sd.SlotConditions.HealingWish = false
	in.HP = in.MaxHP
	if in.Status != StatusNone {
		in.Status = StatusNone
		in.SleepTurns = 0
		in.ToxicCounter = 0
	}
	*log = append(*log, LogLine{Type: "healingwish", Side: side,
		Text: fmt.Sprintf("The healing wish came true for %s!", in.Name)})
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
	return out
}
