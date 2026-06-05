package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// SupportedFlags lists the move flags the engine actually consumes. The grep
// of truth is HasFlag call sites in turn.go / damage.go / abilities.go — any
// flag not in this set is silently a no-op at battle time.
//
// Adding a flag here is a contract: implement its behavior in the engine in
// the same change. Removing one requires removing the corresponding HasFlag
// site (and likely scrubbing it from data/moves.json via transform.go).
var SupportedFlags = map[string]bool{
	"bypass-acc":         true,
	"contact":            true,
	"fixed-damage-level": true,
	"high-crit":          true,
	"punch":              true,
	"recharge":           true,
	"selfdestruct":       true,
	"sound":              true,
	"two-turn":           true,
}

// SupportedVolatiles is the engine's vocabulary of move-inflicted volatile
// conditions — the switch in applyVolatile (turn.go). A move whose upstream
// volatileStatus falls outside this set is silently de-featured to "damage
// only" (e.g. Outrage's "lockedmove", Leech Seed's "leechseed").
var SupportedVolatiles = map[string]bool{
	"confusion":        true,
	"flinch":           true,
	"partiallytrapped": true,
}

// SupportedStatuses is the engine's non-volatile status vocabulary — the
// StatusCond constants in battle.go.
var SupportedStatuses = map[string]bool{
	"burn":      true,
	"freeze":    true,
	"paralysis": true,
	"poison":    true,
	"sleep":     true,
	"toxic":     true,
}

// SupportedWeathers is the engine's weather vocabulary — the WeatherKind
// constants in weather.go.
var SupportedWeathers = map[string]bool{
	"rain":      true,
	"sandstorm": true,
	"snow":      true,
	"sun":       true,
}

// upstreamWeatherToEngine mirrors cmd/data-sync/transform.go's weatherSlug.
// Showdown's mixed-case weather IDs (RainDance, sunnyday, hail, snowscape)
// land in upstream/moves.json verbatim; the audit must apply the same
// mapping as the transform before deciding whether the engine supports
// that weather. Hail and Snowscape both collapse to "snow" (Gen-9 unify).
// Kept in sync manually — the transform's weatherSlug is the source of
// truth; if it grows an entry, mirror it here.
var upstreamWeatherToEngine = map[string]string{
	"raindance": "rain",
	"sunnyday":  "sun",
	"sandstorm": "sandstorm",
	"snowscape": "snow",
	"hail":      "snow",
}

// MoveGap is a curated-and-pickable move whose upstream Showdown definition
// declares behavior the engine does not currently model. Reasons are listed
// in a fixed order so the report diffs cleanly across runs.
//
// Gaps are detected from the **declarative** shape of upstream data only —
// behavior encoded in Showdown JS callbacks (basePowerCallback, onModifyMove,
// etc.) is invisible to this audit. Notably, Moonlight / Morning Sun /
// Synthesis look fully covered here even though their weather-aware heal
// isn't modeled; same for Stored Power's variable basePower. Those need
// their own tests once the engine grows hooks for them.
type MoveGap struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Reasons []string `json:"reasons"`
}

// upstreamMove is the subset of Showdown's per-move fields the audit looks
// at. Every "feature-engaged?" field is RawMessage because Showdown
// overloads the type: selfSwitch can be bool or string ("copyvolatile" for
// Baton Pass); multihit can be int or [low,high]; ohko can be bool or string
// ("ice" for Sheer Cold's type-conditional immunity). isTruthyJSON
// normalises the "is the feature engaged on this move?" question.
type upstreamMove struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	SelfSwitch      json.RawMessage `json:"selfSwitch"`
	ForceSwitch     json.RawMessage `json:"forceSwitch"`
	Multihit        json.RawMessage `json:"multihit"`
	OHKO            json.RawMessage `json:"ohko"`
	ThawsTarget     json.RawMessage `json:"thawsTarget"`
	BreaksProtect   json.RawMessage `json:"breaksProtect"`
	IgnoreAbility   json.RawMessage `json:"ignoreAbility"`
	IgnoreEvasion   json.RawMessage `json:"ignoreEvasion"`
	IgnoreDefensive json.RawMessage `json:"ignoreDefensive"`
	VolatileStatus  string          `json:"volatileStatus"`
	SideCondition   string          `json:"sideCondition"`
	Terrain         string          `json:"terrain"`
	PseudoWeather   string          `json:"pseudoWeather"`
	SlotCondition   string          `json:"slotCondition"`
	Weather         string          `json:"weather"`
}

// dataMove is the subset of our curated moves.json the audit needs — just
// the ID, to scope the audit to the pickable set.
type dataMove struct {
	ID string `json:"id"`
}

