package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// items.go is the held-item layer. It mirrors the ability system (abilities.go):
// a Pokémon carries an ItemKind slug, and the engine consults a registry of
// optional hooks to decide what — if anything — the item does. Items the
// registry doesn't know about are inert holds (the slug rides through from the
// catalog the way an unimplemented ability slug does), so the data catalog can
// list an item a turn before its mechanics land.
//
// This file is the plumbing scaffold: the type, the registry, the lookup, and
// the hook surface the first curated items will use. The registry starts empty
// and the dispatch call sites are wired per item as each mechanic lands —
// keeping the foundation reviewable on its own before any behavior depends on
// it.

// ItemKind identifies a held item by slug (lowercase kebab-case, matching
// domain.Item.ID). The empty string means "no item held" and disables every
// hook.
type ItemKind string

const ItemNone ItemKind = ""

// Item is the registry record for one held item. Every field is optional; an
// item declares only the hooks it participates in, and the dispatchers (added
// alongside each item's wiring) nil-check so call sites stay tight. The hook
// surface deliberately mirrors the matching ability hooks so the integration
// points in computeDamage / effectiveSpeed / ResolveTurn can host both with
// the same shape.
//
// Hook timing reference (for the curated set this scaffold targets):
//
//	OutgoingDamageMult — computeDamage multiplier chain, attacker side
//	                     (Choice Band/Specs ×1.5 by category, Life Orb ×1.3)
//	SpeedMult          — effectiveSpeed (Choice Scarf ×1.5)
//	SurviveOHKO        — post-formula damage cap, defender side
//	                     (Focus Sash: survive a full-HP lethal hit at 1 HP, then consume)
//	EndOfTurn          — after weather residual + tick (Leftovers +1/16 heal)
//	ChoiceLock         — Choice Band/Specs/Scarf lock the holder into the first
//	                     move it picks until it switches out (see executeMove /
//	                     LegalActions). A flag, not a hook: the lock mechanic is
//	                     shared, only the paired stat boost differs per item.
type Item struct {
	Kind ItemKind

	OutgoingDamageMult func(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState, typeEff float64) float64
	SpeedMult          func(p *Pokemon, weather *WeatherState) float64
	SurviveOHKO        func(def *Pokemon, damage int) (int, bool)
	EndOfTurn          func(s *BattleState, side int, log *[]LogLine)
	ChoiceLock         bool
}

// Item slugs the engine models. Mirrors the AbilityKind const block: the
// catalog can list every curated item, but only those wired here fire hooks.
const (
	ItemLeftovers   ItemKind = "leftovers"
	ItemChoiceBand  ItemKind = "choice-band"
	ItemChoiceSpecs ItemKind = "choice-specs"
)

// itemRegistry maps slug → item spec. The catalog (data/items.json) can list
// every curated item; only those present here fire hooks. Adding an item =
// adding an entry once the matching hook integration point exists.
//
// Phase 0 (issue #30): Leftovers and Choice Band. Choice Scarf / Specs stay
// inert until their stat effect lands — registering them with only the shared
// ChoiceLock would ship a pure drawback ("don't ship what we can't honor").
var itemRegistry = map[ItemKind]*Item{
	ItemLeftovers: {
		Kind: ItemLeftovers,
		EndOfTurn: func(s *BattleState, side int, log *[]LogLine) {
			itemHealFraction(s.Active(side), side, 1.0/16, "Leftovers", log)
		},
	},
	ItemChoiceBand: {
		Kind:       ItemChoiceBand,
		ChoiceLock: true,
		OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
			if m.Category == domain.CatPhysical {
				return 1.5
			}
			return 1
		},
	},
	ItemChoiceSpecs: {
		Kind:       ItemChoiceSpecs,
		ChoiceLock: true,
		OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
			if m.Category == domain.CatSpecial {
				return 1.5
			}
			return 1
		},
	},
}

// itemOf returns the registry record for the Pokémon's held item, or nil when
// it holds nothing or holds an item the engine doesn't model yet. Every item
// dispatcher must tolerate nil.
func itemOf(p *Pokemon) *Item {
	if p.Item == ItemNone {
		return nil
	}
	return itemRegistry[p.Item]
}

// itemHealFraction heals p for frac of MaxHP, clamped, logging an "item" line.
// Mirrors the ability healFraction but tagged so the UI can style held-item
// recovery distinctly from ability recovery.
func itemHealFraction(p *Pokemon, side int, frac float64, itemName string, log *[]LogLine) {
	if p.HP >= p.MaxHP {
		return
	}
	amt := int(float64(p.MaxHP) * frac)
	if amt < 1 {
		amt = 1
	}
	if p.HP+amt > p.MaxHP {
		amt = p.MaxHP - p.HP
	}
	p.HP += amt
	*log = append(*log, LogLine{Type: "item", Side: side,
		Text: fmt.Sprintf("%s restored a little HP (%s, +%d).", p.Name, itemName, amt)})
}

// --- dispatchers (call from integration sites) ---

// applyItemEndOfTurn fires the holder's end-of-turn item tick, if any. Called
// after applyAbilityEndOfTurn in ResolveTurn (Leftovers +1/16 heal).
func applyItemEndOfTurn(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted {
		return
	}
	if it := itemOf(p); it != nil && it.EndOfTurn != nil {
		it.EndOfTurn(s, side, log)
	}
}

// itemOutgoingDamageMult returns the attacker-side held-item damage multiplier
// (Choice Band ×1.5 on physical). 1.0 when unset. Mirrors
// abilityOutgoingDamageMult and sits beside it in the computeDamage chain.
func itemOutgoingDamageMult(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState, typeEff float64) float64 {
	if it := itemOf(atk); it != nil && it.OutgoingDamageMult != nil {
		return it.OutgoingDamageMult(atk, m, def, weather, typeEff)
	}
	return 1
}

// isChoiceLockItem reports whether p holds a (modeled) Choice item that locks
// it into a single move. Drives the lock set/enforce logic in executeMove and
// LegalActions.
func isChoiceLockItem(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.ChoiceLock
}

// choiceLockedSlot returns the move-slot index the holder is choice-locked
// into, or -1 if it isn't locked (or the locked move is somehow no longer in
// its move list). Move IDs are unique per Pokémon (team validation forbids
// duplicates), so matching by ID is unambiguous.
func choiceLockedSlot(p *Pokemon) int {
	id := p.Volatiles.ChoiceLockMoveID
	if id == "" {
		return -1
	}
	for i := range p.Moves {
		if p.Moves[i].MoveID == id {
			return i
		}
	}
	return -1
}
