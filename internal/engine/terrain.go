package engine

import (
	"pokearena/internal/domain"
)

// TerrainKind identifies the active terrain. Empty means no terrain;
// concrete values are the slugs the domain layer's Move.Terrain field uses
// (set by the data-sync transform).
type TerrainKind string

const (
	TerrainNone     TerrainKind = ""
	TerrainElectric TerrainKind = "electric"
	TerrainGrassy   TerrainKind = "grassy"
	TerrainMisty    TerrainKind = "misty"
	TerrainPsychic  TerrainKind = "psychic"
)

// TerrainState is the active terrain. TurnsLeft counts down at end of turn
// (after residuals) and the terrain clears when it hits zero. Terrain Extender
// items aren't modeled, so every setter uses defaultTerrainTurns.
type TerrainState struct {
	Kind      TerrainKind `json:"kind"`
	TurnsLeft int         `json:"turns_left"`
}

// defaultTerrainTurns is how long a setter-spawned terrain lasts. Items would
// override this; none ship today.
const defaultTerrainTurns = 5

// isGrounded reports whether p is subject to terrain effects. Flying-types
// and Levitate float; everything else is grounded. (Air Balloon, Magnet
// Rise, Iron Ball, Roost, Smack Down aren't modeled yet — when they land,
// they fold into this predicate.)
func isGrounded(p *Pokemon) bool {
	if p == nil {
		return false
	}
	if isType(p, "flying") {
		return false
	}
	if a := abilityOf(p); a != nil && a.Kind == AbilityLevitate {
		return false
	}
	return true
}

// terrainDamageMult is the multiplier the active terrain applies to an
// incoming move. Three rules:
//
//  1. Type boost (Electric / Grassy / Psychic): the matching type gets
//     ×1.3 when the attacker is grounded. Gen 8+ value — Gen 6/7 used 1.5×;
//     Showdown's current default and our engine target is the modern 1.3×.
//  2. Misty: Dragon-type moves are halved when the *defender* is grounded.
//  3. Grassy: Earthquake / Bulldoze / Magnitude are halved against a
//     grounded defender (terrain absorbs ground-shake moves).
func terrainDamageMult(t *TerrainState, atk, def *Pokemon, m domain.Move) float64 {
	if t == nil {
		return 1.0
	}
	mult := 1.0
	switch t.Kind {
	case TerrainElectric:
		if m.Type == "electric" && isGrounded(atk) {
			mult *= 1.3
		}
	case TerrainGrassy:
		if m.Type == "grass" && isGrounded(atk) {
			mult *= 1.3
		}
		if isGrounded(def) {
			switch m.ID {
			case "earthquake", "bulldoze", "magnitude":
				mult *= 0.5
			}
		}
	case TerrainMisty:
		if m.Type == "dragon" && isGrounded(def) {
			mult *= 0.5
		}
	case TerrainPsychic:
		if m.Type == "psychic" && isGrounded(atk) {
			mult *= 1.3
		}
	}
	return mult
}

// terrainGrassyHeal is the 1/16 max-HP end-of-turn heal Grassy Terrain
// gives every grounded active. Magic Guard is irrelevant — heals don't
// count as indirect damage. Floored to 1.
func terrainGrassyHeal(t *TerrainState, p *Pokemon) int {
	if t == nil || t.Kind != TerrainGrassy || !isGrounded(p) || p.Fainted {
		return 0
	}
	d := p.MaxHP / 16
	if d < 1 {
		d = 1
	}
	return d
}

// terrainBlocksStatus reports whether the terrain refuses a non-volatile
// status on a grounded target. Misty blocks all status; Electric blocks
// Sleep only. Used by inflictStatus before the type-immunity / one-status
// gate so the move-failed path matches Showdown.
func terrainBlocksStatus(t *TerrainState, p *Pokemon, st StatusCond) bool {
	if t == nil || p == nil || !isGrounded(p) {
		return false
	}
	switch t.Kind {
	case TerrainMisty:
		return st == StatusBurn || st == StatusPoison || st == StatusToxic ||
			st == StatusParalysis || st == StatusSleep || st == StatusFreeze
	case TerrainElectric:
		return st == StatusSleep
	}
	return false
}

// terrainBlocksConfusion reports whether Misty Terrain refuses confusion
// on a grounded target (canonical Showdown behavior).
func terrainBlocksConfusion(t *TerrainState, p *Pokemon) bool {
	return t != nil && t.Kind == TerrainMisty && isGrounded(p)
}

// terrainBlocksPriorityAgainst reports whether a priority>0 move targeting
// a grounded foe is blocked by Psychic Terrain. Caller is responsible for
// the "doesn't affect" log line.
func terrainBlocksPriorityAgainst(t *TerrainState, def *Pokemon, prio int) bool {
	return t != nil && t.Kind == TerrainPsychic && prio > 0 && isGrounded(def)
}

// terrainStartedText / terrainContinuesText / terrainClearedText are the
// log-line flavor strings for setter / per-turn / expiry events. Match the
// pattern of weatherStartedText / weatherContinuesText / weatherClearedText.
func terrainStartedText(k TerrainKind) string {
	switch k {
	case TerrainElectric:
		return "An electric current ran across the battlefield!"
	case TerrainGrassy:
		return "Grass grew to cover the battlefield!"
	case TerrainMisty:
		return "Mist swirled around the battlefield!"
	case TerrainPsychic:
		return "The battlefield got weird!"
	}
	return ""
}

func terrainContinuesText(k TerrainKind) string {
	switch k {
	case TerrainElectric:
		return "The electric current is humming."
	case TerrainGrassy:
		return "The grass is rustling."
	case TerrainMisty:
		return "The mist hangs in the air."
	case TerrainPsychic:
		return "The battlefield still feels weird."
	}
	return ""
}

func terrainClearedText(k TerrainKind) string {
	switch k {
	case TerrainElectric:
		return "The electric current disappeared."
	case TerrainGrassy:
		return "The grass withered."
	case TerrainMisty:
		return "The mist disappeared."
	case TerrainPsychic:
		return "The weirdness disappeared."
	}
	return ""
}
