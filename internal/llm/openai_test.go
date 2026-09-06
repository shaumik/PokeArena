package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shaumik/PokeArena/internal/usage"
)

// The raw (no-thinking) OpenAI client sends max_completion_tokens, omits
// reasoning_effort, and splits cached prompt tokens out of the input count.
func TestOpenAI_RawRequestAndUsage(t *testing.T) {
	var got openAIRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "Bearer sk-test" {
			t.Errorf("auth header = %q, want bearer sk-test", h)
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &got)
		io.WriteString(w, `{"choices":[{"message":{"content":"move 2"}}],
			"usage":{"prompt_tokens":100,"completion_tokens":8,
			"prompt_tokens_details":{"cached_tokens":60}}}`)
	}))
	defer srv.Close()

	c := newOpenAI(Config{Key: "sk-test", Model: "gpt-5", BaseURL: srv.URL})
	text, u, err := c.Complete(context.Background(), "SYS", "USER")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "move 2" {
		t.Errorf("text = %q, want %q", text, "move 2")
	}
	if got.MaxCompletionTokens != defaultMaxTokens {
		t.Errorf("max_completion_tokens = %d, want %d", got.MaxCompletionTokens, defaultMaxTokens)
	}
	if got.ReasoningEffort != "" {
		t.Errorf("reasoning_effort should be empty in raw mode, got %q", got.ReasoningEffort)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "system" || got.Messages[1].Role != "user" {
		t.Errorf("messages = %+v, want system then user", got.Messages)
	}
	// prompt_tokens (100) splits into 40 fresh + 60 cached.
	want := usage.Usage{InputTokens: 40, OutputTokens: 8, CacheReadTokens: 60}
	if u != want {
		t.Errorf("usage = %+v, want %+v", u, want)
	}
}

// A cot Config carries a thinking budget and a raised token cap; the adapter
// maps the budget onto a reasoning_effort level and passes the cap through.
func TestOpenAI_CoTReasoningEffort(t *testing.T) {
	var got openAIRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &got)
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
	}))
	defer srv.Close()

	c := newOpenAI(Config{Key: "k", Model: "gpt-5", BaseURL: srv.URL, Thinking: 2048, MaxTokens: 2560})
	if _, _, err := c.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.ReasoningEffort != "medium" {
		t.Errorf("reasoning_effort = %q, want medium (budget 2048)", got.ReasoningEffort)
	}
	if got.MaxCompletionTokens != 2560 {
		t.Errorf("max_completion_tokens = %d, want 2560", got.MaxCompletionTokens)
	}
}

// A reasoning model can spend its whole budget and return empty content. That
// call WAS billed, so Complete must return an error AND the measured usage — not
// zero — or the fallback decision would be counted as free and undercount cost.
func TestOpenAI_EmptyContentStillBillsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":""}}],
			"usage":{"prompt_tokens":500,"completion_tokens":4096}}`)
	}))
	defer srv.Close()

	c := newOpenAI(Config{Key: "k", Model: "o4-mini", BaseURL: srv.URL})
	_, u, err := c.Complete(context.Background(), "s", "u")
	if err == nil {
		t.Fatal("empty content should return an error")
	}
	if u.IsZero() {
		t.Fatalf("empty-content call must still report the tokens it burned, got zero")
	}
	want := usage.Usage{InputTokens: 500, OutputTokens: 4096}
	if u != want {
		t.Errorf("usage on empty content = %+v, want %+v", u, want)
	}
}

// A provider that reports cached >= prompt must not yield a negative input
// count — that would subtract from other agents' run totals.
func TestOpenAI_NegativeInputClampedToZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],
			"usage":{"prompt_tokens":40,"completion_tokens":2,
			"prompt_tokens_details":{"cached_tokens":60}}}`)
	}))
	defer srv.Close()

	c := newOpenAI(Config{Key: "k", Model: "gpt-5", BaseURL: srv.URL})
	_, u, err := c.Complete(context.Background(), "s", "u")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if u.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0 (40-60 clamped)", u.InputTokens)
	}
}

func TestEffortFromBudget(t *testing.T) {
	for _, c := range []struct {
		budget int
		want   string
	}{{1024, "low"}, {1025, "medium"}, {4096, "medium"}, {4097, "high"}} {
		if got := effortFromBudget(c.budget); got != c.want {
			t.Errorf("effortFromBudget(%d) = %q, want %q", c.budget, got, c.want)
		}
	}
}

// Regression: a bare model spec has vendor "", which must resolve to the
// default vendor's key env var — not "no key needed". The empty-vendor bug sent
// keyless requests that 401'd on every turn.
func TestKeyEnvVar_DefaultVendor(t *testing.T) {
	env, needs := KeyEnvVar("")
	if !needs || env != "ANTHROPIC_API_KEY" {
		t.Errorf(`KeyEnvVar("") = (%q, %v), want ("ANTHROPIC_API_KEY", true)`, env, needs)
	}
	if !KnownVendor("") {
		t.Errorf(`KnownVendor("") should be true (default vendor)`)
	}
}

// The factory dispatches by vendor and rejects unknown ones.
func TestNew_VendorDispatch(t *testing.T) {
	if _, err := New("openai", Config{Key: "k", Model: "gpt-5"}); err != nil {
		t.Errorf("New(openai): %v", err)
	}
	if _, err := New("", Config{Key: "k", Model: "claude-haiku-4-5"}); err != nil {
		t.Errorf("New(default anthropic): %v", err)
	}
	if _, err := New("mistral", Config{Model: "x"}); err == nil {
		t.Errorf("New(mistral) should reject an unknown vendor")
	}
}
