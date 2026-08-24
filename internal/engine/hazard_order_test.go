package engine

import (
	"strings"
	"testing"
)

// hazard_order_test.go pins the two ordering rules the Showdown port caught,
// both of which are about *when* an effect fires rather than about what it does.

// TestHazardsFireInTheOrderTheyWereLaid. The engine ran a fixed Stealth Rock →
// Spikes → Toxic Spikes and called that canon's order. Canon has no fixed
// order: the hazard conditions declare neither an order nor a priority, so the
// only thing separating them in the switch-in sort is the creation stamp, and
// that means laying order decides.
//
// It is observable because the sequence stops when the arrival faints. A body
// the rocks will kill, walking onto Toxic Spikes laid first, gets poisoned
// before it dies; the same body under a rocks-first order dies with the poison
// never applied. That difference is the whole upstream fixture — it is what
// gives a Lum Berry something to cure.
func TestHazardsFireInTheOrderTheyWereLaid(t *testing.T) {
	d := loadDex(t)
	const (
		snorlax = 143 // the setter, and the body that walks in
		muk     = 89  // Poison-type: absorbs Toxic Spikes, and observably so
	)
	// laid runs the two setters in the given order against side 0, then walks
	// a Poison-type in. The absorb is the read-out: it clears the layers and
	// says so, and it only happens if Toxic Spikes actually ran.
	laid := func(first, second HazardKind) []LogLine {
		s, err := NewBattle(d, "b", "In", []int{snorlax, muk}, "Setter", []int{snorlax}, 1)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		var log []LogLine
		applyHazardSetter(s, 1, first, &log)
		applyHazardSetter(s, 1, second, &log)
		log = nil
		doSwitch(s, 0, 1, NewRNG(1), &log)
		return log
	}

	rocksFirst := laid(HazardStealthRock, HazardToxicSpikes)
	spikesFirst := laid(HazardToxicSpikes, HazardStealthRock)

	idx := func(log []LogLine, want string) int {
		for i, l := range log {
			if strings.Contains(l.Text, want) {
				return i
			}
		}
		return -1
	}
	if r, a := idx(rocksFirst, "Pointed stones dug into"), idx(rocksFirst, "absorbed the Toxic Spikes"); r < 0 || a < 0 || r > a {
		t.Errorf("rocks laid first should chip before the absorb; rocks=%d absorb=%d in %v",
			r, a, logTexts(rocksFirst))
	}
	if r, a := idx(spikesFirst, "Pointed stones dug into"), idx(spikesFirst, "absorbed the Toxic Spikes"); r < 0 || a < 0 || a > r {
		t.Errorf("Toxic Spikes laid first should absorb before the rocks chip; rocks=%d absorb=%d in %v",
			r, a, logTexts(spikesFirst))
	}
}

// TestHazardsWithNoLayingRecordKeepTheHistoricalOrder. Every state saved before
// the stamps existed, and every test that assigns the layers straight into the
// struct, arrives with all three orders at zero. Those must not become
// order-dependent on the sort's internals — the stable sort over a fixed source
// list is what guarantees they keep the old Stealth Rock → Spikes → Toxic
// Spikes sequence.
func TestHazardsWithNoLayingRecordKeepTheHistoricalOrder(t *testing.T) {
	h := &Hazards{StealthRock: true, Spikes: 1, ToxicSpikes: 1}
	got := orderedHazards(h)
	want := []HazardKind{HazardStealthRock, HazardSpikes, HazardToxicSpikes}
	if len(got) != len(want) {
		t.Fatalf("wanted all three hazards, got %v", got)
	}
	for i := range want {
		if got[i].kind != want[i] {
			t.Errorf("unstamped hazard %d = %q, want %q", i, got[i].kind, want[i])
		}
	}
}

// TestExtraSpikesLayersDoNotResetTheLayingOrder. Canon stamps the effect state
// on the absent→present transition only; a second layer goes through
// onSideRestart, which does not re-init it. So stacking Spikes must not move
// them behind a hazard that was laid later.
func TestExtraSpikesLayersDoNotResetTheLayingOrder(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "In", []int{143}, "Setter", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	var log []LogLine
	applyHazardSetter(s, 1, HazardSpikes, &log)
	applyHazardSetter(s, 1, HazardStealthRock, &log)
	applyHazardSetter(s, 1, HazardSpikes, &log) // second layer, later in the battle

	h := &s.Sides[0].Conditions.Hazards
	if h.Spikes != 2 {
		t.Fatalf("setup: wanted two Spikes layers, got %d", h.Spikes)
	}
	if h.SpikesOrder >= h.StealthRockOrder {
		t.Errorf("the extra layer should not restamp Spikes: spikes=%d rocks=%d",
			h.SpikesOrder, h.StealthRockOrder)
	}
	if got := orderedHazards(h); got[0].kind != HazardSpikes {
		t.Errorf("Spikes were laid first and should still run first, got %v", got)
	}
}

// TestKnockOffBeatsThePinchBerryItWouldOtherwiseFeed. The berry check ran
// inside the hit loop for both sides, so Knock Off's own damage pushed the
// holder into Sitrus range, the berry was eaten, and the removal then found an
// empty belt and said nothing — the holder healed a quarter and kept the use of
// its item off the one move whose entire purpose is to deny it. Canon runs the
// move's onAfterHit before the Update that eats pinch berries.
func TestKnockOffBeatsThePinchBerryItWouldOtherwiseFeed(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 3)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	atk := s.Active(0)
	atk.Moves = []MoveSlot{{MoveID: "knock-off", PP: 20, MaxPP: 20}}
	def := s.Active(1)
	def.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	def.Item = "sitrus-berry"
	// Just above half, so the move's own damage is what crosses the threshold.
	def.HP = def.MaxHP/2 + 1

	log := playTurn(d, s, 0, 0)
	if !logHas(log, "knocked off") {
		t.Errorf("Knock Off should have taken the berry, got %v", logTexts(log))
	}
	if logHas(log, "restored a little HP") {
		t.Errorf("the berry was removed and must not also have been eaten: %v", logTexts(log))
	}
	if def.Item != ItemNone {
		t.Errorf("the belt should be empty, got %q", def.Item)
	}
}

// TestPinchBerriesStillFireBetweenStrikes. The fix above defers one side's
// check for one family of moves; it must not move the check out of the hit loop
// wholesale. Upstream's eachEvent('Update') really is inside
// hitStepMoveHitLoop, so a multi-hit move that crosses the threshold on an
// early strike heals before the later ones land.
func TestPinchBerriesStillFireBetweenStrikes(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 3)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	atk := s.Active(0)
	atk.Moves = []MoveSlot{{MoveID: "double-slap", PP: 10, MaxPP: 10}}
	def := s.Active(1)
	def.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	def.Item = "sitrus-berry"
	def.HP = def.MaxHP/2 + 1

	log := playTurn(d, s, 0, 0)
	eat := -1
	lastHit := -1
	for i, l := range log {
		if strings.Contains(l.Text, "restored a little HP") {
			eat = i
		}
		if strings.Contains(l.Text, "damage") {
			lastHit = i
		}
	}
	if eat < 0 {
		t.Fatalf("the berry should have been eaten mid-move, got %v", logTexts(log))
	}
	if eat > lastHit {
		t.Errorf("the berry should fire between strikes, not after the move: eat=%d lastHit=%d in %v",
			eat, lastHit, logTexts(log))
	}
}
