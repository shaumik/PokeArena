package ai

import (
	"context"
	"fmt"
	"time"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// Harness wraps a primary Agent with a time budget. Panics and budget
// overruns fall through to the HeuristicAgent — those are runtime
// failures the gameplay loop cannot tolerate. Illegal-action returns do
// NOT trigger a fallback: every agent picks from LegalActions, which is
// derived from engine.LegalActions, so an illegal return is a contract
// violation that should surface, not be masked.
type Harness struct {
	primary  Agent
	fallback *HeuristicAgent
	budget   time.Duration
}

// NewHarness builds the agent harness: an ExpectimaxAgent primary backed by a
// HeuristicAgent fallback. budget is the per-decision time limit.
//
// There is exactly one programmatic opponent — there is no user-facing
// difficulty knob. (We removed it: a one-option setting is a fake knob, and the
// Heuristic agent already earns its keep as the timeout/panic fallback rather
// than as a selectable "easy" mode.) LLM play lives client-side of the gateway
// WS protocol (see docs/agent-harness.md); this harness is purely programmatic.
//
// The fallback chain is Expectimax -> Heuristic -> Random; all three are pure
// functions over BattleView and never need a network round-trip.
func NewHarness(dex *domain.Dex, budget time.Duration) *Harness {
	h := &Harness{
		primary:  NewExpectimaxAgent(dex),
		fallback: NewHeuristicAgent(dex),
		budget:   budget,
	}
	if h.budget <= 0 {
		h.budget = 400 * time.Millisecond
	}
	return h
}

// NewHeuristicHarness builds a harness whose primary strategy IS the heuristic.
// The benchmark's own mirror round-robin ranks the heuristic above every
// expectimax depth, so it — not the search — is the strongest programmatic bot
// we have. Using it as the live opponent grades agents against that true
// ceiling, and it is deterministic given the view (no wall-clock-dependent
// search depth), so the opponent is more reproducible than the search harness.
// The panic/timeout safety net is preserved; the heuristic just never trips it.
func NewHeuristicHarness(dex *domain.Dex, budget time.Duration) *Harness {
	heur := NewHeuristicAgent(dex)
	h := &Harness{primary: heur, fallback: heur, budget: budget}
	if h.budget <= 0 {
		h.budget = 400 * time.Millisecond
	}
	return h
}

// Name reports the active primary strategy.
func (h *Harness) Name() string { return h.primary.Name() }

// Decide chooses an action for a side of a live battle state.
func (h *Harness) Decide(s *engine.BattleState, side int) engine.Action {
	return h.DecideView(MakeView(s, side))
}

// DecideView chooses an action from an already-projected View.
func (h *Harness) DecideView(v View) engine.Action {
	ctx, cancel := context.WithTimeout(context.Background(), h.budget)
	defer cancel()

	type result struct {
		action engine.Action
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- result{err: fmt.Errorf("agent panic: %v", r)}
			}
		}()
		a, err := h.primary.Decide(ctx, v)
		ch <- result{action: a, err: err}
	}()

	select {
	case r := <-ch:
		if r.err == nil {
			return r.action
		}
	case <-ctx.Done():
		// primary exceeded its budget — fall through to the fallback
	}

	a, _ := h.fallback.Decide(context.Background(), v)
	return a
}
