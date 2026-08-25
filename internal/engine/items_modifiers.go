package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// items_modifiers.go is the always-on family: items that change a number every
// turn they are held rather than firing once and vanishing. Type boosters, the
// category bands, Expert Belt, Assault Vest, Rocky Helmet, Shell Bell, the crit
// items, and the three species-locked relics.
//
// Everything here is passive by construction — none of these declare a
// consuming hook, which TestConsumableItemsAreNotAlsoPassive enforces from the
// other side. Focus Band is the one borderline case: it can save its holder
// repeatedly, so it is a chance-gated passive rather than a one-shot like Focus
// Sash, and that difference is the whole distinction between the two items.

const (
	// Type boosters — ×1.2 to the matching type, one per type.
	ItemSilkScarf    ItemKind = "silk-scarf"
	ItemCharcoal     ItemKind = "charcoal"
	ItemMysticWater  ItemKind = "mystic-water"
	ItemMagnet       ItemKind = "magnet"
	ItemMiracleSeed  ItemKind = "miracle-seed"
	ItemNeverMeltIce ItemKind = "never-melt-ice"
	ItemBlackBelt    ItemKind = "black-belt"
	ItemPoisonBarb   ItemKind = "poison-barb"
	ItemSoftSand     ItemKind = "soft-sand"
	ItemSharpBeak    ItemKind = "sharp-beak"
	ItemTwistedSpoon ItemKind = "twisted-spoon"
	ItemSilverPowder ItemKind = "silver-powder"
	ItemHardStone    ItemKind = "hard-stone"
	ItemSpellTag     ItemKind = "spell-tag"
	ItemDragonFang   ItemKind = "dragon-fang"
	ItemBlackGlasses ItemKind = "black-glasses"
	ItemMetalCoat    ItemKind = "metal-coat"
	ItemFairyFeather ItemKind = "fairy-feather"

	// Category and coverage boosters.
	ItemExpertBelt    ItemKind = "expert-belt"
	ItemMuscleBand    ItemKind = "muscle-band"
	ItemWiseGlasses   ItemKind = "wise-glasses"
	ItemPunchingGlove ItemKind = "punching-glove"
	ItemMetronome     ItemKind = "metronome"

	// Defensive and recovery.
	ItemAssaultVest ItemKind = "assault-vest"
	ItemRockyHelmet ItemKind = "rocky-helmet"
	ItemShellBell   ItemKind = "shell-bell"
	ItemBigRoot     ItemKind = "big-root"
	ItemFocusBand   ItemKind = "focus-band"

	// Critical-hit ratio.
	ItemScopeLens  ItemKind = "scope-lens"
	ItemRazorClaw  ItemKind = "razor-claw"
	ItemLuckyPunch ItemKind = "lucky-punch"
	ItemLeek       ItemKind = "leek"

	// Species-locked.
	ItemThickClub ItemKind = "thick-club"
)

// Species the three species-locked items answer to. Held by anything else they
// are inert, which is canon — a Thick Club on a Snorlax is a rock in a pocket.
const (
	dexFarfetchd = 83
	dexMarowak   = 105
	dexChansey   = 113
)

// typeBoostNum is the numerator, over 4096, that a type-boosting item applies
// to its type. Gen 4+ value; the Gen 2/3 items used ×1.1. Upstream carries it
// as `chainModify([4915, 4096])` — 1.19995, not 1.2 — and the pair of category
// bands below are the ones where writing the decimal instead of the numerator
// actually costs a point.
const typeBoostNum = 4915

// categoryBandNum and punchingGloveNum are Muscle Band / Wise Glasses
// (`[4505, 4096]`) and Punching Glove (`[4506, 4096]`). They look like the same
// ×1.1 and are not: rounding 1.1 into 4096ths gives 4506, so the bands would
// come out one numerator high if they were written as a decimal, and the glove
// would come out right by accident.
const (
	categoryBandNum  = 4505
	punchingGloveNum = 4506
)

