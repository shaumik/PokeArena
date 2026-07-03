// Package usage is the token-accounting substrate for LLM contestants: a small
// leaf package (no dependencies) that both the provider adapters and the agent
// loop import, so token counts flow out of every model call as structured data
// rather than log text.
//
// The design goal is that cost is a MEASURED number, not an estimate. A
// provider returns exactly what it billed for a call (prompt, completion, and
// prompt-cache read/write, which are priced differently); we carry those four
// counts intact all the way to the results store, and only multiply by a
// price table at the very end. Nothing is rounded or inferred in the middle.
package usage

// Usage is the token accounting for one or more model calls. The four counts
// are kept separate because providers bill them at different rates — cached
// prompt reads are an order of magnitude cheaper than fresh input, and cache
// writes carry a premium — so collapsing them early would throw away the
// information needed to price a run correctly.
type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

// Add returns the element-wise sum, so a run can fold per-call usage into a
// per-game or per-contestant total without mutating either operand.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:      u.InputTokens + o.InputTokens,
		OutputTokens:     u.OutputTokens + o.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens + o.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens + o.CacheWriteTokens,
	}
}

// Total is the raw token count across all four buckets — a coarse size figure,
// not a billing figure (which needs Pricing).
func (u Usage) Total() int {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

// IsZero reports whether any tokens were counted at all. Deterministic agents
// (random, heuristic, expectimax) never call a model, so their usage stays
// zero — that is how the store knows a contestant is free.
func (u Usage) IsZero() bool { return u == Usage{} }

// Pricing is a model's per-million-token rates in USD. Providers quote prices
// this way; keeping the same unit avoids a scaling factor that is easy to get
// wrong by three orders of magnitude.
type Pricing struct {
	Input      float64 `json:"input"`       // per 1M fresh input tokens
	Output     float64 `json:"output"`      // per 1M output tokens
	CacheRead  float64 `json:"cache_read"`  // per 1M cached-prompt read tokens
	CacheWrite float64 `json:"cache_write"` // per 1M cache-write tokens
}

// Cost returns the USD cost of this usage under the given pricing. This is the
// single place tokens become dollars — everything upstream stays in tokens.
func (u Usage) Cost(p Pricing) float64 {
	const perMillion = 1_000_000.0
	return float64(u.InputTokens)/perMillion*p.Input +
		float64(u.OutputTokens)/perMillion*p.Output +
		float64(u.CacheReadTokens)/perMillion*p.CacheRead +
		float64(u.CacheWriteTokens)/perMillion*p.CacheWrite
}
