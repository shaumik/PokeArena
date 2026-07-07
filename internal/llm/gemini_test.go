package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pokearena/internal/usage"
)

// The cot Gemini client passes the thinking budget straight through, targets
// the model in the URL path, sends the key as a header, skips thought parts in
// the answer, and folds thinking tokens into the output count.
func TestGemini_CoTRequestAndUsage(t *testing.T) {
	var got geminiRequest
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if h := r.Header.Get("x-goog-api-key"); h != "gkey" {
			t.Errorf("api key header = %q, want gkey", h)
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &got)
		io.WriteString(w, `{
			"candidates":[{"content":{"parts":[
				{"text":"reasoning...","thought":true},
				{"text":"switch 3"}
			]}}],
			"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":6,
				"cachedContentTokenCount":30,"thoughtsTokenCount":25}}`)
	}))
	defer srv.Close()

	c := newGemini(Config{Key: "gkey", Model: "gemini-2.5-pro", BaseURL: srv.URL, Thinking: 2048, MaxTokens: 2560})
	text, u, err := c.Complete(context.Background(), "SYS", "USER")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "switch 3" {
		t.Errorf("text = %q, want %q (thought part must be skipped)", text, "switch 3")
	}
	if !strings.HasSuffix(path, "/v1beta/models/gemini-2.5-pro:generateContent") {
		t.Errorf("path = %q, want model in generateContent path", path)
	}
	if got.GenerationConfig.ThinkingConfig == nil || got.GenerationConfig.ThinkingConfig.ThinkingBudget != 2048 {
		t.Errorf("thinkingBudget = %+v, want 2048", got.GenerationConfig.ThinkingConfig)
	}
	if got.GenerationConfig.MaxOutputTokens != 2560 {
		t.Errorf("maxOutputTokens = %d, want 2560", got.GenerationConfig.MaxOutputTokens)
	}
	// input 100-30 cached; output 6 answer + 25 thoughts.
	want := usage.Usage{InputTokens: 70, OutputTokens: 31, CacheReadTokens: 30}
	if u != want {
		t.Errorf("usage = %+v, want %+v", u, want)
	}
}

// cachedContentTokenCount >= promptTokenCount must clamp input to 0, never
// produce a negative count that offsets other agents' run totals.
func TestGemini_NegativeInputClampedToZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],
			"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":3,"cachedContentTokenCount":50}}`)
	}))
	defer srv.Close()

	c := newGemini(Config{Key: "k", Model: "gemini-2.5-flash", BaseURL: srv.URL})
	_, u, err := c.Complete(context.Background(), "s", "u")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if u.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0 (20-50 clamped)", u.InputTokens)
	}
}

// Raw mode still sends a thinkingConfig, but with a zero budget.
func TestGemini_RawZeroBudget(t *testing.T) {
	var got geminiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &got)
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"move 0"}]}}],
			"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2}}`)
	}))
	defer srv.Close()

	c := newGemini(Config{Key: "k", Model: "gemini-2.5-flash", BaseURL: srv.URL})
	text, _, err := c.Complete(context.Background(), "s", "u")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "move 0" {
		t.Errorf("text = %q, want move 0", text)
	}
	if got.GenerationConfig.ThinkingConfig == nil || got.GenerationConfig.ThinkingConfig.ThinkingBudget != 0 {
		t.Errorf("raw thinkingBudget should be 0, got %+v", got.GenerationConfig.ThinkingConfig)
	}
}
