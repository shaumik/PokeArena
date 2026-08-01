package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// items_berries.go is the consumable-item family: Berries (plus Berry Juice,
// which behaves like one and differs only in the log verb). Every entry here is
// one-shot — it fires once and the holder is bare afterwards, which is what
// makes the framework's fireItemTrigger contract ("return true and I remove
// you") the right shape for all of them.
//
// The five groups, and where each one's trigger point lives:
//
//	HP restore    (Oran / Sitrus / Berry Juice / the five flavor berries)
//	              → applyItemHPTrigger, at every point HP can drop
//	Status cure   (Cheri / Chesto / Pecha / Rawst / Aspear / Persim / Lum)
//	              → applyItemStatusCure, right after the condition lands
//	Pinch stats   (Liechi / Ganlon / Petaya / Apicot / Salac / Starf, plus
//	              Custap's turn-order jump and Micle's accuracy boost)
//	              → applyItemHPTrigger at 1/4 HP
//	Damage react  (Enigma / Jaboca / Rowap / Kee / Maranga)
//	              → applyItemOnHitTaken, beside the ability contact riders
//	Type resist   (the eighteen one-per-type berries)
//	              → the declarative ResistType field, read by computeDamage
//
// Two documented degradations, both from data the engine does not model:
//
//   - The five flavor berries (Figy / Wiki / Mago / Aguav / Iapapa) confuse a
//     holder whose Nature dislikes their flavor. Natures aren't modeled (every
//     Pokémon battles on a neutral spread — see damage.go), so there is no
//     disliked flavor to check and the confusion never applies. They are pure
//     1/3 heals here, which is the better half of the canonical behavior.
//   - Leppa Berry restores PP the moment a move runs out. The engine pays PP in
//     exactly one place (choosePP, plus Pressure's extra charge right after),
//     so that is where it is checked; PP drained by Spite would not trigger it
//     until the holder next selects a move. Spite is not in the curated move
//     set, so nothing reaches that gap today.

const (
	// HP restore.
	ItemOranBerry   ItemKind = "oran-berry"
	ItemSitrusBerry ItemKind = "sitrus-berry"
	ItemBerryJuice  ItemKind = "berry-juice"
	ItemFigyBerry   ItemKind = "figy-berry"
	ItemWikiBerry   ItemKind = "wiki-berry"
	ItemMagoBerry   ItemKind = "mago-berry"
	ItemAguavBerry  ItemKind = "aguav-berry"
	ItemIapapaBerry ItemKind = "iapapa-berry"

	// Status cure.
	ItemCheriBerry  ItemKind = "cheri-berry"
	ItemChestoBerry ItemKind = "chesto-berry"
	ItemPechaBerry  ItemKind = "pecha-berry"
	ItemRawstBerry  ItemKind = "rawst-berry"
	ItemAspearBerry ItemKind = "aspear-berry"
	ItemPersimBerry ItemKind = "persim-berry"
	ItemLumBerry    ItemKind = "lum-berry"
	ItemLeppaBerry  ItemKind = "leppa-berry"

	// Pinch.
	ItemLiechiBerry ItemKind = "liechi-berry"
	ItemGanlonBerry ItemKind = "ganlon-berry"
	ItemPetayaBerry ItemKind = "petaya-berry"
	ItemApicotBerry ItemKind = "apicot-berry"
	ItemSalacBerry  ItemKind = "salac-berry"
	ItemStarfBerry  ItemKind = "starf-berry"
	ItemCustapBerry ItemKind = "custap-berry"
	ItemMicleBerry  ItemKind = "micle-berry"

	// Damage reaction.
	ItemEnigmaBerry  ItemKind = "enigma-berry"
	ItemJabocaBerry  ItemKind = "jaboca-berry"
	ItemRowapBerry   ItemKind = "rowap-berry"
	ItemKeeBerry     ItemKind = "kee-berry"
	ItemMarangaBerry ItemKind = "maranga-berry"

	// Type resist.
	ItemOccaBerry   ItemKind = "occa-berry"
	ItemPasshoBerry ItemKind = "passho-berry"
	ItemWacanBerry  ItemKind = "wacan-berry"
	ItemRindoBerry  ItemKind = "rindo-berry"
	ItemYacheBerry  ItemKind = "yache-berry"
	ItemChopleBerry ItemKind = "chople-berry"
	ItemKebiaBerry  ItemKind = "kebia-berry"
	ItemShucaBerry  ItemKind = "shuca-berry"
	ItemCobaBerry   ItemKind = "coba-berry"
	ItemPayapaBerry ItemKind = "payapa-berry"
	ItemTangaBerry  ItemKind = "tanga-berry"
	ItemChartiBerry ItemKind = "charti-berry"
	ItemKasibBerry  ItemKind = "kasib-berry"
	ItemHabanBerry  ItemKind = "haban-berry"
	ItemColburBerry ItemKind = "colbur-berry"
	ItemBabiriBerry ItemKind = "babiri-berry"
	ItemRoseliBerry ItemKind = "roseli-berry"
	ItemChilanBerry ItemKind = "chilan-berry"
)

