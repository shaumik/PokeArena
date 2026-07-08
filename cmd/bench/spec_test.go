package main

import (
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/eval"
)

// firstDuplicateName must catch a collision (e.g. -agents random,random or a
// label= that shadows a baseline) before any match runs, and pass a unique set.
func TestFirstDuplicateName(t *testing.T) {
	c := func(name string) eval.Contestant {
		return eval.Contestant{Name: name, New: func(uint64) ai.Agent { return ai.NewRandomAgent(0) }}
	}
	if got := firstDuplicateName([]eval.Contestant{c("random"), c("heuristic"), c("random")}); got != "random" {
		t.Errorf("firstDuplicateName = %q, want random", got)
	}
	if got := firstDuplicateName([]eval.Contestant{c("a"), c("b"), c("expectimax")}); got != "" {
		t.Errorf("firstDuplicateName on unique set = %q, want empty", got)
	}
}

func TestParseLLMSpec(t *testing.T) {
	cases := []struct {
		in                         string
		label, vendor, model, cond string
	}{
		// Bare model: raw condition, default (anthropic) vendor, model as label.
		{"claude-haiku-4-5", "claude-haiku-4-5", "", "claude-haiku-4-5", "raw"},
		// Explicit label.
		{"haiku=claude-haiku-4-5", "haiku", "", "claude-haiku-4-5", "raw"},
		// Condition suffix, no label: the label picks up the condition so the
		// same model in two conditions gets distinct names.
		{"claude-sonnet-4-6/cot", "claude-sonnet-4-6:cot", "", "claude-sonnet-4-6", "cot"},
		{"claude-sonnet-4-6/raw", "claude-sonnet-4-6", "", "claude-sonnet-4-6", "raw"},
		// Label and condition together — label is used verbatim.
		{"son-cot=claude-sonnet-4-6/cot", "son-cot", "", "claude-sonnet-4-6", "cot"},
		// Vendor prefix.
		{"openai:gpt-5", "gpt-5", "openai", "gpt-5", "raw"},
		{"gpt5=openai:gpt-5/cot", "gpt5", "openai", "gpt-5", "cot"},
		// Unknown prefix is not a vendor: the whole colon-bearing string stays
		// the model (a local id like "llama3.1:8b" under the default vendor).
		{"llama3.1:8b", "llama3.1:8b", "", "llama3.1:8b", "raw"},
		// Explicit ollama prefix keeps the model's own colon tag intact.
		{"ollama:llama3.1:8b", "llama3.1:8b", "ollama", "llama3.1:8b", "raw"},
		{"l3=ollama:llama3.1:8b/cot", "l3", "ollama", "llama3.1:8b", "cot"},
	}
	for _, c := range cases {
		got, err := parseLLMSpec(c.in)
		if err != nil {
			t.Errorf("parseLLMSpec(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got.label != c.label || got.vendor != c.vendor || got.model != c.model || got.condition != c.cond {
			t.Errorf("parseLLMSpec(%q) = {label:%q vendor:%q model:%q cond:%q}, want {label:%q vendor:%q model:%q cond:%q}",
				c.in, got.label, got.vendor, got.model, got.condition, c.label, c.vendor, c.model, c.cond)
		}
	}
}

func TestParseLLMSpec_Errors(t *testing.T) {
	for _, in := range []string{
		"claude/thinking", // unknown condition
		"/cot",            // no model
		"label=/cot",      // empty model
	} {
		if _, err := parseLLMSpec(in); err == nil {
			t.Errorf("parseLLMSpec(%q) should error", in)
		}
	}
}
