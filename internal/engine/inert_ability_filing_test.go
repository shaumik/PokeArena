package engine

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// abilities.go groups its registry under two headings that mean opposite
// things:
//
//	--- recognized but inert ---            registered so a lookup can tell
//	                                        "not modeled" from "not an
//	                                        ability", and does nothing
//	--- hook-free but fully functional ---  carries only Kind because some
//	                                        *other* layer reads the slug
//
// Five slugs were filed under the second heading while the first heading's own
// documentation comment, twenty lines above, listed them as inert. A referee
// in the agent tournament read the heading, concluded Neutralizing Gas worked,
// and a team spent a Pokémon switching in to suppress an ability that has no
// suppression code anywhere in the repo. The engine behaved correctly; its
// description of itself did not, which is the more expensive kind of wrong,
// because it is the kind a reader acts on.
//
// This pins the two against each other: the doc comment is the specification,
// the registrations under each heading are the implementation, and a slug may
// not claim both.
func TestInertAbilitiesAreFiledAsInert(t *testing.T) {
	b, err := os.ReadFile("abilities.go")
	if err != nil {
		t.Fatalf("read abilities.go: %v", err)
	}
	src := string(b)

	inertStart := strings.Index(src, "--- recognized but inert ---")
	funcStart := strings.Index(src, "--- hook-free but fully functional ---")
	if inertStart < 0 || funcStart < 0 || funcStart < inertStart {
		t.Fatal("abilities.go no longer carries both group headings in order; " +
			"this test pins their contents and needs updating with them")
	}
	// Each group runs from its heading to the next heading (or, for the last
	// one, to the first entry that carries a hook — the groups are contiguous).
	inertBlock := src[inertStart:funcStart]
	funcBlock := src[funcStart:]
	if end := strings.Index(funcBlock, "\n\t\t\"pressure\": {"); end > 0 {
		funcBlock = funcBlock[:end]
	}

	// Slugs the doc comment *names* as inert: the bullet lines under the two
	// "Blocked on…" / "Inert by design…" sub-headings.
	docNamed := map[string]bool{}
	bullet := regexp.MustCompile(`^\s*//\s{2,}([a-z-]+(?:\s*/\s*[a-z-]+)*)\s+—`)
	for _, line := range strings.Split(inertBlock, "\n") {
		m := bullet.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, slug := range strings.Split(m[1], "/") {
			docNamed[strings.TrimSpace(slug)] = true
		}
	}
	if len(docNamed) == 0 {
		t.Fatal("parsed no slugs out of the inert group's documentation; the comment shape changed")
	}

	registered := func(block string) map[string]bool {
		out := map[string]bool{}
		re := regexp.MustCompile(`(?m)^\t\t"([a-z-]+)":\s*\{Kind:`)
		for _, m := range re.FindAllStringSubmatch(block, -1) {
			out[m[1]] = true
		}
		return out
	}
	inertReg, funcReg := registered(inertBlock), registered(funcBlock)

	for slug := range docNamed {
		if funcReg[slug] {
			t.Errorf("%q is documented as inert but registered under "+
				"\"hook-free but fully functional\" — a reader consulting the "+
				"registry is told it works", slug)
		}
		if !inertReg[slug] {
			t.Errorf("%q is documented as inert but is not registered in the inert group; "+
				"either it moved and the comment did not, or it is missing entirely", slug)
		}
	}

	// The other direction: a slug filed as fully functional has to be read by
	// *something*, or the heading is a claim nothing backs.
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for slug := range funcReg {
		found := false
		for _, f := range files {
			n := f.Name()
			if f.IsDir() || !strings.HasSuffix(n, ".go") ||
				strings.HasSuffix(n, "_test.go") || n == "abilities.go" {
				continue
			}
			c, err := os.ReadFile(n)
			if err != nil {
				t.Fatalf("read %s: %v", n, err)
			}
			// The consuming layer names the slug, one way or another —
			// abilityIsX helpers and direct Kind comparisons both count.
			if strings.Contains(string(c), slug) ||
				strings.Contains(string(c), strings.ReplaceAll(slug, "-", "")) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q is filed as \"hook-free but fully functional\" but no other "+
				"file in the package mentions it — nothing consults it, so it is inert", slug)
		}
	}
}