// pinchThreshold is the HP fraction the Gen-7+ pinch berries wait for. The
// healing berries use halfThreshold instead: Oran / Sitrus / Berry Juice fire
// at half HP, the flavor berries at a quarter.
const (
	pinchThreshold = 0.25
	halfThreshold  = 0.5
)

func init() {
	registerHealBerries()
	registerCureBerries()
	registerPinchBerries()
	registerReactionBerries()
	registerResistBerries()
}

// --- HP restore ---

func registerHealBerries() {
	registerItem(&Item{
		Kind: ItemOranBerry, Name: "Oran Berry", Berry: true,
		Desc:        "Restores 10 HP when the holder drops to half HP or less. Consumed on use.",
		HPThreshold: halfThreshold,
		OnHPThreshold: func(s *BattleState, side int, _ *RNG, log *[]LogLine) bool {
			itemHealAmount(s.Active(side), side, 10, "Oran Berry", log)
			return true
		},
	})

	registerItem(&Item{
		Kind: ItemSitrusBerry, Name: "Sitrus Berry", Berry: true,
		Desc:        "Restores 1/4 of max HP when the holder drops to half HP or less. Consumed on use.",
		HPThreshold: halfThreshold,
		OnHPThreshold: func(s *BattleState, side int, _ *RNG, log *[]LogLine) bool {
			itemHealFraction(s.Active(side), side, 0.25, "Sitrus Berry", log)
			return true
		},
	})

	// Berry Juice is a drink, not a Berry: same trigger, different log verb
	// (and immune to anything that keys off the Berry flag).
	registerItem(&Item{
		Kind: ItemBerryJuice, Name: "Berry Juice",
		Desc:        "Restores 20 HP when the holder drops to half HP or less. Consumed on use.",
		HPThreshold: halfThreshold,
		OnHPThreshold: func(s *BattleState, side int, _ *RNG, log *[]LogLine) bool {
			itemHealAmount(s.Active(side), side, 20, "Berry Juice", log)
			return true
		},
	})

	// The five flavor berries are mechanically identical here — see the file
	// header on why the Nature-disliked confusion doesn't apply.
	for _, b := range []struct {
		kind ItemKind
		name string
	}{
		{ItemFigyBerry, "Figy Berry"},
		{ItemWikiBerry, "Wiki Berry"},
		{ItemMagoBerry, "Mago Berry"},
		{ItemAguavBerry, "Aguav Berry"},
		{ItemIapapaBerry, "Iapapa Berry"},
	} {
		name := b.name
		registerItem(&Item{
			Kind: b.kind, Name: name, Berry: true,
			Desc:        "Restores 1/3 of max HP when the holder drops to a quarter HP or less. Consumed on use.",
			HPThreshold: pinchThreshold,
			OnHPThreshold: func(s *BattleState, side int, _ *RNG, log *[]LogLine) bool {
				itemHealFraction(s.Active(side), side, 1.0/3, name, log)
				return true
			},
		})
	}
}

