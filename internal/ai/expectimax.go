package ai

import (
	"context"
	"math"
	"time"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// ExpectimaxAgent is the "Hard" strategy: a depth-limited search over what is
// really a simultaneous-move, stochastic game.
//
//   - Simultaneous moves: for each of my actions it builds the payoff matrix
//     against the foe's plausible actions and takes the MAXIMIN action — the
//     one whose worst case is best.
//   - Chance nodes: damage rolls, crits and secondary effects are not branched
//     exhaustively; each (my, foe) pair is simulated K times with different RNG
//     and the leaf values are averaged — expectation, hence "expecti"max.
//   - Fog of war: it reconstructs a battle to simulate from the View alone, so
//     it only ever reasons about the opponent's visible active Pokémon.
//   - Iterative deepening: it searches depth 1, then 2, returning the best
//     result found before the context deadline.
type ExpectimaxAgent struct {
	dex      *domain.Dex
	maxDepth int
	heur     *HeuristicAgent
}

// NewExpectimaxAgent creates the search agent over the given dataset.
//
// maxDepth=3 is the strategic sweet spot: it's the shallowest depth at which
// the agent can see "switch -> foe attacks the new mon -> I retaliate"
// sequences, which is where Pokémon play stops being one-ply matchup math.
// Branching is roughly squared per ply (~50x the work over depth 2); the
// harness time budget (AI_TIME_BUDGET_MS, default 1500ms) is paired with
// this. Iterative deepening means we still return the best depth-2 result if
// depth 3 doesn't complete in time, so the AI never hangs.
func NewExpectimaxAgent(dex *domain.Dex) *ExpectimaxAgent {
	return &ExpectimaxAgent{dex: dex, maxDepth: 3, heur: NewHeuristicAgent(dex)}
}

func (a *ExpectimaxAgent) Name() string { return "expectimax" }

// samplesPerNode is K — the number of RNG samples averaged at each chance node.
const samplesPerNode = 3

func (a *ExpectimaxAgent) Decide(ctx context.Context, v View) (engine.Action, error) {
	acts := LegalActions(v)
	if len(acts) == 1 {
		return acts[0], nil
	}
	// Forced replacement is a one-ply decision — defer to the matchup heuristic.
	if v.Replace {
		return a.heur.Decide(ctx, v)
	}

	deadline, hasDeadline := ctx.Deadline()
	best := acts[0]
	for depth := 1; depth <= a.maxDepth; depth++ {
		choice, completed := a.searchRoot(v, depth, deadline, hasDeadline)
		if !completed {
			break // deadline hit mid-search; keep the last completed depth
		}
		best = choice
	}
	return best, nil
}

// searchRoot evaluates every action for the deciding side and returns the
// maximin choice. completed is false if the deadline interrupted the search.
func (a *ExpectimaxAgent) searchRoot(v View, depth int, deadline time.Time, hasDeadline bool) (engine.Action, bool) {
	sim := a.reconstruct(v)
	myActs := LegalActions(v)
	foeActs := a.foeActions(sim, v.Me)

	best := myActs[0]
	bestVal := math.Inf(-1)
	for _, my := range myActs {
		if hasDeadline && time.Now().After(deadline) {
			return best, false
		}
		worst := math.Inf(1)
		for _, fo := range foeActs {
			if val := a.evalPair(sim, v.Me, my, fo, depth); val < worst {
				worst = val
			}
		}
		if worst > bestVal {
			bestVal, best = worst, my
		}
	}
	return best, true
}

// evalPair simulates one (my, foe) action pair K times over the chance space
// and averages the resulting position values.
func (a *ExpectimaxAgent) evalPair(sim *engine.BattleState, me int, my, fo engine.Action, depth int) float64 {
	var actions [2]engine.Action
	actions[me] = my
	actions[1-me] = fo

	var sum float64
	for k := 0; k < samplesPerNode; k++ {
		c := sim.Clone()
		c.RNGState = sim.RNGState + uint64(k+1)*0x9E3779B97F4A7C15 // distinct rolls per sample
		engine.ResolveTurn(a.dex, c, actions)
		sum += a.value(c, me, depth-1)
	}
	return sum / samplesPerNode
}

// value scores a position: a terminal result, a heuristic leaf evaluation, or
// (with depth remaining) the maximin value of searching one turn deeper.
func (a *ExpectimaxAgent) value(s *engine.BattleState, me, depth int) float64 {
	if s.Ended() {
		switch s.Winner {
		case me:
			return 1e6
		case 2:
			return 0
		default:
			return -1e6
		}
	}
	if depth <= 0 || s.Phase != engine.PhaseChoosing {
		return a.evalState(s, me)
	}
	myActs := movesOnly(s, me) // deeper plies consider moves only, to bound branching
	foeActs := a.foeActions(s, me)
	best := math.Inf(-1)
	for _, my := range myActs {
		worst := math.Inf(1)
		for _, fo := range foeActs {
			if val := a.evalPair(s, me, my, fo, depth); val < worst {
				worst = val
			}
		}
		if worst > best {
			best = worst
		}
	}
	return best
}

// evalState is the leaf evaluation: team HP differential, active matchup, and
// status conditions.
func (a *ExpectimaxAgent) evalState(s *engine.BattleState, me int) float64 {
	val := (a.teamHP(s, me) - a.teamHP(s, 1-me)) * 1000.0

	mine := s.Active(me)
	foe := s.Active(1 - me)
	if !mine.Fainted && !foe.Fainted {
		val += float64(a.heur.bestDamage(*mine, *foe, s.Weather, s.Terrain, &s.Sides[1-me].Conditions)) * 0.25
		val -= float64(a.heur.bestDamage(*foe, *mine, s.Weather, s.Terrain, &s.Sides[me].Conditions)) * 0.25
	}
	val -= statusPenalty(mine)
	val += statusPenalty(foe)
	return val
}

func (a *ExpectimaxAgent) teamHP(s *engine.BattleState, side int) float64 {
	var cur, max float64
	for i := range s.Sides[side].Team {
		cur += float64(s.Sides[side].Team[i].HP)
		max += float64(s.Sides[side].Team[i].MaxHP)
	}
	if max == 0 {
		return 0
	}
	return cur / max
}

func statusPenalty(p *engine.Pokemon) float64 {
	switch p.Status {
	case engine.StatusSleep, engine.StatusFreeze:
		return 60
	case engine.StatusParalysis:
		return 35
	case engine.StatusBurn, engine.StatusPoison:
		return 25
	}
	return 0
}

// reconstruct builds a simulatable battle from the View. The opponent is
// modeled by its visible active Pokémon only — the search deliberately knows
// nothing of the foe's bench.
func (a *ExpectimaxAgent) reconstruct(v View) *engine.BattleState {
	s := &engine.BattleState{
		Phase:    engine.PhaseChoosing,
		Winner:   -1,
		Turn:     v.Turn,
		RNGState: uint64(v.Turn+1) * 0x9E3779B97F4A7C15,
	}
	s.Sides[v.Me] = cloneSide(v.Self)
	s.Sides[1-v.Me] = engine.Side{
		Trainer:    "Foe",
		Team:       []engine.Pokemon{clonePokemon(v.Foe)},
		Active:     0,
		Conditions: engine.CloneSideConditions(v.FoeConditions),
	}
	return s
}

// foeActions enumerates the opponent's usable moves in the simulated battle.
func (a *ExpectimaxAgent) foeActions(s *engine.BattleState, me int) []engine.Action {
	return movesOnly(s, 1-me)
}

func movesOnly(s *engine.BattleState, side int) []engine.Action {
	act := s.Active(side)
	var out []engine.Action
	for i := range act.Moves {
		if act.Moves[i].PP > 0 {
			out = append(out, engine.Action{Kind: engine.ActionMove, Index: i})
		}
	}
	if len(out) == 0 {
		out = append(out, engine.Action{Kind: engine.ActionMove, Index: -1})
	}
	return out
}