// TestAbilityInertMapMatchesItsFiling: AbilityInertReason answers the question
// "is this pick built on nothing?" for `royale validate`, and it answers it out
// of the abilityInert map rather than by inspecting hooks — because a slug can
// carry no hooks and still work (Gluttony, Sticky Hold, Arena Trap: the layer
// that implements them reads the slug). That makes the map a third statement
// of the same fact, next to the doc comment and the registry group, so this
// pins it to both.
//
// The second half is the one that matters as the registry grows: every
// hookless registration that is *not* filed as inert has to be named by some
// other file in the package. Own Tempo was registered with a comment saying
// its guard lived "elsewhere", nothing read the slug, and the ability was
// inert while describing itself as working — exactly the shape this catches.
func TestAbilityInertMapMatchesItsFiling(t *testing.T) {
	b, err := os.ReadFile("abilities.go")
	if err != nil {
		t.Fatalf("read abilities.go: %v", err)
	}
	src := string(b)
	inertStart := strings.Index(src, "--- recognized but inert ---")
	funcStart := strings.Index(src, "--- hook-free but fully functional ---")
	if inertStart < 0 || funcStart < 0 || funcStart < inertStart {
		t.Fatal("abilities.go no longer carries both group headings in order")
	}
	inertBlock := src[inertStart:funcStart]

	// Filed under the inert heading ⇔ present in the map, with a reason.
	re := regexp.MustCompile(`(?m)^\t\t"([a-z-]+)":\s*\{Kind:`)
	registered := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(inertBlock, -1) {
		registered[m[1]] = true
	}
	for slug := range registered {
		if AbilityInertReason(slug) == "" {
			t.Errorf("%q is registered in the inert group but AbilityInertReason "+
				"calls it functional — `royale validate` would pass a roster built on it", slug)
		}
	}
	for _, slug := range InertAbilities() {
		if !registered[slug] {
			t.Errorf("%q is filed as inert in abilityInert but is not registered in "+
				"the inert group; either it moved and the map did not, or it now works", slug)
		}
	}

	// Every other hookless registration has to be read by name somewhere.
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, slug := range hooklessAbilities() {
		if registered[slug] {
			continue
		}
		found := false
		for _, f := range files {
			n := f.Name()
			if f.IsDir() || !strings.HasSuffix(n, ".go") ||
				strings.HasSuffix(n, "_test.go") || n == "abilities.go" {
				continue
			}
			c, err := os.ReadFile(n)
			if err != nil {
				t.Fatalf("read %s: %v", n, err)
			}
			// Case-insensitive: a consumer may name the slug through its
			// AbilityKind constant (AbilityGluttony) rather than the literal.
			hay := strings.ToLower(string(c))
			if strings.Contains(hay, slug) ||
				strings.Contains(hay, strings.ReplaceAll(slug, "-", "")) {
				found = true
				break
			}
		}
		// abilities.go itself counts too: several hookless slugs are consulted
		// by helpers that live beside the registry (abilityTrapsFoe and
		// friends). What must not happen is a slug nothing reads at all.
		if !found {
			body := strings.ToLower(src[:inertStart] + src[funcStart:])
			bare := strings.ReplaceAll(slug, "-", "")
			// A registration mentions its slug exactly twice — as the map key
			// and as the Kind — so a third mention is the first one that is a
			// helper beside the registry actually reading it.
			found = strings.Count(body, slug)+strings.Count(body, bare) > 2
		}
		if !found {
			t.Errorf("%q carries no hooks, is not filed as inert, and nothing reads "+
				"the slug — it does nothing while claiming to work", slug)
		}
	}
}