// focusBandChance is the percent chance Focus Band saves its holder from an
// otherwise-lethal hit.
const focusBandChance = 10

// metronomeStep is how much each consecutive use of the same move adds, and
// metronomeMax caps the total multiplier — five repeats reach the ceiling.
const (
	metronomeStep = 0.2
	metronomeMax  = 2.0
)

func init() {
	registerTypeBoosters()
	registerCategoryBoosters()
	registerDefensiveItems()
	registerCritItems()
}

// --- type boosters ---

func typeBooster(kind ItemKind, name string, t domain.Type) *Item {
	return &Item{
		Kind: kind, Name: name,
		Desc: fmt.Sprintf("The holder's %s-type moves deal 1.2x damage.", t),
		// Base-power group: every one of the eighteen registers `onBasePower`
		// upstream, so the boost lands on base power before the formula runs.
		// One constructor, so the whole family moved in a single edit.
		BasePowerPriority: bpPrioTypeBooster,
		BasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
			if m.Type == t {
				return mod4096(typeBoostNum)
			}
			return 1
		},
	}
}

func registerTypeBoosters() {
	for _, b := range []struct {
		kind ItemKind
		name string
		typ  domain.Type
	}{
		{ItemSilkScarf, "Silk Scarf", "normal"},
		{ItemCharcoal, "Charcoal", "fire"},
		{ItemMysticWater, "Mystic Water", "water"},
		{ItemMagnet, "Magnet", "electric"},
		{ItemMiracleSeed, "Miracle Seed", "grass"},
		{ItemNeverMeltIce, "Never-Melt Ice", "ice"},
		{ItemBlackBelt, "Black Belt", "fighting"},
		{ItemPoisonBarb, "Poison Barb", "poison"},
		{ItemSoftSand, "Soft Sand", "ground"},
		{ItemSharpBeak, "Sharp Beak", "flying"},
		{ItemTwistedSpoon, "Twisted Spoon", "psychic"},
		{ItemSilverPowder, "Silver Powder", "bug"},
		{ItemHardStone, "Hard Stone", "rock"},
		{ItemSpellTag, "Spell Tag", "ghost"},
		{ItemDragonFang, "Dragon Fang", "dragon"},
		{ItemBlackGlasses, "Black Glasses", "dark"},
		{ItemMetalCoat, "Metal Coat", "steel"},
		{ItemFairyFeather, "Fairy Feather", "fairy"},
	} {
		registerItem(typeBooster(b.kind, b.name, b.typ))
	}
}

// --- category and coverage boosters ---

func registerCategoryBoosters() {
	registerItem(&Item{
		Kind: ItemExpertBelt, Name: "Expert Belt",
		Desc: "Super-effective moves deal 1.2x damage.",
		OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
			if typeEff > 1 {
				return 1.2
			}
			return 1
		},
	})

	registerItem(&Item{
		Kind: ItemMuscleBand, Name: "Muscle Band",
		Desc:              "Physical moves deal 1.1x damage.",
		BasePowerPriority: bpPrioCategoryBand,
		BasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
			if m.Category == domain.CatPhysical {
				return mod4096(categoryBandNum)
			}
			return 1
		},
	})

	registerItem(&Item{
		Kind: ItemWiseGlasses, Name: "Wise Glasses",
		Desc:              "Special moves deal 1.1x damage.",
		BasePowerPriority: bpPrioCategoryBand,
		BasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
			if m.Category == domain.CatSpecial {
				return mod4096(categoryBandNum)
			}
			return 1
		},
	})

	// Punching Glove is two effects in one: the boost, and the loss of the
	// contact flag — which is the reason to run it over Muscle Band, since a
	// gloved punch no longer wakes Rocky Helmet, Static, or Rough Skin.
	registerItem(&Item{
		Kind: ItemPunchingGlove, Name: "Punching Glove",
		Desc: "Punching moves deal 1.1x damage and no longer make contact.",
		// Punches only: a gloved Body Slam still makes contact, so the scope has
		// to match the boost rather than blanket-decontacting the holder.
		SuppressesContact: func(m domain.Move) bool { return m.HasFlag("punch") },
		BasePowerPriority: bpPrioPunchBoost,
		BasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
			if m.HasFlag("punch") {
				return mod4096(punchingGloveNum)
			}
			return 1
		},
	})

	// Metronome reads a counter the move loop maintains (see tickMetronome).
	// The item itself is pure lookup so the same count drives computeDamage and
	// ExpectedDamage identically.
	registerItem(&Item{
		Kind: ItemMetronome, Name: "Metronome",
		Desc: "Damage rises by 20% for each consecutive use of the same move, up to 2x.",
		OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
			return metronomeMult(atk, m)
		},
	})
}