// --- status cure ---

// cureBerry builds a status-cure berry that clears exactly the conditions in
// cures (and confusion when alsoConfusion). Returning false when the holder has
// nothing to cure is what keeps the berry in reserve instead of eating it on
// the first trigger check.
func cureBerry(kind ItemKind, name, desc string, alsoConfusion bool, cures ...StatusCond) *Item {
	set := make(map[StatusCond]bool, len(cures))
	for _, c := range cures {
		set[c] = true
	}
	return &Item{
		Kind: kind, Name: name, Berry: true, Desc: desc,
		OnStatus: func(p *Pokemon, side int, log *[]LogLine) bool {
			fired := false
			if set[p.Status] {
				prev := p.Status
				clearStatus(p)
				*log = append(*log, LogLine{
					Type: "item", Side: side,
					Text: fmt.Sprintf("%s was cured of its %s!", p.Name, prev),
				})
				fired = true
			}
			if alsoConfusion && p.Volatiles.Confusion != nil {
				p.Volatiles.Confusion = nil
				*log = append(*log, LogLine{
					Type: "item", Side: side,
					Text: fmt.Sprintf("%s snapped out of its confusion!", p.Name),
				})
				fired = true
			}
			return fired
		},
	}
}

func registerCureBerries() {
	registerItem(cureBerry(ItemCheriBerry, "Cheri Berry",
		"Cures paralysis the moment the holder is paralyzed. Consumed on use.",
		false, StatusParalysis))
	registerItem(cureBerry(ItemChestoBerry, "Chesto Berry",
		"Wakes the holder the moment it falls asleep — including its own Rest. Consumed on use.",
		false, StatusSleep))
	// Pecha cures both poison grades: badly poisoned is still poison.
	registerItem(cureBerry(ItemPechaBerry, "Pecha Berry",
		"Cures poison the moment the holder is poisoned. Consumed on use.",
		false, StatusPoison, StatusToxic))
	registerItem(cureBerry(ItemRawstBerry, "Rawst Berry",
		"Cures a burn the moment the holder is burned. Consumed on use.",
		false, StatusBurn))
	registerItem(cureBerry(ItemAspearBerry, "Aspear Berry",
		"Thaws the holder the moment it is frozen. Consumed on use.",
		false, StatusFreeze))
	registerItem(cureBerry(ItemPersimBerry, "Persim Berry",
		"Snaps the holder out of confusion. Consumed on use.",
		true))
	registerItem(cureBerry(ItemLumBerry, "Lum Berry",
		"Cures any status condition and confusion. Consumed on use.",
		true, StatusBurn, StatusPoison, StatusToxic, StatusParalysis, StatusSleep, StatusFreeze))

	// Leppa is a cure of a different kind — it restores PP, so it hangs off
	// its own trigger (applyItemPPRestore) rather than OnStatus. Registered
	// with no hooks beyond the metadata; see applyItemPPRestore for the logic,
	// which is keyed on the slug because "the move that just ran out" is
	// caller context the generic hook signature doesn't carry.
	registerItem(&Item{
		Kind: ItemLeppaBerry, Name: "Leppa Berry", Berry: true,
		Desc: "Restores 10 PP to the first move that runs out of PP. Consumed on use.",
	})
}

// leppaPP is how much PP a Leppa Berry restores.
const leppaPP = 10

