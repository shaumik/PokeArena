package agentloop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/usage"
)

func loadDex(t *testing.T) *domain.Dex {
	t.Helper()
	d, err := domain.LoadDex("../../data", "test")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	return d
}

// stubClient is a deterministic, offline LLMClient: it returns a canned reply
// (or error) and records what it was asked, so the adapter can be tested with
// no network and no API key.
type stubClient struct {
	reply      string
	usage      usage.Usage
	err        error
	lastSystem string
	lastUser   string
	calls      int
}

func (s *stubClient) Complete(_ context.Context, system, user string) (string, usage.Usage, error) {
	s.calls++
	s.lastSystem, s.lastUser = system, user
	return s.reply, s.usage, s.err
}

func startView(t *testing.T, d *domain.Dex) ai.View {
	t.Helper()
	s, err := engine.NewBattle(d, "b", "Red", []int{6, 9, 26}, "Blue", []int{3, 65, 143}, 7)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	return ai.MakeView(s, 0)
}

// TestAgent_ReturnsChosenAction: the model's choice index maps to exactly the
// action at that index in the legal-action list the prompt was built from.
func TestAgent_ReturnsChosenAction(t *testing.T) {
	d := loadDex(t)
	v := startView(t, d)
	acts := ai.LegalActions(v)
	if len(acts) < 2 {
		t.Fatalf("need >=2 legal actions, got %d", len(acts))
	}

	stub := &stubClient{reply: `{"choice": 1, "reasoning": "test"}`}
	a := NewAgent("stub", d, stub)

	got, err := a.Decide(context.Background(), v)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got != acts[1] {
		t.Fatalf("got action %+v, want acts[1]=%+v", got, acts[1])
	}
}

// TestAgent_SendsSystemAndUserPrompt: the adapter forwards the static system
// block and a rendered per-turn user message to the client.
func TestAgent_SendsSystemAndUserPrompt(t *testing.T) {
	d := loadDex(t)
	v := startView(t, d)

	stub := &stubClient{reply: `{"choice": 0, "reasoning": "x"}`}
	a := NewAgent("stub", d, stub)
	if _, err := a.Decide(context.Background(), v); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if stub.lastSystem != SystemPrompt {
		t.Fatalf("system prompt not forwarded verbatim")
	}
	if !strings.Contains(stub.lastUser, "Turn") {
		t.Fatalf("user prompt missing rendered view: %q", stub.lastUser)
	}
}

// TestAgent_ClientErrorPropagates: a transport failure surfaces as an error so
// the eval driver can record a fallback.
func TestAgent_ClientErrorPropagates(t *testing.T) {
	d := loadDex(t)
	v := startView(t, d)

	a := NewAgent("stub", d, &stubClient{err: errors.New("boom")})
	if _, err := a.Decide(context.Background(), v); err == nil {
		t.Fatal("expected error from client failure, got nil")
	}
}

// TestAgent_MalformedReplyErrors: a non-JSON reply is a parse failure, again
// surfaced as an error rather than a random action.
func TestAgent_MalformedReplyErrors(t *testing.T) {
	d := loadDex(t)
	v := startView(t, d)

	a := NewAgent("stub", d, &stubClient{reply: "I choose Charizard!"})
	if _, err := a.Decide(context.Background(), v); err == nil {
		t.Fatal("expected error from malformed reply, got nil")
	}
}

// TestAgent_OutOfRangeChoiceErrors: an in-range-looking but too-large index is
// rejected by ParseDecision, not indexed blindly.
func TestAgent_OutOfRangeChoiceErrors(t *testing.T) {
	d := loadDex(t)
	v := startView(t, d)

	a := NewAgent("stub", d, &stubClient{reply: `{"choice": 9999, "reasoning": "oops"}`})
	if _, err := a.Decide(context.Background(), v); err == nil {
		t.Fatal("expected error from out-of-range choice, got nil")
	}
}

// TestAgent_ReportsUsage: the token accounting the client returned is exposed
// via LastUsage — this is the seam the benchmark reads per-decision cost from.
func TestAgent_ReportsUsage(t *testing.T) {
	d := loadDex(t)
	v := startView(t, d)

	want := usage.Usage{InputTokens: 120, OutputTokens: 15, CacheReadTokens: 900}
	a := NewAgent("stub", d, &stubClient{reply: `{"choice": 0, "reasoning": "x"}`, usage: want})
	if _, err := a.Decide(context.Background(), v); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if a.LastUsage() != want {
		t.Fatalf("LastUsage() = %+v, want %+v", a.LastUsage(), want)
	}
}

// TestAgent_CountsUsageOnFallback: a malformed reply still burned tokens, so
// LastUsage must reflect the call even though Decide returns an error. A
// benchmark that dropped these would under-report the true cost of a flaky
// model.
func TestAgent_CountsUsageOnFallback(t *testing.T) {
	d := loadDex(t)
	v := startView(t, d)

	want := usage.Usage{InputTokens: 100, OutputTokens: 8}
	a := NewAgent("stub", d, &stubClient{reply: "not json", usage: want})
	if _, err := a.Decide(context.Background(), v); err == nil {
		t.Fatal("expected parse error")
	}
	if a.LastUsage() != want {
		t.Fatalf("LastUsage() = %+v, want %+v (tokens spent despite fallback)", a.LastUsage(), want)
	}
}