// metronomeMult is the holder's current Metronome multiplier for move m. The
// counter is the number of *consecutive prior* uses, so the first use is ×1.0
// and each repeat adds 20% to the cap.
//
// It takes the move rather than reading only the counter so a mismatched move
// scores 1.0 even before tickMetronome resets the counter — ExpectedDamage
// scores hypothetical moves the holder has not used yet, and must not hand them
// the boost the current streak earned.
func metronomeMult(atk *Pokemon, m domain.Move) float64 {
	if atk.Volatiles.MetronomeMoveID == "" || atk.Volatiles.MetronomeMoveID != m.ID {
		return 1
	}
	mult := 1 + metronomeStep*float64(atk.Volatiles.MetronomeCount)
	if mult > metronomeMax {
		mult = metronomeMax
	}
	return mult
}

// tickMetronome updates the holder's consecutive-use streak. Called from
// executeMove once the move to be used is settled and before damage resolves,
// so the boost applies from the second consecutive use onward.
//
// A different move resets the streak to zero. The counter lives on Volatiles,
// so switching out clears it — canon, and it also means the counter can't
// outlive the Pokémon that earned it.
func tickMetronome(atk *Pokemon, m domain.Move, twoTurnStrike bool) {
	if it := itemOf(atk); it == nil || it.Kind != ItemMetronome {
		return
	}
	// Struggle carries no ID (it is a literal, not a dex entry), and it is a
	// different move from whatever the holder was repeating — so it resets the
	// streak rather than being skipped. Returning early here would let a
	// Metronome user run out of PP, Struggle, and resume its streak intact.
	if m.ID != "" && atk.Volatiles.MetronomeMoveID == m.ID {
		atk.Volatiles.MetronomeCount++
		return
	}
	atk.Volatiles.MetronomeMoveID = m.ID
	atk.Volatiles.MetronomeCount = 0
	// A two-turn move's first strike starts at the *first* boost step rather
	// than at none. That is upstream's `volatiles['twoturnmove']` branch, which
	// sets numConsecutive to 1 instead of 0 when the move is new — and it is
	// checked on the strike leg only, because the charge leg never reaches
	// onTryMove. Upstream's own case pins the figures: a Dusknoir's first Dig
	// under a Metronome is Metronome-1 boosted and its second is Metronome-2.
	//
	// A charge the sun or a Power Herb skips is not a two-turn strike and gets
	// nothing: the move resolved on the turn it was chosen, so there is no
	// charge leg for the step to stand in for. That is the other upstream case
	// here, and the pair of them is what makes the flag necessary rather than a
	// blanket "two-turn moves start at 1".
	if twoTurnStrike {
		atk.Volatiles.MetronomeCount = 1
	}
}

// breakMetronomeStreak zeroes the consecutive-use count without forgetting
// which move the holder was on. Canon keys the streak on "the last move
// succeeded", so a miss or a failure breaks it — otherwise Metronome plus a
// shaky move (Focus Blast, Hydro Pump) would ramp on whiffs alone.
func breakMetronomeStreak(atk *Pokemon) {
	if it := itemOf(atk); it == nil || it.Kind != ItemMetronome {
		return
	}
	// The move ID is cleared as well as the count: leaving it set lets the next
	// use re-match and tick straight back to x1.2, so a hit/hit/miss/hit run
	// would resume the streak instead of restarting it.
	atk.Volatiles.MetronomeMoveID = ""
	atk.Volatiles.MetronomeCount = 0
}

