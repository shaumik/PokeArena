package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"pokearena/internal/usage"
)

// Client is the provider-agnostic decision boundary: given the static system
// prompt and the per-turn user prompt, return the model's text, the token usage
// the provider reported, and an error. It matches agentloop.LLMClient
// structurally, so any adapter here drops into the agent loop and the benchmark
// without either side importing the other.
type Client interface {
	Complete(ctx context.Context, system, user string) (string, usage.Usage, error)
}

// Config is the vendor-agnostic knob set the factory hands to an adapter. Each
// adapter maps these onto its own wire format; a zero value in a field means
// "use the adapter's default". Thinking is a token budget for native reasoning
// (extended thinking / reasoning effort); 0 leaves it off — this is what the
// benchmark's Raw (0) and CoT (>0) columns set.
type Config struct {
	Key       string
	Model     string
	MaxTokens int
	Thinking  int
	Timeout   time.Duration
	BaseURL   string
}

// vendorEnv maps a vendor to the environment variable holding its API key. An
// empty value means the vendor needs no key (a local runner). Vendors are added
// as their adapters land.
var vendorEnv = map[string]string{
	"anthropic": "ANTHROPIC_API_KEY",
	"openai":    "OPENAI_API_KEY",
	"gemini":    "GEMINI_API_KEY",
	"ollama":    "", // local runner, no key
}

// canonVendor maps the empty vendor to the default (anthropic), matching New's
// dispatch, so every lookup treats a bare model spec as Anthropic.
func canonVendor(name string) string {
	v := strings.ToLower(name)
	if v == "" {
		return "anthropic"
	}
	return v
}

// KnownVendor reports whether name is a vendor the factory can build. The empty
// name is the default vendor, so it is known.
func KnownVendor(name string) bool {
	_, ok := vendorEnv[canonVendor(name)]
	return ok
}

// KeyEnvVar returns the environment variable a vendor's key is read from, and
// whether the vendor needs a key at all. The empty vendor is the default
// (anthropic); a local vendor (Ollama) returns ("", false).
func KeyEnvVar(vendor string) (string, bool) {
	env, ok := vendorEnv[canonVendor(vendor)]
	return env, ok && env != ""
}

// Vendors lists the buildable vendors in a stable order, for help text.
func Vendors() []string { return []string{"anthropic", "openai", "gemini", "ollama"} }

// IsLocal reports whether a vendor runs locally and therefore has no API cost.
func IsLocal(vendor string) bool { return strings.ToLower(vendor) == "ollama" }

// trimSlash drops a trailing slash from a base URL so path joins stay clean.
func trimSlash(u string) string { return strings.TrimRight(u, "/") }

// New builds the adapter for a vendor from a shared Config. Vendor "" defaults
// to anthropic, preserving the original single-vendor behaviour.
func New(vendor string, cfg Config) (Client, error) {
	switch strings.ToLower(vendor) {
	case "", "anthropic":
		return newAnthropicFromConfig(cfg), nil
	case "openai":
		return newOpenAI(cfg), nil
	case "gemini":
		return newGemini(cfg), nil
	case "ollama":
		return newOllama(cfg), nil
	default:
		return nil, fmt.Errorf("unknown vendor %q (want one of %s)", vendor, strings.Join(Vendors(), ", "))
	}
}
