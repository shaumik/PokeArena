package ai

import (
	"encoding/json"
	"testing"

	"pokearena/internal/engine"
)

// TestView_FoeItemNeverReachesWire locks the held item into the same
// hidden-information bucket as the foe's ability and exact stats. A held item
// is inferred from what activates, never announced up front: sending the slug
// would hand a client the foe's whole set for free (a Choice lock read on turn
// zero, a Focus Sash the attacker can play around, a Leftovers tick it can
// out-stall). In-process agents still see it on the View struct — the same
// documented asymmetry that already applies to ability and stats.
func TestView_FoeItemNeverReachesWire(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	foe := &s.Sides[1].Team[0]
	foe.Item = engine.ItemChoiceBand
	// A live Choice lock is the sharper leak: its mere presence names the item.
	foe.Volatiles.ChoiceLockMoveID = foe.Moves[0].MoveID

	v := MakeView(s, 0)

	// In-process agents keep the read (heuristic damage math consults it).
	if v.Foe.Item != engine.ItemChoiceBand {
		t.Errorf("in-process View lost the foe item: got %q, want %q", v.Foe.Item, engine.ItemChoiceBand)
	}

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var wire struct {
		Foe map[string]json.RawMessage `json:"foe"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	if _, ok := wire.Foe["item"]; ok {
		t.Errorf("foe leaked its held item on the wire: %s", raw)
	}
	if _, ok := wire.Foe["choice_lock_move_id"]; ok {
		t.Errorf("foe leaked its choice lock on the wire (names the item): %s", raw)
	}

	// Redaction must not mutate the battle state it projects from.
	if foe.Item != engine.ItemChoiceBand || foe.Volatiles.ChoiceLockMoveID == "" {
		t.Errorf("marshaling the view mutated the source state: item=%q lock=%q",
			foe.Item, foe.Volatiles.ChoiceLockMoveID)
	}
}

// TestView_SelfItemReachesWire is the other half of the contract: your own
// side is unredacted, so a client can render what its own Pokémon are holding.
// Without this, hiding the foe's item could be "fixed" by dropping the field
// from Pokemon entirely and nobody would notice until the builder went blank.
func TestView_SelfItemReachesWire(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	s.Sides[0].Team[0].Item = engine.ItemLeftovers

	raw, err := json.Marshal(MakeView(s, 0))
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var wire struct {
		Self struct {
			Team []struct {
				Item string `json:"item"`
			} `json:"team"`
		} `json:"self"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	if len(wire.Self.Team) == 0 {
		t.Fatalf("self side serialized with no team: %s", raw)
	}
	if got := wire.Self.Team[0].Item; got != string(engine.ItemLeftovers) {
		t.Errorf("own item missing from the wire: got %q, want %q", got, engine.ItemLeftovers)
	}
}

// TestView_FoeVolatilesNameNoItem is the generalization of the item-leak test:
// any volatile that exists *only because* of a held item names that item as
// surely as the slug does, and the two that do (the Choice lock and the
// Metronome streak) were added a batch apart — the second one leaked because
// the first test enumerated field names rather than the rule.
//
// This asserts the rule: no item-derived volatile reaches the wire.
func TestView_FoeVolatilesNameNoItem(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	foe := &s.Sides[1].Team[0]
	foe.Item = engine.ItemMetronome
	foe.Volatiles.ChoiceLockMoveID = foe.Moves[0].MoveID
	foe.Volatiles.MetronomeMoveID = foe.Moves[0].MoveID
	foe.Volatiles.MetronomeCount = 3

	raw, err := json.Marshal(MakeView(s, 0))
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var wire struct {
		Foe struct {
			Volatiles map[string]json.RawMessage `json:"volatiles"`
		} `json:"foe"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	// Volatiles that only ever appear because the holder carries a specific
	// item. Add to this list when you add such a volatile — and clear it in
	// marshalFoe at the same time.
	for _, key := range []string{"choice_lock_move_id", "metronome_move_id", "metronome_count"} {
		if _, leaked := wire.Foe.Volatiles[key]; leaked {
			t.Errorf("foe volatile %q reached the wire; it names the held item: %s", key, raw)
		}
	}
	// The holder's own side keeps them — it needs to render its own state.
	if s.Sides[1].Team[0].Volatiles.MetronomeCount != 3 {
		t.Errorf("marshaling the view mutated the source state")
	}
}
