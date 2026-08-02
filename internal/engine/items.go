package engine

import (
	"fmt"
	"math"
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
// This file owns the framework only: the type, the hook surface, the registry
// and its dispatchers. The items themselves live in sibling files grouped by
// what they do — items_core.go (the original curated six), items_berries.go —
// each registering its entries from an init(), the same pattern the volatile
// and side-condition handlers already use. Splitting by family keeps a diff
// that adds one item family from touching every other item.

// ItemKind identifies a held item by slug (lowercase kebab-case, matching
// domain.Item.ID). The empty string means "no item held" and disables every
// hook.
type ItemKind string

const ItemNone ItemKind = ""

// Item is the registry record for one held item. Every field is optional; an
// item declares only the hooks it participates in, and the dispatchers
// nil-check so call sites stay tight. The hook surface deliberately mirrors the
// matching ability hooks so the integration points in computeDamage /
// effectiveSpeed / ResolveTurn can host both with the same shape.
//
// Hook timing reference:
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
//	ResistType         — declarative one-shot damage halving on the defender
//	                     (type-resist berries). Read by computeDamage and by
//	                     the consume decision in dealDamage through the same
//	                     predicate, so the two can't disagree.
//	OnHPThreshold      — the holder's HP dropped to or below HPThreshold × max
//	                     (pinch berries). Checked at every point HP can fall.
//	OnStatus           — the holder just gained a non-volatile status or
//	                     confusion (status-cure berries).
//	OnHitTaken         — a damaging move connected on the holder (Enigma /
//	                     Jaboca / Rowap / Kee / Maranga berries).
//	OnHitTakenPassive  — same trigger, permanent item (Rocky Helmet)
//	OnDealtDamage      — the holder's move connected, attacker side (Shell Bell)
//	StatMult           — offensiveDefensiveStats, per stat (Assault Vest, Thick Club)
//	CritStage          — added to the crit-stage total in computeDamage
//	DrainMult          — scales an HP-draining move's recovery (Big Root)
//	SuppressesContact  — the holder's moves stop counting as contact (Punching Glove)
//	BlocksStatusMoves  — the holder may not select a status move (Assault Vest)
//	SurviveOHKOChance  — percent chance to survive a lethal hit at 1 HP (Focus Band)
//
// A hook that returns bool reports "I fired, consume me": the dispatcher then
// logs the consume line ahead of whatever the hook logged and removes the item.
// Returning false must leave no trace — the hook is re-checked at the next
// trigger point.
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
	// Berry marks the item as a Berry, which only changes the consume log line
	// ("ate its Sitrus Berry" vs "used its White Herb"). Kept explicit rather
	// than sniffed off the slug so Berry Juice (a drink, not a berry) and any
	// future non-"-berry" berry are both right.
	Berry bool

	OutgoingDamageMult func(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState, typeEff float64) float64
	SpeedMult          func(p *Pokemon, weather *WeatherState) float64
	SurviveOHKO        func(def *Pokemon, damage int) (int, bool)
	EndOfTurn          func(s *BattleState, side int, log *[]LogLine)
	ChoiceLock         bool
	Recoil             float64

	// ResistType is the attacking type a resist berry softens to half damage,
	// and ResistAnyEffectiveness lifts the "only on a super-effective hit"
	// gate (Chilan Berry halves every Normal hit; the other sixteen only fire
	// when the matchup is super-effective).
	ResistType             domain.Type
	ResistAnyEffectiveness bool

	// HPThreshold is the fraction of max HP at or below which OnHPThreshold
	// fires. Meaningless without OnHPThreshold, and a zero threshold with a
	// hook set would never fire — TestPinchItemsDeclareAThreshold guards both.
	HPThreshold   float64
	OnHPThreshold func(s *BattleState, side int, rng *RNG, log *[]LogLine) bool

	OnStatus   func(p *Pokemon, side int, log *[]LogLine) bool
	OnHitTaken func(s *BattleState, defSide int, m domain.Move, res DamageResult, log *[]LogLine) bool

	// OnHitTakenPassive is the permanent counterpart of OnHitTaken: same
	// trigger, but the item is never consumed (Rocky Helmet chips every contact
	// attacker, not just the first). Kept as a separate field rather than a
	// bool on OnHitTaken because "consume me" and "I am permanent" are
	// genuinely different contracts, and folding them into one signature with
	// an ignored return is how a permanent item eventually gets eaten.
	OnHitTakenPassive func(s *BattleState, defSide int, m domain.Move, res DamageResult, log *[]LogLine)

	// OnDealtDamage fires on the *attacker* after its move connects, with the
	// damage it dealt and the move that dealt it (Shell Bell's drain, the
	// flinch items' added chance).
	OnDealtDamage func(s *BattleState, atkSide, dmg int, m domain.Move, rng *RNG, log *[]LogLine)

	// OnMoveUsed fires on the attacker once its move has reached its target
	// (Throat Spray). NOT on a miss, and not on a move stopped short of the
	// target — see applyItemOnMoveUsed for the exact gate before writing a new
	// one of these. OnMoveMissed is the separate hook for a failed accuracy roll
	// (Blunder Policy). Both are one-shot.
	OnMoveUsed   func(s *BattleState, side int, m domain.Move, log *[]LogLine) bool
	OnMoveMissed func(s *BattleState, side int, m domain.Move, log *[]LogLine) bool

	// OnStatCheck is the "something about my stats or restrictions changed"
	// trigger the herbs use. Checked wherever a pinch item is, since a stat drop
	// or a Taunt can land at any of those points. One-shot.
	OnStatCheck func(p *Pokemon, side int, log *[]LogLine) bool

	// AccuracyMult scales the holder's own accuracy rolls (Wide Lens); zero
	// means unset. AccuracyMultIf is the state-dependent form, for Zoom Lens,
	// which only pays out when the holder moves second. AccuracyMultVs scales
	// the accuracy of moves aimed *at* the holder (Bright Powder, Lax Incense).
	AccuracyMult   float64
	AccuracyMultIf func(s *BattleState, side int) float64
	AccuracyMultVs float64

	// QuickDrawChance is a percent chance, rolled at the top of each turn, that
	// the holder moves first within its priority bracket (Quick Claw). MovesLast
	// is the opposite and needs no roll (Lagging Tail, Full Incense). Both ride
	// the same bracket-precedence machinery Custap Berry uses.
	QuickDrawChance int
	MovesLast       bool

	// MinMultihit raises the floor on a variable multi-hit move's strike count
	// (Loaded Dice). Zero means unset.
	MinMultihit int

	// DrainFraction is the share of a move's *total* damage the holder recovers
	// once the move has fully resolved (Shell Bell's 1/8). Distinct from
	// OnDealtDamage, which fires per strike: a fraction of the total is not the
	// sum of the fractions once integer truncation is involved, and canon takes
	// the total. Zero means unset.
	DrainFraction float64

	// StatMult scales one of the holder's battle stats where the damage formula
	// reads it. stat is a stagePtr slug ("attack", "defense", "spatk",
	// "spdef"); Speed goes through SpeedMult instead, since turn order reads it
	// outside the formula. Assault Vest (SpD ×1.5), Thick Club (Atk ×2).
	StatMult func(p *Pokemon, stat string) float64

	// CritStage adds to the holder's critical-hit stage. A function rather than
	// an int so the species-locked relics (Lucky Punch, Leek) can return 0 for
	// the wrong holder instead of needing their own hook.
	CritStage func(p *Pokemon) int

	// DrainMult scales how much an HP-draining move restores (Big Root ×1.3).
	// Zero means unset and the dispatcher reads it as 1.
	DrainMult float64

	// SuppressesContact reports, per move, whether the holder's item strips the
	// contact flag from it — so contact-reactive effects on the target don't
	// fire. Per-move rather than a flag because the items differ in scope:
	// Punching Glove only decontacts punches (a gloved Body Slam still makes
	// contact), while Protective Pads covers everything the holder throws.
	SuppressesContact func(m domain.Move) bool

	// BlocksStatusMoves bars the holder from selecting a status move (Assault
	// Vest). Enforced in LegalActions so the option never appears, and again in
	// executeMove so a controller that ignores the legal set still can't use one.
	BlocksStatusMoves bool

	// SurviveOHKOChance is a percent chance to survive an otherwise-lethal hit
	// at 1 HP, from any starting HP and without being consumed (Focus Band).
	// Distinct from SurviveOHKO, the deterministic one-shot clamp Focus Sash
	// uses — the difference between the two items is exactly this.
	SurviveOHKOChance int

	// EndOfTurnLate is the second residual slot, run after everything else in
	// the turn. Canon splits the item residuals in two and the gap matters:
	// Leftovers and Black Sludge heal at order 5, ahead of the poison and burn
	// chip they are meant to out-pace, while Flame Orb, Toxic Orb and Sticky
	// Barb sit at the very end so the turn they fire costs the holder nothing.
	EndOfTurnLate func(s *BattleState, side int, rng *RNG, log *[]LogLine)

	// Field-duration extenders. Read by the matching setter at the moment the
	// condition is created, from the item held by the Pokémon that set it —
	// snapshotting, because the setter may faint long before the timer runs
	// out. ExtendsWeather names the one weather the rock covers; the empty
	// WeatherKind means "extends nothing".
	ExtendsScreens bool
	ExtendsTerrain bool
	ExtendsWeather WeatherKind

	// Immunity grants. Each names a specific gate the engine already has, and
	// each is consulted by a dispatcher rather than read inline at the gate.
	IgnoresHazards    bool // Heavy-Duty Boots: entry hazards skip the holder
	ImmuneToSandstorm bool // Safety Goggles: no sandstorm chip
	BlocksPowder      bool // Safety Goggles: powder-flagged moves don't affect the holder
	AllowsSwitchOut   bool // Shed Shell: trapping never applies
	Grounds           bool // Iron Ball: the holder is grounded even if it would float
	Floats            bool // Air Balloon: the holder is ungrounded until the balloon pops
	IgnoresWeather    bool // Utility Umbrella: rain and sun don't reach the holder
	//
	// LiftsOwnImmunities (Ring Target) is the inverse — it *removes* the
	// holder's type-chart immunities, so a Ghost holding one takes Normal hits.
	LiftsOwnImmunities bool
	//
	// BlocksSecondaries (Covert Cloak) refuses the added effects of attacks
	// aimed at the holder, exactly like the Shield Dust ability.
	BlocksSecondaries bool
	// BlocksStatDrops (Clear Amulet) refuses foe-induced stat drops, like Clear
	// Body. Self-inflicted drops still apply.
	BlocksStatDrops bool

	// TypeImmunity overrides the type chart for moves aimed at the holder,
	// same shape as the ability hook (Air Balloon's Ground immunity).
	TypeImmunity func(atkType domain.Type) (mult float64, override bool)
}

