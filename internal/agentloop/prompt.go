package agentloop

import (
	"fmt"
	"strings"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// SystemPrompt is the static instructions block sent every turn. Adapters
// that support prompt caching should mark this for caching: it is
// identical across every decision in a battle.
const SystemPrompt = `You are an expert Pokémon battle AI playing a single battle one turn at a time.

Each turn you will receive your active Pokémon, the opponent's active Pokémon (fog-of-war: you cannot see their bench), and a numbered list of legal actions you may take this turn. Choose the best action.

Respond with ONLY a single JSON object, no surrounding prose, no code fences:
{"choice": <integer index>, "reasoning": "<one short sentence>"}

The "choice" index must be in range of the legal-actions list. Do not invent actions outside the list. Keep reasoning under 25 words.`

// RenderUserPrompt produces the per-turn user message: a compact snapshot
// of the battle state plus the numbered list of legal actions.
//
// The action list ordering is significant — the LLM picks by index and the
// loop maps the index back to an engine.Action. This function and
// ai.LegalActions are the single source of that ordering; callers must
// pass the same []engine.Action they will index into after parsing.
//
// The foe's HP% comes straight from pctHP(v.Foe.HP, v.Foe.MaxHP) on every path:
// a fresh in-process view carries the foe's bucketed absolute HP, and a view
// decoded off the wire carries the percentage as HP-out-of-100 (ai.View's
// UnmarshalJSON recovers it from hp_pct), so both yield the right number here.
func RenderUserPrompt(dex *domain.Dex, v ai.View, acts []engine.Action) string {
	var b strings.Builder
	me := v.Self.Team[v.Self.Active]

	fmt.Fprintf(&b, "Turn %d", v.Turn)
	if v.Replace {
		b.WriteString(" — you must replace your fainted Pokémon")
	}
	b.WriteString(".\n\n")

	if field := fieldLine(v); field != "" {
		fmt.Fprintf(&b, "FIELD: %s\n", field)
	}

	fmt.Fprintf(&b, "YOUR ACTIVE: %s (%s) HP %d/%d%s%s%s%s\n",
		me.Name, typeLabel(me.Type1, me.Type2), me.HP, me.MaxHP,
		statusTag(me.Status), boostTag(me.Stages),
		condTag(v.Self.Conditions, "your side"), selfWishTag(v.Self.SlotConditions))
	// The foe's HP is fog-bucketed — render the approximate percentage, not a
	// precise-looking count the model would do exact math on.
	fmt.Fprintf(&b, "OPPONENT ACTIVE: %s (%s) HP ~%d%%%s%s%s%s\n",
		v.Foe.Name, typeLabel(v.Foe.Type1, v.Foe.Type2), pctHP(v.Foe.HP, v.Foe.MaxHP),
		statusTag(v.Foe.Status), boostTag(v.Foe.Stages),
		condTag(v.FoeConditions, "their side"), foeWishTag(v.FoeSlotConditions))
	if revealed := revealedMoves(dex, v.Foe.Moves); revealed != "" {
		fmt.Fprintf(&b, "Opponent's revealed moves: %s\n", revealed)
	}
	fmt.Fprintf(&b, "Opponent reserve: %d Pokémon (movesets hidden)\n", v.FoeBenchAlive)

	b.WriteString("\nLEGAL ACTIONS:\n")
	for i, act := range acts {
		switch {
		case act.Kind == engine.ActionSwitch:
			t := v.Self.Team[act.Index]
			fmt.Fprintf(&b, "[%d] Switch to %s (%s) HP %d/%d\n",
				i, t.Name, typeLabel(t.Type1, t.Type2), t.HP, t.MaxHP)
		case act.Index < 0:
			fmt.Fprintf(&b, "[%d] Struggle (no moves with PP)\n", i)
		default:
			m := dex.Moves[me.Moves[act.Index].MoveID]
			fmt.Fprintf(&b, "[%d] Move: %s (%s, %s, power %d, acc %d, PP %d)\n",
				i, m.Name, m.Type, m.Category, m.Power, m.Accuracy, me.Moves[act.Index].PP)
		}
	}
	return b.String()
}

// describeAction is the short human-readable form used in agent log lines.
func describeAction(dex *domain.Dex, v ai.View, a engine.Action) string {
	if a.Kind == engine.ActionSwitch {
		return "switch to " + v.Self.Team[a.Index].Name
	}
	if a.Index < 0 {
		return "Struggle"
	}
	return dex.Moves[v.Self.Team[v.Self.Active].Moves[a.Index].MoveID].Name
}

func typeLabel(t1, t2 domain.Type) string {
	if t2 == "" {
		return string(t1)
	}
	return string(t1) + "/" + string(t2)
}

func statusTag(s engine.StatusCond) string {
	if s == engine.StatusNone {
		return ""
	}
	return " [" + string(s) + "]"
}

// pctHP renders fog-bucketed HP as the percentage it approximates.
// Floors, but a live Pokémon reads ≥1% — same contract as the wire.
func pctHP(hp, maxHP int) int {
	if maxHP <= 0 || hp <= 0 {
		return 0
	}
	pct := hp * 100 / maxHP
	if pct < 1 {
		pct = 1
	}
	return pct
}

// boostTag renders non-zero stat stages: " [+2 Atk, -1 Spe]".
func boostTag(st engine.Stages) string {
	parts := []string{}
	for _, s := range []struct {
		n     int
		label string
	}{
		{st.Atk, "Atk"},
		{st.Def, "Def"},
		{st.SpA, "SpA"},
		{st.SpD, "SpD"},
		{st.Spe, "Spe"},
		{st.Acc, "Acc"},
		{st.Eva, "Eva"},
	} {
		if s.n != 0 {
			parts = append(parts, fmt.Sprintf("%+d %s", s.n, s.label))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, ", ") + "]"
}

// condTag renders one side's field conditions: screens and buffs with
// turns left, hazards with layers — all public info. label names whose
// half of the field this is ("your side" / "their side").
func condTag(c engine.SideConditions, label string) string {
	parts := []string{}
	add := func(label string, turns int) { parts = append(parts, fmt.Sprintf("%s %dt", label, turns)) }
	if c.Reflect != nil {
		add("Reflect", c.Reflect.TurnsLeft)
	}
	if c.LightScreen != nil {
		add("Light Screen", c.LightScreen.TurnsLeft)
	}
	if c.AuroraVeil != nil {
		add("Aurora Veil", c.AuroraVeil.TurnsLeft)
	}
	if c.Tailwind != nil {
		add("Tailwind", c.Tailwind.TurnsLeft)
	}
	if c.Safeguard != nil {
		add("Safeguard", c.Safeguard.TurnsLeft)
	}
	if c.Mist != nil {
		add("Mist", c.Mist.TurnsLeft)
	}
	if c.QuickGuard != nil {
		add("Quick Guard", c.QuickGuard.TurnsLeft)
	}
	if c.WideGuard != nil {
		add("Wide Guard", c.WideGuard.TurnsLeft)
	}
	if c.Hazards.StealthRock {
		parts = append(parts, "Stealth Rock")
	}
	if c.Hazards.Spikes > 0 {
		parts = append(parts, fmt.Sprintf("Spikes x%d", c.Hazards.Spikes))
	}
	if c.Hazards.ToxicSpikes > 0 {
		parts = append(parts, fmt.Sprintf("Toxic Spikes x%d", c.Hazards.ToxicSpikes))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + label + ": " + strings.Join(parts, ", ") + ")"
}

// selfWishTag renders our own pending slot heals with full knowledge —
// our Wish amount is our own information.
func selfWishTag(sc engine.SlotConditions) string {
	parts := []string{}
	if sc.Wish != nil {
		parts = append(parts, fmt.Sprintf("Wish lands in %dt, +%d HP", sc.Wish.TurnsLeft, sc.Wish.Amount))
	}
	if sc.HealingWish {
		parts = append(parts, "Healing Wish pending")
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, "; ") + "]"
}

// foeWishTag renders the foe's pending slot heals from the redacted
// projection: the event and countdown are public, the amount is not.
func foeWishTag(sc ai.FoeSlotConditions) string {
	parts := []string{}
	if sc.Wish != nil {
		parts = append(parts, fmt.Sprintf("their Wish lands in %dt", sc.Wish.TurnsLeft))
	}
	if sc.HealingWish {
		parts = append(parts, "their Healing Wish pending")
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, "; ") + "]"
}

// fieldLine renders the global field state — weather, terrain, rooms,
// Gravity — with turns remaining. Empty when the field is clear.
func fieldLine(v ai.View) string {
	parts := []string{}
	if v.Weather != nil && v.Weather.Kind != engine.WeatherClear {
		parts = append(parts, fmt.Sprintf("%s (%d turns left)", v.Weather.Kind, v.Weather.TurnsLeft))
	}
	if v.Terrain != nil && v.Terrain.Kind != engine.TerrainNone {
		parts = append(parts, fmt.Sprintf("%s terrain (%d turns left)", v.Terrain.Kind, v.Terrain.TurnsLeft))
	}
	pw := v.PseudoWeather
	for _, t := range []struct {
		timer *engine.PWTimer
		label string
	}{
		{pw.TrickRoom, "Trick Room"},
		{pw.WonderRoom, "Wonder Room"},
		{pw.MagicRoom, "Magic Room"},
		{pw.Gravity, "Gravity"},
	} {
		if t.timer != nil {
			parts = append(parts, fmt.Sprintf("%s (%d turns left)", t.label, t.timer.TurnsLeft))
		}
	}
	return strings.Join(parts, ", ")
}

// revealedMoves lists the foe moves seen so far, "2/4 revealed" style.
func revealedMoves(dex *domain.Dex, slots []engine.MoveSlot) string {
	names := []string{}
	for _, ms := range slots {
		if ms.MoveID == "" {
			continue
		}
		if m, ok := dex.Moves[ms.MoveID]; ok {
			names = append(names, m.Name)
		} else {
			names = append(names, ms.MoveID)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("%s (%d of %d slots revealed)", strings.Join(names, ", "), len(names), len(slots))
}
