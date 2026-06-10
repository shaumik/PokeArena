// Package specs holds the engine's vocabulary — the slugs that name every
// volatile, side condition, weather, terrain, status, flag, and boost stat
// the engine understands. It is the single source of truth read by the
// dataset validator (internal/domain), the data-sync filter
// (cmd/data-sync), and the coverage audit (internal/engine).
//
// Static vocabularies (Statuses, Flags, BoostStats) are declared inline —
// they don't grow per move-batch. Dynamic vocabularies (Volatiles, Side
// Conditions, Weathers, Terrains) are populated by Register* calls in
// init() functions from the matching mechanic file in internal/engine.
//
// Because population happens at engine package init, any binary that
// consumes specs must also import internal/engine (even if only for side
// effects: `_ "pokearena/internal/engine"`). cmd/data-validate and
// cmd/data-sync do exactly that — their validation work runs after engine
// init has populated the registries.
package specs

// Statuses is the engine's non-volatile status vocabulary. Stable across
// move batches — additions here mean a new StatusCond constant in
// internal/engine/battle.go.
var Statuses = map[string]bool{
	"burn":      true,
	"poison":    true,
	"toxic":     true,
	"paralysis": true,
	"sleep":     true,
	"freeze":    true,
}

// BoostStats is the engine's stat-stage vocabulary — the keys the Effect
// schema's Boosts map may carry. Mirrors stagePtr in state.go.
var BoostStats = map[string]bool{
	"attack":   true,
	"defense":  true,
	"spatk":    true,
	"spdef":    true,
	"speed":    true,
	"accuracy": true,
	"evasion":  true,
}

// Flags is the move-flag vocabulary the engine actually consumes plus
// the hook anchors reserved for future abilities and items. Stable
// across move batches; additions track new ability/item integration.
var Flags = map[string]bool{
	"contact":            true,
	"punch":              true,
	"bite":               true,
	"sound":              true,
	"powder":             true,
	"bypass-acc":         true,
	"high-crit":          true,
	"two-turn":           true,
	"multi-hit":          true,
	"recharge":           true,
	"selfdestruct":       true,
	"fixed-damage-level": true,
	// Ability/item hook anchors — informational until their consumers ship.
	"bullet":          true,
	"slicing":         true,
	"wind":            true,
	"dance":           true,
	"pulse":           true,
	"heal":            true,
	"defrost":         true,
	"bypass-sub":      true,
	"bypass-protect":  true,
	"ignore-immunity": true,
}

// Volatiles, SideConditions, Weathers, Terrains, PseudoWeathers are
// populated by Register* calls from engine mechanic files at package
// init time. They start empty; a binary that doesn't import
// internal/engine will see them empty (and reject every move that
// names a slug, which is the safe default for "engine not loaded").
var (
	Volatiles      = map[string]bool{}
	SideConditions = map[string]bool{}
	SlotConditions = map[string]bool{}
	Weathers       = map[string]bool{}
	Terrains       = map[string]bool{}
	PseudoWeathers = map[string]bool{}
)

// RegisterVolatile adds slug to the volatile vocabulary. Called from
// mechanic files in internal/engine via init().
func RegisterVolatile(slug string) { Volatiles[slug] = true }

// RegisterSideCondition adds slug to the side-condition vocabulary.
func RegisterSideCondition(slug string) { SideConditions[slug] = true }

// RegisterWeather adds slug to the weather vocabulary.
func RegisterWeather(slug string) { Weathers[slug] = true }

// RegisterTerrain adds slug to the terrain vocabulary.
func RegisterTerrain(slug string) { Terrains[slug] = true }

// RegisterPseudoWeather adds slug to the pseudo-weather vocabulary.
// Pseudo-weathers are field-wide non-weather conditions (Trick Room,
// Wonder Room, Magic Room, Gravity); see internal/engine/pseudoweather.go.
func RegisterPseudoWeather(slug string) { PseudoWeathers[slug] = true }

// RegisterSlotCondition adds slug to the slot-condition vocabulary.
// Slot conditions attach to a side's active slot — not the Pokémon —
// so they persist across switches. Wish and Healing Wish are the only
// modeled instances. See internal/engine/slotconditions.go.
func RegisterSlotCondition(slug string) { SlotConditions[slug] = true }
