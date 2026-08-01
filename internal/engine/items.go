package engine

import (
	"fmt"
	"sort"

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
//	Recoil             — fraction of max HP the holder loses after a damaging
//	                     move connects (Life Orb 1/10). Suppressed when Sheer
//	                     Force boosted the move and by Magic Guard (see
//	                     lifeOrbRecoilApplies).
type Item struct {
	Kind ItemKind

	// Name is the display name used in log lines ("Leftovers"). It duplicates
	// domain.Item.Name deliberately: the engine emits log lines without a Dex
	// in hand, and every registry entry is asserted against the catalog by
	// TestItemNamesMatchCatalog so the two can't drift.
	Name string
	// Desc is the one-line player-facing explanation of what the item does,
	// served by ItemCatalog to the team builder and MCP agents. The engine
	// never reads it; it lives here so behavior and description are edited in
	// the same place and can't disagree.
	Desc string

	OutgoingDamageMult func(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState, typeEff float64) float64
	SpeedMult          func(p *Pokemon, weather *WeatherState) float64
	SurviveOHKO        func(def *Pokemon, damage int) (int, bool)
	EndOfTurn          func(s *BattleState, side int, log *[]LogLine)
	ChoiceLock         bool
	Recoil             float64
}

// Item slugs the engine models. Mirrors the AbilityKind const block: the
// catalog can list every curated item, but only those wired here fire hooks.
const (
	ItemLeftovers   ItemKind = "leftovers"
	ItemChoiceBand  ItemKind = "choice-band"
	ItemChoiceSpecs ItemKind = "choice-specs"
	ItemChoiceScarf ItemKind = "choice-scarf"
	ItemLifeOrb     ItemKind = "life-orb"
	ItemFocusSash   ItemKind = "focus-sash"
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
		Name: "Leftovers", Desc: "Restores 1/16 of max HP at the end of every turn.",
		EndOfTurn: func(s *BattleState, side int, log *[]LogLine) {
			itemHealFraction(s.Active(side), side, 1.0/16, "Leftovers", log)
		},
	},
	ItemChoiceBand: {
		Kind:       ItemChoiceBand,
		Name:       "Choice Band",
		Desc:       "Physical moves deal 1.5x damage, but the holder is locked into its first move until it switches out.",
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
		Name:       "Choice Specs",
		Desc:       "Special moves deal 1.5x damage, but the holder is locked into its first move until it switches out.",
		ChoiceLock: true,
		OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
			if m.Category == domain.CatSpecial {
				return 1.5
			}
			return 1
		},
	},
	ItemChoiceScarf: {
		Kind:       ItemChoiceScarf,
		Name:       "Choice Scarf",
		Desc:       "Speed is 1.5x, but the holder is locked into its first move until it switches out.",
		ChoiceLock: true,
		SpeedMult:  func(p *Pokemon, w *WeatherState) float64 { return 1.5 },
	},
	ItemLifeOrb: {
		Kind:   ItemLifeOrb,
		Name:   "Life Orb",
		Desc:   "Damaging moves deal 1.3x damage, but the holder loses 1/10 of max HP after each one connects.",
		Recoil: 1.0 / 10,
		// ×1.3 to every damaging move. computeDamage / ExpectedDamage only
		// reach this hook on damaging, non-fixed-damage moves, so the boost
		// never touches status or Seismic Toss-style moves.
		OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
			return 1.3
		},
	},
	ItemFocusSash: {
		Kind: ItemFocusSash,
		Name: "Focus Sash",
		Desc: "If the holder is at full HP, it survives an otherwise-lethal hit at 1 HP. Consumed on use.",
		// Identical clamp to Sturdy, but one-shot: a full-HP holder survives an
		// otherwise-lethal hit at 1 HP, then dealDamage consumes the sash.
		SurviveOHKO: func(def *Pokemon, damage int) (int, bool) {
			if def.HP != def.MaxHP || damage < def.HP {
				return damage, false
			}
			return def.HP - 1, true
		},
	},
}

