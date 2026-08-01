package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// items_reactive.go is the fourth item family: things that answer an *event*
// rather than a state. A hit of the right type lands, a move misses, the holder
// gets outsped — and the item does something once. Plus the three that bend
// turn order and accuracy, which are events of a different kind (the moment the
// engine decides who goes first, and the moment it rolls to hit).
//
// Most of these are one-shot and go through the same fireItemTrigger contract
// the berries use. The accuracy and turn-order items are the exceptions: they
// are permanent, because "the holder is a bit harder to hit" is not an event
// that can be spent.
//
// Deferred, with reasons, rather than half-built:
//
//   - Eject Button / Eject Pack / Red Card force a switch in the middle of a
//     move's resolution. The engine can drag a Pokémon out (applyForceSwitch)
//     but doing it from inside dealDamage — after damage, before the attacker's
//     post-move tail — reorders faint resolution, self-switch, and the pinch
//     checks all at once. That is a turn-resolution change, not an item, and it
//     wants its own reviewed pass.
//   - Power Herb skips a two-turn move's charge turn, which means resolving the
//     strike from inside the charge branch of executeMove.
//   - Mirror Herb copies the foe's boosts as they happen, needing a
//     stat-change event the engine does not emit today.
//   - Room Service keys on Trick Room *starting*, which pseudo-weather setters
//     don't announce to items.

const (
	// Reactive stat boosts.
	ItemWeaknessPolicy ItemKind = "weakness-policy"
	ItemAbsorbBulb     ItemKind = "absorb-bulb"
	ItemCellBattery    ItemKind = "cell-battery"
	ItemLuminousMoss   ItemKind = "luminous-moss"
	ItemSnowball       ItemKind = "snowball"
	ItemThroatSpray    ItemKind = "throat-spray"
	ItemBlunderPolicy  ItemKind = "blunder-policy"

	// Herbs.
	ItemWhiteHerb  ItemKind = "white-herb"
	ItemMentalHerb ItemKind = "mental-herb"

	// Flinch.
	ItemKingsRock ItemKind = "king-s-rock"
	ItemRazorFang ItemKind = "razor-fang"

	// Accuracy and evasion.
	ItemWideLens     ItemKind = "wide-lens"
	ItemZoomLens     ItemKind = "zoom-lens"
	ItemBrightPowder ItemKind = "bright-powder"
	ItemLaxIncense   ItemKind = "lax-incense"

	// Turn order.
	ItemQuickClaw   ItemKind = "quick-claw"
	ItemLaggingTail ItemKind = "lagging-tail"
	ItemFullIncense ItemKind = "full-incense"

	// Multi-hit.
	ItemLoadedDice ItemKind = "loaded-dice"
)

const (
	// flinchItemChance is the percent chance King's Rock / Razor Fang add a
	// flinch to a damaging move that doesn't already cause one.
	flinchItemChance = 10
	// quickClawChance is the percent chance the holder jumps its bracket.
	quickClawChance = 20
	// loadedDiceMinHits is the floor Loaded Dice puts under a [2,5] multi-hit.
	loadedDiceMinHits = 4
)

func init() {
	registerReactiveBoosts()
	registerHerbs()
	registerFlinchItems()
	registerAccuracyItems()
	registerTurnOrderItems()
	registerMultihitItems()
}

// --- reactive stat boosts ---

// typeReactBoost builds an Absorb Bulb / Cell Battery / Luminous Moss /
// Snowball: a connecting move of the named type raises one of the holder's
// stages, once.
//
// The type is checked, not the effectiveness — Absorb Bulb answers any Water
// hit, including a resisted one. A holder the hit KO'd gets nothing, since the
// stage would be wiped by the faint a moment later, but the item is still spent
// (canon consumes before it applies).
func typeReactBoost(kind ItemKind, name string, t domain.Type, stat string) *Item {
	desc := fmt.Sprintf("Raises %s by 1 when the holder is hit by a %s-type move. Consumed on use.",
		statName(stat), t)
	return &Item{
		Kind: kind, Name: name, Desc: desc,
		OnHitTaken: func(s *BattleState, defSide int, m domain.Move, _ DamageResult, log *[]LogLine) bool {
			// A holder the hit KO'd never gets to use it: canon leaves the item
			// on the fainted Pokémon rather than announcing a boost the faint
			// would wipe a moment later.
			def := s.Active(defSide)
			if m.Type != t || def.HP <= 0 {
				return false
			}
			applyStages(def, defSide, stat, 1, log)
			return true
		},
	}
}

