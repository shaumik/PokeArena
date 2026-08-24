package engine

import (
	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

func init() {
	specs.RegisterTerrain("electric")
	specs.RegisterTerrain("grassy")
	specs.RegisterTerrain("misty")
	specs.RegisterTerrain("psychic")
}

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
// (after residuals) and the terrain clears when it hits zero. A setter reads
// the caster's held Terrain Extender through terrainTurnsFor, which pushes the
// duration past defaultTerrainTurns.
type TerrainState struct {
	Kind      TerrainKind `json:"kind"`
	TurnsLeft int         `json:"turns_left"`
}

// defaultTerrainTurns is how long a setter-spawned terrain lasts without an
// extender. Terrain Extender overrides it; see terrainTurnsFor.
const defaultTerrainTurns = 5

// groundState is the answer to "is this Pokemon on the ground?", and it has
// three values rather than two because canon's does. Showdown's
// Pokemon#isGrounded (sim/pokemon.ts) returns true, false, or null, where null
// means "airborne because of an ability" — the one flavor of airborne that a
// mold-breaking attacker sees through, and the one that announces itself in the
// log. Go has no null, so the third state gets a name.
type groundState int

const (
	// airborne: off the ground for a reason no attacker can ignore — the
	// Flying type, Magnet Rise, Telekinesis, an Air Balloon.
	airborne groundState = iota
	// grounded: on the ground, and therefore subject to Ground-type moves,
	// the entry hazards, Arena Trap and every terrain effect.
	grounded
	// airborneByAbility: off the ground because of Levitate. Airborne
	// everywhere the plain question is asked; grounded to an attacker whose
	// ability ignores the defender's.
	airborneByAbility
)

// groundedness is the single predicate every rule that cares about the ground
// reads. Canon has exactly one, and this engine used to have two — isGrounded
// here for terrain, hazards and Arena Trap, and an ad-hoc chain inside
// computeDamage for Ground-move immunity — with neither list a superset of the
// other and Gravity in neither. Porting Showdown's test suite priced that
// split at around twenty cases across five subsystems, so the two are now one.
//
// The order is Showdown's, and every step of it is load-bearing: Gravity,
// Ingrain and Smack Down drag a floater down and beat everything below them;
// an Iron Ball beats the Flying type and Levitate; the Flying type beats
// Levitate; Levitate beats Magnet Rise, Telekinesis and an Air Balloon.
//
// negateImmunity is upstream's parameter of the same name, raised by the
// NegateImmunity event — in this dataset only Ring Target raises it. It skips
// the *type-chart* leg and nothing else, which is why a Ring Target holder with
// Levitate or Magnet Rise is still untouchable by Earthquake.
func groundedness(p *Pokemon, pw *PseudoWeather, negateImmunity bool) groundState {
	if p == nil {
		return airborne
	}
	// Gravity, Ingrain and Smack Down ground unconditionally.
	if pw != nil && pw.Gravity != nil {
		return grounded
	}
	if p.Volatiles.Ingrain || p.Volatiles.SmackDown {
		return grounded
	}
	// Iron Ball drags the holder down regardless of typing or Levitate, so it
	// is checked before either of them.
	if itemGrounds(p) {
		return grounded
	}
	// roostTypes is the Flying-suppressing view of the defender's typing, so a
	// Pokemon that spent the turn roosting is on the ground for this turn —
	// which is upstream's rule too, by way of the roost volatile's onType.
	t1, t2 := roostTypes(p)
	if !negateImmunity && (t1 == "flying" || t2 == "flying") {
		return airborne
	}
	if a := abilityOf(p); a != nil && a.Kind == AbilityLevitate {
		return airborneByAbility
	}
	if p.Volatiles.MagnetRise != nil || p.Volatiles.Telekinesis != nil {
		return airborne
	}
	// Air Balloon is Iron Ball's mirror image, and loses to it above.
	if itemFloats(p) {
		return airborne
	}
	return grounded
}

// isGrounded is the plain two-valued question — the one terrain and Arena Trap
// ask. Levitate reads as airborne here, the same way upstream's null does in a
// boolean context.
func isGrounded(p *Pokemon, pw *PseudoWeather) bool {
	return groundedness(p, pw, false) == grounded
}

// isGroundedOnEntry is the same question asked by the entry hazards, which run
// inside whatever move brought the Pokemon in. That matters for exactly one
// thing: a Levitate holder dragged out by a mold breaker's Roar lands on the
// Spikes, because upstream's suppression lasts as long as the move is
// resolving and the drag is part of that move.
func isGroundedOnEntry(s *BattleState, p *Pokemon) bool {
	switch groundedness(p, &s.PseudoWeather, false) {
	case grounded:
		return true
	case airborneByAbility:
		return abilitySuppressed(s, p)
	}
	return false
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
func terrainDamageMult(t *TerrainState, pw *PseudoWeather, atk, def *Pokemon, m domain.Move) float64 {
	if t == nil {
		return 1.0
	}
	mult := 1.0
	switch t.Kind {
	case TerrainElectric:
		if m.Type == "electric" && isGrounded(atk, pw) {
			mult *= 1.3
		}
	case TerrainGrassy:
		if m.Type == "grass" && isGrounded(atk, pw) {
			mult *= 1.3
		}
		if isGrounded(def, pw) {
			switch m.ID {
			case "earthquake", "bulldoze", "magnitude":
				mult *= 0.5
			}
		}
	case TerrainMisty:
		if m.Type == "dragon" && isGrounded(def, pw) {
			mult *= 0.5
		}
	case TerrainPsychic:
		if m.Type == "psychic" && isGrounded(atk, pw) {
			mult *= 1.3
		}
	}
	return mult
}

// terrainGrassyHeal is the 1/16 max-HP end-of-turn heal Grassy Terrain
// gives every grounded active. Magic Guard is irrelevant — heals don't
// count as indirect damage. Floored to 1.
func terrainGrassyHeal(t *TerrainState, pw *PseudoWeather, p *Pokemon) int {
	if t == nil || t.Kind != TerrainGrassy || !isGrounded(p, pw) || p.Fainted {
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
func terrainBlocksStatus(t *TerrainState, pw *PseudoWeather, p *Pokemon, st StatusCond) bool {
	if t == nil || p == nil || !isGrounded(p, pw) {
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
func terrainBlocksConfusion(t *TerrainState, pw *PseudoWeather, p *Pokemon) bool {
	return t != nil && t.Kind == TerrainMisty && isGrounded(p, pw)
}

// terrainBlocksPriorityAgainst reports whether a priority>0 move targeting
// a grounded foe is blocked by Psychic Terrain. Caller is responsible for
// the "doesn't affect" log line.
func terrainBlocksPriorityAgainst(t *TerrainState, pw *PseudoWeather, def *Pokemon, prio int) bool {
	return t != nil && t.Kind == TerrainPsychic && prio > 0 && isGrounded(def, pw)
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

// applyTerrainSetter spawns or refreshes the battle-level terrain. Mirrors
// applyWeatherSetter — setting the same terrain that's already active
// fails. Called from applyStatusMove's terrain-field dispatch.
func applyTerrainSetter(s *BattleState, side int, kind TerrainKind, log *[]LogLine) {
	if s.Terrain != nil && s.Terrain.Kind == kind {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	s.Terrain = &TerrainState{Kind: kind, TurnsLeft: terrainTurnsFor(s.Active(side), defaultTerrainTurns)}
	*log = append(*log, LogLine{Type: "terrain", Side: -1, Text: terrainStartedText(kind)})
}

// clearTerrain sweeps the field terrain, returning true if there was one.
// Ice Spinner's onAfterHit is the only caller today; Defog clears the terrain
// too in canon and should join it — hazards.go already carries a note saying
// so, and the missing helper was the reason it hadn't.
func clearTerrain(s *BattleState, log *[]LogLine) bool {
	if s.Terrain == nil {
		return false
	}
	kind := s.Terrain.Kind
	s.Terrain = nil
	*log = append(*log, LogLine{Type: "terrain", Side: -1, Text: terrainClearedText(kind)})
	return true
}