// itemRegistry maps slug → item spec. The catalog (data/items.json) can list
// every curated item; only those present here fire hooks. Populated by
// registerItem from each item file's init().
var itemRegistry = map[ItemKind]*Item{}

// registerItem adds one item to the registry. Panics on a duplicate slug: two
// files claiming the same item is a merge accident whose surviving half would
// otherwise depend on package init order.
func registerItem(it *Item) {
	if _, dup := itemRegistry[it.Kind]; dup {
		panic(fmt.Sprintf("item %q registered twice", it.Kind))
	}
	itemRegistry[it.Kind] = it
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
	if p == nil || p.Item == ItemNone || itemSuppressed(p) {
		return nil
	}
	return itemRegistry[p.Item]
}

// itemSuppressed reports whether the holder's item does nothing right now. The
// item is still *held* — it can be knocked off, stolen, traded, and it still
// fills the slot for Acrobatics and Unburden — it just has no effect.
//
// Three sources, matching canon's ignoringItem:
//
//	Embargo     a per-Pokémon volatile, five turns
//	Magic Room  field-wide, mirrored onto the active (see MagicRoomHere)
//	Klutz       the holder's ability, for as long as it has it
//
// Reading the raw slot rather than itemOf is deliberate everywhere it happens:
// Acrobatics does not double under Magic Room, Knock Off still removes a
// suppressed item, and an Unburden holder under Embargo is still "holding
// something" and stays slow.
func itemSuppressed(p *Pokemon) bool {
	if p == nil {
		return false
	}
	if p.Volatiles.Embargo != nil || p.Volatiles.MagicRoomHere {
		return true
	}
	a := abilityOf(p)
	return a != nil && a.Kind == "klutz"
}