// ItemInfo is one row of the player-facing item catalog: the slug a TeamPick
// carries, the display name, and what the item does. Served by the gateway at
// GET /api/items and mirrored into the MCP tool surface, so a team builder (or
// an agent) can discover the legal item set without reading the registry.
type ItemInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// ItemCatalog returns every catalog item, sorted by id, joined with the
// registry's description. Name comes from the catalog (the identity layer is
// the source of truth for display text); Desc comes from the registry, and is
// empty for an item the engine ships but doesn't model — which AuditItems
// reports and TestItemCoverage guards, so an empty Desc in the API response is
// a visible signal rather than a silent one.
func ItemCatalog(dex *domain.Dex) []ItemInfo {
	out := make([]ItemInfo, 0, len(dex.Items))
	for id, it := range dex.Items {
		info := ItemInfo{ID: it.ID, Name: it.Name}
		if reg, ok := itemRegistry[ItemKind(id)]; ok {
			info.Desc = reg.Desc
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
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
	*log = append(*log, LogLine{
		Type: "item", Side: side,
		Text: fmt.Sprintf("%s restored a little HP (%s, +%d).", p.Name, itemName, amt),
	})
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

// itemSpeedMult returns the holder's held-item speed multiplier (Choice Scarf
// ×1.5). 1.0 when unset. Mirrors abilitySpeedMult and sits beside it in
// effectiveSpeed.
func itemSpeedMult(p *Pokemon, weather *WeatherState) float64 {
	if it := itemOf(p); it != nil && it.SpeedMult != nil {
		return it.SpeedMult(p, weather)
	}
	return 1
}

// lifeOrbRecoilApplies reports whether the attacker takes Life Orb-style
// post-hit recoil for move m. The Sheer Force exclusion is the canonical
// quirk: Sheer Force strips a move's secondary before it resolves, and the
// Life Orb recoil trigger keys off that secondary — so a Sheer-Force-boosted
// move (the same predicate as Sheer Force's own damage boost: holder has the
// ability AND the move carries a secondary) deals ×1.69 with NO recoil. Magic
// Guard blocks the recoil like any other indirect damage.
func lifeOrbRecoilApplies(atk *Pokemon, m domain.Move) bool {
	it := itemOf(atk)
	if it == nil || it.Recoil <= 0 {
		return false
	}
	if abilityBlocksIndirectDamage(atk) {
		return false
	}
	if a := abilityOf(atk); a != nil && a.Kind == "sheer-force" && len(m.Secondaries) > 0 {
		return false
	}
	return true
}

// applyLifeOrbRecoil subtracts the holder's item Recoil fraction of max HP.
// Does not faint the holder — executeMove's existing atk-faint check handles
// that — so a recoil KO reports after the move's own faint resolution.
func applyLifeOrbRecoil(atk *Pokemon, side int, log *[]LogLine) {
	if atk.HP <= 0 {
		return
	}
	frac := itemOf(atk).Recoil
	amt := int(float64(atk.MaxHP) * frac)
	if amt < 1 {
		amt = 1
	}
	if amt > atk.HP {
		amt = atk.HP
	}
	atk.HP -= amt
	*log = append(*log, LogLine{
		Type: "item", Side: side,
		Text: fmt.Sprintf("%s was hurt by its Life Orb! (-%d)", atk.Name, amt),
	})
}

// itemSurviveOHKO clamps an otherwise-lethal hit when the defender holds an
// OHKO-survive item (Focus Sash). Returns (cappedDamage, fired); mirrors
// abilitySurviveOHKO. The caller (dealDamage) consumes the item when fired.
func itemSurviveOHKO(def *Pokemon, damage int) (int, bool) {
	if def == nil || damage <= 0 {
		return damage, false
	}
	if it := itemOf(def); it != nil && it.SurviveOHKO != nil {
		return it.SurviveOHKO(def, damage)
	}
	return damage, false
}

// consumeItem removes the holder's item (one-shot items like Focus Sash after
// they fire). itemOf returns nil afterward, so every dispatcher no-ops. An
// Unburden holder that just lost its item arms the Speed-doubling volatile.
func consumeItem(p *Pokemon) {
	had := p.Item != ItemNone
	p.Item = ItemNone
	if had {
		if a := abilityOf(p); a != nil && a.Kind == "unburden" {
			p.Volatiles.Unburden = true
		}
	}
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
