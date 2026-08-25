package engine

import "fmt"

// items_seeds.go is the family of items that watch the *field* and pay out
// once when it turns their way: the four terrain seeds and Room Service.
//
// They share a shape that nothing else in the item registry has. Every other
// one-shot here is triggered by something happening to its holder — a hit
// landing, HP crossing a line, a stat moving. These are triggered by the board
// changing under a holder that did nothing at all, and upstream gives each of
// them the same pair of hooks to say so:
//
//	onStart / onSwitchInPriority   the holder arrives and the field is already right
//	onTerrainChange / onAnyPseudoWeatherChange   the field turns while it stands there
//
// So there are two firing points, not one, and missing the second is the
// natural bug: a Grassy Seed holder that was already out when the terrain went
// up is the commoner case in play, and it is the one a switch-in-only
// implementation silently drops.
//
// Room Service is filed with the seeds rather than with the field items next
// door because it is the same mechanism pointed at a pseudo-weather instead of
// a terrain — down to the sign of the boost being the only real difference.

const (
	ItemElectricSeed ItemKind = "electric-seed"
	ItemGrassySeed   ItemKind = "grassy-seed"
	ItemMistySeed    ItemKind = "misty-seed"
	ItemPsychicSeed  ItemKind = "psychic-seed"
	ItemRoomService  ItemKind = "room-service"
)

// seedTerrain maps each seed to the terrain that pays it out and the stat it
// raises. The Electric and Grassy seeds raise Defense; the Misty and Psychic
// ones raise Special Defense, matching the side of the board their terrain
// protects.
//
// All four ship although only one ported case wants two of them: they are the
// same three lines each, and a half-populated family is the kind of asymmetry
// that reads as a bug later.
var seedTerrain = map[ItemKind]struct {
	name     string
	terrain  TerrainKind
	stat     string
	statName string
}{
	ItemElectricSeed: {"Electric Seed", TerrainElectric, "defense", "Defense"},
	ItemGrassySeed:   {"Grassy Seed", TerrainGrassy, "defense", "Defense"},
	ItemMistySeed:    {"Misty Seed", TerrainMisty, "spdef", "Sp. Def"},
	ItemPsychicSeed:  {"Psychic Seed", TerrainPsychic, "spdef", "Sp. Def"},
}

func init() {
	for kind, spec := range seedTerrain {
		registerItem(&Item{
			Kind: kind,
			Name: spec.name,
			Desc: fmt.Sprintf("Raises the holder's %s by 1 stage on %s terrain, then is used up.",
				spec.statName, spec.terrain),
		})
	}
	registerItem(&Item{
		Kind: ItemRoomService,
		Name: "Room Service",
		Desc: "Lowers the holder's Speed by 1 stage while Trick Room is up, then is used up.",
	})
}

// applyFieldReactiveItems pays out any seed or Room Service whose field state
// is currently true, on both sides.
//
// Called from three places, which between them stand in for canon's four hooks:
// the switch-in tail (the holder arrived to a field already set), the terrain
// setter, and the Trick Room setter. There is no separate "the terrain expired"
// case because none of these fire on the field going *away*.
//
// Idempotent by construction: each item is consumed as it pays, so a second
// call over the same board finds nothing.
func applyFieldReactiveItems(s *BattleState, log *[]LogLine) {
	for side := 0; side < 2; side++ {
		applyFieldReactiveItem(s, side, log)
	}
}

// applyFieldReactiveItem is the one-side form, used by the switch-in path where
// only the arriving Pokémon can have been triggered.
func applyFieldReactiveItem(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p == nil || p.Fainted || p.HP <= 0 {
		return
	}
	it := itemOf(p)
	if it == nil {
		return
	}
	if spec, ok := seedTerrain[it.Kind]; ok {
		if s.Terrain != nil && s.Terrain.Kind == spec.terrain {
			consumeItemAnnounced(p, side, it, log)
			applyStages(p, side, spec.stat, 1, log)
		}
		return
	}
	if it.Kind == ItemRoomService && s.PseudoWeather.TrickRoom != nil {
		consumeItemAnnounced(p, side, it, log)
		// Self-inflicted, so it goes through applyStages rather than the
		// foe-induced path: Clear Body and Mist have nothing to say about an
		// item its own holder is using. Upstream agrees — the boost comes from
		// the item's own onUse with no source.
		applyStages(p, side, "speed", -1, log)
	}
}
