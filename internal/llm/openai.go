package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"pokearena/internal/usage"
)

// OpenAI talks to the Chat Completions API. It is the second vendor behind the
// Client boundary; the benchmark reaches it through the New factory, never
// directly. Reasoning models (o-series, gpt-5) are addressed through the same
// thinking toggle as Anthropic: a budget of 0 leaves reasoning at the model
// default (the Raw column); a positive budget maps to a reasoning_effort level
// (the CoT column).
type OpenAI struct {
	key       string
	model     string
	http      *http.Client
	baseURL   string
	maxTokens int
	effort    string // "", "low", "medium", "high"
}

const openAIBaseURL = "https://api.openai.com"

func newOpenAI(cfg Config) *OpenAI {
	c := &OpenAI{
		key:       cfg.Key,
		model:     cfg.Model,
		http:      &http.Client{Timeout: 60 * time.Second},
		baseURL:   openAIBaseURL,
		maxTokens: defaultMaxTokens,
	}
	if cfg.MaxTokens > 0 {
		c.maxTokens = cfg.MaxTokens
	}
	if cfg.Timeout > 0 {
		c.http.Timeout = cfg.Timeout
	}
	if cfg.BaseURL != "" {
		c.baseURL = trimSlash(cfg.BaseURL)
	}
	if cfg.Thinking > 0 {
		c.effort = effortFromBudget(cfg.Thinking)
	}
	return c
}

// effortFromBudget maps a thinking token budget onto OpenAI's coarse reasoning
// levels, so the same -cot-budget knob means "think harder" across vendors.
func effortFromBudget(budget int) string {
	switch {
	case budget <= 1024:
		return "low"
	case budget <= 4096:
		return "medium"
	default:
		return "high"
	}
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model string `json:"model"`
	// max_completion_tokens (not the deprecated max_tokens) is what current and
	// reasoning models accept; for a reasoning model it bounds reasoning+answer
	// together, which is why the CoT column sends a budget-sized cap.
	MaxCompletionTokens int             `json:"max_completion_tokens"`
	Messages            []openAIMessage `json:"messages"`
	ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// Complete implements Client.
func (c *OpenAI) Complete(ctx context.Context, system, user string) (string, usage.Usage, error) {
	body := openAIRequest{
		Model:               c.model,
		MaxCompletionTokens: c.maxTokens,
		Messages: []openAIMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ReasoningEffort: c.effort,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", usage.Usage{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", usage.Usage{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", usage.Usage{}, fmt.Errorf("HTTP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", usage.Usage{}, fmt.Errorf("openai API status %d: %s", resp.StatusCode, snippet)
	}

	var parsed openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", usage.Usage{}, fmt.Errorf("decode response: %w", err)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", usage.Usage{}, fmt.Errorf("empty response from openai")
	}
	cached := parsed.Usage.PromptTokensDetails.CachedTokens
	u := usage.Usage{
		// prompt_tokens includes cached; split so the cheaper cached reads are
		// priced at the cache_read rate. Reasoning tokens are billed as output
		// and already fold into completion_tokens.
		InputTokens:     parsed.Usage.PromptTokens - cached,
		OutputTokens:    parsed.Usage.CompletionTokens,
		CacheReadTokens: cached,
	}
	return parsed.Choices[0].Message.Content, u, nil
}
