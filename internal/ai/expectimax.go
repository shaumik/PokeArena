package ai

import (
	"context"
	"math"
	"time"

	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
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
	// fixedDepth makes the search always run to exactly maxDepth, ignoring any
	// context deadline. This trades responsiveness for reproducibility: because
	// the depth reached no longer depends on wall-clock speed, the agent's
	// choices are identical on every machine — required when expectimax is the
	// benchmark's ground-truth pilot for per-move regret.
	fixedDepth bool
	heur       *HeuristicAgent
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

// NewExpectimaxAgentFixed creates an expectimax agent that always searches to
// exactly depth, ignoring any context deadline. Its choices are a pure function
// of (View, depth) — fully reproducible across machines — so it can serve as
// the benchmark's ground-truth pilot. The search is already deterministic given
// a depth (chance nodes use fixed RNG offsets, not wall-clock randomness); the
// only nondeterminism in the time-budgeted path is which depth finishes before
// the deadline, and pinning the depth removes it. Prefer NewExpectimaxAgent for
// interactive play, where the time budget keeps the AI responsive.
func NewExpectimaxAgentFixed(dex *domain.Dex, depth int) *ExpectimaxAgent {
	return &ExpectimaxAgent{dex: dex, maxDepth: depth, fixedDepth: true, heur: NewHeuristicAgent(dex)}
}

func (a *ExpectimaxAgent) Name() string { return "expectimax" }

// samplesPerNode is K — the number of RNG samples averaged at each chance node.
const samplesPerNode = 3

const (
	// winValue is the leaf score for a genuinely won game (the foe is out of
	// Pokémon, bench included). It dwarfs any material or matchup term so a real
	// win always outranks a mere advantage.
	winValue = 1e6
	// materialValue is what one whole Pokémon is worth, in eval points. It sits
	// far above the HP-chip and matchup terms below, so the search prefers
	// removing a Pokémon to chipping one — but far BELOW winValue, so KOing the
	// foe's active while it still has bench is a strong lead, not a won game.
	materialValue = 1000.0
)

// searchCtx carries the fog-of-war facts a search line needs but the truncated
// simulation state can't hold: which side we are, and how many unfainted
// Pokémon the foe has on its (hidden) bench. The reconstruction gives the foe
// only its visible active, so without this the search would mistake KOing that
// active for winning the whole game.
type searchCtx struct {
	me       int
	foeBench int // foe's unfainted, hidden bench count at the root
}

func (a *ExpectimaxAgent) Decide(ctx context.Context, v View) (engine.Action, error) {
	acts := LegalActions(v)
	if len(acts) == 0 {
		return fallbackAction(v), nil
	}
	if len(acts) == 1 {
		return acts[0], nil
	}
	// Forced replacement is a one-ply decision — defer to the matchup heuristic.
	if v.Replace {
		return a.heur.Decide(ctx, v)
	}

	// Fixed-depth mode: search to exactly maxDepth, deadline-free and
	// reproducible. Iterative deepening keeps the last completed depth, so a
	// direct depth-maxDepth search yields the identical choice without the
	// wasted shallower passes.
	if a.fixedDepth {
		choice, _ := a.searchRoot(v, a.maxDepth, time.Time{}, false)
		return choice, nil
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
	sc := searchCtx{me: v.Me, foeBench: v.FoeBenchAlive}
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
			if val := a.evalPair(sc, sim, my, fo, depth); val < worst {
				worst = val
			}
		}
		if worst > bestVal {
			bestVal, best = worst, my
		}
	}
	return best, true
}

// ActionValue is a root action paired with its maximin search value, in the
// eval points value() uses (materialValue = one whole Pokémon).
type ActionValue struct {
	Action engine.Action
	Value  float64
}

// ScoreActions returns the maximin value of every legal action for the deciding
// side at the agent's max depth — the same per-action scores searchRoot ranks to
// pick its move, exposed so a caller can measure the value gap (regret) between a
// policy's actual choice and the best one. Deadline-free and reproducible, like
// fixed-depth Decide, and it breaks ties toward the first legal action exactly as
// Decide does, so the top-valued action here equals Decide's choice. Returns nil
// where regret is undefined: a forced replacement (a one-ply decision expectimax
// defers to the heuristic) or a turn with a single legal action.
func (a *ExpectimaxAgent) ScoreActions(v View) []ActionValue {
	if v.Replace {
		return nil
	}
	myActs := LegalActions(v)
	if len(myActs) <= 1 {
		return nil
	}
	sim := a.reconstruct(v)
	sc := searchCtx{me: v.Me, foeBench: v.FoeBenchAlive}
	foeActs := a.foeActions(sim, v.Me)
	out := make([]ActionValue, 0, len(myActs))
	for _, my := range myActs {
		worst := math.Inf(1)
		for _, fo := range foeActs {
			if val := a.evalPair(sc, sim, my, fo, a.maxDepth); val < worst {
				worst = val
			}
		}
		out = append(out, ActionValue{Action: my, Value: worst})
	}
	return out
}

