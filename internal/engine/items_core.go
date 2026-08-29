package engine

import "github.com/shaumik/PokeArena/internal/domain"

// items_core.go holds the original curated six: the always-on stat and damage
// modifiers that shipped with the item scaffold. Their hooks are the ones the
// damage formula and the end-of-turn residual block were built around; the
// item families added since (berries, and the rest) sit in sibling files.

const (
	ItemLeftovers   ItemKind = "leftovers"
	ItemChoiceBand  ItemKind = "choice-band"
	ItemChoiceSpecs ItemKind = "choice-specs"
	ItemChoiceScarf ItemKind = "choice-scarf"
	ItemLifeOrb     ItemKind = "life-orb"
	ItemFocusSash   ItemKind = "focus-sash"
)

func init() {
	registerItem(&Item{
		Kind: ItemLeftovers,
		Name: "Leftovers",
		Desc: "Restores 1/16 of max HP at the end of every turn.",
		EndOfTurn: func(s *BattleState, side int, log *[]LogLine) {
			itemHealFraction(s.Active(side), side, 1.0/16, "Leftovers", log)
		},
	})

	registerItem(&Item{
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
	})

	registerItem(&Item{
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
	})

	registerItem(&Item{
		Kind:       ItemChoiceScarf,
		Name:       "Choice Scarf",
		Desc:       "Speed is 1.5x, but the holder is locked into its first move until it switches out.",
		ChoiceLock: true,
		SpeedMult:  func(p *Pokemon, w *WeatherState) float64 { return 1.5 },
	})

	registerItem(&Item{
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
	})

	registerItem(&Item{
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
	})
}
