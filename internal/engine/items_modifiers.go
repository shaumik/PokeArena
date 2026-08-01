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

// typeBoostMult is the multiplier a type-boosting item applies to its type.
// Gen 4+ value; the Gen 2/3 items used ×1.1.
const typeBoostMult = 1.2

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
		OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
			if m.Type == t {
				return typeBoostMult
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
		Desc: "Physical moves deal 1.1x damage.",
		OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
			if m.Category == domain.CatPhysical {
				return 1.1
			}
			return 1
		},
	})

	registerItem(&Item{
		Kind: ItemWiseGlasses, Name: "Wise Glasses",
		Desc: "Special moves deal 1.1x damage.",
		OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
			if m.Category == domain.CatSpecial {
				return 1.1
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
		OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
			if m.HasFlag("punch") {
				return 1.1
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
func tickMetronome(atk *Pokemon, m domain.Move) {
	if m.ID == "" {
		return
	}
	if it := itemOf(atk); it == nil || it.Kind != ItemMetronome {
		return
	}
	if atk.Volatiles.MetronomeMoveID == m.ID {
		atk.Volatiles.MetronomeCount++
		return
	}
	atk.Volatiles.MetronomeMoveID = m.ID
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
				atk.Name+" was hurt by the Rocky Helmet! (-%d)", log)
			if atk.HP <= 0 {
				faint(atk, 1-defSide, log)
			}
		},
	})

	registerItem(&Item{
		Kind: ItemShellBell, Name: "Shell Bell",
		Desc: "The holder restores 1/8 of the damage its moves deal.",
		OnDealtDamage: func(s *BattleState, atkSide, dmg int, log *[]LogLine) {
			p := s.Active(atkSide)
			if dmg <= 0 || p.Fainted || p.HP >= p.MaxHP {
				return
			}
			itemHealAmount(p, atkSide, dmg/8, "Shell Bell", log)
		},
	})

	registerItem(&Item{
		Kind: ItemBigRoot, Name: "Big Root",
		Desc:      "HP-draining moves restore 1.3x as much.",
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
