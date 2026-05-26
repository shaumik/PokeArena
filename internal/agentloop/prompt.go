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
func RenderUserPrompt(dex *domain.Dex, v ai.View, acts []engine.Action) string {
	var b strings.Builder
	me := v.Self.Team[v.Self.Active]

	fmt.Fprintf(&b, "Turn %d", v.Turn)
	if v.Replace {
		b.WriteString(" — you must replace your fainted Pokémon")
	}
	b.WriteString(".\n\n")

	fmt.Fprintf(&b, "YOUR ACTIVE: %s (%s) HP %d/%d%s\n",
		me.Name, typeLabel(me.Type1, me.Type2), me.HP, me.MaxHP, statusTag(me.Status))
	fmt.Fprintf(&b, "OPPONENT ACTIVE: %s (%s) HP %d/%d%s\n",
		v.Foe.Name, typeLabel(v.Foe.Type1, v.Foe.Type2), v.Foe.HP, v.Foe.MaxHP, statusTag(v.Foe.Status))
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
