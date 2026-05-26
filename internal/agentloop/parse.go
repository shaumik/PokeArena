package agentloop

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Decision is the structured form of the LLM's per-turn reply.
type Decision struct {
	Choice    int    `json:"choice"`
	Reasoning string `json:"reasoning"`
}

// ParseDecision extracts the {choice, reasoning} JSON object from the
// model's raw reply. The prompt asks for *only* the object, but models
// sometimes ignore that — so we scan for the first '{' and the last '}'
// and parse the slice between them. The choice is validated against
// numLegal so callers can safely index into their actions slice.
func ParseDecision(text string, numLegal int) (Decision, error) {
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end <= start {
		return Decision{}, fmt.Errorf("no JSON object in reply: %s", trimForLog(text))
	}
	var d Decision
	if err := json.Unmarshal([]byte(text[start:end+1]), &d); err != nil {
		return Decision{}, fmt.Errorf("malformed JSON: %w (raw: %s)", err, trimForLog(text))
	}
	if numLegal <= 0 {
		return Decision{}, fmt.Errorf("no legal actions available")
	}
	if d.Choice < 0 || d.Choice >= numLegal {
		return Decision{}, fmt.Errorf("choice %d out of range [0,%d)", d.Choice, numLegal)
	}
	return d, nil
}

// trimForLog flattens whitespace and caps length so a malformed reply can
// be logged without flooding the terminal.
func trimForLog(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 200
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
