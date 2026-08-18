package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The fog-of-war reveal set (Pokemon.AbilityRevealed / ItemRevealed) is only
// as good as its coverage: one announcement site that forgets to reveal leaves
// an ability or item permanently invisible to the opposing agent even after it
// has visibly fired, which is a subtler and more annoying bug than the leak the
// reveal set was added to fix — the agent watches Static paralyze it and still
// reads the foe's ability as blank.
//
// Behavioral tests cannot catch that: they would have to enumerate all 67
// announcement sites, which is the same list this test derives from the source.
// So this scans instead. Every `Type: "ability"` / `Type: "item"` LogLine
// literal in the package must carry a revealAbility / revealItem call (or an
// explicit `// no-reveal:` waiver) in the few lines above the append that emits
// it.
//
// The waiver exists for one real case: Fling landing a berry on a target that
// never held it. The thrower's item is already revealed at the throw, and the
// target's own slot is nobody's business.
func TestEveryAbilityAndItemAnnouncementReveals(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	// How far above the announcement the reveal (or waiver) may sit. The
	// LogLine literal spans up to ~4 lines, and a two-line waiver comment or a
	// pair of reveal calls sits above that.
	const window = 8

	appendLine := regexp.MustCompile(`append\(.*LogLine\{`)
	var misses []string

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		lines := strings.Split(string(b), "\n")

		for i, l := range lines {
			trimmed := strings.TrimSpace(l)
			if strings.HasPrefix(trimmed, "//") {
				continue // the field's own doc comment quotes these literals
			}
			var want string
			switch {
			case strings.Contains(l, `Type: "ability"`):
				want = "revealAbility("
			case strings.Contains(l, `Type: "item"`):
				want = "revealItem("
			default:
				continue
			}

			// Walk back to the append that opens this literal, then look at
			// the handful of lines above it.
			start := i
			for start >= 0 && !appendLine.MatchString(lines[start]) {
				start--
			}
			if start < 0 {
				misses = append(misses, f+":"+strconv.Itoa(i+1)+" — no enclosing append() found")
				continue
			}
			lo := start - window
			if lo < 0 {
				lo = 0
			}
			region := strings.Join(lines[lo:start+1], "\n")
			if strings.Contains(region, want) || strings.Contains(region, "// no-reveal:") {
				continue
			}
			misses = append(misses, f+":"+strconv.Itoa(i+1)+" — announces but never calls "+want+")")
		}
	}

	if len(misses) > 0 {
		t.Errorf("%d announcement site(s) do not reveal what they announce:\n  %s\n\n"+
			"Add a %s / %s call above the append, or an explicit `// no-reveal: why` "+
			"comment if the announcement genuinely does not expose the announcer's own "+
			"ability or item.",
			len(misses), strings.Join(misses, "\n  "), "revealAbility(p)", "revealItem(p)")
	}
}
