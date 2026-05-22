package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// LLMAgent is the optional "Nightmare" strategy: it asks Claude to pick the
// best action. It is gated behind an API key; when unset the harness never
// constructs it. Any failure (network, timeout, malformed reply, illegal
// choice) surfaces as an error, and the harness falls back to expectimax.
type LLMAgent struct {
	dex    *domain.Dex
	apiKey string
	model  string
	http   *http.Client
}

// NewLLMAgent creates the LLM-backed agent.
func NewLLMAgent(dex *domain.Dex, apiKey string) *LLMAgent {
	return &LLMAgent{
		dex:    dex,
		apiKey: apiKey,
		model:  "claude-haiku-4-5-20251001", // fast model: a move decision is latency-sensitive
		http:   &http.Client{},
	}
}

func (a *LLMAgent) Name() string { return "llm" }

func (a *LLMAgent) Decide(ctx context.Context, v View) (engine.Action, error) {
	acts := legalActions(v)
	choice, err := a.ask(ctx, a.describe(v, acts))
	if err != nil {
		return engine.Action{}, err
	}
	if choice < 0 || choice >= len(acts) {
		return engine.Action{}, fmt.Errorf("llm chose out-of-range index %d", choice)
	}
	return acts[choice], nil
}

// describe renders the fog-of-war View as a numbered list of options.
func (a *LLMAgent) describe(v View, acts []engine.Action) string {
	var b strings.Builder
	me := v.Self.Team[v.Self.Active]
	fmt.Fprintf(&b, "Pokémon battle — choose the best action.\n\n")
	fmt.Fprintf(&b, "YOUR ACTIVE: %s (%s) HP %d/%d%s\n", me.Name, types(me.Type1, me.Type2), me.HP, me.MaxHP, statusTag(me.Status))
	fmt.Fprintf(&b, "OPPONENT ACTIVE: %s (%s) HP %d/%d%s\n", v.Foe.Name, types(v.Foe.Type1, v.Foe.Type2), v.Foe.HP, v.Foe.MaxHP, statusTag(v.Foe.Status))
	fmt.Fprintf(&b, "Opponent reserve: %d Pokémon\n\nOPTIONS:\n", v.FoeBenchAlive)
	for i, act := range acts {
		if act.Kind == engine.ActionSwitch {
			t := v.Self.Team[act.Index]
			fmt.Fprintf(&b, "[%d] Switch to %s (%s) HP %d/%d\n", i, t.Name, types(t.Type1, t.Type2), t.HP, t.MaxHP)
			continue
		}
		if act.Index < 0 {
			fmt.Fprintf(&b, "[%d] Struggle\n", i)
			continue
		}
		m := a.dex.Moves[me.Moves[act.Index].MoveID]
		fmt.Fprintf(&b, "[%d] Move: %s (%s, %s, power %d)\n", i, m.Name, m.Type, m.Category, m.Power)
	}
	b.WriteString("\nReply with ONLY a JSON object: {\"choice\": <index>, \"reasoning\": \"<short>\"}")
	return b.String()
}

// --- Anthropic Messages API plumbing ---

type llmBlock struct {
	Type         string    `json:"type"`
	Text         string    `json:"text"`
	CacheControl *llmCache `json:"cache_control,omitempty"`
}

type llmCache struct {
	Type string `json:"type"`
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    []llmBlock   `json:"system"`
	Messages  []llmMessage `json:"messages"`
}

type llmResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (a *LLMAgent) ask(ctx context.Context, prompt string) (int, error) {
	reqBody := llmRequest{
		Model:     a.model,
		MaxTokens: 200,
		System: []llmBlock{{
			Type: "text",
			// The rules text is static across every call — cache it.
			Text:         "You are an expert Pokémon battle AI. Respond with only a single JSON object, no other text.",
			CacheControl: &llmCache{Type: "ephemeral"},
		}},
		Messages: []llmMessage{{Role: "user", Content: prompt}},
	}
	raw, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("anthropic API status %d", resp.StatusCode)
	}

	var parsed llmResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, err
	}
	if len(parsed.Content) == 0 {
		return 0, fmt.Errorf("empty LLM response")
	}
	return extractChoice(parsed.Content[0].Text)
}

// extractChoice pulls {"choice": N} out of the model's text reply.
func extractChoice(text string) (int, error) {
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end <= start {
		return 0, fmt.Errorf("no JSON object in LLM reply")
	}
	var out struct {
		Choice int `json:"choice"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return 0, err
	}
	return out.Choice, nil
}

func types(t1, t2 domain.Type) string {
	if t2 == "" {
		return string(t1)
	}
	return string(t1) + "/" + string(t2)
}

func statusTag(s engine.StatusCond) string {
	if s == engine.StatusNone {
		return ""
	}
	return " [" + string(s) + "]"
}
