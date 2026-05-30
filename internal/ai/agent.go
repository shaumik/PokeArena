// Package ai is the agent harness — a switchable strategy interface plus a
// timeout-and-fallback runtime. Every agent decides from a View, which is the
// strict fog-of-war projection a side legitimately sees: its own team in full
// and the opponent's active Pokémon. There is no agent that reads more than a
// human would: fairness is by construction, because the hidden data simply is
// not in the View.
package ai

import (
	"context"

	"pokearena/internal/engine"
)

// View is everything one side may legitimately see — exactly what the human
// player's UI renders. Notably it does NOT contain the opponent's bench
// movesets or stats, so an agent cannot plan around unrevealed Pokémon.
//
// JSON tags are part of the wire protocol now: the pvp WS handler and the
// future MCP server both serialize View to clients. Lowercase, snake_case
// matches the rest of the engine types.
type View struct {
	Me            int                  `json:"me"`              // side index this agent controls
	Self          engine.Side          `json:"self"`            // own team, in full
	Foe           engine.Pokemon       `json:"foe"`             // opponent's active Pokémon
	FoeBenchAlive int                  `json:"foe_bench_alive"` // unfainted Pokémon the opponent has benched
	Phase         engine.Phase         `json:"phase"`
	Turn          int                  `json:"turn"`
	Replace       bool                 `json:"replace"` // true when this side must replace a fainted active
	Weather       *engine.WeatherState `json:"weather,omitempty"`
}

// MakeView projects the fog-of-war view for one side of a battle, per
// docs/team-picker-room.md §6. Self is unredacted; the foe's active
// Pokémon is passed through redactFoeActive (unused moves blanked, HP
// bucketed to nearest 1% of max, internal status counters zeroed).
// Bench species are hidden by construction — only the active foe is in
// the view, plus a count of unfainted bench members.
func MakeView(s *engine.BattleState, side int) View {
	opp := 1 - side
	bench := 0
	for i := range s.Sides[opp].Team {
		if i != s.Sides[opp].Active && !s.Sides[opp].Team[i].Fainted {
			bench++
		}
	}
	var w *engine.WeatherState
	if s.Weather != nil {
		ww := *s.Weather
		w = &ww
	}
	return View{
		Me:            side,
		Self:          cloneSide(s.Sides[side]),
		Foe:           redactFoeActive(s.Sides[opp].Team[s.Sides[opp].Active]),
		FoeBenchAlive: bench,
		Phase:         s.Phase,
		Turn:          s.Turn,
		Replace:       s.Replace[side],
		Weather:       w,
	}
}

// redactFoeActive applies the fog-of-war filter to the opponent's
// active Pokémon. Move slots count is preserved (so the viewer can
// see "the foe has 4 moves, I've seen 1"); but unused slots — those
// whose PP still equals MaxPP — are blanked.
//
// HP is rounded to the nearest 1%-of-MaxHP bucket: a non-fainted
// Pokémon will never round to zero (the bucket floors at 1), so the
// faint signal stays load-bearing. Engine-internal counters (sleep
// turns, toxic counter, confusion turns) are zeroed — the *status*
// itself is visible, the *clock* is not.
func redactFoeActive(p engine.Pokemon) engine.Pokemon {
	c := clonePokemon(p)
	for i := range c.Moves {
		if c.Moves[i].PP == c.Moves[i].MaxPP {
			c.Moves[i] = engine.MoveSlot{}
		}
	}
	c.HP = bucketHP(c.HP, c.MaxHP)
	c.SleepTurns = 0
	c.ToxicCounter = 0
	if c.Volatiles.Confusion != nil {
		c.Volatiles.Confusion = &engine.ConfusionState{} // presence visible, turn count hidden
	}
	return c
}

// bucketHP rounds hp to the nearest 5%-of-MaxHP bucket. 5% matches
// Showdown's HP-bar granularity — enough for human strategy, not
// enough to be a damage calculator. A live Pokémon (hp>0) never
// buckets to zero: the smallest non-zero bucket is always returned so
// the faint distinction stays load-bearing.
//
// Bucket width is `MaxHP/20` clamped to ≥1; at our HP ranges
// (≈150–350 MaxHP) that gives ~7–17 HP buckets, which the test
// TestMakeView_RedactsFoeFog locks in.
func bucketHP(hp, maxHP int) int {
	if maxHP <= 0 || hp <= 0 {
		return hp
	}
	bucket := maxHP / 20
	if bucket < 1 {
		bucket = 1
	}
	r := ((hp + bucket/2) / bucket) * bucket
	if r > maxHP {
		r = maxHP
	}
	if r == 0 {
		r = bucket
	}
	return r
}

// Agent is a battle-decision strategy. Implementations must respect the
// context deadline; the Harness enforces it as a backstop regardless.
type Agent interface {
	Name() string
	Decide(ctx context.Context, view View) (engine.Action, error)
}

// LegalActions returns every action legal from a View — its usable moves and
// switches to live teammates, or (in the replace phase) switches only.
//
// Exported so callers outside this package (notably internal/agentloop,
// which enumerates options for an LLM to pick from) can build prompts and
// validate decisions against the same rule the agents themselves use.
func LegalActions(v View) []engine.Action {
	var out []engine.Action
	act := v.Self.Team[v.Self.Active]

	if v.Replace {
		for i := range v.Self.Team {
			if !v.Self.Team[i].Fainted && i != v.Self.Active {
				out = append(out, engine.Action{Kind: engine.ActionSwitch, Index: i})
			}
		}
		return out
	}

	for i := range act.Moves {
		if act.Moves[i].PP > 0 {
			out = append(out, engine.Action{Kind: engine.ActionMove, Index: i})
		}
	}
	if len(out) == 0 {
		out = append(out, engine.Action{Kind: engine.ActionMove, Index: -1}) // Struggle
	}
	for i := range v.Self.Team {
		if !v.Self.Team[i].Fainted && i != v.Self.Active {
			out = append(out, engine.Action{Kind: engine.ActionSwitch, Index: i})
		}
	}
	return out
}

func isLegal(v View, a engine.Action) bool {
	for _, x := range LegalActions(v) {
		if x == a {
			return true
		}
	}
	return false
}

func clonePokemon(p engine.Pokemon) engine.Pokemon {
	c := p
	c.Moves = make([]engine.MoveSlot, len(p.Moves))
	copy(c.Moves, p.Moves)
	return c
}

func cloneSide(sd engine.Side) engine.Side {
	c := sd
	c.Team = make([]engine.Pokemon, len(sd.Team))
	for i := range sd.Team {
		c.Team[i] = clonePokemon(sd.Team[i])
	}
	return c
}
