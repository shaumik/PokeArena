package livebattle

import (
	"fmt"
	"os"
	"testing"

	"pokearena/internal/engine"
)

// main_test.go turns every test in this package into an invariant test. See
// internal/engine/state_invariants.go (OnInvariantViolation) for why the hook
// exists, and internal/engine/main_test.go for the same wiring at the source.
//
// This package matters because it drives whole battles rather than single
// turns: a corruption that only shows up after a switch, a replacement, or a
// fog-of-war round trip surfaces here and nowhere else.

var invariantBreach error

func TestMain(m *testing.M) {
	engine.OnInvariantViolation = func(err error) {
		if invariantBreach == nil {
			invariantBreach = err
		}
	}
	code := m.Run()
	if invariantBreach != nil {
		fmt.Fprintf(os.Stderr,
			"\nFAIL: a resolved turn left the battle state malformed:\n    %v\n",
			invariantBreach)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
