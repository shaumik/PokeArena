package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"pokearena/internal/usage"
)

// Gemini talks to the Generative Language API (generateContent). Third vendor
// behind the Client boundary. Gemini takes a thinking budget as a token count
// directly, so the -cot-budget knob maps one-to-one: 0 requests no thinking
// (the Raw column), a positive budget enables it (the CoT column).
type Gemini struct {
	key            string
	model          string
	http           *http.Client
	baseURL        string
	maxTokens      int
	thinkingBudget int
}

const geminiBaseURL = "https://generativelanguage.googleapis.com"

func newGemini(cfg Config) *Gemini {
	c := &Gemini{
		key:       cfg.Key,
		model:     cfg.Model,
		http:      &http.Client{Timeout: 60 * time.Second},
		baseURL:   geminiBaseURL,
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
	c.thinkingBudget = cfg.Thinking
	return c
}

type geminiPart struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type geminiGenConfig struct {
	MaxOutputTokens int                   `json:"maxOutputTokens"`
	ThinkingConfig  *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"system_instruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  geminiGenConfig `json:"generationConfig"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
}

// Complete implements Client.
func (c *Gemini) Complete(ctx context.Context, system, user string) (string, usage.Usage, error) {
	body := geminiRequest{
		SystemInstruction: &geminiContent{Parts: []geminiPart{{Text: system}}},
		Contents:          []geminiContent{{Role: "user", Parts: []geminiPart{{Text: user}}}},
		GenerationConfig: geminiGenConfig{
			MaxOutputTokens: c.maxTokens,
			// Always pin the budget: 0 asks a 2.5-class model to skip thinking
			// (Raw), a positive value funds it (CoT).
			ThinkingConfig: &geminiThinkingConfig{ThinkingBudget: c.thinkingBudget},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", usage.Usage{}, fmt.Errorf("encode request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", c.baseURL, c.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", usage.Usage{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-goog-api-key", c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", usage.Usage{}, fmt.Errorf("HTTP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", usage.Usage{}, fmt.Errorf("gemini API status %d: %s", resp.StatusCode, snippet)
	}

	var parsed geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", usage.Usage{}, fmt.Errorf("decode response: %w", err)
	}
	cached := parsed.UsageMetadata.CachedContentTokenCount
	u := usage.Usage{
		// max(0,…): guard the prompt-minus-cached subtraction from going negative
		// (a negative count would offset other agents' totals via Add/Cost).
		InputTokens: max(0, parsed.UsageMetadata.PromptTokenCount-cached),
		// Thinking tokens are billed as output; fold them in.
		OutputTokens:    parsed.UsageMetadata.CandidatesTokenCount + parsed.UsageMetadata.ThoughtsTokenCount,
		CacheReadTokens: cached,
	}
	if len(parsed.Candidates) == 0 {
		return "", u, fmt.Errorf("no candidates in gemini response")
	}
	// Concatenate the answer parts, skipping thought parts (the reasoning
	// summary) so only the decision text comes back.
	var text strings.Builder
	for _, p := range parsed.Candidates[0].Content.Parts {
		if p.Thought {
			continue
		}
		text.WriteString(p.Text)
	}
	if text.Len() == 0 {
		// Billed but no answer text (e.g. the whole budget went to thoughts).
		// Keep the measured usage so the fallback decision isn't counted free.
		return "", u, fmt.Errorf("empty answer in gemini response")
	}
	return text.String(), u, nil
}
