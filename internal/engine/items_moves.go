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
//
// Deferred to a follow-up, and listed in docs/battle-state.md: Fling and
// Natural Gift (both need a per-item data table synced from upstream), Pluck /
// Bug Bite / Incinerate (need the berry's own effect to fire for someone other
// than its holder), and Embargo / Magic Room (need every itemOf read in the
// engine to become suppression-aware, which is a much wider change).
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
	"poltergeist": true, "recycle": true,
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
func applyItemMoveAfterHit(s *BattleState, side int, m domain.Move, hitSub bool, log *[]LogLine) {
	if !itemMoveIDs[m.ID] {
		return
	}
	atk, def := s.Active(side), s.Active(1-side)
	if atk.Fainted || atk.HP <= 0 {
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
	}
}

// knockItemOff removes the target's item and destroys it. Nobody ends up
// holding it, and it is not recyclable — takeItem, not eatItem.
func knockItemOff(s *BattleState, atkSide int, def *Pokemon, defSide int, log *[]LogLine) {
	if def.Fainted {
		return
	}
	if !itemIsRemovable(def) {
		// Silent when the target simply has nothing; loud when an ability
		// refused, because that is information the attacker acted on.
		if def.Item != ItemNone {
			*log = append(*log, LogLine{
				Type: "ability", Side: defSide,
				Text: fmt.Sprintf("%s's ability kept hold of its item!", def.Name),
			})
		}
		return
	}
	name := itemDisplayName(def.Item)
	loseItem(def)
	*log = append(*log, LogLine{
		Type: "item", Side: defSide,
		Text: fmt.Sprintf("%s knocked off %s's %s!", s.Active(atkSide).Name, def.Name, name),
	})
}

// stealItem moves the target's item into an empty-handed attacker's slot.
func stealItem(s *BattleState, atkSide int, atk, def *Pokemon, defSide int, m domain.Move, log *[]LogLine) {
	if atk.Item != ItemNone || def.Fainted {
		return
	}
	if !itemIsRemovable(def) {
		if def.Item != ItemNone {
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
	if def.Fainted || bothSlotsEmpty(atk, def) {
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
	if atk.Item == ItemNone || def.Item != ItemNone || def.Fainted {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	kind := atk.Item
	loseItem(atk)
	giveItem(def, kind)
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
	if def.Fainted || def.Item == ItemNone {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if !itemIsRemovable(def) {
		*log = append(*log, LogLine{
			Type: "ability", Side: 1 - side,
			Text: fmt.Sprintf("%s's ability kept hold of its item!", def.Name),
		})
		return
	}
	name := itemDisplayName(def.Item)
	loseItem(def)
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
	*log = append(*log, LogLine{
		Type: "item", Side: side,
		Text: fmt.Sprintf("%s found one %s!", p.Name, itemDisplayName(kind)),
	})
}
