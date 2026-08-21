package engine

import (
	"reflect"
	"sort"
)

// Auditing the registry for mechanics that do nothing.
//
// A roster is legal long before it is sound: ValidateTeam checks that a
// species exists, that it can learn the move, that the spread is inside the
// caps. None of that notices when a pick is built on an ability the engine
// models as nothing at all. A tournament team declared Harvest a pillar of
// its strategy and found out mid-match, by reading source, that the slug was
// registered inert — it played the whole tournament a Pokémon and a half
// short. The check below is what would have said so beforehand.

// abilityInert is the machine-readable half of the "--- recognized but inert
// ---" group in abilities.go: slug → why it does nothing. The doc comment on
// that group is the prose version and the registrations under it are the
// implementation; TestInertAbilitiesAreFiledAsInert pins all three against each
// other, and additionally requires that any *other* hookless registration be
// read by name somewhere in the package — so an inert ability added without an
// entry here fails the build rather than quietly passing this audit.
var abilityInert = map[AbilityKind]string{
	"unnerve":          "registered but inert: needs the foe's berries suppressed while the holder is out",
	"neutralizing-gas": "registered but inert: needs a battle-state-aware ability lookup",
	"forewarn":         "registered but inert: needs the dex threaded into the switch-in hook",
	"illuminate":       "inert by design: it changes wild-encounter rates, and there are none here",
	"run-away":         "inert by design: it guarantees fleeing a wild battle, and there are none here",
	"healer":           "inert by design: it heals an ally, and singles has no ally",
}

// abilityHasBehaviour reports whether a registry entry carries anything at all
// beyond its own name. Every field except Kind is a hook, a flag or a
// multiplier, so a record that is otherwise entirely zero is a record that no
// dispatcher will ever act on. Reflection rather than a hand-written list: a
// hook added to the struct is covered the day it is added, and cannot be
// forgotten here.
func abilityHasBehaviour(a *Ability) bool {
	v := reflect.ValueOf(*a)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Name == "Kind" {
			continue
		}
		if !v.Field(i).IsZero() {
			return true
		}
	}
	return false
}

// AbilityInertReason returns "" for an ability the engine actually models, and
// a short explanation for one it does not. An unregistered slug counts as
// inert in the strongest sense: abilityOf returns nil for it, so every lookup
// in the engine passes it by.
func AbilityInertReason(slug string) string {
	if slug == "" {
		return ""
	}
	kind := AbilityKind(slug)
	if _, registered := abilityRegistry[kind]; !registered {
		return "the engine has no record of this ability at all — every lookup passes it by"
	}
	return abilityInert[kind]
}

// ItemInertReason is the same question for a held item. The item catalog can
// list an item the engine does not model; itemOf returns nil for it and every
// item dispatcher no-ops, so the holder is effectively playing bare.
func ItemInertReason(slug string) string {
	if slug == "" {
		return ""
	}
	if _, modeled := itemRegistry[ItemKind(slug)]; modeled {
		return ""
	}
	return "the engine models no behaviour for this item — the holder plays as if bare"
}

// InertAbilities lists every registered slug that does nothing, sorted.
func InertAbilities() []string {
	out := make([]string, 0, len(abilityInert))
	for kind := range abilityInert {
		out = append(out, string(kind))
	}
	sort.Strings(out)
	return out
}

// hooklessAbilities lists every registered slug whose record is empty beyond
// its own name, sorted. A slug is here either because it is inert or because
// the layer that implements it reads the slug directly (Gluttony via
// pinchThresholdFor, Sticky Hold via itemIsRemovable, Arena Trap via the
// switch-blocking check). The filing test uses it to tell those two apart, and
// that is what keeps abilityInert honest as the registry grows.
func hooklessAbilities() []string {
	var out []string
	for kind, a := range abilityRegistry {
		if !abilityHasBehaviour(a) {
			out = append(out, string(kind))
		}
	}
	sort.Strings(out)
	return out
}
