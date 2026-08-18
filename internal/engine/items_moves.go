package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// items_moves.go is the item-manipulation move family: the moves whose whole
// point is moving an item between slots or taking one off the field.
//
// These are moves, not items, so they don't live in the registry — but they are
// squarely part of the item feature, and until they existed the curated
// learnsets taught sixteen of them that resolved as plain damage or as nothing
// at all. Knock Off in particular is one of the most-used moves in real play and
// was a vanilla 65 BP hit here.
//
// What this file covers, and the exact gate each one uses:
//
//	knock-off      ×1.5 BP against a removable item, then removes it
//	thief / covet  steal the target's item if the user is empty-handed
//	trick /        swap items with the target; fails unless at least one side
//	switcheroo     holds something and both slots end up filled
//	bestow         hand the user's item to an empty-handed target
//	corrosive-gas  destroy the target's item outright
//	poltergeist    110 BP, fails outright against an empty-handed target
//	recycle        restore the item the user last *consumed*
//	fling          base power from the thrown item; the target eats a berry
//	natural-gift   type and power from the held berry
//	pluck /        eat the target's berry and gain its effect
//	bug-bite
//	incinerate     burn the target's berry up
//
// The two data tables Fling and Natural Gift read live in items_fling.go.
//
// Still deferred, and listed in docs/battle-state.md: Embargo and Magic Room.
// Both need every itemOf read in the engine to become suppression-aware, which
// is a much wider change than anything here — the volatile and the pseudo-
// weather already exist and tick, they just don't gate anything yet.
//
// Two rules that every entry here shares:
//
//   - A Substitute stops item theft. The doll is what got hit, and canon runs
//     the removal off the hit connecting with the target itself.
//   - Sticky Hold refuses. See itemIsRemovable for the one place this is
//     decided, and for the documented divergence from Showdown on Knock Off.

// itemMoveIDs is every move this file claims. Used by the dispatcher gates so a
// move that is *not* in the family costs one map lookup rather than a switch.
var itemMoveIDs = map[string]bool{
	"knock-off": true, "thief": true, "covet": true, "trick": true,
	"switcheroo": true, "bestow": true, "corrosive-gas": true,
	"poltergeist": true, "recycle": true, "fling": true,
	"natural-gift": true, "pluck": true, "bug-bite": true, "incinerate": true,
}

// isDown reports whether a Pokémon is out of the fight for the purpose of the
// item moves. Fainted alone is not enough: between the damage loop and the
// faint block in executeMove a killed Pokémon sits at HP 0 with Fainted still
// false, and every one of these moves runs inside that window.
//
// Getting this wrong was a real bug rather than a theoretical one. A thrown
// heal berry delivered to a target at 0 HP healed it off zero, which made the
// faint block's `def.HP <= 0` false — so Fling with an Oran or a Sitrus could
// never KO anything, and the victim ate the berry instead of fainting.
func isDown(p *Pokemon) bool {
	return p == nil || p.Fainted || p.HP <= 0
}

// knockOffBoosts reports whether Knock Off gets its 1.5× base-power bonus
// against this target. Canon keys the boost on the target *having* an item
// rather than on the removal succeeding, which is why this reads the slot
// directly instead of going through itemIsRemovable: a Sticky Hold holder keeps
// its item and still eats the bigger hit.
func knockOffBoosts(m domain.Move, def *Pokemon) bool {
	return m.ID == "knock-off" && def != nil && def.Item != ItemNone
}

// poltergeistFails reports whether Poltergeist should fail before the accuracy
// roll. Canon's onTry checks the target's item and nothing else — Sticky Hold
// is irrelevant, because the move never removes anything, it just needs
// something to throw around.
func poltergeistFails(m domain.Move, def *Pokemon) bool {
	return m.ID == "poltergeist" && (def == nil || def.Item == ItemNone)
}

// applyItemMoveAfterHit runs the damaging half of the family — the moves whose
// item effect lands after damage. Called from executeMove once the hit loop has
// resolved, with hitSub reporting whether a Substitute ate the hit.
//
// The user having fainted to a contact reaction (Rocky Helmet, Rough Skin) is
// checked here rather than by each move: canon gates all of these on the source
// still being alive, since a fainted thief cannot pocket anything.
func applyItemMoveAfterHit(s *BattleState, side int, m domain.Move, hitSub bool, rng *RNG, log *[]LogLine) {
	if !itemMoveIDs[m.ID] {
		return
	}
	atk, def := s.Active(side), s.Active(1-side)
	if isDown(atk) {
		return
	}
	// The doll took the hit, so nothing reached the target's belt.
	if hitSub {
		return
	}
	switch m.ID {
	case "knock-off":
		knockItemOff(s, side, def, 1-side, log)
	case "thief", "covet":
		stealItem(s, side, atk, def, 1-side, m, log)
	case "pluck", "bug-bite", "incinerate":
		applyBerryEatingMove(s, side, m, hitSub, rng, log)
	}
}

