package agentloop

import "testing"

func TestParseDecision_PureJSON(t *testing.T) {
	d, err := ParseDecision(`{"choice": 2, "reasoning": "STAB hits hard"}`, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Choice != 2 || d.Reasoning != "STAB hits hard" {
		t.Errorf("got %+v", d)
	}
}

func TestParseDecision_WrappedInProse(t *testing.T) {
	// Models sometimes ignore the "no prose" instruction.
	raw := `Let me think about this...
{"choice": 0, "reasoning": "best matchup"}
That's my answer.`
	d, err := ParseDecision(raw, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Choice != 0 {
		t.Errorf("got choice %d, want 0", d.Choice)
	}
}

func TestParseDecision_CodeFences(t *testing.T) {
	raw := "```json\n{\"choice\": 1, \"reasoning\": \"r\"}\n```"
	d, err := ParseDecision(raw, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Choice != 1 {
		t.Errorf("got %+v", d)
	}
}

func TestParseDecision_OutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name      string
		choice    int
		numLegal  int
		wantError bool
	}{
		{"negative", -1, 4, true},
		{"equal to len", 4, 4, true},
		{"past end", 7, 4, true},
		{"zero with one legal", 0, 1, false},
		{"last valid", 3, 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDecision(`{"choice":`+itoa(tc.choice)+`}`, tc.numLegal)
			if (err != nil) != tc.wantError {
				t.Errorf("got err=%v, wantError=%v", err, tc.wantError)
			}
		})
	}
}

func TestParseDecision_NoJSON(t *testing.T) {
	if _, err := ParseDecision("I refuse to play", 4); err == nil {
		t.Error("expected error for no-JSON reply, got nil")
	}
}

func TestParseDecision_MalformedJSON(t *testing.T) {
	if _, err := ParseDecision(`{"choice": "not-a-number"}`, 4); err == nil {
		t.Error("expected error for non-int choice, got nil")
	}
}

func TestParseDecision_NoLegalActions(t *testing.T) {
	if _, err := ParseDecision(`{"choice": 0}`, 0); err == nil {
		t.Error("expected error when numLegal=0, got nil")
	}
}

// itoa is tiny strconv to avoid importing for one test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
