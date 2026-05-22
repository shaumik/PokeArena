package ai

import (
	"context"
	"errors"
	"fmt"
	"time"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// Sentinel errors returned by NewHarness when the requested configuration
// cannot be satisfied. These are *configuration* errors — distinct from the
// runtime failures the harness silently absorbs via the fallback chain. A
// caller that ignores them is silently downgrading the operator's stated
// intent, which is what we want to forbid.
var (
	// ErrUnknownDifficulty means the difficulty string is not one of
	// {"easy", "hard", "nightmare"}.
	ErrUnknownDifficulty = errors.New("ai: unknown difficulty")

	// ErrLLMKeyMissing means "nightmare" was requested but no ANTHROPIC_API_KEY
	// is configured. We refuse to silently substitute a weaker agent — that
	// would betray the operator's intent on every decision, forever.
	ErrLLMKeyMissing = errors.New("ai: nightmare difficulty requires ANTHROPIC_API_KEY")
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

// ValidateDifficulty reports whether (difficulty, llmKey) is a serveable
// combination for *this* process — without constructing an agent. Callers
// that only need to validate input (API intake, startup self-check) should
// use this instead of NewHarness so they don't pay for an Expectimax tree
// just to reject a request.
//
// The rule is intentionally identical to what NewHarness will accept: this is
// the one source of truth.
func ValidateDifficulty(difficulty, llmKey string) error {
	switch difficulty {
	case "easy", "hard":
		return nil
	case "nightmare":
		if llmKey == "" {
			return ErrLLMKeyMissing
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownDifficulty, difficulty)
	}
}

// NewHarness builds the harness for a difficulty:
//
//	easy      -> HeuristicAgent
//	hard      -> ExpectimaxAgent
//	nightmare -> LLMAgent (requires llmKey; returns ErrLLMKeyMissing if unset)
//
// budget is the per-decision time limit; it is widened automatically for the
// (slower) LLM agent. An unknown difficulty returns ErrUnknownDifficulty
// rather than silently picking a default — the runtime fallback chain
// (timeout/panic/illegal-action -> HeuristicAgent) exists for *transient*
// failures, not for misconfiguration. Misconfiguration must surface where it
// can be fixed: at the call site, ideally at process startup.
func NewHarness(dex *domain.Dex, difficulty string, budget time.Duration, llmKey string) (*Harness, error) {
	if err := ValidateDifficulty(difficulty, llmKey); err != nil {
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
	case "nightmare":
		h.primary = NewLLMAgent(dex, llmKey)
		h.budget = 12 * time.Second // an LLM round-trip needs room
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