// knockItemOff removes the target's item and destroys it. Nobody ends up
// holding it, and it is not recyclable — takeItem, not eatItem.
func knockItemOff(s *BattleState, atkSide int, def *Pokemon, defSide int, log *[]LogLine) {
	if isDown(def) {
		return
	}
	if !itemIsRemovable(def) {
		// Silent when the target simply has nothing; loud when an ability
		// refused, because that is information the attacker acted on.
		if def.Item != ItemNone {
			revealAbility(def)
			*log = append(*log, LogLine{
				Type: "ability", Side: defSide,
				Text: fmt.Sprintf("%s's ability kept hold of its item!", def.Name),
			})
		}
		return
	}
	name := itemDisplayName(def.Item)
	loseItem(def)
	revealItem(def)
	*log = append(*log, LogLine{
		Type: "item", Side: defSide,
		Text: fmt.Sprintf("%s knocked off %s's %s!", s.Active(atkSide).Name, def.Name, name),
	})
}

// stealItem moves the target's item into an empty-handed attacker's slot.
func stealItem(s *BattleState, atkSide int, atk, def *Pokemon, defSide int, m domain.Move, log *[]LogLine) {
	if atk.Item != ItemNone || isDown(def) {
		return
	}
	if !itemIsRemovable(def) {
		if def.Item != ItemNone {
			revealAbility(def)
			*log = append(*log, LogLine{
				Type: "ability", Side: defSide,
				Text: fmt.Sprintf("%s's ability kept hold of its item!", def.Name),
			})
		}
		return
	}
	kind := def.Item
	loseItem(def)
	giveItem(atk, kind)
	revealItem(atk)
	revealItem(def)
	*log = append(*log, LogLine{
		Type: "item", Side: atkSide,
		Text: fmt.Sprintf("%s stole %s's %s with %s!", atk.Name, def.Name, itemDisplayName(kind), m.Name),
	})
}

// applyItemStatusMove runs the status half of the family. Returns whether it
// claimed the move, so applyStatusMove can fall through for anything else.
func applyItemStatusMove(s *BattleState, side int, m domain.Move, log *[]LogLine) (claimed bool) {
	switch m.ID {
	case "trick", "switcheroo":
		applyItemSwap(s, side, m, log)
	case "bestow":
		applyBestow(s, side, log)
	case "corrosive-gas":
		applyCorrosiveGas(s, side, log)
	case "recycle":
		applyRecycle(s, side, log)
	default:
		return false
	}
	return true
}

