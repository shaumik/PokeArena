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
type View struct {
	Me            int             // side index this agent controls
	Self          engine.Side     // own team, in full
	Foe           engine.Pokemon  // opponent's active Pokémon
	FoeBenchAlive int             // how many unfainted Pokémon the opponent has benched
	Phase         engine.Phase
	Turn          int
	Replace       bool // true when this side must replace a fainted active
}

// MakeView projects the fog-of-war view for one side of a battle.
func MakeView(s *engine.BattleState, side int) View {
	opp := 1 - side
	bench := 0
	for i := range s.Sides[opp].Team {
		if i != s.Sides[opp].Active && !s.Sides[opp].Team[i].Fainted {
			bench++
		}
	}
	return View{
		Me:            side,
		Self:          cloneSide(s.Sides[side]),
		Foe:           clonePokemon(s.Sides[opp].Team[s.Sides[opp].Active]),
		FoeBenchAlive: bench,
		Phase:         s.Phase,
		Turn:          s.Turn,
		Replace:       s.Replace[side],
	}
}

// Agent is a battle-decision strategy. Implementations must respect the
// context deadline; the Harness enforces it as a backstop regardless.
type Agent interface {
	Name() string
	Decide(ctx context.Context, view View) (engine.Action, error)
}

// legalActions returns every action legal from a View — its usable moves and
// switches to live teammates, or (in the replace phase) switches only.
func legalActions(v View) []engine.Action {
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
	for _, x := range legalActions(v) {
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