// applyItemPPRestore fires a Leppa Berry when one of the holder's moves has hit
// zero PP. Called from executeMove right after PP is paid (including Pressure's
// extra charge), which is the only place PP leaves a slot.
//
// The restore is capped at the slot's MaxPP so a low-PP move can't end up above
// its ceiling, and the lowest-indexed empty slot wins so the choice is
// deterministic across replays.
func applyItemPPRestore(p *Pokemon, side int, log *[]LogLine) {
	it := itemOf(p)
	if it == nil || it.Kind != ItemLeppaBerry {
		return
	}
	slot := -1
	for i := range p.Moves {
		if p.Moves[i].MoveID != "" && p.Moves[i].PP <= 0 {
			slot = i
			break
		}
	}
	if slot < 0 {
		return
	}
	restored := leppaPP
	if restored > p.Moves[slot].MaxPP {
		restored = p.Moves[slot].MaxPP
	}
	p.Moves[slot].PP = restored
	moveID := p.Moves[slot].MoveID
	fireItemTrigger(p, side, it, log, func(sub *[]LogLine) bool {
		*sub = append(*sub, LogLine{
			Type: "item", Side: side,
			Text: fmt.Sprintf("%s restored %d PP to %s!", p.Name, restored, moveID),
		})
		return true
	})
}

// --- pinch ---

// pinchBoostBerry builds a berry that raises one stat stage at a quarter HP.
// applyStages is the self-induced path: Clear Body and friends guard against
// foe-induced drops, never the holder's own boost.
func pinchBoostBerry(kind ItemKind, name, stat string, delta int) *Item {
	label := statName(stat)
	desc := fmt.Sprintf("Raises %s by %d when the holder drops to a quarter HP or less. Consumed on use.", label, delta)
	return &Item{
		Kind: kind, Name: name, Berry: true, Desc: desc,
		HPThreshold: pinchThreshold,
		OnHPThreshold: func(s *BattleState, side int, _ *RNG, log *[]LogLine) bool {
			applyStages(s.Active(side), side, stat, delta, log)
			return true
		},
	}
}

// starfStats is the pool Starf Berry draws from. Accuracy and evasion are
// excluded, matching canon, and the order is fixed so the RNG draw replays
// identically from a seed.
var starfStats = []string{"attack", "defense", "spatk", "spdef", "speed"}

func registerPinchBerries() {
	registerItem(pinchBoostBerry(ItemLiechiBerry, "Liechi Berry", "attack", 1))
	registerItem(pinchBoostBerry(ItemGanlonBerry, "Ganlon Berry", "defense", 1))
	registerItem(pinchBoostBerry(ItemPetayaBerry, "Petaya Berry", "spatk", 1))
	registerItem(pinchBoostBerry(ItemApicotBerry, "Apicot Berry", "spdef", 1))
	registerItem(pinchBoostBerry(ItemSalacBerry, "Salac Berry", "speed", 1))

	registerItem(&Item{
		Kind: ItemStarfBerry, Name: "Starf Berry", Berry: true,
		Desc:        "Sharply raises one random stat when the holder drops to a quarter HP or less. Consumed on use.",
		HPThreshold: pinchThreshold,
		OnHPThreshold: func(s *BattleState, side int, rng *RNG, log *[]LogLine) bool {
			p := s.Active(side)
			// Canon filters to stats that can still rise before drawing, so a
			// holder sitting at +6 Speed can't roll "speed" and get nothing.
			// With every stat maxed there is no stat to raise, and the berry
			// stays in reserve rather than being spent on a guaranteed no-op.
			eligible := make([]string, 0, len(starfStats))
			for _, stat := range starfStats {
				if ptr := stagePtr(p, stat); ptr != nil && *ptr < 6 {
					eligible = append(eligible, stat)
				}
			}
			if len(eligible) == 0 {
				return false
			}
			applyStages(p, side, eligible[rng.IntN(len(eligible))], 2, log)
			return true
		},
	})

	// Custap grants precedence inside the holder's priority bracket rather
	// than a stat. The volatile is the carrier: ResolveTurn arms it before
	// movers are ordered and goesFirst reads it. See applyCustapBerry.
	registerItem(&Item{
		Kind: ItemCustapBerry, Name: "Custap Berry", Berry: true,
		Desc: "Lets the holder move first within its priority bracket when it drops to a quarter HP or less. Consumed on use.",
	})

	registerItem(&Item{
		Kind: ItemMicleBerry, Name: "Micle Berry", Berry: true,
		Desc:        "Raises the accuracy of the holder's next move when it drops to a quarter HP or less. Consumed on use.",
		HPThreshold: pinchThreshold,
		OnHPThreshold: func(s *BattleState, side int, _ *RNG, log *[]LogLine) bool {
			p := s.Active(side)
			p.Volatiles.MicleTurns = micleDuration
			*log = append(*log, LogLine{
				Type: "item", Side: side,
				Text: fmt.Sprintf("%s boosted the accuracy of its next move!", p.Name),
			})
			return true
		},
	})
}

