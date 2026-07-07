package agentloop

import (
	"context"
	"fmt"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/usage"
)

// Agent adapts an LLMClient to the ai.Agent interface, so a language model can
// compete in the eval harness and benchmark exactly like the search and
// heuristic baselines. Each Decide renders the fog-of-war View into a prompt,
// asks the model, parses its {choice, reasoning} reply, and maps the choice
// back to the engine action at that index.
//
// The action slice passed to RenderUserPrompt is the SAME one indexed after
// parsing (ai.LegalActions is the single source of ordering), so the index the
// model picks and the action taken can never drift.
//
// On any failure — network error, malformed reply, out-of-range choice — Decide
// returns an error. The eval driver treats that as a legality fallback (first
// legal action, flagged), so a model that can't produce a valid move is scored
// on it rather than crashing the run. That failure rate is itself a signal.
type Agent struct {
	name   string
	dex    *domain.Dex
	client LLMClient

	// lastUsage is the token accounting of the most recent Decide call. The
	// eval driver reads it back through the LastUsage capability after each
	// decision, which is how per-decision token cost enters the trace without
	// widening the shared ai.Agent interface (deterministic agents have none).
	lastUsage usage.Usage
}

// NewAgent wraps an LLMClient as a named benchmark contestant.
func NewAgent(name string, dex *domain.Dex, client LLMClient) *Agent {
	return &Agent{name: name, dex: dex, client: client}
}

func (a *Agent) Name() string { return a.name }

// LastUsage returns the token accounting of the most recent Decide call. It is
// an opt-in capability (not part of ai.Agent): the eval driver type-asserts for
// it, so only model-backed agents contribute token cost while random/heuristic
// stay free. The value covers the call even when the decision was a fallback —
// a malformed reply still burned tokens, and the benchmark must count them.
func (a *Agent) LastUsage() usage.Usage { return a.lastUsage }

// Decide implements ai.Agent.
func (a *Agent) Decide(ctx context.Context, v ai.View) (engine.Action, error) {
	a.lastUsage = usage.Usage{}
	acts := ai.LegalActions(v)
	if len(acts) == 0 {
		return engine.Action{}, fmt.Errorf("no legal actions")
	}
	// The eval harness passes a freshly built MakeView (foe HP populated and
	// bucketed), so the foe HP% comes straight from the typed view here — unlike
	// the live WS harness, which must recover it from the wire (see loop.go).
	user := RenderUserPrompt(a.dex, v, pctHP(v.Foe.HP, v.Foe.MaxHP), acts)
	text, u, err := a.client.Complete(ctx, SystemPrompt, user)
	a.lastUsage = u // record even on the error paths below: the call was billed.
	if err != nil {
		return engine.Action{}, fmt.Errorf("llm complete: %w", err)
	}
	d, err := ParseDecision(text, len(acts))
	if err != nil {
		return engine.Action{}, fmt.Errorf("parse decision: %w", err)
	}
	return acts[d.Choice], nil
}

// compile-time proof the adapter satisfies the interface the ladder expects.
var _ ai.Agent = (*Agent)(nil)
