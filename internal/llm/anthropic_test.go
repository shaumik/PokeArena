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

// captureServer records the last request body the client sent and replies with
// whatever the handler writes.
func captureServer(t *testing.T, handler func(w http.ResponseWriter, body request)) (*httptest.Server, *request) {
	t.Helper()
	var got request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("server got un-decodable body: %v", err)
		}
		handler(w, got)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// The default client reproduces the thin harness: 256-token cap, no thinking,
// no streaming — and it decodes a plain JSON response.
func TestComplete_DefaultRequestAndParse(t *testing.T) {
	srv, got := captureServer(t, func(w http.ResponseWriter, _ request) {
		json.NewEncoder(w).Encode(jsonResponse{
			Content: []block{{Type: "text", Text: "move 0"}},
			Usage:   apiUsage{InputTokens: 40, OutputTokens: 3, CacheReadInputTokens: 100},
		})
	})

	c := NewAnthropic("k", "claude-haiku-4-5", WithBaseURL(srv.URL))
	text, u, err := c.Complete(context.Background(), "SYS", "USER")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "move 0" {
		t.Errorf("text = %q, want %q", text, "move 0")
	}
	want := usage.Usage{InputTokens: 40, OutputTokens: 3, CacheReadTokens: 100}
	if u != want {
		t.Errorf("usage = %+v, want %+v", u, want)
	}
	if got.MaxTokens != defaultMaxTokens {
		t.Errorf("max_tokens = %d, want %d", got.MaxTokens, defaultMaxTokens)
	}
	if got.Stream {
		t.Errorf("stream should be off by default")
	}
	if got.Thinking != nil {
		t.Errorf("thinking should be absent by default, got %+v", got.Thinking)
	}
	if len(got.System) != 1 || got.System[0].CacheControl == nil {
		t.Errorf("system block should be present and cached: %+v", got.System)
	}
}

// A non-streamed response that leads with a thinking block must still yield the
// text block, not the empty thinking one.
func TestComplete_SkipsThinkingBlock(t *testing.T) {
	srv, _ := captureServer(t, func(w http.ResponseWriter, _ request) {
		json.NewEncoder(w).Encode(jsonResponse{
			Content: []block{
				{Type: "thinking", Text: ""},
				{Type: "text", Text: "switch 2"},
			},
		})
	})
	c := NewAnthropic("k", "m", WithBaseURL(srv.URL))
	text, _, err := c.Complete(context.Background(), "SYS", "USER")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "switch 2" {
		t.Errorf("text = %q, want %q", text, "switch 2")
	}
}

// WithMaxTokens raises the output cap without touching anything else.
func TestWithMaxTokens(t *testing.T) {
	srv, got := captureServer(t, func(w http.ResponseWriter, _ request) {
		json.NewEncoder(w).Encode(jsonResponse{Content: []block{{Type: "text", Text: "ok"}}})
	})
	c := NewAnthropic("k", "m", WithBaseURL(srv.URL), WithMaxTokens(4096))
	if _, _, err := c.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.MaxTokens != 4096 {
		t.Errorf("max_tokens = %d, want 4096", got.MaxTokens)
	}
}

// WithThinking flips on the CoT column: it sets the thinking budget, forces
// streaming, guarantees max_tokens exceeds the budget, and the client parses
// the SSE stream — accumulating text_delta while discarding thinking_delta,
// and stitching the split usage report.
func TestComplete_ThinkingStreaming(t *testing.T) {
	srv, got := captureServer(t, func(w http.ResponseWriter, body request) {
		if !body.Stream {
			t.Errorf("thinking client must stream")
		}
		if body.Thinking == nil || body.Thinking.BudgetTokens != 2000 {
			t.Errorf("thinking config = %+v, want budget 2000", body.Thinking)
		}
		if body.MaxTokens <= body.Thinking.BudgetTokens {
			t.Errorf("max_tokens %d must exceed budget %d", body.MaxTokens, body.Thinking.BudgetTokens)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"usage":{"input_tokens":50,"cache_read_input_tokens":200}}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hmm foe is faster"}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"move "}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"1"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","usage":{"output_tokens":37}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))
	})

	c := NewAnthropic("k", "claude-sonnet-4-6", WithBaseURL(srv.URL), WithThinking(2000))
	text, u, err := c.Complete(context.Background(), "SYS", "USER")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "move 1" {
		t.Errorf("text = %q, want %q (thinking_delta must be discarded)", text, "move 1")
	}
	want := usage.Usage{InputTokens: 50, OutputTokens: 37, CacheReadTokens: 200}
	if u != want {
		t.Errorf("usage = %+v, want %+v", u, want)
	}
	if !got.Stream {
		t.Errorf("captured request should have stream=true")
	}
}