// micleAccuracyNum / micleAccuracyDen express the primed Micle Berry's ×1.2
// accuracy boost as integer math. The float 1.2 is just under 1.2 in binary, so
// `int(70 * 1.2)` truncates to 83 where canon gives 84 — a whole percentage
// point of accuracy lost to a rounding artifact on exactly the shaky moves the
// berry exists for.
const (
	micleAccuracyNum = 12
	micleAccuracyDen = 10
)

// micleDuration is how many end-of-turn ticks a primed Micle Berry survives.
// Canon gives the volatile a duration of 2: it is armed, lives through the
// following turn, and lapses if the holder never gets a move off. An indefinite
// prime would let a holder bank the boost through a long sleep.
const micleDuration = 2

// applyCustapBerry arms the holder's Custap Berry at the top of the turn, so
// the ordering pass can see it. Canon activates the berry before anyone moves
// (that is what lets a near-fainted holder outspeed a faster attacker), and the
// berry is spent whether or not the jump ends up mattering.
//
// Only a holder that is about to use a move qualifies: a switching holder isn't
// competing for a slot in the move order, so the berry stays in reserve.
func applyCustapBerry(s *BattleState, side int, act Action, log *[]LogLine) {
	if act.Kind != ActionMove {
		return
	}
	p := s.Active(side)
	if p.Fainted || p.HP <= 0 {
		return
	}
	it := itemOf(p)
	if it == nil || it.Kind != ItemCustapBerry {
		return
	}
	if float64(p.HP) > pinchThreshold*float64(p.MaxHP) {
		return
	}
	fireItemTrigger(p, side, it, log, func(sub *[]LogLine) bool {
		p.Volatiles.CustapBoost = true
		*sub = append(*sub, LogLine{
			Type: "item", Side: side,
			Text: fmt.Sprintf("%s can act faster than normal!", p.Name),
		})
		return true
	})
}

// --- damage reaction ---

// attackerChipBerry builds a Jaboca / Rowap berry: when a move of the given
// category connects on the holder, the attacker loses 1/8 of its max HP. Magic
// Guard on the attacker blocks it like any other indirect damage.
func attackerChipBerry(kind ItemKind, name string, cat domain.Category) *Item {
	desc := fmt.Sprintf("When hit by a %s move, the attacker loses 1/8 of its max HP. Consumed on use.", cat)
	return &Item{
		Kind: kind, Name: name, Berry: true, Desc: desc,
		OnHitTaken: func(s *BattleState, defSide int, m domain.Move, _ DamageResult, log *[]LogLine) bool {
			if m.Category != cat {
				return false
			}
			atk := s.Active(1 - defSide)
			if atk.Fainted || atk.HP <= 0 || abilityBlocksIndirectDamage(atk) {
				return false
			}
			itemDamage(atk, 1-defSide, atk.MaxHP/8, atk.Name+" was hurt! (-%d)", log)
			if atk.HP <= 0 {
				faint(atk, 1-defSide, log)
			}
			return true
		},
	}
}

// reactBoostBerry builds a Kee / Maranga berry: a connecting move of the given
// category raises one of the holder's defensive stages.
func reactBoostBerry(kind ItemKind, name, stat string, cat domain.Category) *Item {
	desc := fmt.Sprintf("Raises %s by 1 when the holder is hit by a %s move. Consumed on use.", statName(stat), cat)
	return &Item{
		Kind: kind, Name: name, Berry: true, Desc: desc,
		OnHitTaken: func(s *BattleState, defSide int, m domain.Move, _ DamageResult, log *[]LogLine) bool {
			if m.Category != cat {
				return false
			}
			def := s.Active(defSide)
			// Spent either way (canon eats the berry before running its
			// effect), but a Pokémon on 0 HP is about to faint and lose its
			// stages anyway — skip the boost line, keep the consume.
			if def.HP > 0 {
				applyStages(def, defSide, stat, 1, log)
			}
			return true
		},
	}
}

