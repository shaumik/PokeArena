package protocol

import (
	"net/url"
	"strings"
	"testing"
)

func TestSanitizeTrainerName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Red", "Red"},
		{"trims surrounding space", "  Red  ", "Red"},
		{"keeps interior space", "Trainer Red", "Trainer Red"},
		{"empty stays empty", "", ""},
		{"whitespace only collapses to empty", "   \t  ", ""},
		// A newline in a name would break every log line and room frame that
		// prints it, so controls are folded to a space rather than kept.
		{"folds newline to space", "Trainer\nRed", "Trainer Red"},
		{"drops other controls", "Red\x00\x07", "Red"},
		{"caps at the max", strings.Repeat("a", MaxTrainerNameLen+10), strings.Repeat("a", MaxTrainerNameLen)},
		{"trims after capping", strings.Repeat("a", MaxTrainerNameLen-1) + "   tail", strings.Repeat("a", MaxTrainerNameLen-1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SanitizeTrainerName(c.in); got != c.want {
				t.Errorf("SanitizeTrainerName(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// A byte-based cap would slice a multi-byte rune in half and store invalid
// UTF-8 under a name that is then a leaderboard key. Count runes.
func TestSanitizeTrainerName_CapsRunesNotBytes(t *testing.T) {
	in := strings.Repeat("é", MaxTrainerNameLen+5) // 2 bytes per rune
	got := SanitizeTrainerName(in)
	if n := len([]rune(got)); n != MaxTrainerNameLen {
		t.Errorf("kept %d runes, want %d", n, MaxTrainerNameLen)
	}
	if !isValidUTF8(got) {
		t.Errorf("truncation produced invalid UTF-8: %q", got)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

func TestPlayPath(t *testing.T) {
	t.Run("omits the name query when none is declared", func(t *testing.T) {
		got := PlayPath("b1", "p2", "tok", "")
		want := "/api/battles/b1/play?slot=p2&token=tok"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("appends a declared name", func(t *testing.T) {
		got := PlayPath("b1", "p2", "tok", "Red")
		want := "/api/battles/b1/play?slot=p2&token=tok&name=Red"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// A name is the only join field that can contain a space or '&' — the
	// others are a UUID, "p1"/"p2", and base64url. Unescaped, "A&slot=p1"
	// would smuggle a second slot param into the query.
	t.Run("escapes a name that could forge a query param", func(t *testing.T) {
		got := PlayPath("b1", "p2", "tok", "A&slot=p1")
		q, err := url.Parse(got)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if slot := q.Query().Get("slot"); slot != "p2" {
			t.Errorf("slot = %q, want p2 — the name forged a query param", slot)
		}
		if name := q.Query().Get("name"); name != "A&slot=p1" {
			t.Errorf("name = %q, want it round-tripped intact", name)
		}
	})

	// A client building its own path must not be able to bypass the shared
	// rule by passing something the gateway would have rejected.
	t.Run("sanitizes before appending", func(t *testing.T) {
		got := PlayPath("b1", "p2", "tok", "  Red\n  ")
		if name := mustQuery(t, got).Get("name"); name != "Red" {
			t.Errorf("name = %q, want %q", name, "Red")
		}
	})
}

func TestLivePlayPath(t *testing.T) {
	t.Run("stays slotless and queryless without a name", func(t *testing.T) {
		got := LivePlayPath("b1", "")
		if got != "/api/battles/b1/play" {
			t.Errorf("got %q, want the bare path", got)
		}
	})

	// The gateway routes to the live handler on the *absence of a slot param*,
	// not on an empty query — so adding "name" must not reroute the join.
	t.Run("a name does not introduce a slot param", func(t *testing.T) {
		got := LivePlayPath("b1", "Red")
		q := mustQuery(t, got)
		if q.Has("slot") {
			t.Errorf("got %q, which would route to the pvp handler", got)
		}
		if q.Get("name") != "Red" {
			t.Errorf("name = %q, want Red", q.Get("name"))
		}
	})
}

func mustQuery(t *testing.T, rawPath string) url.Values {
	t.Helper()
	u, err := url.Parse(rawPath)
	if err != nil {
		t.Fatalf("parse %q: %v", rawPath, err)
	}
	return u.Query()
}