// --- defensive and recovery ---

func registerDefensiveItems() {
	registerItem(&Item{
		Kind: ItemAssaultVest, Name: "Assault Vest",
		Desc:              "Sp. Def is 1.5x, but the holder cannot select status moves.",
		BlocksStatusMoves: true,
		StatMult: func(p *Pokemon, stat string) float64 {
			if stat == "spdef" {
				return 1.5
			}
			return 1
		},
	})

	registerItem(&Item{
		Kind: ItemRockyHelmet, Name: "Rocky Helmet",
		Desc: "An attacker that makes contact with the holder loses 1/6 of its max HP.",
		OnHitTakenPassive: func(s *BattleState, defSide int, m domain.Move, _ DamageResult, log *[]LogLine) {
			atk := s.Active(1 - defSide)
			if !moveMakesContact(m, atk) || atk.Fainted || atk.HP <= 0 {
				return
			}
			// Indirect damage, so Magic Guard walks away clean — the same gate
			// Rough Skin uses.
			if abilityBlocksIndirectDamage(atk) {
				return
			}
			itemDamage(atk, 1-defSide, atk.MaxHP/6,
				"%s was hurt by the Rocky Helmet! (-%d)", log)
			if atk.HP <= 0 {
				faint(atk, 1-defSide, log)
			}
		},
	})

	registerItem(&Item{
		Kind: ItemShellBell, Name: "Shell Bell",
		Desc: "The holder restores 1/8 of the damage its moves deal.",
		// DrainFraction rather than OnDealtDamage: the drain is computed off the
		// move's total damage once it has fully resolved, not per strike. A
		// per-hit hook would truncate each strike's eighth independently and
		// round every sub-8 hit up to 1, healing on a 2-damage Tackle where
		// canon heals nothing.
		DrainFraction: 1.0 / 8,
	})

	registerItem(&Item{
		Kind: ItemBigRoot, Name: "Big Root",
		Desc:      "HP-draining moves, Leech Seed, Aqua Ring and Ingrain restore 1.3x as much.",
		DrainMult: 1.3,
	})

	// Focus Band is a chance-gated passive, not a one-shot: it can save the
	// same holder more than once, and unlike Focus Sash it works from any HP.
	registerItem(&Item{
		Kind: ItemFocusBand, Name: "Focus Band",
		Desc:              "10% chance to survive an otherwise-lethal hit at 1 HP. Not consumed.",
		SurviveOHKOChance: focusBandChance,
	})
}

// --- critical-hit ratio ---

// critItem builds a crit-stage item. species is 0 for the unrestricted ones
// (Scope Lens, Razor Claw) and a Pokédex number for the species-locked relics,
// which are inert on anything else.
func critItem(kind ItemKind, name string, stages, species int) *Item {
	desc := fmt.Sprintf("Raises the holder's critical-hit ratio by %d stage(s).", stages)
	if species > 0 {
		desc += " Only works for one species."
	}
	return &Item{
		Kind: kind, Name: name, Desc: desc,
		CritStage: func(p *Pokemon) int {
			if species > 0 && p.DexNo != species {
				return 0
			}
			return stages
		},
	}
}

func registerCritItems() {
	registerItem(critItem(ItemScopeLens, "Scope Lens", 1, 0))
	registerItem(critItem(ItemRazorClaw, "Razor Claw", 1, 0))
	registerItem(critItem(ItemLuckyPunch, "Lucky Punch", 2, dexChansey))
	registerItem(critItem(ItemLeek, "Leek", 2, dexFarfetchd))

	registerItem(&Item{
		Kind: ItemThickClub, Name: "Thick Club",
		Desc: "Doubles Attack. Only works for one species.",
		StatMult: func(p *Pokemon, stat string) float64 {
			if stat == "attack" && p.DexNo == dexMarowak {
				return 2
			}
			return 1
		},
	})
}