// applyItemSwap is Trick / Switcheroo: the two actives exchange slots. Fails if
// neither is holding anything (there is nothing to trade) or if the target's
// item is protected. A one-sided swap is legal and is how the move is usually
// used — handing a Choice Scarf to a wall.
func applyItemSwap(s *BattleState, side int, m domain.Move, log *[]LogLine) {
	atk, def := s.Active(side), s.Active(1-side)
	if isDown(def) || bothSlotsEmpty(atk, def) {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	// A Substitute blocks the swap outright: Trick carries neither the sound nor
	// the bypass-sub flag, so the doll is what it reaches.
	if hasSubstitute(def) && !bypassesSubstitute(m, atk) {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	// Sticky Hold refuses to let go, and a swap needs both halves to move.
	if def.Item != ItemNone && !itemIsRemovable(def) {
		revealAbility(def)
		*log = append(*log, LogLine{
			Type: "ability", Side: 1 - side,
			Text: fmt.Sprintf("%s's ability kept hold of its item!", def.Name),
		})
		return
	}
	mine, theirs := atk.Item, def.Item
	loseItem(atk)
	loseItem(def)
	giveItem(atk, theirs)
	giveItem(def, mine)
	revealItem(atk)
	revealItem(def)
	*log = append(*log, LogLine{
		Type: "item", Side: side,
		Text: fmt.Sprintf("%s switched items with %s!", atk.Name, def.Name),
	})
	describeSwap(atk, side, log)
	describeSwap(def, 1-side, log)
}

// bothSlotsEmpty is the "nothing to trade" case Trick fails on.
func bothSlotsEmpty(a, b *Pokemon) bool {
	return a.Item == ItemNone && b.Item == ItemNone
}

// describeSwap logs what a side ended up with, so the two "obtained" lines read
// the way canon's paired -item messages do. Silent for an empty slot.
func describeSwap(p *Pokemon, side int, log *[]LogLine) {
	if p.Item == ItemNone {
		return
	}
	revealItem(p)
	*log = append(*log, LogLine{
		Type: "item", Side: side,
		Text: fmt.Sprintf("%s obtained one %s.", p.Name, itemDisplayName(p.Item)),
	})
}

// applyBestow hands the user's item to the target. Fails if the user has
// nothing to give or the target's hands are already full. Sticky Hold is not
// consulted: nothing is being taken *from* the target.
func applyBestow(s *BattleState, side int, log *[]LogLine) {
	atk, def := s.Active(side), s.Active(1-side)
	if atk.Item == ItemNone || def.Item != ItemNone || isDown(def) {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	kind := atk.Item
	loseItem(atk)
	giveItem(def, kind)
	revealItem(atk)
	revealItem(def)
	*log = append(*log, LogLine{
		Type: "item", Side: 1 - side,
		Text: fmt.Sprintf("%s received %s from %s!", def.Name, itemDisplayName(kind), atk.Name),
	})
}

// applyCorrosiveGas melts the target's item. Same destruction as Knock Off —
// nobody ends up holding it — but it is the status-move form, so it has to
// announce its own failure.
func applyCorrosiveGas(s *BattleState, side int, log *[]LogLine) {
	def := s.Active(1 - side)
	if isDown(def) || def.Item == ItemNone {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if !itemIsRemovable(def) {
		revealAbility(def)
		*log = append(*log, LogLine{
			Type: "ability", Side: 1 - side,
			Text: fmt.Sprintf("%s's ability kept hold of its item!", def.Name),
		})
		return
	}
	name := itemDisplayName(def.Item)
	loseItem(def)
	revealItem(def)
	*log = append(*log, LogLine{
		Type: "item", Side: 1 - side,
		Text: fmt.Sprintf("%s's %s was corroded away!", def.Name, name),
	})
}

// applyRecycle restores the item the user last consumed. Fails if the user is
// already holding something or has consumed nothing this battle. The memory is
// spent on use, so Recycle twice off one berry does not work.
func applyRecycle(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p.Item != ItemNone || p.LastConsumedItem == ItemNone {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	kind := p.LastConsumedItem
	// giveItem clears LastConsumedItem, which is exactly right: the memory is
	// spent, and what the holder now has is a real item again.
	giveItem(p, kind)
	revealItem(p)
	*log = append(*log, LogLine{
		Type: "item", Side: side,
		Text: fmt.Sprintf("%s found one %s!", p.Name, itemDisplayName(kind)),
	})
}

// --- Fling, Natural Gift, and the berry-eating moves ---

// flingBasePower returns the base power Fling gets from the user's item, and
// whether the move can be thrown at all. An item with no entry cannot be flung
// — canon has a fixed table and anything off it simply fails.
func flingBasePower(p *Pokemon) (int, bool) {
	if p == nil || p.Item == ItemNone {
		return 0, false
	}
	bp, ok := flingPower[p.Item]
	return bp, ok && bp > 0
}

// naturalGiftFor returns the type and power Natural Gift takes from the user's
// held berry. Non-berries have no entry, which is how the move fails.
func naturalGiftFor(p *Pokemon) (domain.Type, int, bool) {
	if p == nil || p.Item == ItemNone {
		return "", 0, false
	}
	e, ok := naturalGift[p.Item]
	return e.Type, e.Power, ok
}

// applyItemMovePrepare rewrites a move's type and power from the user's item
// and spends the item, returning what was thrown. Called from executeMove
// before the accuracy roll, which is where canon's onPrepareHit sits.
//
// The item is consumed *here*, not at the end of the move, and that placement
// is load-bearing. Deferring it left the thrown item live for the whole throw:
// a Life Orb gave its ×1.3 and took its recoil for the orb being thrown, and —
// worse — the pinch check at the tail of executeMove let the user eat the very
// berry it was supposed to be throwing, so the target never received it and the
// user healed off the move instead.
//
// The move is passed by pointer because both of these replace fields on the
// caller's local copy — the same copy the damage formula reads.
func applyItemMovePrepare(s *BattleState, side int, m *domain.Move, log *[]LogLine) (thrown ItemKind, failed bool) {
	if m.ID != "fling" && m.ID != "natural-gift" {
		return ItemNone, false
	}
	atk := s.Active(side)
	// Canon gates both on ignoringItem: an Embargoed, Magic-Roomed or Klutzed
	// holder cannot throw what it cannot use. This is the one place in the
	// family that consults suppression — the theft moves all read the raw slot
	// on purpose, because a suppressed item is still there to be taken.
	if itemSuppressed(atk) {
		return ItemNone, true
	}
	switch m.ID {
	case "fling":
		bp, ok := flingBasePower(atk)
		if !ok {
			return ItemNone, true
		}
		m.Power = bp
	case "natural-gift":
		t, bp, ok := naturalGiftFor(atk)
		if !ok {
			return ItemNone, true
		}
		m.Type, m.Power = t, bp
	}
	thrown = atk.Item
	name := itemDisplayName(thrown)
	// consumeItem, not loseItem: canon treats both moves as using the item up,
	// so Recycle can hand it back.
	consumeItem(atk)
	revealItem(atk)
	*log = append(*log, LogLine{
		Type: "item", Side: side,
		Text: fmt.Sprintf("%s used up its %s!", atk.Name, name),
	})
	return thrown, false
}

// applyItemMoveDelivery feeds a thrown berry to the target. Separate from the
// spending above because the two have different gates: the item is spent the
// moment the move is committed (a missed Fling still wastes the Choice Scarf),
// but the berry only reaches a target the move actually connected with.
//
// Natural Gift converts its berry to energy rather than throwing it, so nobody
// eats anything and this is a no-op for it.
func applyItemMoveDelivery(s *BattleState, side int, m domain.Move, thrown ItemKind, connected bool, rng *RNG, log *[]LogLine) {
	if m.ID != "fling" || thrown == ItemNone || !connected {
		return
	}
	flingBerryOnto(s, 1-side, thrown, rng, log)
}

// flingBerryOnto feeds a thrown berry to the target.
//
// Canon activates the berry for the target *even if its usual trigger condition
// is not satisfied* — a full-HP target still eats a thrown Sitrus, and a thrown
// Liechi genuinely hands the opponent +1 Attack. That last part is not a bug in
// this function; it is the well-known trap that makes Fling a bad idea with a
// stat berry, and both hooks below are fired on those terms.
//
// The damage-reaction berries (Jaboca, Rowap, Kee, Maranga) hang off OnHitTaken
// rather than these two hooks, and have no meaning for a berry thrown at
// someone, so they are not fired.
func flingBerryOnto(s *BattleState, tgtSide int, kind ItemKind, rng *RNG, log *[]LogLine) {
	it := itemRegistry[kind]
	if it == nil || !it.Berry {
		return
	}
	tgt := s.Active(tgtSide)
	if isDown(tgt) {
		return
	}
	// no-reveal: the berry was the *thrower's*, already revealed as it was
	// no-reveal: spent. tgt only eats it — its own held slot stays hidden.
	*log = append(*log, LogLine{
		Type: "item", Side: tgtSide,
		Text: fmt.Sprintf("%s ate the thrown %s!", tgt.Name, it.Name),
	})
	if it.OnStatus != nil {
		it.OnStatus(tgt, tgtSide, log)
	}
	// The heal berries hang their restore off OnHPThreshold, which reads the
	// holder's HP. Firing it on a target above the threshold is exactly what
	// canon does for a thrown berry, so the dispatcher's own gate is bypassed
	// deliberately here.
	if it.OnHPThreshold != nil && tgt.HP < tgt.MaxHP {
		it.OnHPThreshold(s, tgtSide, rng, log)
	}
}

// applyBerryEatingMove is Pluck / Bug Bite (eat the target's berry and gain its
// effect) and Incinerate (burn it up). All three run after damage, off the same
// connecting-hit gate as the theft moves.
func applyBerryEatingMove(s *BattleState, side int, m domain.Move, hitSub bool, rng *RNG, log *[]LogLine) {
	eat := m.ID == "pluck" || m.ID == "bug-bite"
	if !eat && m.ID != "incinerate" {
		return
	}
	atk, def := s.Active(side), s.Active(1-side)
	if hitSub || isDown(atk) || isDown(def) {
		return
	}
	it := itemRegistry[def.Item]
	if it == nil || !it.Berry {
		return
	}
	// Sticky Hold holds on to a berry the same way it holds on to anything —
	// Incinerate included, since the berry has to leave the belt to burn.
	if !itemIsRemovable(def) {
		revealAbility(def)
		*log = append(*log, LogLine{
			Type: "ability", Side: 1 - side,
			Text: fmt.Sprintf("%s's ability kept hold of its item!", def.Name),
		})
		return
	}
	name := it.Name
	loseItem(def)
	if !eat {
		revealItem(def)
		*log = append(*log, LogLine{
			Type: "item", Side: 1 - side,
			Text: fmt.Sprintf("%s's %s was burnt up!", def.Name, name),
		})
		return
	}
	revealItem(def)
	*log = append(*log, LogLine{
		Type: "item", Side: side,
		Text: fmt.Sprintf("%s stole and ate %s's %s!", atk.Name, def.Name, name),
	})
	// The eater gets the berry's effect, on the same unconditional terms as a
	// thrown berry: it never held the thing, so its own thresholds never applied.
	if it.OnStatus != nil {
		it.OnStatus(atk, side, log)
	}
	if it.OnHPThreshold != nil && atk.HP < atk.MaxHP {
		it.OnHPThreshold(s, side, rng, log)
	}
}