func registerReactiveBoosts() {
	registerItem(&Item{
		Kind: ItemWeaknessPolicy, Name: "Weakness Policy", Desc: "Sharply raises Attack and Sp. Atk when the holder is hit by a super-effective move. Consumed on use.",
		OnHitTaken: func(s *BattleState, defSide int, _ domain.Move, res DamageResult, log *[]LogLine) bool {
			def := s.Active(defSide)
			if res.Effectiveness <= 1 || def.HP <= 0 {
				return false
			}
			applyStages(def, defSide, "attack", 2, log)
			applyStages(def, defSide, "spatk", 2, log)
			return true
		},
	})

	registerItem(typeReactBoost(ItemAbsorbBulb, "Absorb Bulb", "water", "spatk"))
	registerItem(typeReactBoost(ItemCellBattery, "Cell Battery", "electric", "attack"))
	registerItem(typeReactBoost(ItemLuminousMoss, "Luminous Moss", "water", "spdef"))
	registerItem(typeReactBoost(ItemSnowball, "Snowball", "ice", "attack"))

	// Throat Spray answers the holder's *own* move rather than an incoming one,
	// so it hangs off the attacker-side hook. Sound moves only. The dispatcher
	// decides *when*: canon's onAfterMoveSecondarySelf runs at the tail of the
	// hit loop, so a sound move that resolved pays out and one stopped by
	// Protect, a miss, or an immunity does not.
	registerItem(&Item{
		Kind: ItemThroatSpray, Name: "Throat Spray", Desc: "Raises Sp. Atk by 1 when the holder uses a sound-based move. Consumed on use.",
		OnMoveUsed: func(s *BattleState, side int, m domain.Move, log *[]LogLine) bool {
			if !m.HasFlag("sound") {
				return false
			}
			applyStages(s.Active(side), side, "spatk", 1, log)
			return true
		},
	})

	// Blunder Policy is the mirror image: it answers the holder's own failure.
	// No accuracy gate here — the dispatcher only reaches this hook on a
	// genuine accuracy-roll failure, and a 100-accuracy move whiffing into a
	// +6-evasion target is exactly the blunder the item exists for. A move
	// *refused* rather than missed (Soundproof, Safety Goggles) never rolls, so
	// it never reaches this hook at all.
	registerItem(&Item{
		Kind: ItemBlunderPolicy, Name: "Blunder Policy", Desc: "Sharply raises Speed when the holder's move misses. Consumed on use.",
		OnMoveMissed: func(s *BattleState, side int, _ domain.Move, log *[]LogLine) bool {
			applyStages(s.Active(side), side, "speed", 2, log)
			return true
		},
	})
}

// --- herbs ---

func registerHerbs() {
	registerItem(&Item{
		Kind: ItemWhiteHerb, Name: "White Herb", Desc: "Restores any lowered stats to normal. Consumed on use.",
		// Checked at the same points a pinch berry is, since a stat drop can
		// land at any of them. Returning false when nothing is negative is what
		// keeps the herb in reserve through a turn of pure boosts.
		OnStatCheck: func(p *Pokemon, side int, log *[]LogLine) bool {
			restored := false
			for _, stat := range allStatSlugs {
				if ptr := stagePtr(p, stat); ptr != nil && *ptr < 0 {
					*ptr = 0
					restored = true
				}
			}
			if !restored {
				return false
			}
			*log = append(*log, LogLine{
				Type: "item", Side: side,
				Text: fmt.Sprintf("%s restored its lowered stats!", p.Name),
			})
			return true
		},
	})

	registerItem(&Item{
		Kind: ItemMentalHerb, Name: "Mental Herb", Desc: "Frees the holder from Attract, Taunt, Encore, Disable, and Torment. Consumed on use.",
		OnStatCheck: func(p *Pokemon, side int, log *[]LogLine) bool {
			v := &p.Volatiles
			if !v.Attract && v.Taunt == nil && v.Encore == nil && v.Disable == nil && !v.Torment {
				return false
			}
			v.Attract, v.Torment = false, false
			v.Taunt, v.Encore, v.Disable = nil, nil, nil
			*log = append(*log, LogLine{
				Type: "item", Side: side,
				Text: fmt.Sprintf("%s snapped out of it!", p.Name),
			})
			return true
		},
	})
}

