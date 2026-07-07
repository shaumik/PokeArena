// Package llm holds provider adapters that satisfy agentloop.LLMClient — the
// Complete(ctx, system, user) boundary — so any binary (the live agent, the
// benchmark) can drive a model without re-implementing the transport. The
// interface is matched structurally, so this package imports no agentloop.
package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"pokearena/internal/usage"
)

// defaultMaxTokens is the output cap for a plain (no-thinking) decision. One
// action + brief rationale fits comfortably; the benchmark's Raw column runs
// at this budget.
const defaultMaxTokens = 256

// baseURL is the Anthropic Messages endpoint. Overridable via WithBaseURL for
// tests and OpenAI-incompatible proxies.
const baseURL = "https://api.anthropic.com"

// minThinkingBudget is the smallest extended-thinking budget the API accepts; a
// smaller positive budget is a 400, so we clamp up to it rather than reject.
const minThinkingBudget = 1024

// Anthropic talks to the Anthropic Messages API. The system block is marked for
// ephemeral caching — it is identical across every turn of a battle, so
// subsequent calls hit Anthropic's prompt cache; the dynamic per-turn content
// goes in the (uncached) user message.
//
// Two knobs turn one adapter into the benchmark's separate columns:
//   - maxTokens caps output — small for Raw, large enough to hold a chain of
//     thought for CoT.
//   - thinkingBudget > 0 enables extended thinking (the CoT column) and forces
//     streaming, since a long deliberation on a single non-streamed request
//     drifts toward the socket timeout and reads as a hang — the "frozen at 0%
//     CPU" symptom that made Sonnet look broken.
type Anthropic struct {
	key            string
	model          string
	http           *http.Client
	baseURL        string
	maxTokens      int
	thinkingBudget int
	stream         bool
}

// Option configures an Anthropic client. Zero options reproduces the original
// thin harness: 256-token cap, no thinking, non-streaming, 60s timeout.
type Option func(*Anthropic)

// WithMaxTokens sets the output token cap. For the CoT column this must exceed
// the thinking budget, since the budget is spent before any answer tokens.
func WithMaxTokens(n int) Option {
	return func(a *Anthropic) { a.maxTokens = n }
}

// WithThinking enables extended thinking with the given budget (in tokens) and
// switches the client to streaming. A budget of 0 leaves thinking off. This is
// the toggle that separates the benchmark's Raw (off) and CoT (on) conditions.
func WithThinking(budgetTokens int) Option {
	return func(a *Anthropic) {
		a.thinkingBudget = budgetTokens
		if budgetTokens > 0 {
			a.stream = true
		}
	}
}

// WithStreaming forces streaming on or off independently of the thinking
// toggle. Streaming keeps the socket active on slow, deliberate models even
// without extended thinking, so a long turn no longer looks frozen.
func WithStreaming(on bool) Option {
	return func(a *Anthropic) { a.stream = on }
}

// WithTimeout overrides the HTTP client timeout. Extended-thinking calls want a
// generous ceiling; streaming makes a long timeout safe because progress is
// continuous.
func WithTimeout(d time.Duration) Option {
	return func(a *Anthropic) { a.http.Timeout = d }
}

// WithBaseURL overrides the API host (no trailing slash). For tests and
// Anthropic-compatible proxies.
func WithBaseURL(u string) Option {
	return func(a *Anthropic) { a.baseURL = strings.TrimRight(u, "/") }
}

// newAnthropicFromConfig adapts the vendor-agnostic Config onto Anthropic's
// options, so the factory builds every vendor from the same knobs.
func newAnthropicFromConfig(cfg Config) *Anthropic {
	var opts []Option
	if cfg.MaxTokens > 0 {
		opts = append(opts, WithMaxTokens(cfg.MaxTokens))
	}
	if cfg.Thinking > 0 {
		opts = append(opts, WithThinking(cfg.Thinking))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, WithTimeout(cfg.Timeout))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, WithBaseURL(cfg.BaseURL))
	}
	return NewAnthropic(cfg.Key, cfg.Model, opts...)
}

