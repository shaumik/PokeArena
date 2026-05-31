package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// AbilityKind identifies a Pokémon's ability by slug (lowercase kebab-case,
// matching domain.Species.Abilities). The empty string means no ability is
// set (older data without the abilities field, or a hypothetical "no
// ability" entry); empty disables every hook.
//
// Hooks dispatch by switching on the kind, the same way weather.go does for
// field conditions. With four implemented abilities the switch is shorter
// and clearer than a registry of closures; we'll graduate to a table when
// the count climbs past ~10.
type AbilityKind string

const (
	AbilityNone      AbilityKind = ""
	AbilityIntimidate AbilityKind = "intimidate"
	AbilitySturdy     AbilityKind = "sturdy"
	AbilityLevitate   AbilityKind = "levitate"
	AbilityThickFat   AbilityKind = "thick-fat"
)

// defaultAbility picks slot 0 from a species' ability list — the convention
// from issue #30 for batches before the picker UI grows an ability dropdown.
// Returns AbilityNone when the species has no abilities (older curated
// dumps without the field). The engine treats unknown slugs as no-ops, so
// this is safe even if a species's slot 0 is an ability we haven't
// implemented yet.
func defaultAbility(sp domain.Species) AbilityKind {
	if len(sp.Abilities) == 0 {
		return AbilityNone
	}
	return AbilityKind(sp.Abilities[0])
}

// applyOnSwitchIn fires any on-switch-in ability effect for the active
// Pokémon on side. Called from doSwitch (post-switch) and from the first
// ResolveTurn (for the leads, who never went through doSwitch). Safe to
// call when the active is fainted or has no ability — no-op.
func applyOnSwitchIn(s *BattleState, side int, log *[]LogLine) {
	user := s.Active(side)
	if user.Fainted {
		return
	}
	switch user.Ability {
	case AbilityIntimidate:
		foeSide := 1 - side
		foe := s.Active(foeSide)
		if foe.Fainted {
			return
		}
		*log = append(*log, LogLine{Type: "ability", Side: side,
			Text: fmt.Sprintf("%s's Intimidate cuts %s's Attack!", user.Name, foe.Name)})
		// applyStages handles the -6 clamp + emits the "X's Attack fell!" line.
		applyStages(foe, foeSide, "attack", -1, log)
	}
}

// abilityTypeMultOverride lets an ability replace the type-effectiveness
// lookup. Returns (multiplier, true) when the ability overrides — currently
// only Levitate (Ground → 0). Callers should use this in place of the
// dex-only effectiveness when the boolean is true, since the ability fully
// supplants the type chart for that match-up.
func abilityTypeMultOverride(def *Pokemon, atkType domain.Type) (float64, bool) {
	if def == nil {
		return 1.0, false
	}
	switch def.Ability {
	case AbilityLevitate:
		if atkType == "ground" {
			return 0, true
		}
	}
	return 1.0, false
}

// abilityIncomingDamageMult returns the multiplier the defender's ability
// applies to incoming damage of the given move. 1.0 when no ability rule
// matches — Thick Fat is the only batch-1 ability that touches this hook
// (Fire and Ice → 0.5).
func abilityIncomingDamageMult(def *Pokemon, m domain.Move) float64 {
	if def == nil {
		return 1.0
	}
	switch def.Ability {
	case AbilityThickFat:
		if m.Type == "fire" || m.Type == "ice" {
			return 0.5
		}
	}
	return 1.0
}

// abilitySurviveOHKO clamps an incoming damage value to leave the defender
// at minimum 1 HP when its ability prevents one-shot KOs from full HP.
// Sturdy is the only batch-1 ability with this behavior. Returns the
// (possibly clamped) damage and whether the ability fired — callers use
// the boolean to emit the right log line on the survive turn.
func abilitySurviveOHKO(def *Pokemon, damage int) (int, bool) {
	if def == nil || damage <= 0 {
		return damage, false
	}
	if def.HP != def.MaxHP || damage < def.HP {
		// Sturdy only fires when the hit would KO from full HP; otherwise
		// the formula's normal damage applies.
		return damage, false
	}
	switch def.Ability {
	case AbilitySturdy:
		return def.HP - 1, true
	}
	return damage, false
}
