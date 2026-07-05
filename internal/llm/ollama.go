package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"pokearena/internal/usage"
)

// Ollama talks to a local Ollama server's /api/chat. It is the open/local arm
// of the board: no API key, no per-token cost. Tokens are still measured (they
// tell you how much work the local model did), but they price to zero — the run
// record marks a local model's cost as known-and-free, not unknown.
//
// Ollama has no server-side prompt cache and no separate reasoning channel, so
// the thinking budget only bounds output length here; a model that reasons in
// its answer does so inline. The default host is overridable with
// OLLAMA_HOST or the shared BaseURL knob.
type Ollama struct {
	model     string
	http      *http.Client
	baseURL   string
	maxTokens int
}

const ollamaBaseURL = "http://localhost:11434"

func newOllama(cfg Config) *Ollama {
	base := ollamaBaseURL
	if h := os.Getenv("OLLAMA_HOST"); h != "" {
		base = h
	}
	if cfg.BaseURL != "" {
		base = cfg.BaseURL
	}
	c := &Ollama{
		model: cfg.Model,
		// Local models can be slow to first token on a cold load; give them a
		// generous ceiling. There is no metered cost to a long call here.
		http:      &http.Client{Timeout: 120 * time.Second},
		baseURL:   trimSlash(base),
		maxTokens: defaultMaxTokens,
	}
	if cfg.MaxTokens > 0 {
		c.maxTokens = cfg.MaxTokens
	}
	if cfg.Timeout > 0 {
		c.http.Timeout = cfg.Timeout
	}
	return c
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	NumPredict int `json:"num_predict"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []ollamaMessage `json:"messages"`
	Options  ollamaOptions   `json:"options"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
}

// Complete implements Client.
func (c *Ollama) Complete(ctx context.Context, system, user string) (string, usage.Usage, error) {
	body := ollamaRequest{
		Model:  c.model,
		Stream: false,
		Messages: []ollamaMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Options: ollamaOptions{NumPredict: c.maxTokens},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", usage.Usage{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return "", usage.Usage{}, err
	}
	req.Header.Set("content-type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", usage.Usage{}, fmt.Errorf("HTTP: %w (is `ollama serve` running at %s?)", err, c.baseURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", usage.Usage{}, fmt.Errorf("ollama status %d: %s", resp.StatusCode, snippet)
	}

	var parsed ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", usage.Usage{}, fmt.Errorf("decode response: %w", err)
	}
	if parsed.Message.Content == "" {
		return "", usage.Usage{}, fmt.Errorf("empty response from ollama")
	}
	// prompt_eval_count is input, eval_count is output. No cache tokens locally.
	u := usage.Usage{
		InputTokens:  parsed.PromptEvalCount,
		OutputTokens: parsed.EvalCount,
	}
	return parsed.Message.Content, u, nil
}