// AuditUpstream returns the sorted list of curated-and-pickable moves whose
// upstream definition contains behavior the engine does not model.
//
// upstreamPath points at tools/data-sync/upstream/moves.json (the raw
// Showdown dump); dataMovesPath points at data/moves.json (the post-
// transform pickable set). The audit only flags moves that exist in BOTH —
// upstream moves filtered out by the data-sync denylist are not our problem.
func AuditUpstream(upstreamPath, dataMovesPath string) ([]MoveGap, error) {
	upstream, err := readUpstreamMoves(upstreamPath)
	if err != nil {
		return nil, fmt.Errorf("read upstream: %w", err)
	}
	curated, err := readCuratedIDs(dataMovesPath)
	if err != nil {
		return nil, fmt.Errorf("read curated: %w", err)
	}

	var gaps []MoveGap
	for _, u := range upstream {
		if !curated[u.ID] {
			continue
		}
		reasons := auditOne(u)
		if len(reasons) == 0 {
			continue
		}
		gaps = append(gaps, MoveGap{ID: u.ID, Name: u.Name, Reasons: reasons})
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].ID < gaps[j].ID })
	return gaps, nil
}

func auditOne(u upstreamMove) []string {
	var reasons []string
	// selfSwitch=true (plain U-turn variant) and "copyvolatile" (Baton
	// Pass) are handled by the engine. "shedtail" (Gen 9 Shed Tail) and
	// any future string variants still surface here.
	if isTruthyJSON(u.SelfSwitch) {
		s := strings.Trim(compactJSON(u.SelfSwitch), `"`)
		if s != "true" && s != "copyvolatile" {
			reasons = append(reasons, fmt.Sprintf("selfSwitch=%s: user switches out after damage (not modeled)", s))
		}
	}
	if isTruthyJSON(u.ForceSwitch) {
		reasons = append(reasons, "forceSwitch: move forces target to switch (not modeled)")
	}
	if isTruthyJSON(u.BreaksProtect) {
		reasons = append(reasons, "breaksProtect: bypasses Protect (Protect itself not modeled)")
	}
	if isTruthyJSON(u.IgnoreAbility) {
		reasons = append(reasons, "ignoreAbility: bypasses target ability (not modeled)")
	}
	if u.VolatileStatus != "" && !SupportedVolatiles[u.VolatileStatus] {
		reasons = append(reasons, fmt.Sprintf("volatileStatus=%q not in SupportedVolatiles", u.VolatileStatus))
	}
	if u.SideCondition != "" {
		reasons = append(reasons, fmt.Sprintf("sideCondition=%q (Reflect / Light Screen / hazards / Tailwind not modeled)", u.SideCondition))
	}
	if u.Terrain != "" {
		reasons = append(reasons, fmt.Sprintf("terrain=%q (terrains not modeled)", u.Terrain))
	}
	if u.PseudoWeather != "" {
		reasons = append(reasons, fmt.Sprintf("pseudoWeather=%q (Trick Room / Tailwind not modeled)", u.PseudoWeather))
	}
	if u.SlotCondition != "" {
		reasons = append(reasons, fmt.Sprintf("slotCondition=%q (Wish / Healing Wish not modeled)", u.SlotCondition))
	}
	if u.Weather != "" {
		engineSlug, mapped := upstreamWeatherToEngine[strings.ToLower(u.Weather)]
		if !mapped || !SupportedWeathers[engineSlug] {
			reasons = append(reasons, fmt.Sprintf("weather=%q not in SupportedWeathers", u.Weather))
		}
	}
	return reasons
}

// isTruthyJSON reports whether a raw JSON value represents a "feature is
// engaged" signal. Showdown uses several shapes (bool / string / int /
// array) for these flags; everything that isn't an explicit falsy literal
// counts as truthy.
func isTruthyJSON(r json.RawMessage) bool {
	s := strings.TrimSpace(string(r))
	switch s {
	case "", "null", "false", `""`, "0":
		return false
	}
	return true
}

// compactJSON strips whitespace from a RawMessage so interpolated values
// (multihit arrays especially) render on one line in the report.
func compactJSON(r json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, r); err != nil {
		return string(r)
	}
	return buf.String()
}

func readUpstreamMoves(path string) ([]upstreamMove, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var moves []upstreamMove
	if err := json.Unmarshal(b, &moves); err != nil {
		return nil, err
	}
	return moves, nil
}

func readCuratedIDs(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var moves []dataMove
	if err := json.Unmarshal(b, &moves); err != nil {
		return nil, err
	}
	ids := make(map[string]bool, len(moves))
	for _, m := range moves {
		ids[m.ID] = true
	}
	return ids, nil
}
