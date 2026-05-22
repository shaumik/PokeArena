package ai

import (
	"context"
	"fmt"
	"time"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// Harness wraps a primary Agent with a time budget and a fallback. If the
// primary panics, errors, exceeds its budget, or returns an illegal action,
// the harness silently falls back to the HeuristicAgent — which is instant
// and never fails. A battle therefore can never hang or crash on the AI.
type Harness struct {
	primary  Agent
	fallback *HeuristicAgent
	budget   time.Duration
}

// NewHarness builds the harness for a difficulty:
//
//	easy      -> HeuristicAgent
//	hard      -> ExpectimaxAgent
//	nightmare -> LLMAgent if an API key is set, otherwise ExpectimaxAgent
//
// budget is the per-decision time limit; it is widened automatically for the
// (slower) LLM agent.
func NewHarness(dex *domain.Dex, difficulty string, budget time.Duration, llmKey string) *Harness {
	h := &Harness{
		fallback: NewHeuristicAgent(dex),
		budget:   budget,
	}
	switch difficulty {
	case "easy":
		h.primary = NewHeuristicAgent(dex)
	case "nightmare":
		if llmKey != "" {
			h.primary = NewLLMAgent(dex, llmKey)
			h.budget = 12 * time.Second // an LLM round-trip needs room
		} else {
			h.primary = NewExpectimaxAgent(dex)
		}
	default: // "hard"
		h.primary = NewExpectimaxAgent(dex)
	}
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
		if r.err == nil && isLegal(v, r.action) {
			return r.action
		}
	case <-ctx.Done():
		// primary exceeded its budget — fall through to the fallback
	}

	a, _ := h.fallback.Decide(context.Background(), v)
	return a
}
