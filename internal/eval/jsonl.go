package eval

import (
	"encoding/json"
	"io"
)

// The JSONL trace is the benchmark's shareable artifact: a line-delimited
// stream a third party can replay to reproduce every score. Two row shapes,
// distinguished by "type":
//
//   - "game"     — one per game: participants, seat assignment, outcome.
//   - "decision" — one per recorded choice: the acting agent, the decision
//     point (turn/side/phase), the action taken, whether it was a legality
//     fallback, and the state fingerprint that anchors reproducibility.
//
// Per-move regret (Step 3) attaches to decision rows: re-derive the state from
// the seed, score the action against the fixed-depth expectimax optimum.

type gameRow struct {
	Type      string `json:"type"`
	Match     string `json:"match"`
	Team      string `json:"team"`
	Seed      uint64 `json:"seed"`
	Side0     string `json:"side0"`
	Side1     string `json:"side1"`
	Winner    string `json:"winner"`
	Turns     int    `json:"turns"`
	Decisions int    `json:"decisions"`
	// Side0Tokens/Side1Tokens are the raw token totals each seat spent this
	// game (zero for deterministic agents). The per-bucket breakdown and cost
	// live in the run summary; the trace carries the coarse count per game.
	Side0Tokens int `json:"side0_tokens,omitempty"`
	Side1Tokens int `json:"side1_tokens,omitempty"`
}

type decisionRow struct {
	Type        string `json:"type"`
	Match       string `json:"match"`
	Team        string `json:"team"`
	Seed        uint64 `json:"seed"`
	Agent       string `json:"agent"`
	Turn        int    `json:"turn"`
	Side        int    `json:"side"`
	Phase       string `json:"phase"`
	ActionKind  string `json:"action_kind"`
	ActionIndex int    `json:"action_index"`
	Fallback    bool   `json:"fallback"`
	StateHash   string `json:"state_hash"`
	// InputTokens/OutputTokens/CacheReadTokens/CacheWriteTokens are the token
	// cost of this one decision, present only for model-backed agents. Omitted
	// when zero so deterministic traces stay unchanged and diffable.
	InputTokens      int `json:"input_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// WriteMatch streams every game and decision in a match as JSONL to w.
func WriteMatch(w io.Writer, mr MatchResult) error {
	enc := json.NewEncoder(w)
	for _, g := range mr.Games {
		if err := enc.Encode(gameRow{
			Type:        "game",
			Match:       g.Match,
			Team:        g.Team,
			Seed:        g.Seed,
			Side0:       g.Side0,
			Side1:       g.Side1,
			Winner:      g.Winner,
			Turns:       g.Result.Turns,
			Decisions:   len(g.Result.Decisions),
			Side0Tokens: g.Result.Usage[0].Total(),
			Side1Tokens: g.Result.Usage[1].Total(),
		}); err != nil {
			return err
		}
		for _, d := range g.Result.Decisions {
			agent := g.Side0
			if d.Side == 1 {
				agent = g.Side1
			}
			if err := enc.Encode(decisionRow{
				Type:             "decision",
				Match:            g.Match,
				Team:             g.Team,
				Seed:             g.Seed,
				Agent:            agent,
				Turn:             d.Turn,
				Side:             d.Side,
				Phase:            string(d.Phase),
				ActionKind:       string(d.Action.Kind),
				ActionIndex:      d.Action.Index,
				Fallback:         d.Fallback,
				StateHash:        d.StateHash,
				InputTokens:      d.Usage.InputTokens,
				OutputTokens:     d.Usage.OutputTokens,
				CacheReadTokens:  d.Usage.CacheReadTokens,
				CacheWriteTokens: d.Usage.CacheWriteTokens,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
