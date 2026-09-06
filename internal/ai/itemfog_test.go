package ai

import (
	"encoding/json"
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
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
	// Revealed, so the in-process half of the contract is exercised. Fog is
	// TestView_FoeItemHiddenUntilRevealed's job.
	foe.ItemRevealed = true

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
		// The reactive register and Bide's store are both records of hits that
		// have already landed in front of both players, and Bide announces
		// itself when it opens. Nothing here is inferred from a held item or an
		// ability, so there is nothing to hide.
		"reactive_physical": true, "reactive_special": true,
		"took_physical_hit": true, "took_special_hit": true,
		"bide": true,
		// Lock-On announces itself when it lands and the aim is the point of
		// the move: a player who was aimed at knows it.
		"lock_on": true,
		// Magic Room is field state, announced when it goes up and visible to
		// both players. The per-Pokémon flag is a mirror of it (see
		// Volatiles.MagicRoomHere), so it reveals nothing the foe cannot see.
		"magic_room_here": true,
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

// TestView_FoeTopLevelKeysAreAllowlisted is the sibling of the volatiles
// allowlist above, and exists because that one had a blind spot: it enumerates
// keys inside `foe.volatiles` only, so a new *top-level* field on
// engine.Pokemon reaches the wire untouched. That is how last_consumed_item
// shipped — a field naming an item the foe used to hold, which is the same
// hidden information as the slot itself.
//
// foeWire shadows the hidden fields by redeclaring them as pointers that stay
// nil. Every shadow is a decision someone made; this test forces the next
// person to make one too.
func TestView_FoeTopLevelKeysAreAllowlisted(t *testing.T) {
	d := loadDex(t)
	s, err := engine.NewBattle(d, "b", "P0", []int{143}, "P1", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	// Fill in everything a real mid-battle foe would carry, so nothing is
	// omitted by omitempty and slips past.
	foe := s.Active(1)
	foe.Item = engine.ItemLeftovers
	foe.LastConsumedItem = engine.ItemSitrusBerry
	foe.HP = foe.MaxHP / 2
	foe.Status = engine.StatusPoison
	foe.ToxicCounter = 3
	foe.SleepTurns = 1
	foe.Stages.Atk = 2
	// A distinctive spread: evs/ivs/nature are the inputs the foe's exact
	// stats are derived from, so leaking them undoes the stats redaction one
	// step upstream. Set to non-zero values so no omitempty hides the leak.
	foe.Nature = "adamant"
	foe.EVs = domain.Stats{HP: 252, Atk: 252, Spe: 4}
	foe.IVs = domain.Uniform(31)

	raw, err := MakeView(s, 0).MarshalJSON()
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var wire struct {
		Foe map[string]json.RawMessage `json:"foe"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}

	// Public: what a Showdown spectator sees on the foe's side of the field.
	allowed := map[string]bool{
		"dex_no": true, "name": true, "type1": true, "type2": true,
		"status": true, "sleep_turns": true, "toxic_counter": true,
		"stages": true, "volatiles": true, "fainted": true,
		"moves": true, "hp_pct": true,
		// Gender is public in canon — the battle UI shows it, and both sides
		// have to know it to reason about Attract, Cute Charm and Rivalry.
		// Unlike the spread it reveals nothing about the foe's stats.
		"gender": true,
	}
	for key := range wire.Foe {
		if !allowed[key] {
			t.Errorf("foe field %q reached the wire and is not on the public allowlist. "+
				"If it names the foe's item, ability or exact stats, shadow it in foeWire "+
				"as a nil pointer; if it is genuinely public, add it here. Payload: %s", key, raw)
		}
	}
	// The ones that matter most, asserted by name so the failure is
	// unmissable. evs/ivs/nature are on this list for the same reason stats
	// is: any two of them reconstruct the third, and together they hand over
	// the foe's exact Speed and both attacking stats.
	for _, hidden := range []string{
		"item", "last_consumed_item", "ability", "stats", "hp", "max_hp",
		"evs", "ivs", "nature",
	} {
		if _, leaked := wire.Foe[hidden]; leaked {
			t.Errorf("foe %q is hidden information and reached the wire", hidden)
		}
	}
}

// TestView_FoeFogRevealsOnTrigger is the end-to-end half of the OPEN-2
// decision: nothing is public up front, and an item becomes public the moment
// it visibly fires in a real battle — not before, and without dragging the
// ability along with it.
//
// Leftovers is the cleanest probe: it announces every end-of-turn tick on a
// damaged holder, needs no roll, and leaves the holder's ability untouched.
func TestView_FoeFogRevealsOnTrigger(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)

	// Both sides idle so the only thing that happens all turn is the tick.
	idle := []engine.MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Sides[0].Team[0].Moves = idle
	foe := &s.Sides[1].Team[0]
	foe.Moves = idle
	foe.Item = engine.ItemLeftovers
	// Thick Fat is the control: a silent damage modifier that announces
	// nothing all turn, so it must still read as unknown afterwards. An
	// ability that does announce (Intimidate fires on entry) would be revealed
	// correctly and prove nothing about field independence.
	foe.Ability = engine.AbilityThickFat
	foe.HP = foe.MaxHP / 2 // Leftovers only announces when it actually heals

	v := MakeView(s, 0)
	if v.Foe.Item != engine.ItemNone || v.Foe.Ability != engine.AbilityNone {
		t.Fatalf("turn 0 view is not foggy: item=%q ability=%q", v.Foe.Item, v.Foe.Ability)
	}

	log := engine.ResolveTurn(d, s, [2]engine.Action{
		{Kind: engine.ActionMove, Index: 0},
		{Kind: engine.ActionMove, Index: 0},
	})

	if !foe.ItemRevealed {
		t.Fatalf("Leftovers ticked but never set ItemRevealed; log: %v", texts(log))
	}
	v = MakeView(s, 0)
	if v.Foe.Item != engine.ItemLeftovers {
		t.Errorf("item stayed hidden after it fired: got %q, want %q", v.Foe.Item, engine.ItemLeftovers)
	}
	if v.Foe.Ability != engine.AbilityNone {
		t.Errorf("an item firing revealed the ability too: got %q", v.Foe.Ability)
	}
}

func texts(log []engine.LogLine) []string {
	out := make([]string, 0, len(log))
	for _, l := range log {
		out = append(out, l.Text)
	}
	return out
}