// NewAnthropic builds a client for the given API key and model id. Behaviour is
// tuned via Options; with none it matches the original thin harness.
func NewAnthropic(key, model string, opts ...Option) *Anthropic {
	a := &Anthropic{
		key:       key,
		model:     model,
		http:      &http.Client{Timeout: 60 * time.Second},
		baseURL:   baseURL,
		maxTokens: defaultMaxTokens,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

type cacheControl struct {
	Type string `json:"type"`
}

type block struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type thinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type request struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    []block         `json:"system"`
	Messages  []message       `json:"messages"`
	Thinking  *thinkingConfig `json:"thinking,omitempty"`
	Stream    bool            `json:"stream,omitempty"`
}

// apiUsage mirrors the token accounting Anthropic reports. Cached-prompt reads
// (cache_read_input_tokens) are separate from and cheaper than fresh
// input_tokens; cache_creation_input_tokens is the one-time write premium.
type apiUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

func (u apiUsage) toUsage() usage.Usage {
	return usage.Usage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
}

// Complete implements agentloop.LLMClient.
func (c *Anthropic) Complete(ctx context.Context, system, user string) (string, usage.Usage, error) {
	body := request{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System: []block{{
			Type:         "text",
			Text:         system,
			CacheControl: &cacheControl{Type: "ephemeral"},
		}},
		Messages: []message{{Role: "user", Content: user}},
		Stream:   c.stream,
	}
	if c.thinkingBudget > 0 {
		// Clamp up to the API minimum: a positive budget below it is a 400.
		budget := c.thinkingBudget
		if budget < minThinkingBudget {
			budget = minThinkingBudget
		}
		body.Thinking = &thinkingConfig{Type: "enabled", BudgetTokens: budget}
		// The output cap must leave room for an answer after the budget is
		// spent on thinking, or the API rejects the request. Guarantee it.
		if body.MaxTokens <= budget {
			body.MaxTokens = budget + defaultMaxTokens
		}
	}

	resp, err := postJSON(ctx, c.http, c.baseURL+"/v1/messages", map[string]string{
		"x-api-key":         c.key,
		"anthropic-version": "2023-06-01",
	}, body)
	if err != nil {
		return "", usage.Usage{}, err
	}
	defer resp.Body.Close()

	if c.stream {
		return parseStream(resp.Body)
	}
	return parseJSON(resp.Body)
}

type jsonResponse struct {
	Content []block  `json:"content"`
	Usage   apiUsage `json:"usage"`
}

// parseJSON reads a non-streamed Messages response. The first text block is the
// answer — with thinking enabled a thinking block precedes it, so we cannot
// blindly take Content[0].
func parseJSON(r io.Reader) (string, usage.Usage, error) {
	var parsed jsonResponse
	if err := json.NewDecoder(r).Decode(&parsed); err != nil {
		return "", usage.Usage{}, fmt.Errorf("decode response: %w", err)
	}
	text, ok := firstText(parsed.Content)
	if !ok {
		// Billed but no text block (thinking consumed the whole budget). Return
		// the measured usage so the fallback decision counts its real cost.
		return "", parsed.Usage.toUsage(), fmt.Errorf("no text block in anthropic response")
	}
	return text, parsed.Usage.toUsage(), nil
}

func firstText(blocks []block) (string, bool) {
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text, true
		}
	}
	return "", false
}

// Server-sent event envelopes we care about. text_delta carries the answer;
// thinking_delta is the model's private reasoning and is discarded. Usage
// arrives split: input/cache counts in message_start, the final output count in
// message_delta.
type streamEvent struct {
	Type    string `json:"type"`
	Message struct {
		Usage apiUsage `json:"usage"`
	} `json:"message"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	Usage apiUsage `json:"usage"`
}

// parseStream accumulates text_delta chunks from an SSE Messages stream and
// stitches together the split usage report.
func parseStream(r io.Reader) (string, usage.Usage, error) {
	var text strings.Builder
	var u usage.Usage
	sc := bufio.NewScanner(r)
	// Thinking streams can push individual data lines past the default 64KiB
	// scanner cap; give it room.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue // ping / unknown event shape
		}
		switch ev.Type {
		case "message_start":
			// Input and cache counts are final here; output is still 0.
			su := ev.Message.Usage.toUsage()
			u.InputTokens = su.InputTokens
			u.CacheReadTokens = su.CacheReadTokens
			u.CacheWriteTokens = su.CacheWriteTokens
		case "content_block_delta":
			if ev.Delta.Type == "text_delta" {
				text.WriteString(ev.Delta.Text)
			}
		case "message_delta":
			if ev.Usage.OutputTokens > 0 {
				u.OutputTokens = ev.Usage.OutputTokens
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", usage.Usage{}, fmt.Errorf("read stream: %w", err)
	}
	if text.Len() == 0 {
		// Billed but no answer text; return the usage stitched from the stream so
		// the fallback decision counts its real cost.
		return "", u, fmt.Errorf("no text in anthropic stream")
	}
	return text.String(), u, nil
}
