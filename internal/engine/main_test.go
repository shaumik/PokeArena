package engine

import (
	"fmt"
	"os"
	"testing"
)

// main_test.go turns every test in this package into an invariant test.
//
// ValidateStateInvariants was written as a guardrail and then only ever called
// where a test remembered to call it — about a dozen places out of several
// hundred that resolve turns. With the hook set here, any test that calls
// ResolveTurn or ResolveReplace and leaves the state malformed fails at the
// point of corruption rather than surfacing turns later as a wrong damage
// number, or not at all.
//
// Measured value, rather than hoped-for: neither of the two corruptions found
// in this engine so far would have been caught this way, because no test outside
// the dedicated ones produces those states. What it caught immediately was a
// fixture setting a Snorlax to 999 HP against a MaxHP of 235 — an impossible
// state quietly exercised for as long as that test existed. Treat it as cheap
// insurance and a fixture-quality gate, not as a substitute for a test that
// targets the mechanic.
//
// The hook records rather than calling t.Fatal because ResolveTurn is not handed
// a *testing.T, and a panic from inside the engine would produce a stack that
// points at the engine rather than at the test that got there. Recording the
// first breach and failing the run keeps the message readable.

// invariantBreach is the first violation seen during the run, if any.
var invariantBreach error

func TestMain(m *testing.M) {
	OnInvariantViolation = func(err error) {
		if invariantBreach == nil {
			invariantBreach = err
		}
	}
	code := m.Run()
	if invariantBreach != nil {
		fmt.Fprintf(os.Stderr,
			"\nFAIL: a resolved turn left the battle state malformed:\n    %v\n\n"+
				"This is ValidateStateInvariants running automatically after every\n"+
				"ResolveTurn and ResolveReplace in this package (see TestMain in\n"+
				"main_test.go). Run the failing test alone to find which one; the\n"+
				"breach is reported from the first turn that produced it.\n",
			invariantBreach)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