// syncMagicRoomFlags pushes the field's Magic Room state onto both actives. The
// only writer of MagicRoomHere. Called wherever either half can change: the
// setter, the expiry tick, and every switch-in (a fresh active arrives with
// Volatiles zeroed and has to be told).
func syncMagicRoomFlags(s *BattleState) {
	up := s.PseudoWeather.MagicRoom != nil
	for i := 0; i < 2; i++ {
		s.Active(i).Volatiles.MagicRoomHere = up
	}
}

// itemHealFraction heals p for frac of MaxHP, clamped, logging an "item" line.
// Mirrors the ability healFraction but tagged so the UI can style held-item
// recovery distinctly from ability recovery.
func itemHealFraction(p *Pokemon, side int, frac float64, itemName string, log *[]LogLine) {
	amt := int(float64(p.MaxHP) * frac)
	itemHealAmount(p, side, amt, itemName, log)
}

// itemHealAmount heals p for an absolute amount (Oran's flat 10 HP, Berry
// Juice's 20), clamped to MaxHP, with the same log shape as
// itemHealFraction. Both round a sub-1 heal up to 1 so a heal never silently
// no-ops on integer truncation.
func itemHealAmount(p *Pokemon, side, amt int, itemName string, log *[]LogLine) {
	if p.HP >= p.MaxHP {
		return
	}
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

// itemDamage subtracts amt HP from p as held-item chip damage (Sticky Barb,
// Jaboca / Rowap recoil), clamped to the current HP so it can bring the holder
// to exactly 0. It does NOT faint the holder — callers decide when the faint
// resolves relative to the rest of their sequence.
// format takes the holder's name and the amount, in that order — the name is
// never baked into the format string, so a species name containing a percent
// sign can't corrupt the line.
func itemDamage(p *Pokemon, side, amt int, format string, log *[]LogLine) {
	if p.HP <= 0 {
		return
	}
	if amt < 1 {
		amt = 1
	}
	if amt > p.HP {
		amt = p.HP
	}
	p.HP -= amt
	*log = append(*log, LogLine{Type: "item", Side: side, Text: fmt.Sprintf(format, p.Name, amt)})
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

// itemResistBerryApplies reports whether the defender's held resist berry
// softens move m at the effectiveness the type chart produced. This single
// predicate is the whole contract: computeDamage / ExpectedDamage consult it
// for the ×0.5, and dealDamage consults it to decide whether to consume the
// berry and log its line. Any condition checked in one place and not the other
// is a berry that halves damage without being spent (or is spent without
// halving), so every gate belongs here and nowhere else.
//
// The gates, and why each one is a gate:
//
//   - Type must match, and — for every berry except Chilan — the hit must be
//     super-effective. Chilan answers any Normal hit, since nothing is weak to
//     Normal (ResistAnyEffectiveness).
//   - Status moves deal no damage to soften.
//   - A Substitute takes the hit in the holder's place, so the berry neither
//     reduces nor fires. Sound and bypass-sub moves go through the doll and do
//     reach the holder, which is why this asks bypassesSubstitute rather than
//     just whether a doll is up.
//   - An OHKO move ignores the damage formula entirely (dealDamage overwrites
//     the computed figure with the target's full HP), so halving it is a no-op
//     — and a berry consumed for a no-op is strictly worse than not having one.
func itemResistBerryApplies(atk, def *Pokemon, m domain.Move, typeEff float64) bool {
	it := itemOf(def)
	if it == nil || it.ResistType == "" {
		return false
	}
	if m.Category == domain.CatStatus || m.Type != it.ResistType {
		return false
	}
	if m.OHKO != "" {
		return false
	}
	if hasSubstitute(def) && !bypassesSubstitute(m, atk) {
		return false
	}
	return it.ResistAnyEffectiveness || typeEff > 1
}

// itemIncomingDamageMult is the defender-side held-item multiplier in the
// damage chain: ×0.5 from a resist berry that applies, 1.0 otherwise.
func itemIncomingDamageMult(atk, def *Pokemon, m domain.Move, typeEff float64) float64 {
	if itemResistBerryApplies(atk, def, m, typeEff) {
		return 0.5
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
	frac := itemOf(atk).Recoil
	itemDamage(atk, side, int(float64(atk.MaxHP)*frac),
		"%s was hurt by its Life Orb! (-%d)", log)
}

// itemSurviveOHKO clamps an otherwise-lethal hit when the defender holds an
// OHKO-survive item (Focus Sash). Returns (cappedDamage, fired); mirrors
// abilitySurviveOHKO. The caller (dealDamage) consumes the item when fired.
func itemSurviveOHKO(def *Pokemon, damage int, rng *RNG) (int, bool) {
	if def == nil || damage <= 0 {
		return damage, false
	}
	it := itemOf(def)
	if it == nil {
		return damage, false
	}
	if it.SurviveOHKO != nil {
		return it.SurviveOHKO(def, damage)
	}
	// Focus Band: a chance to hang on from any HP, and the item survives to try
	// again. The roll happens only on a hit that would actually be lethal, so a
	// Focus Band holder consumes no RNG on a normal exchange — battles where it
	// never comes up replay identically to battles without it.
	// No `def.HP > 1` guard: canon rolls whenever the hit would be lethal, and
	// a holder already at 1 HP is the case the band matters most in (the Sash
	// or Sturdy survivor that just got clipped). At 1 HP the clamp yields 0
	// damage, which is exactly right.
	if it.SurviveOHKOChance > 0 && damage >= def.HP && rng.Chance(it.SurviveOHKOChance) {
		return def.HP - 1, true
	}
	return damage, false
}

// consumeItem removes the holder's item because it was *used up* — a berry
// eaten, a one-shot like Focus Sash spent. itemOf returns nil afterward, so
// every dispatcher no-ops.
//
// Two side effects, and the distinction between them is the whole reason this
// is three functions rather than one:
//   - Unburden arms. Any item loss does this, so loseItem does it too.
//   - LastConsumedItem records the slug, which is what Recycle restores. Only
//     a consumption does this: canon will not recycle an item that was knocked
//     off, stolen, or given away.
func consumeItem(p *Pokemon) {
	if p.Item == ItemNone {
		return
	}
	p.LastConsumedItem = p.Item
	loseItem(p)
}

// loseItem removes the holder's item because it *left* — knocked off, stolen,
// traded, handed over. Arms Unburden like a consumption does, but leaves
// LastConsumedItem alone so Recycle can't launder a stolen item back.
func loseItem(p *Pokemon) {
	if p.Item == ItemNone {
		return
	}
	p.Item = ItemNone
	if a := abilityOf(p); a != nil && a.Kind == "unburden" {
		p.Volatiles.Unburden = true
	}
}

// giveItem puts an item in the holder's slot. Clears the recycle memory the way
// canon's setItem clears lastItem — once you are holding something new, the
// thing you ate three turns ago is no longer what Recycle would hand back.
//
// Unburden's flag is deliberately NOT cleared here: canon keeps the volatile
// and gates the Speed doubling on the slot still being empty, which is where
// the check lives (see the unburden ability).
func giveItem(p *Pokemon, kind ItemKind) {
	p.Item = kind
	p.LastConsumedItem = ItemNone
}

// itemIsRemovable reports whether p's item can be taken by a foe. Sticky Hold
// refuses; so does an empty slot. Canon also protects Mega Stones and Z-Crystals
// from their rightful owner, neither of which is in this dataset.
//
// Divergence worth naming: Showdown's sticky-hold onTakeItem exempts Knock Off
// specifically, so Knock Off removes an item through Sticky Hold there. Every
// other reference — and the reason the ability exists — says Sticky Hold stops
// the removal while Knock Off still collects its damage boost. We follow the
// latter, so knockOffBoosts and this predicate deliberately disagree.
func itemIsRemovable(p *Pokemon) bool {
	if p == nil || p.Item == ItemNone {
		return false
	}
	if a := abilityOf(p); a != nil && a.Kind == "sticky-hold" {
		return false
	}
	return true
}

// consumeItemAnnounced removes the item and logs the canonical consume line
// ahead of whatever the effect itself logged: "Snorlax ate its Sitrus Berry!"
// for a Berry, "Snorlax used its White Herb!" otherwise. Callers that need the
// effect's own lines to follow build them into a scratch slice and append it
// after this call — see fireItemTrigger.
func consumeItemAnnounced(p *Pokemon, side int, it *Item, log *[]LogLine) {
	verb := "used"
	if it.Berry {
		verb = "ate"
	}
	*log = append(*log, LogLine{
		Type: "item", Side: side,
		Text: fmt.Sprintf("%s %s its %s!", p.Name, verb, it.Name),
	})
	consumeItem(p)
}

// fireItemTrigger runs a one-shot item hook and, if it reports firing, emits
// the consume line *before* the hook's own log lines and removes the item.
// Buffering into a scratch slice is what lets the log read in canonical order
// ("ate its Sitrus Berry!" then "restored 62 HP") while the hook still decides
// whether it fires at all.
func fireItemTrigger(p *Pokemon, side int, it *Item, log *[]LogLine, fn func(sub *[]LogLine) bool) {
	var sub []LogLine
	if !fn(&sub) {
		return
	}
	consumeItemAnnounced(p, side, it, log)
	*log = append(*log, sub...)
}

// applyItemHPTrigger fires the holder's pinch item if its HP has fallen to or
// below the declared threshold. Called from every point HP can drop — the
// damage step, the post-move recoil tail, end-of-turn residuals, and hazard
// chip on switch-in — because canon activates a pinch berry the moment the
// effect that lowered HP finishes resolving, not at a fixed point in the turn.
// A fainted holder never eats.
func applyItemHPTrigger(s *BattleState, side int, rng *RNG, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted || p.HP <= 0 {
		return
	}
	it := itemOf(p)
	if it == nil || it.OnHPThreshold == nil {
		return
	}
	if float64(p.HP) > pinchThresholdFor(p, it.HPThreshold)*float64(p.MaxHP) {
		return
	}
	fireItemTrigger(p, side, it, log, func(sub *[]LogLine) bool {
		return it.OnHPThreshold(s, side, rng, sub)
	})
}

// pinchThresholdFor returns the HP fraction a holder's pinch item actually
// waits for. Gluttony lifts a quarter-HP trigger to half HP — the ability's
// entire effect, and the reason it was registered inert back when no berry
// existed for it to act on. Items that already trigger at half HP (Sitrus,
// Oran) are untouched: Gluttony makes you eat *earlier*, never later.
func pinchThresholdFor(p *Pokemon, declared float64) float64 {
	if declared < halfThreshold && abilityIsGluttony(p) {
		return halfThreshold
	}
	return declared
}

// applyItemHPTriggers checks both actives, side 0 first for log determinism.
// Used at the turn-scoped call sites (end of turn) where an effect may have
// changed either side's HP.
func applyItemHPTriggers(s *BattleState, rng *RNG, log *[]LogLine) {
	applyItemHPTrigger(s, 0, rng, log)
	applyItemHPTrigger(s, 1, rng, log)
}

// applyItemStatChecks runs the herb check on both actives, side 0 first for log
// determinism — the same shape as applyItemHPTriggers.
func applyItemStatChecks(s *BattleState, log *[]LogLine) {
	applyItemStatCheck(s.Active(0), 0, log)
	applyItemStatCheck(s.Active(1), 1, log)
}

// applyItemStatusCure fires a status-cure item (Lum / Cheri / Chesto / ...)
// right after the holder gains a condition. Called from inflictStatus, the
// confusion volatile handler, and doRest — every path that can leave the
// holder with something a berry cures.
//
// side is the holder's side. p is passed explicitly rather than read off the
// state because inflictStatus operates on a *Pokemon that is not always the
// active one (Synchronize bounces, hazard chip mid-switch).
func applyItemStatusCure(p *Pokemon, side int, log *[]LogLine) {
	if p == nil || p.Fainted {
		return
	}
	it := itemOf(p)
	if it == nil || it.OnStatus == nil {
		return
	}
	fireItemTrigger(p, side, it, log, func(sub *[]LogLine) bool {
		return it.OnStatus(p, side, sub)
	})
}

// applyItemOnHitTaken fires the defender's reactive item after a damaging move
// connected on it (Enigma heal, Jaboca / Rowap attacker chip, Kee / Maranga
// boosts). Called from dealDamage beside applyOnHit, so it sees the same
// "connected for real" gate the ability contact riders do — a hit absorbed by
// a Substitute never reaches the holder's berry.
// Note the gate is Fainted only, NOT HP <= 0: applyItemOnHitTaken runs after
// def.HP has already been reduced and before faint() resolves, so HP <= 0 is
// exactly the "this hit KO'd me" case — and the attacker-punishing berries fire
// on that hit in canon, the same way Rough Skin and Rocky Helmet do. It also
// matches applyOnHit, which the ability contact riders use; gating the two
// differently would mean Static fires on a lethal contact hit and Jaboca does
// not. Berries that act on the *holder* (Enigma's heal, Kee/Maranga's boosts)
// check HP for themselves — there is no point boosting a Pokémon on its way out.
func applyItemOnHitTaken(s *BattleState, defSide int, m domain.Move, res DamageResult, log *[]LogLine) {
	def := s.Active(defSide)
	if def.Fainted {
		return
	}
	it := itemOf(def)
	if it == nil || it.OnHitTaken == nil {
		return
	}
	fireItemTrigger(def, defSide, it, log, func(sub *[]LogLine) bool {
		return it.OnHitTaken(s, defSide, m, res, sub)
	})
}

// itemStatMult returns the holder's held-item multiplier for one battle stat.
// 1.0 when unset. Consulted by offensiveDefensiveStats for both the attacker's
// offensive stat and the defender's defensive one, so Assault Vest bulks the
// holder up on defense and Thick Club swings for it on offense through one hook.
func itemStatMult(p *Pokemon, stat string) float64 {
	if it := itemOf(p); it != nil && it.StatMult != nil {
		return it.StatMult(p, stat)
	}
	return 1
}

// itemCritStage returns the holder's held-item bonus to the critical-hit stage.
func itemCritStage(p *Pokemon) int {
	if it := itemOf(p); it != nil && it.CritStage != nil {
		return it.CritStage(p)
	}
	return 0
}

// scaleByDrainItem applies the holder's drain multiplier to a recovery amount
// and rounds. Used by every recovery Big Root covers — the declarative
// Effect.Drain path plus Leech Seed, Aqua Ring and Ingrain, which heal outside
// that path and would otherwise silently miss the item.
func scaleByDrainItem(p *Pokemon, amt int) int {
	mult := itemDrainMult(p)
	if mult == 1 {
		return amt
	}
	return int(math.Round(float64(amt) * mult))
}

// itemDrainMult returns the multiplier the holder's item applies to an
// HP-draining move's recovery (Big Root). 1.0 when unset.
func itemDrainMult(p *Pokemon) float64 {
	if it := itemOf(p); it != nil && it.DrainMult > 0 {
		return it.DrainMult
	}
	return 1
}

// moveMakesContact reports whether m counts as a contact move coming from atk.
// Every contact-reactive effect must ask this rather than reading the flag
// directly, or a Punching Glove holder would still trip Rocky Helmet.
func moveMakesContact(m domain.Move, atk *Pokemon) bool {
	if !m.HasFlag("contact") {
		return false
	}
	if it := itemOf(atk); it != nil && it.SuppressesContact != nil && it.SuppressesContact(m) {
		return false
	}
	return true
}

// itemBlocksStatusMoves reports whether the holder's item bars status moves
// (Assault Vest). Consulted by LegalActions and enforced again in executeMove.
func itemBlocksStatusMoves(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.BlocksStatusMoves
}

// applyItemOnHitTakenPassive fires the defender's permanent on-hit item (Rocky
// Helmet). Runs after the consuming variant so a berry reacting to the same hit
// is spent first, matching the order Showdown reports them in.
func applyItemOnHitTakenPassive(s *BattleState, defSide int, m domain.Move, res DamageResult, log *[]LogLine) {
	def := s.Active(defSide)
	if it := itemOf(def); it != nil && it.OnHitTakenPassive != nil {
		it.OnHitTakenPassive(s, defSide, m, res, log)
	}
}

// applyItemOnDealtDamage fires the attacker's post-damage item (Shell Bell's
// drain, King's Rock's flinch). Called once per connecting strike, so a
// multi-hit move drains — and rolls for flinch — per hit, which is canon.
func applyItemOnDealtDamage(s *BattleState, atkSide, dmg int, m domain.Move, rng *RNG, log *[]LogLine) {
	atk := s.Active(atkSide)
	if atk.Fainted {
		return
	}
	if it := itemOf(atk); it != nil && it.OnDealtDamage != nil {
		it.OnDealtDamage(s, atkSide, dmg, m, rng, log)
	}
}

// applyItemDrainOnDamageDealt heals the attacker a fraction of the total damage
// its move dealt (Shell Bell). Truncating, with no round-up floor: a move too
// weak to earn a whole point of recovery earns none, which is what canon does
// and what the flat-amount heals (Oran, Berry Juice) deliberately do not.
func applyItemDrainOnDamageDealt(s *BattleState, atkSide, totalDmg int, log *[]LogLine) {
	p := s.Active(atkSide)
	if totalDmg <= 0 || p.Fainted || p.HP >= p.MaxHP {
		return
	}
	it := itemOf(p)
	if it == nil || it.DrainFraction <= 0 {
		return
	}
	amt := int(float64(totalDmg) * it.DrainFraction)
	if amt < 1 {
		return
	}
	itemHealAmount(p, atkSide, amt, it.Name, log)
}

// applyItemOnMoveUsed fires the attacker's one-shot post-move item (Throat
// Spray). Called from the two points in executeMove where a move has actually
// reached its target — the status dispatcher and the tail of the damage loop —
// mirroring canon's onAfterMoveSecondarySelf.
//
// A move stopped *before* its target (Protect, an immunity, a miss, Snatch,
// Magic Coat) never gets here. A move that reached its target and then failed
// to accomplish anything — Roar with no live bench, Sing on a sleeping foe —
// does pay out, which is canon: Showdown aborts those in moveHit, downstream of
// the event. Both call sites run before applySelfSwitch, so s.Active(side) is
// still the Pokémon that swung.
func applyItemOnMoveUsed(s *BattleState, side int, m domain.Move, log *[]LogLine) {
	p := s.Active(side)
	// HP <= 0 as well as Fainted: Destiny Bond and applySelfDestruct zero the
	// attacker's HP directly and leave the faint to a later step, and canon
	// leaves the item on a Pokémon that is on its way out.
	if p.Fainted || p.HP <= 0 {
		return
	}
	it := itemOf(p)
	if it == nil || it.OnMoveUsed == nil {
		return
	}
	fireItemTrigger(p, side, it, log, func(sub *[]LogLine) bool {
		return it.OnMoveUsed(s, side, m, sub)
	})
}

// applyItemOnMoveMissed fires the attacker's one-shot miss reaction (Blunder
// Policy). Same zeroed-but-not-yet-fainted guard as applyItemOnMoveUsed.
func applyItemOnMoveMissed(s *BattleState, side int, m domain.Move, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted || p.HP <= 0 {
		return
	}
	it := itemOf(p)
	if it == nil || it.OnMoveMissed == nil {
		return
	}
	fireItemTrigger(p, side, it, log, func(sub *[]LogLine) bool {
		return it.OnMoveMissed(s, side, m, sub)
	})
}

// applyItemStatCheck fires a herb if the holder now has something to fix (a
// lowered stat, a restriction). Called from the same places as the pinch check,
// since a drop or a Taunt can land at any of them.
func applyItemStatCheck(p *Pokemon, side int, log *[]LogLine) {
	if p == nil || p.Fainted {
		return
	}
	it := itemOf(p)
	if it == nil || it.OnStatCheck == nil {
		return
	}
	fireItemTrigger(p, side, it, log, func(sub *[]LogLine) bool {
		return it.OnStatCheck(p, side, sub)
	})
}

// itemAccuracyMult is the attacker-side accuracy multiplier (Wide Lens, Zoom
// Lens). 1.0 when unset.
func itemAccuracyMult(s *BattleState, side int) float64 {
	it := itemOf(s.Active(side))
	if it == nil {
		return 1
	}
	mult := 1.0
	if it.AccuracyMult > 0 {
		mult = it.AccuracyMult
	}
	if it.AccuracyMultIf != nil {
		mult *= it.AccuracyMultIf(s, side)
	}
	return mult
}

// itemAccuracyMultVs is the defender-side accuracy multiplier: how much harder
// the holder's item makes it to land a move on them (Bright Powder).
func itemAccuracyMultVs(def *Pokemon) float64 {
	if it := itemOf(def); it != nil && it.AccuracyMultVs > 0 {
		return it.AccuracyMultVs
	}
	return 1
}

// itemMovesLast reports whether the holder is pushed to the back of its
// priority bracket (Lagging Tail, Full Incense).
func itemMovesLast(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.MovesLast
}

// itemMinMultihit returns the floor an item puts under a variable multi-hit
// move's strike count (Loaded Dice), or 0 when unset.
func itemMinMultihit(p *Pokemon) int {
	if it := itemOf(p); it != nil {
		return it.MinMultihit
	}
	return 0
}

// applyQuickClaw rolls the holder's Quick Claw at the top of the turn and arms
// the same bracket-precedence volatile Custap Berry uses. Unlike Custap it is
// not consumed — and unlike Focus Band it rolls every turn, so a Quick Claw
// holder does shift the RNG stream. That is canon; the item is a coin flip
// every turn, not a saved one.
func applyQuickClaw(s *BattleState, side int, act Action, rng *RNG, log *[]LogLine) {
	if act.Kind != ActionMove {
		return
	}
	p := s.Active(side)
	if p.Fainted || p.HP <= 0 {
		return
	}
	it := itemOf(p)
	if it == nil || it.QuickDrawChance <= 0 {
		return
	}
	if !rng.Chance(it.QuickDrawChance) {
		return
	}
	p.Volatiles.CustapBoost = true
	*log = append(*log, LogLine{
		Type: "item", Side: side,
		Text: fmt.Sprintf("%s's %s let it move first!", p.Name, it.Name),
	})
}

// applyItemEndOfTurnLate fires the holder's late residual tick (the orbs,
// Sticky Barb). Called at the very end of ResolveTurn's residual block, after
// the heals and chips, so the turn an orb fires costs the holder nothing.
func applyItemEndOfTurnLate(s *BattleState, side int, rng *RNG, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted {
		return
	}
	if it := itemOf(p); it != nil && it.EndOfTurnLate != nil {
		it.EndOfTurnLate(s, side, rng, log)
	}
}

// fieldTurnsFor returns how long a field condition set by p should last: the
// extended duration when p holds the matching extender, otherwise base. The
// weather variant needs the kind too, since each rock covers exactly one.
func fieldTurnsFor(p *Pokemon, base int, want func(*Item) bool) int {
	if it := itemOf(p); it != nil && want(it) {
		return extendedFieldTurns
	}
	return base
}

func screenTurnsFor(p *Pokemon, base int) int {
	return fieldTurnsFor(p, base, func(it *Item) bool { return it.ExtendsScreens })
}

func terrainTurnsFor(p *Pokemon, base int) int {
	return fieldTurnsFor(p, base, func(it *Item) bool { return it.ExtendsTerrain })
}

func weatherTurnsFor(p *Pokemon, base int, kind WeatherKind) int {
	return fieldTurnsFor(p, base, func(it *Item) bool { return it.ExtendsWeather == kind })
}

// itemIgnoresHazards reports whether p walks over entry hazards (Heavy-Duty
// Boots).
func itemIgnoresHazards(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.IgnoresHazards
}

// itemImmuneToSandstorm reports whether p takes no sandstorm chip (Safety
// Goggles).
func itemImmuneToSandstorm(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.ImmuneToSandstorm
}

// itemBlocksPowderMove reports whether m is a powder move the holder's item
// refuses (Safety Goggles). Non-powder moves are never blocked.
func itemBlocksPowderMove(p *Pokemon, m domain.Move) bool {
	if !m.HasFlag("powder") {
		return false
	}
	it := itemOf(p)
	return it != nil && it.BlocksPowder
}

// itemAllowsSwitchOut reports whether p can leave regardless of trapping (Shed
// Shell).
func itemAllowsSwitchOut(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.AllowsSwitchOut
}

// itemGrounds reports whether p is dragged down to the ground by its item
// (Iron Ball), overriding Flying / Levitate for terrain and Ground moves.
func itemGrounds(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.Grounds
}

// itemFloats reports whether p is lifted off the ground by its item (Air
// Balloon). This is broader than the balloon's Ground-type immunity: an
// ungrounded Pokémon also skips Spikes and Toxic Spikes and sits outside
// terrain entirely — no Electric/Grassy/Psychic boost, no Grassy heal, no Misty
// status shield, no Psychic-terrain priority block. Iron Ball wins if somehow
// both applied, which is why isGrounded checks Grounds first.
func itemFloats(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.Floats
}

// itemLiftsOwnImmunities reports whether p has given up its type-chart
// immunities (Ring Target).
func itemLiftsOwnImmunities(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.LiftsOwnImmunities
}

// itemIgnoresWeather reports whether p is shielded from rain and sun (Utility
// Umbrella). Sandstorm and snow are weather the umbrella does not cover — it
// keeps rain and sun off, not grit and cold.
func itemIgnoresWeather(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.IgnoresWeather
}

// weatherFor returns the weather as p experiences it: nil in place of rain or
// sun for a Utility Umbrella holder, and the field value otherwise. Used by the
// damage formula's per-side weather reads, so an umbrella holder neither takes
// nor deals weather-boosted damage in rain or sun.
func weatherFor(p *Pokemon, w *WeatherState) *WeatherState {
	if w == nil || !itemIgnoresWeather(p) {
		return w
	}
	if w.Kind == WeatherRain || w.Kind == WeatherSun {
		return nil
	}
	return w
}

// itemBlocksSecondaries reports whether added move effects bounce off p
// (Covert Cloak), mirroring the Shield Dust ability.
func itemBlocksSecondaries(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.BlocksSecondaries
}

// itemBlocksStatDrops reports whether p refuses foe-induced stat drops (Clear
// Amulet).
func itemBlocksStatDrops(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.BlocksStatDrops
}

// itemTypeMultOverride is the item counterpart of abilityTypeMultOverride: an
// item-granted type immunity (Air Balloon vs Ground).
func itemTypeMultOverride(def *Pokemon, atkType domain.Type) (float64, bool) {
	if it := itemOf(def); it != nil && it.TypeImmunity != nil {
		return it.TypeImmunity(atkType)
	}
	return 1, false
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
