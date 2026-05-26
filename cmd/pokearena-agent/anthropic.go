package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// anthropicClient implements agentloop.LLMClient against the Anthropic
// Messages API. It is the v1 provider adapter; adding OpenAI / Gemini /
// Ollama later is a sibling file with the same interface and a different
// endpoint.
//
// The system block is marked for ephemeral caching — it is identical
// across every turn of a battle, so subsequent calls hit Anthropic's
// prompt cache. The dynamic per-turn content goes in the user message,
// which is not cached.
type anthropicClient struct {
	key   string
	model string
	http  *http.Client
}

func newAnthropicClient(key, model string) *anthropicClient {
	return &anthropicClient{
		key:   key,
		model: model,
		http:  &http.Client{Timeout: 60 * time.Second},
	}
}

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    []anthropicBlock   `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Complete implements agentloop.LLMClient.
func (c *anthropicClient) Complete(ctx context.Context, system, user string) (string, error) {
	body := anthropicRequest{
		Model:     c.model,
		MaxTokens: 256,
		System: []anthropicBlock{{
			Type:         "text",
			Text:         system,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		}},
		Messages: []anthropicMessage{{Role: "user", Content: user}},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Capture some of the error body so the user sees what went wrong
		// (auth failure, rate limit, model not found, etc.).
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("anthropic API status %d: %s", resp.StatusCode, snippet)
	}

	var parsed anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(parsed.Content) == 0 {
		return "", fmt.Errorf("empty response from anthropic")
	}
	return parsed.Content[0].Text, nil
}