// allStatSlugs is the stage set White Herb sweeps, in the stable order
// orderedBoostStats uses so the restore is deterministic.
var allStatSlugs = []string{"attack", "defense", "spatk", "spdef", "speed", "accuracy", "evasion"}

// --- flinch ---

// flinchItem builds a King's Rock / Razor Fang. Both add a flat 10% flinch to
// the holder's damaging moves. A move that already carries a flinch secondary
// is untouched — canon does not stack the two, and doubling Iron Head's flinch
// rate would be a much bigger item than either of these is.
func flinchItem(kind ItemKind, name string) *Item {
	return &Item{
		Kind: kind, Name: name,
		Desc: "The holder's damaging moves gain a 10% chance to make the target flinch.",
		OnDealtDamage: func(s *BattleState, atkSide, dmg int, m domain.Move, rng *RNG, log *[]LogLine) {
			if dmg <= 0 || moveAlreadyFlinches(m) {
				return
			}
			def := s.Active(1 - atkSide)
			if def.Fainted || def.HP <= 0 || abilityBlocksFlinch(def) {
				return
			}
			// Canon implements these items as an *added effect* pushed onto the
			// move, so anything that refuses added effects refuses them too.
			if abilityBlocksSecondaries(def) || itemBlocksSecondaries(def) {
				return
			}
			if !rng.Chance(flinchItemChance) {
				return
			}
			applyVolatile(def, 1-atkSide, "flinch", m, s, rng, log)
		},
	}
}

// moveAlreadyFlinches reports whether m carries its own flinch secondary, in
// which case the flinch items add nothing.
func moveAlreadyFlinches(m domain.Move) bool {
	for i := range m.Secondaries {
		if m.Secondaries[i].Volatile == "flinch" {
			return true
		}
	}
	return false
}

func registerFlinchItems() {
	registerItem(flinchItem(ItemKingsRock, "King's Rock"))
	registerItem(flinchItem(ItemRazorFang, "Razor Fang"))
}

// --- accuracy and evasion ---

func registerAccuracyItems() {
	registerItem(&Item{
		Kind: ItemWideLens, Name: "Wide Lens", Desc: "Raises the accuracy of the holder's moves by 10%.",
		AccuracyMult: 1.1,
	})

	// Zoom Lens only pays out when the holder is moving second, which the turn
	// loop records on the foe as "already moved".
	registerItem(&Item{
		Kind: ItemZoomLens, Name: "Zoom Lens", Desc: "Raises the holder's accuracy by 20% when it moves after the target.",
		AccuracyMultIf: func(s *BattleState, side int) float64 {
			if s.Active(1 - side).Volatiles.MovedThisTurn {
				return 1.2
			}
			return 1
		},
	})

	registerItem(&Item{
		Kind: ItemBrightPowder, Name: "Bright Powder", Desc: "Lowers the accuracy of moves aimed at the holder by 10%.",
		AccuracyMultVs: 0.9,
	})
	registerItem(&Item{
		Kind: ItemLaxIncense, Name: "Lax Incense", Desc: "Lowers the accuracy of moves aimed at the holder by 10%.",
		AccuracyMultVs: 0.9,
	})
}

// --- turn order ---

func registerTurnOrderItems() {
	// Quick Claw rolls at the top of the turn, like Custap, and rides the same
	// bracket-precedence volatile. The roll happens for every holder every turn
	// (canon), so it does consume RNG — unlike Focus Band, which only rolls on
	// a hit that would be lethal.
	registerItem(&Item{
		Kind: ItemQuickClaw, Name: "Quick Claw", Desc: "20% chance each turn to move first within the holder's priority bracket.",
		QuickDrawChance: quickClawChance,
	})

	for _, tc := range []struct {
		kind ItemKind
		name string
	}{
		{ItemLaggingTail, "Lagging Tail"},
		{ItemFullIncense, "Full Incense"},
	} {
		registerItem(&Item{
			Kind: tc.kind, Name: tc.name,
			Desc:      "The holder always moves last within its priority bracket.",
			MovesLast: true,
		})
	}
}

// --- multi-hit ---

func registerMultihitItems() {
	registerItem(&Item{
		Kind: ItemLoadedDice, Name: "Loaded Dice", Desc: "The holder's multi-hit moves hit at least 4 times.",
		MinMultihit: loadedDiceMinHits,
	})
}