// evalPair simulates one (my, foe) action pair K times over the chance space
// and averages the resulting position values.
func (a *ExpectimaxAgent) evalPair(sc searchCtx, sim *engine.BattleState, my, fo engine.Action, depth int) float64 {
	var actions [2]engine.Action
	actions[sc.me] = my
	actions[1-sc.me] = fo

	var sum float64
	for k := 0; k < samplesPerNode; k++ {
		c := sim.Clone()
		c.RNGState = sim.RNGState + uint64(k+1)*0x9E3779B97F4A7C15 // distinct rolls per sample
		engine.ResolveTurn(a.dex, c, actions)
		sum += a.value(sc, c, depth-1)
	}
	return sum / samplesPerNode
}

// value scores a position. Terminality is judged by TRUE material — the foe's
// hidden bench included — not by the truncated reconstruction: a side has won
// only when its opponent is out of Pokémon for real. Removing the foe's visible
// active while it still has bench is NOT a win (that was the old model's fatal
// lie); it falls through to evalState, which credits it as one Pokémon gained.
func (a *ExpectimaxAgent) value(sc searchCtx, s *engine.BattleState, depth int) float64 {
	myAlive := aliveCount(s, sc.me)
	foeAlive := aliveCount(s, 1-sc.me) + sc.foeBench // visible active (if up) + hidden bench
	// A double-KO — both sides wiped in the same line — is a DRAW, worth 0, not a
	// loss. Order matters: this must be checked before the one-sided terminals
	// below, which would otherwise book a mutual faint (Explosion, recoil, burn)
	// as a total loss and make the pilot irrationally avoid even trades.
	if myAlive == 0 && foeAlive == 0 {
		return 0
	}
	if myAlive == 0 {
		return -winValue
	}
	if foeAlive == 0 {
		return winValue
	}
	// Depth exhausted, or the reconstruction "ended" only because the foe's lone
	// visible mon fainted while bench remains (a phantom end). Both are scored by
	// material — no phantom win.
	if depth <= 0 || s.Ended() || s.Phase != engine.PhaseChoosing {
		return a.evalState(sc, s)
	}
	myActs := movesOnly(s, sc.me) // deeper plies consider moves only, to bound branching
	foeActs := a.foeActions(s, sc.me)
	best := math.Inf(-1)
	for _, my := range myActs {
		worst := math.Inf(1)
		for _, fo := range foeActs {
			if val := a.evalPair(sc, s, my, fo, depth); val < worst {
				worst = val
			}
		}
		if worst > best {
			best = worst
		}
	}
	return best
}

// evalState is the leaf evaluation: material (Pokémon still standing, hidden foe
// bench included), the active matchup, and status. Material dominates, so the
// search values removing a whole Pokémon far above chipping one — while a real
// win (winValue) still dominates material.
func (a *ExpectimaxAgent) evalState(sc searchCtx, s *engine.BattleState) float64 {
	me := sc.me
	// Foe material counts the hidden bench as full-HP Pokémon — the neutral
	// assumption for a fresh, unknown switch-in.
	myMat := teamHPFraction(s, me)
	foeMat := teamHPFraction(s, 1-me) + float64(sc.foeBench)
	val := (myMat - foeMat) * materialValue

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

// aliveCount is the number of unfainted Pokémon on a side of the (possibly
// truncated) reconstruction.
func aliveCount(s *engine.BattleState, side int) int {
	n := 0
	for i := range s.Sides[side].Team {
		if !s.Sides[side].Team[i].Fainted {
			n++
		}
	}
	return n
}

// teamHPFraction sums each Pokémon's remaining-HP fraction over a side, so one
// full Pokémon contributes 1.0 — a material unit comparable to a hidden bench
// member counted as 1.0.
func teamHPFraction(s *engine.BattleState, side int) float64 {
	var sum float64
	for i := range s.Sides[side].Team {
		p := &s.Sides[side].Team[i]
		if p.MaxHP > 0 {
			sum += float64(p.HP) / float64(p.MaxHP)
		}
	}
	return sum
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

// reconstruct builds a simulatable battle from the View. Shares the
// projection with reconstructFromView so the same gating rules apply,
// then layers a deterministic RNG seed for repeatable rollouts.
// The opponent is modeled by its visible active Pokémon only — the
// search deliberately knows nothing of the foe's bench.
func (a *ExpectimaxAgent) reconstruct(v View) *engine.BattleState {
	s := reconstructFromView(v)
	s.Phase = engine.PhaseChoosing
	s.RNGState = uint64(v.Turn+1) * 0x9E3779B97F4A7C15
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
		out = append(out, engine.Action{Kind: engine.ActionMove, Index: engine.StruggleMoveIndex})
	}
	return out
}
