package ai

import (
	"context"

	"pokearena/internal/engine"
)

// RandomAgent picks a uniformly random legal action. It is not meant for real
// play — it is the test control and the harness's absolute last resort.
type RandomAgent struct {
	rng *engine.RNG
}

// NewRandomAgent creates a RandomAgent with a fixed seed for reproducibility.
func NewRandomAgent(seed uint64) *RandomAgent {
	return &RandomAgent{rng: engine.NewRNG(seed)}
}

func (a *RandomAgent) Name() string { return "random" }

func (a *RandomAgent) Decide(ctx context.Context, v View) (engine.Action, error) {
	acts := LegalActions(v)
	return acts[a.rng.IntN(len(acts))], nil
}
