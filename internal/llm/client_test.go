package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postJSON is the shared transport for every adapter, so its 200 path is
// exercised by all four adapter tests. This pins the error paths it centralizes:
// a non-200 surfaces a bounded snippet of the response body, and the request
// carries the caller's headers and content-type.
func TestPostJSON_StatusErrorCarriesBodySnippet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":"rate limited, slow down"}`)
	}))
	defer srv.Close()

	resp, err := postJSON(context.Background(), srv.Client(), srv.URL, nil, map[string]any{"x": 1})
	if err == nil {
		resp.Body.Close()
		t.Fatal("non-200 must return an error")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error should carry status and body snippet, got: %v", err)
	}
}

func TestPostJSON_SetsHeadersAndContentType(t *testing.T) {
	var gotCT, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("content-type")
		gotAuth = r.Header.Get("authorization")
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	resp, err := postJSON(context.Background(), srv.Client(), srv.URL,
		map[string]string{"authorization": "Bearer tok"}, map[string]any{})
	if err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	resp.Body.Close()
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("authorization = %q, want Bearer tok", gotAuth)
	}
}
