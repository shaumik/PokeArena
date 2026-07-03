// Package llm holds provider adapters that satisfy agentloop.LLMClient — the
// Complete(ctx, system, user) boundary — so any binary (the live agent, the
// benchmark) can drive a model without re-implementing the transport. The
// interface is matched structurally, so this package imports no agentloop.
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

// Anthropic talks to the Anthropic Messages API. The system block is marked for
// ephemeral caching — it is identical across every turn of a battle, so
// subsequent calls hit Anthropic's prompt cache; the dynamic per-turn content
// goes in the (uncached) user message.
type Anthropic struct {
	key   string
	model string
	http  *http.Client
}

// NewAnthropic builds a client for the given API key and model id.
func NewAnthropic(key, model string) *Anthropic {
	return &Anthropic{
		key:   key,
		model: model,
		http:  &http.Client{Timeout: 60 * time.Second},
	}
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

type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    []block   `json:"system"`
	Messages  []message `json:"messages"`
}

type response struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	// Usage is what Anthropic billed for this call. Cached-prompt reads
	// (cache_read_input_tokens) are separate from and cheaper than fresh
	// input_tokens; cache_creation_input_tokens is the one-time write premium.
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// Complete implements agentloop.LLMClient.
func (c *Anthropic) Complete(ctx context.Context, system, user string) (string, usage.Usage, error) {
	body := request{
		Model:     c.model,
		MaxTokens: 256,
		System: []block{{
			Type:         "text",
			Text:         system,
			CacheControl: &cacheControl{Type: "ephemeral"},
		}},
		Messages: []message{{Role: "user", Content: user}},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", usage.Usage{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return "", usage.Usage{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", usage.Usage{}, fmt.Errorf("HTTP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", usage.Usage{}, fmt.Errorf("anthropic API status %d: %s", resp.StatusCode, snippet)
	}

	var parsed response
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", usage.Usage{}, fmt.Errorf("decode response: %w", err)
	}
	if len(parsed.Content) == 0 {
		return "", usage.Usage{}, fmt.Errorf("empty response from anthropic")
	}
	u := usage.Usage{
		InputTokens:      parsed.Usage.InputTokens,
		OutputTokens:     parsed.Usage.OutputTokens,
		CacheReadTokens:  parsed.Usage.CacheReadInputTokens,
		CacheWriteTokens: parsed.Usage.CacheCreationInputTokens,
	}
	return parsed.Content[0].Text, u, nil
}
