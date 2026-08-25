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

// The Ollama client posts to /api/chat with streaming off, maps max_tokens onto
// num_predict, reports prompt/eval counts as input/output, and carries no
// cache tokens (there is no server-side cache locally).
func TestOllama_RequestAndUsage(t *testing.T) {
	var got ollamaRequest
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &got)
		io.WriteString(w, `{"message":{"role":"assistant","content":"move 1"},
			"prompt_eval_count":33,"eval_count":7}`)
	}))
	defer srv.Close()

	c := newOllama(Config{Model: "llama3.1:8b", BaseURL: srv.URL, MaxTokens: 300})
	text, u, err := c.Complete(context.Background(), "SYS", "USER")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "move 1" {
		t.Errorf("text = %q, want move 1", text)
	}
	if path != "/api/chat" {
		t.Errorf("path = %q, want /api/chat", path)
	}
	if got.Stream {
		t.Errorf("stream should be false")
	}
	if got.Model != "llama3.1:8b" {
		t.Errorf("model = %q", got.Model)
	}
	if got.Options.NumPredict != 300 {
		t.Errorf("num_predict = %d, want 300", got.Options.NumPredict)
	}
	want := usage.Usage{InputTokens: 33, OutputTokens: 7}
	if u != want {
		t.Errorf("usage = %+v, want %+v", u, want)
	}
}

// A local vendor is flagged so the run record can price it at zero rather than
// unknown, and it needs no API key.
func TestOllama_LocalAndKeyless(t *testing.T) {
	if !IsLocal("ollama") {
		t.Errorf("ollama should be local")
	}
	if IsLocal("openai") {
		t.Errorf("openai is not local")
	}
	if _, needs := KeyEnvVar("ollama"); needs {
		t.Errorf("ollama should need no key")
	}
	if _, err := New("ollama", Config{Model: "llama3.1:8b"}); err != nil {
		t.Errorf("New(ollama): %v", err)
	}
}
