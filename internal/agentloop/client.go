// Package agentloop is the reusable agent loop that plays a PokéArena
// battle as a trainer client: it dials the gateway, renders each turn's
// fog-of-war view into a prompt, asks an LLM for a decision, parses the
// reply, and submits the action — until the battle ends.
//
// The package is provider-agnostic. The only LLM-shaped thing it knows
// about is the LLMClient interface; concrete adapters (Anthropic,
// OpenAI-compatible, etc.) live in their callers (today: cmd/pokearena-agent).
// This boundary is intentional — see docs/agent-harness.md.
package agentloop

import (
	"context"

	"pokearena/internal/usage"
)

// LLMClient is the provider-agnostic boundary between the agent loop and
// whatever model is making decisions. Adapters live in cmd/pokearena-agent
// (and any future agent binary); this package never imports a provider SDK.
//
// system is the static instructions block — the same string every turn, so
// adapters that support prompt caching should cache it. user is the
// per-turn rendered view + action menu.
//
// The first return value is the model's raw text; parsing into a structured
// decision is the loop's job, not the adapter's. The second is the token
// accounting the provider reported for this exact call — the substrate for
// measured (not estimated) cost. Adapters that cannot report usage return the
// zero Usage, which prices to nothing.
type LLMClient interface {
	Complete(ctx context.Context, system, user string) (string, usage.Usage, error)
}