func registerReactionBerries() {
	registerItem(&Item{
		Kind: ItemEnigmaBerry, Name: "Enigma Berry", Berry: true,
		Desc: "Restores 1/4 of max HP when the holder is hit by a super-effective move. Consumed on use.",
		OnHitTaken: func(s *BattleState, defSide int, _ domain.Move, res DamageResult, log *[]LogLine) bool {
			if res.Effectiveness <= 1 {
				return false
			}
			def := s.Active(defSide)
			// The hook runs after the hit has been subtracted, so a holder that
			// is still standing is always missing HP. A holder the hit KO'd has
			// nothing to heal — canon eats the berry either way, but reviving a
			// 0-HP Pokémon is not what "restore 1/4" means.
			if def.HP <= 0 {
				return true
			}
			itemHealFraction(def, defSide, 0.25, "Enigma Berry", log)
			return true
		},
	})

	registerItem(attackerChipBerry(ItemJabocaBerry, "Jaboca Berry", domain.CatPhysical))
	registerItem(attackerChipBerry(ItemRowapBerry, "Rowap Berry", domain.CatSpecial))
	registerItem(reactBoostBerry(ItemKeeBerry, "Kee Berry", "defense", domain.CatPhysical))
	registerItem(reactBoostBerry(ItemMarangaBerry, "Maranga Berry", "spdef", domain.CatSpecial))
}

// --- type resist ---

// resistBerry builds one of the type-resist berries. Every one halves an
// incoming super-effective hit of its type and is then consumed; Chilan is the
// exception that fires on any Normal-type hit (anyEff), since nothing is weak
// to Normal.
func resistBerry(kind ItemKind, name string, t domain.Type, anyEff bool) *Item {
	desc := fmt.Sprintf("Halves the damage from a super-effective %s move. Consumed on use.", t)
	if anyEff {
		desc = fmt.Sprintf("Halves the damage from any %s move. Consumed on use.", t)
	}
	return &Item{
		Kind: kind, Name: name, Berry: true, Desc: desc,
		ResistType:             t,
		ResistAnyEffectiveness: anyEff,
	}
}

func registerResistBerries() {
	for _, b := range []struct {
		kind ItemKind
		name string
		typ  domain.Type
	}{
		{ItemOccaBerry, "Occa Berry", "fire"},
		{ItemPasshoBerry, "Passho Berry", "water"},
		{ItemWacanBerry, "Wacan Berry", "electric"},
		{ItemRindoBerry, "Rindo Berry", "grass"},
		{ItemYacheBerry, "Yache Berry", "ice"},
		{ItemChopleBerry, "Chople Berry", "fighting"},
		{ItemKebiaBerry, "Kebia Berry", "poison"},
		{ItemShucaBerry, "Shuca Berry", "ground"},
		{ItemCobaBerry, "Coba Berry", "flying"},
		{ItemPayapaBerry, "Payapa Berry", "psychic"},
		{ItemTangaBerry, "Tanga Berry", "bug"},
		{ItemChartiBerry, "Charti Berry", "rock"},
		{ItemKasibBerry, "Kasib Berry", "ghost"},
		{ItemHabanBerry, "Haban Berry", "dragon"},
		{ItemColburBerry, "Colbur Berry", "dark"},
		{ItemBabiriBerry, "Babiri Berry", "steel"},
		{ItemRoseliBerry, "Roseli Berry", "fairy"},
	} {
		registerItem(resistBerry(b.kind, b.name, b.typ, false))
	}
	registerItem(resistBerry(ItemChilanBerry, "Chilan Berry", "normal", true))
}
