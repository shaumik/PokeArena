package ai

import (
	"context"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// HeuristicAgent is the "Easy" strategy: a depth-0 evaluator. It scores every
// legal action with a hand-tuned function — expected damage, KO bonuses,
// matchup-aware switching, situational status moves — and takes the best. No
// search, no lookahead, microsecond-fast, and it never fails.
type HeuristicAgent struct {
	dex *domain.Dex
}

// NewHeuristicAgent creates a HeuristicAgent over the given dataset.
func NewHeuristicAgent(dex *domain.Dex) *HeuristicAgent {
	return &HeuristicAgent{dex: dex}
}

func (a *HeuristicAgent) Name() string { return "heuristic" }

func (a *HeuristicAgent) Decide(ctx context.Context, v View) (engine.Action, error) {
	acts := LegalActions(v)
	if len(acts) == 0 {
		return fallbackAction(v), nil
	}
	best := acts[0]
	bestScore := -1e18
	for _, act := range acts {
		if s := a.score(v, act); s > bestScore {
			bestScore, best = s, act
		}
	}
	return best, nil
}

// ScoreActions exposes the per-action scores Decide already ranks, so the
// heuristic can serve as a decision-quality oracle alongside expectimax.
//
// Why a second oracle exists at all: scoring choices against expectimax alone
// conflates "played well" with "searched like the oracle" — expectimax d2 posts
// the best blunder rate against a d3 oracle while winning the fewest games (see
// docs/decision-quality.md). This agent is the opposite family — depth-0, no
// lookahead, no opponent model — so agreement with *both* is much stronger
// evidence than agreement with either.
//
// Its values are hand-tuned damage points, NOT expectimax's eval points, and the
// two scales are unrelated. Regrets and blunder rates from the two oracles must
// never be compared as magnitudes; what is comparable is the *ordering* they
// induce over policies, and match rate, which is scale-free.
//
// Contract matches ExpectimaxAgent.ScoreActions: nil where regret is undefined
// (a forced replacement or a single legal action), and ties break toward the
// first legal action exactly as Decide does, so the top-valued action here is
// Decide's pick.
func (a *HeuristicAgent) ScoreActions(v View) []ActionValue {
	if v.Replace {
		return nil
	}
	acts := LegalActions(v)
	if len(acts) <= 1 {
		return nil
	}
	out := make([]ActionValue, len(acts))
	for i, act := range acts {
		out[i] = ActionValue{Action: act, Value: a.score(v, act)}
	}
	return out
}

func (a *HeuristicAgent) score(v View, act engine.Action) float64 {
	me := v.Self.Team[v.Self.Active]
	foe := v.Foe
	mySc := &v.Self.Conditions
	foeSc := &v.FoeConditions

	if act.Kind == engine.ActionSwitch {
		return a.switchScore(v.Self.Team[act.Index], foe, me, v.Weather, v.Terrain, mySc, foeSc)
	}
	if act.Index < 0 { // Struggle: better than nothing
		return 25
	}

	m := a.dex.Moves[me.Moves[act.Index].MoveID]
	if m.Category == domain.CatStatus {
		return a.statusScore(m, me, foe)
	}

	dmg := engine.ExpectedDamage(a.dex, &me, &foe, m, v.Weather, v.Terrain, foeSc)
	score := float64(dmg)
	if dmg >= foe.HP { // a likely knockout — strongly preferred
		score += 1000
	}
	return score
}

// switchScore rewards a switch that improves the defensive matchup, dampened
// by the tempo cost of giving up a turn. mySc / foeSc are the side-condition
// (screens + hazards) bags for each side, so the AI sees Light Screen /
// Reflect / Aurora Veil shrinking the damage figures both directions and
// Stealth Rock / Spikes / Toxic Spikes adding entry chip on the switch-in.
func (a *HeuristicAgent) switchScore(in, foe, cur engine.Pokemon, w *engine.WeatherState, tr *engine.TerrainState, mySc, foeSc *engine.SideConditions) float64 {
	incomingDanger := a.bestDamage(foe, in, w, tr, mySc) // damage the foe would deal to the switch-in
	currentDanger := a.bestDamage(foe, cur, w, tr, mySc) // damage the foe deals to who is out now
	myOffense := a.bestDamage(in, foe, w, tr, foeSc)     // damage the switch-in threatens back
	improvement := float64(currentDanger - incomingDanger)
	hazardChip := engine.HazardChipOnSwitchIn(&in, mySc)
	return float64(myOffense)*0.3 + improvement*0.5 - float64(hazardChip) - 40 // -40: a switch costs a turn
}

// statusScore values non-damaging moves by the situation. With the new move
// schema, status moves carry a Primary block declaring what they do; we look
// at which fields it touches to bucket the move.
func (a *HeuristicAgent) statusScore(m domain.Move, me, foe engine.Pokemon) float64 {
	if m.Primary == nil {
		return 5
	}
	p := m.Primary
	switch {
	case p.Heal > 0:
		missing := float64(me.MaxHP-me.HP) / float64(me.MaxHP)
		return missing * 220 // worth more the more HP is missing
	case p.Status != "":
		if foe.Status == engine.StatusNone {
			return 60
		}
		return 0 // a status move is wasted on an already-statused foe
	case len(p.Boosts) > 0:
		if m.Target == domain.TargetSelf && float64(me.HP)/float64(me.MaxHP) > 0.6 {
			return 55 // set up while healthy
		}
		return 20
	}
	return 10
}

// bestDamage returns the highest expected damage atk can deal to def.
// defSc is the defender side's screens so the estimate honors Reflect /
// Light Screen / Aurora Veil.
func (a *HeuristicAgent) bestDamage(atk, def engine.Pokemon, w *engine.WeatherState, tr *engine.TerrainState, defSc *engine.SideConditions) int {
	best := 0
	for _, ms := range atk.Moves {
		if ms.PP <= 0 {
			continue
		}
		if d := engine.ExpectedDamage(a.dex, &atk, &def, a.dex.Moves[ms.MoveID], w, tr, defSc); d > best {
			best = d
		}
	}
	return best
}

// fallbackAction is what an agent returns when LegalActions hands it nothing.
// That should be unreachable — updatePhase ends the battle the moment a side's
// LiveCount hits zero, so PhaseReplace always implies a live bench member — but
// it was a panic before, and a panic in a coordinator goroutine takes down every
// battle on the host.
//
// The action has to be one the engine will actually accept, which the first
// attempt at this got wrong: it returned {ActionMove, Index: 0}, called it
// Struggle in the comment, and shipped an *illegal* action. Index 0 means "move
// slot 0"; Struggle is index -1. In a replace phase the coordinator refuses it,
// logs an AI contract violation, kills the battle and tells the human their
// opponent disconnected — a quieter failure than a crash, but a wrong one, and
// it blames the wrong side.
func fallbackAction(v View) engine.Action {
	if v.Replace {
		for i := range v.Self.Team {
			if !v.Self.Team[i].Fainted && i != v.Self.Active {
				return engine.Action{Kind: engine.ActionSwitch, Index: i}
			}
		}
	}
	// Struggle: the engine's own answer to "no usable move".
	return engine.Action{Kind: engine.ActionMove, Index: engine.StruggleMoveIndex}
}
