package main

import "testing"

func TestParseLLMSpec(t *testing.T) {
	cases := []struct {
		in                 string
		label, model, cond string
	}{
		// Bare model defaults to the raw condition and the model as its label.
		{"claude-haiku-4-5", "claude-haiku-4-5", "claude-haiku-4-5", "raw"},
		// Explicit label.
		{"haiku=claude-haiku-4-5", "haiku", "claude-haiku-4-5", "raw"},
		// Condition suffix, no label: the label picks up the condition so the
		// same model in two conditions gets distinct names.
		{"claude-sonnet-4-6/cot", "claude-sonnet-4-6:cot", "claude-sonnet-4-6", "cot"},
		{"claude-sonnet-4-6/raw", "claude-sonnet-4-6", "claude-sonnet-4-6", "raw"},
		// Label and condition together — label is used verbatim.
		{"son-cot=claude-sonnet-4-6/cot", "son-cot", "claude-sonnet-4-6", "cot"},
	}
	for _, c := range cases {
		got, err := parseLLMSpec(c.in)
		if err != nil {
			t.Errorf("parseLLMSpec(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got.label != c.label || got.model != c.model || got.condition != c.cond {
			t.Errorf("parseLLMSpec(%q) = {%q %q %q}, want {%q %q %q}",
				c.in, got.label, got.model, got.condition, c.label, c.model, c.cond)
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
