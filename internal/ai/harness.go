package ai

import (
	"context"
	"errors"
	"fmt"
	"time"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// ErrUnknownDifficulty means the difficulty string is not one of
// {"easy", "hard"}. We refuse to silently substitute a default — that would
// betray the operator's intent on every decision, forever.
var ErrUnknownDifficulty = errors.New("ai: unknown difficulty")

// Harness wraps a primary Agent with a time budget and a fallback. If the
// primary panics, errors, exceeds its budget, or returns an illegal action,
// the harness silently falls back to the HeuristicAgent — which is instant
// and never fails. A battle therefore can never hang or crash on the AI.
type Harness struct {
	primary  Agent
	fallback *HeuristicAgent
	budget   time.Duration
}

// ValidateDifficulty reports whether the difficulty string is serveable.
// Callers that only need to validate input (API intake, startup self-check)
// should use this instead of NewHarness so they don't pay for an Expectimax
// tree just to reject a request.
//
// The rule is intentionally identical to what NewHarness will accept: this is
// the one source of truth.
func ValidateDifficulty(difficulty string) error {
	switch difficulty {
	case "easy", "hard":
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownDifficulty, difficulty)
	}
}

// NewHarness builds the harness for a difficulty:
//
//	easy -> HeuristicAgent
//	hard -> ExpectimaxAgent
//
// budget is the per-decision time limit. An unknown difficulty returns
// ErrUnknownDifficulty rather than silently picking a default — the runtime
// fallback chain (timeout/panic/illegal-action -> HeuristicAgent) exists for
// *transient* failures, not for misconfiguration. Misconfiguration must
// surface where it can be fixed: at the call site, ideally at process startup.
//
// There is no LLM rung in this harness — LLM play lives client-side of the
// gateway WS protocol (see docs/agent-harness.md). The fallback chain is
// Expectimax -> Heuristic -> Random; all three are pure functions over
// BattleView and never need a network round-trip.
func NewHarness(dex *domain.Dex, difficulty string, budget time.Duration) (*Harness, error) {
	if err := ValidateDifficulty(difficulty); err != nil {
		return nil, err
	}
	h := &Harness{
		fallback: NewHeuristicAgent(dex),
		budget:   budget,
	}
	switch difficulty {
	case "easy":
		h.primary = NewHeuristicAgent(dex)
	case "hard":
		h.primary = NewExpectimaxAgent(dex)
	}
	if h.budget <= 0 {
		h.budget = 400 * time.Millisecond
	}
	return h, nil
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
