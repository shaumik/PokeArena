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
	foe.Volatiles.MicleTurns = 2
	foe.Volatiles.Unburden = true

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
	// An allowlist, not a denylist. Enumerating the *forbidden* keys is how
	// metronome_move_id shipped: the previous version of this test listed the
	// one volatile that was known to leak, so the next one added sailed past
	// it. Anything not named here fails, which forces a decision when a new
	// volatile appears — and the decision for anything item- or
	// ability-derived is to clear it in marshalFoe.
	//
	// Everything on this list is publicly announced in a real battle and shows
	// on Showdown's UI: the foe used Confuse Ray, put up a Substitute, is
	// visibly charging, is behind a Protect.
	allowed := map[string]bool{
		"confusion": true, "flinch": true, "charging": true, "locked_move": true,
		"must_recharge": true, "partial_trap": true, "substitute": true,
		"protect": true, "endure": true, "protect_counter": true, "roost": true,
		"flash_fire_charged": true, "leech_seed": true, "aqua_ring": true,
		"ingrain": true, "disable": true, "encore": true, "taunt": true,
		"torment": true, "imprison": true, "embargo": true,
		"last_move_id": true, "last_move_name": true,
		"focus_energy": true, "laser_focus": true, "charge": true,
		"defense_curl": true, "minimize": true, "foresight": true, "miracle_eye": true,
		"attract": true, "yawn": true, "nightmare": true, "curse": true,
		"destiny_bond": true, "magnet_rise": true, "telekinesis": true,
		"smack_down": true, "snatch": true, "magic_coat": true, "stockpile": true,
		"grudge": true, "gastro_acid": true,
		"moved_last": true, "moved_this_turn": true, "damaged_this_turn": true,
		"custap_boost": true,
	}
	for key := range wire.Foe.Volatiles {
		if !allowed[key] {
			t.Errorf("foe volatile %q reached the wire and is not on the public allowlist. "+
				"If it exists only because of a held item or an ability, clear it in "+
				"marshalFoe; if it is genuinely public, add it here. Payload: %s", key, raw)
		}
	}
	// The holder's own side keeps them — it needs to render its own state.
	if s.Sides[1].Team[0].Volatiles.MetronomeCount != 3 {
		t.Errorf("marshaling the view mutated the source state")
	}
}
