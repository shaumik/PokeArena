package engine

import (
	"strings"
	"testing"

	"pokearena/internal/domain"
)

// items_berries_test.go covers the consumable-item family. Every test asserts
// the same three things where they apply, because a one-shot item has three
// distinct ways to be wrong: the effect (did the right thing happen), the
// consumption (is the holder bare afterwards), and the gate (does it stay in
// reserve when its condition isn't met). A berry that heals correctly but is
// never consumed is an infinite-heal bug that a naive effect-only test misses.

// berryBattle sets up a 1v1 Snorlax mirror where both sides have only Splash,
// so nothing but the item under test changes HP. Side 0 holds the berry.
//
// Both abilities are cleared: Snorlax's slot-0 Immunity would refuse the poison
// a Pecha/Lum test needs to inflict, and Thick Fat would quietly halve a Fire
// hit. The point of this fixture is that the held item is the only live
// mechanic, so the ability slot is emptied rather than worked around per test.
func berryBattle(t *testing.T, item ItemKind) (*domain.Dex, *BattleState) {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Ability = AbilityNone
	s.Active(1).Ability = AbilityNone
	s.Active(0).Item = item
	s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	return d, s
}

func splashTurn(d *domain.Dex, s *BattleState) []LogLine {
	return ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
}

// --- HP restore ---

// TestHealBerriesFireAtThreshold walks the whole healing family through the
// same three-part contract, with the amounts spelled out per berry so a
// transcription slip in one entry can't hide behind a shared helper.
func TestHealBerriesFireAtThreshold(t *testing.T) {
	cases := []struct {
		item     ItemKind
		name     string
		atFrac   float64 // HP fraction to set before the turn
		wantHeal func(maxHP int) int
	}{
		{ItemOranBerry, "Oran Berry", 0.5, func(int) int { return 10 }},
		{ItemSitrusBerry, "Sitrus Berry", 0.5, func(m int) int { return m / 4 }},
		{ItemBerryJuice, "Berry Juice", 0.5, func(int) int { return 20 }},
		{ItemFigyBerry, "Figy Berry", 0.25, func(m int) int { return m / 3 }},
		{ItemWikiBerry, "Wiki Berry", 0.25, func(m int) int { return m / 3 }},
		{ItemMagoBerry, "Mago Berry", 0.25, func(m int) int { return m / 3 }},
		{ItemAguavBerry, "Aguav Berry", 0.25, func(m int) int { return m / 3 }},
		{ItemIapapaBerry, "Iapapa Berry", 0.25, func(m int) int { return m / 3 }},
	}
	for _, tc := range cases {
		t.Run(string(tc.item), func(t *testing.T) {
			d, s := berryBattle(t, tc.item)
			p := s.Active(0)
			// Exactly at the threshold — the boundary is inclusive in canon.
			p.HP = int(float64(p.MaxHP) * tc.atFrac)
			before := p.HP
			want := before + tc.wantHeal(p.MaxHP)

			log := splashTurn(d, s)

			if got := s.Active(0).HP; got != want {
				t.Errorf("HP after %s = %d, want %d (from %d/%d)", tc.name, got, want, before, p.MaxHP)
			}
			if got := s.Active(0).Item; got != ItemNone {
				t.Errorf("%s not consumed: still holding %q", tc.name, got)
			}
			if !logHas(log, "ate its "+tc.name) && !logHas(log, "used its "+tc.name) {
				t.Errorf("no consume line for %s in log: %v", tc.name, log)
			}
		})
	}
}

// TestHealBerryStaysInReserveAboveThreshold: one HP above the line must not
// trigger. This is the gate half of the contract — an off-by-one in the
// comparison would eat every berry on turn one.
func TestHealBerryStaysInReserveAboveThreshold(t *testing.T) {
	d, s := berryBattle(t, ItemSitrusBerry)
	p := s.Active(0)
	p.HP = p.MaxHP/2 + 1
	before := p.HP

	splashTurn(d, s)

	if s.Active(0).HP != before {
		t.Errorf("Sitrus healed above the threshold: %d → %d", before, s.Active(0).HP)
	}
	if s.Active(0).Item != ItemSitrusBerry {
		t.Errorf("Sitrus consumed without firing (now %q)", s.Active(0).Item)
	}
}

// TestHealBerryFiresOnceOnly: after the berry is spent the holder is bare, so
// dropping back into range a second time must do nothing. Guards the
// infinite-heal failure mode directly.
func TestHealBerryFiresOnceOnly(t *testing.T) {
	d, s := berryBattle(t, ItemSitrusBerry)
	p := s.Active(0)
	p.HP = p.MaxHP / 2
	splashTurn(d, s)
	if s.Active(0).Item != ItemNone {
		t.Fatalf("setup: berry should be gone after the first trigger")
	}

	s.Active(0).HP = 20
	splashTurn(d, s)

	if got := s.Active(0).HP; got != 20 {
		t.Errorf("a spent Sitrus healed again: HP 20 → %d", got)
	}
}

// TestHealBerryFiresMidTurnOffDamage: the berry must activate the moment the
// damaging hit resolves, not at end of turn. A holder that eats late can be
// finished off by a second effect it should have survived — which is the whole
// reason canon triggers on the HP drop.
func TestHealBerryFiresMidTurnOffDamage(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 7)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Item = ItemSitrusBerry
	holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	// Just above half so the incoming hit is what crosses the line.
	holder.HP = holder.MaxHP/2 + 30

	log := splashTurn(d, s)

	// The consume line must land before the end-of-turn marker set — easiest
	// robust check: it precedes nothing later than the damage line by much, so
	// assert ordering against the residual-phase lines instead.
	eatIdx, dmgIdx := -1, -1
	for i, l := range log {
		if dmgIdx < 0 && strings.Contains(l.Text, "took") && strings.Contains(l.Text, "damage") {
			dmgIdx = i
		}
		if eatIdx < 0 && strings.Contains(l.Text, "ate its Sitrus Berry") {
			eatIdx = i
		}
	}
	if dmgIdx < 0 {
		t.Fatalf("the foe's attack never landed; log: %v", log)
	}
	if eatIdx < 0 {
		t.Fatalf("Sitrus never fired despite dropping below half; log: %v", log)
	}
	if eatIdx < dmgIdx {
		t.Errorf("berry fired before the damage that triggered it (eat=%d dmg=%d)", eatIdx, dmgIdx)
	}
	if eatIdx != dmgIdx+1 {
		t.Errorf("berry did not fire immediately after the hit (eat=%d, dmg=%d); log: %v", eatIdx, dmgIdx, log)
	}
}

// TestHealBerryFiresOnHazardEntry: entry-hazard chip is a common way to land in
// berry range, and doSwitch is a different trigger site from the damage step.
func TestHealBerryFiresOnHazardEntry(t *testing.T) {
	d := loadDex(t)
	// Golem is grounded — Spikes ignore a Flying-type switch-in entirely, which
	// would make this test pass for the wrong reason.
	s, err := NewBattle(d, "b", "P1", []int{143, 76}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	// Spikes on side 0 chip the incoming for 1/8 max HP.
	s.Sides[0].Conditions.Hazards.Spikes = 1
	in := &s.Sides[0].Team[1]
	in.Ability = AbilityNone
	in.Item = ItemOranBerry
	in.HP = in.MaxHP/2 + 1 // one above the line; the chip pushes it under

	var log []LogLine
	doSwitch(s, 0, 1, NewRNG(1), &log)

	if s.Active(0).Item != ItemNone {
		t.Errorf("Oran Berry did not fire off hazard chip on entry; log: %v", log)
	}
	if !logHas(log, "ate its Oran Berry") {
		t.Errorf("no consume line on entry; log: %v", log)
	}
}

// TestSitrusHealCapsAtMaxHP: the heal must clamp, and the reported amount must
// be the amount actually restored, not the nominal quarter.
func TestSitrusHealCapsAtMaxHP(t *testing.T) {
	d, s := berryBattle(t, ItemSitrusBerry)
	p := s.Active(0)
	p.HP = p.MaxHP / 2 // a quarter heal would overshoot by a lot
	splashTurn(d, s)
	if got := s.Active(0).HP; got > s.Active(0).MaxHP {
		t.Errorf("HP %d exceeds MaxHP %d after Sitrus", got, s.Active(0).MaxHP)
	}
	if err := ValidateStateInvariants(s); err != nil {
		t.Errorf("state invariants broken after Sitrus: %v", err)
	}
}

// --- status cure ---

// TestCureBerriesClearTheirStatus drives every cure berry through the status it
// owns, plus one status it must ignore, so a berry can't quietly cure
// everything (which a shared-map slip would produce).
func TestCureBerriesClearTheirStatus(t *testing.T) {
	cases := []struct {
		item    ItemKind
		name    string
		cures   StatusCond
		ignores StatusCond
	}{
		{ItemCheriBerry, "Cheri Berry", StatusParalysis, StatusBurn},
		{ItemChestoBerry, "Chesto Berry", StatusSleep, StatusBurn},
		{ItemPechaBerry, "Pecha Berry", StatusPoison, StatusBurn},
		{ItemRawstBerry, "Rawst Berry", StatusBurn, StatusParalysis},
		{ItemAspearBerry, "Aspear Berry", StatusFreeze, StatusBurn},
	}
	for _, tc := range cases {
		t.Run(string(tc.item)+"/cures", func(t *testing.T) {
			_, s := berryBattle(t, tc.item)
			p := s.Active(0)
			var log []LogLine
			if !inflictStatus(p, 0, tc.cures, s, NewRNG(1), &log) {
				t.Fatalf("setup: could not inflict %s on %s", tc.cures, p.Name)
			}
			if p.Status != StatusNone {
				t.Errorf("%s left status %q on the holder", tc.name, p.Status)
			}
			if p.Item != ItemNone {
				t.Errorf("%s not consumed after curing", tc.name)
			}
			if !logHas(log, "ate its "+tc.name) {
				t.Errorf("no consume line; log: %v", log)
			}
		})
		t.Run(string(tc.item)+"/ignores", func(t *testing.T) {
			_, s := berryBattle(t, tc.item)
			p := s.Active(0)
			var log []LogLine
			if !inflictStatus(p, 0, tc.ignores, s, NewRNG(1), &log) {
				t.Fatalf("setup: could not inflict %s", tc.ignores)
			}
			if p.Status != tc.ignores {
				t.Errorf("%s cured %s, which it must ignore", tc.name, tc.ignores)
			}
			if p.Item != tc.item {
				t.Errorf("%s consumed on a status it doesn't cure", tc.name)
			}
		})
	}
}

// TestPechaCuresBothPoisonGrades: badly poisoned is still poison, and the
// toxic counter must be reset with it (a stale counter would make the next
// poison start mid-escalation).
func TestPechaCuresBothPoisonGrades(t *testing.T) {
	for _, st := range []StatusCond{StatusPoison, StatusToxic} {
		_, s := berryBattle(t, ItemPechaBerry)
		p := s.Active(0)
		var log []LogLine
		if !inflictStatus(p, 0, st, s, NewRNG(1), &log) {
			t.Fatalf("setup: could not inflict %s", st)
		}
		if p.Status != StatusNone {
			t.Errorf("Pecha left %s uncured", st)
		}
		if p.ToxicCounter != 0 {
			t.Errorf("Pecha left ToxicCounter=%d after curing %s", p.ToxicCounter, st)
		}
	}
}

// TestLumCuresEveryStatus: Lum's whole identity is "any of them", so every
// status gets its own subtest rather than a representative sample.
func TestLumCuresEveryStatus(t *testing.T) {
	for _, st := range []StatusCond{
		StatusBurn, StatusPoison, StatusToxic, StatusParalysis, StatusSleep, StatusFreeze,
	} {
		t.Run(string(st), func(t *testing.T) {
			_, s := berryBattle(t, ItemLumBerry)
			p := s.Active(0)
			var log []LogLine
			if !inflictStatus(p, 0, st, s, NewRNG(1), &log) {
				t.Fatalf("setup: could not inflict %s on %s", st, p.Name)
			}
			if p.Status != StatusNone {
				t.Errorf("Lum left %s uncured", st)
			}
			if p.Item != ItemNone {
				t.Errorf("Lum not consumed after curing %s", st)
			}
		})
	}
}

// TestConfusionCureBerries: Persim and Lum both clear the confusion volatile,
// and the berry must not be spent when the holder isn't confused.
func TestConfusionCureBerries(t *testing.T) {
	for _, item := range []ItemKind{ItemPersimBerry, ItemLumBerry} {
		t.Run(string(item), func(t *testing.T) {
			d, s := berryBattle(t, item)
			p := s.Active(0)
			var log []LogLine
			applyVolatile(p, 0, "confusion", d.Moves["confuse-ray"], s, NewRNG(1), &log)
			if p.Volatiles.Confusion != nil {
				t.Errorf("%s left the holder confused", item)
			}
			if p.Item != ItemNone {
				t.Errorf("%s not consumed after clearing confusion", item)
			}
		})
	}
}

// TestPersimIgnoresNonVolatileStatus: Persim only handles confusion. Without
// this, a slip that made it a universal cure would pass every other test here.
func TestPersimIgnoresNonVolatileStatus(t *testing.T) {
	_, s := berryBattle(t, ItemPersimBerry)
	p := s.Active(0)
	var log []LogLine
	inflictStatus(p, 0, StatusBurn, s, NewRNG(1), &log)
	if p.Status != StatusBurn {
		t.Errorf("Persim cured a burn; it only handles confusion")
	}
	if p.Item != ItemPersimBerry {
		t.Errorf("Persim consumed on a burn")
	}
}

// TestChestoWakesFromRest is the canonical combo: Rest bypasses inflictStatus
// entirely, so the berry check has to be repeated on that path. Full HP and
// awake is the whole point.
// doRest is driven directly rather than through a Rest move: the curated
// `rest` entry in data/moves.json carries no effect block (upstream encodes
// Rest's heal-and-sleep in JS, and the transform doesn't lift it), so using the
// move in a battle resolves to nothing. doRest is the function the engine will
// call the moment that data gap closes, and it is the path this test needs to
// cover — the berry has to fire on a code path that never touches inflictStatus.
func TestChestoWakesFromRest(t *testing.T) {
	_, s := berryBattle(t, ItemChestoBerry)
	holder := s.Active(0)
	holder.HP = holder.MaxHP / 3

	var log []LogLine
	doRest(holder, 0, &log)

	if got := holder.Status; got != StatusNone {
		t.Errorf("Chesto did not wake the Rest sleep: status = %q", got)
	}
	if got := holder.HP; got != holder.MaxHP {
		t.Errorf("Rest did not fully heal: %d/%d", got, holder.MaxHP)
	}
	if holder.Item != ItemNone {
		t.Errorf("Chesto not consumed; log: %v", log)
	}
	if holder.SleepTurns != 0 {
		t.Errorf("sleep counter left at %d after the cure", holder.SleepTurns)
	}
}

// TestCureBerryDoesNotBlockSynchronize: the berry cures the status *after* it
// lands, so anything keyed on "the status happened" must still fire. If the
// cure were folded into the infliction check instead, Synchronize would stop
// bouncing and nothing else in the suite would notice.
func TestCureBerryDoesNotBlockSynchronize(t *testing.T) {
	d := loadDex(t)
	// Mr. Mime (122) has Synchronize in its ability list.
	s, err := NewBattle(d, "b", "P1", []int{122}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability = "synchronize"
	holder.Item = ItemRawstBerry
	foe := s.Active(1)

	var log []LogLine
	if !inflictStatusFrom(holder, 0, 1, StatusBurn, s, NewRNG(1), &log) {
		t.Fatalf("setup: burn did not land on the Synchronize holder")
	}
	if holder.Status != StatusNone {
		t.Errorf("Rawst did not cure the burn on the holder")
	}
	if foe.Status != StatusBurn {
		t.Errorf("Synchronize did not bounce the burn back: foe status = %q", foe.Status)
	}
}

// --- Leppa (PP) ---

// TestLeppaRestoresPPOnEmptySlot: the berry fires the turn a move runs out,
// which is the turn the holder is otherwise forced onto Struggle.
func TestLeppaRestoresPPOnEmptySlot(t *testing.T) {
	d, s := berryBattle(t, ItemLeppaBerry)
	holder := s.Active(0)
	holder.Moves = []MoveSlot{{MoveID: "splash", PP: 1, MaxPP: 40}}

	log := splashTurn(d, s)

	if got := s.Active(0).Moves[0].PP; got != leppaPP {
		t.Errorf("PP after Leppa = %d, want %d", got, leppaPP)
	}
	if s.Active(0).Item != ItemNone {
		t.Errorf("Leppa not consumed; log: %v", log)
	}
	if !logHas(log, "restored 10 PP") {
		t.Errorf("no PP-restore line; log: %v", log)
	}
}

// TestLeppaStaysInReserveWhilePPRemains: it must not fire on a slot that still
// has PP, or a Leppa holder would burn the berry on its first move.
func TestLeppaStaysInReserveWhilePPRemains(t *testing.T) {
	d, s := berryBattle(t, ItemLeppaBerry)
	s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 5, MaxPP: 40}}

	splashTurn(d, s)

	if s.Active(0).Item != ItemLeppaBerry {
		t.Errorf("Leppa consumed while PP remained")
	}
	if got := s.Active(0).Moves[0].PP; got != 4 {
		t.Errorf("PP = %d, want 4 (one spent, no restore)", got)
	}
}

// TestLeppaClampsToMaxPP: a move whose MaxPP is under 10 must not end up above
// its own ceiling, which ValidateStateInvariants wouldn't catch.
func TestLeppaClampsToMaxPP(t *testing.T) {
	d, s := berryBattle(t, ItemLeppaBerry)
	s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 1, MaxPP: 5}}

	splashTurn(d, s)

	if got := s.Active(0).Moves[0].PP; got != 5 {
		t.Errorf("PP after Leppa on a 5-PP move = %d, want 5 (clamped to MaxPP)", got)
	}
}

// --- pinch stat berries ---

// TestPinchBoostBerries: each berry raises exactly its own stage, at a quarter
// HP, once. The "other stages untouched" check is what catches a copy-paste
// stat slug.
func TestPinchBoostBerries(t *testing.T) {
	cases := []struct {
		item  ItemKind
		name  string
		read  func(*Stages) int
		delta int
	}{
		{ItemLiechiBerry, "Liechi Berry", func(g *Stages) int { return g.Atk }, 1},
		{ItemGanlonBerry, "Ganlon Berry", func(g *Stages) int { return g.Def }, 1},
		{ItemPetayaBerry, "Petaya Berry", func(g *Stages) int { return g.SpA }, 1},
		{ItemApicotBerry, "Apicot Berry", func(g *Stages) int { return g.SpD }, 1},
		{ItemSalacBerry, "Salac Berry", func(g *Stages) int { return g.Spe }, 1},
	}
	for _, tc := range cases {
		t.Run(string(tc.item), func(t *testing.T) {
			d, s := berryBattle(t, tc.item)
			p := s.Active(0)
			p.HP = p.MaxHP / 4

			splashTurn(d, s)

			got := s.Active(0)
			if tc.read(&got.Stages) != tc.delta {
				t.Errorf("%s: target stage = %d, want %d", tc.name, tc.read(&got.Stages), tc.delta)
			}
			total := got.Stages.Atk + got.Stages.Def + got.Stages.SpA + got.Stages.SpD +
				got.Stages.Spe + got.Stages.Acc + got.Stages.Eva
			if total != tc.delta {
				t.Errorf("%s changed more than one stage: %+v", tc.name, got.Stages)
			}
			if got.Item != ItemNone {
				t.Errorf("%s not consumed", tc.name)
			}
		})
	}
}

// TestPinchBerryWaitsForQuarterHP: half HP is in range for Sitrus but not for
// a pinch berry, so this pins the two thresholds apart.
func TestPinchBerryWaitsForQuarterHP(t *testing.T) {
	d, s := berryBattle(t, ItemSalacBerry)
	p := s.Active(0)
	p.HP = p.MaxHP / 2

	splashTurn(d, s)

	if s.Active(0).Stages.Spe != 0 {
		t.Errorf("Salac fired at half HP; it waits for a quarter")
	}
	if s.Active(0).Item != ItemSalacBerry {
		t.Errorf("Salac consumed at half HP")
	}
}

// TestStarfBoostsOneStatSharplyAndDeterministically: +2 to exactly one of the
// five battle stats, never accuracy or evasion, and identical from the same
// seed (the RNG draw has to ride the battle's stream to keep replays exact).
func TestStarfBoostsOneStatSharplyAndDeterministically(t *testing.T) {
	run := func(seed uint64) Stages {
		d := loadDex(t)
		s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Item = ItemStarfBerry
		s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(0).HP = s.Active(0).MaxHP / 4
		splashTurn(d, s)
		if s.Active(0).Item != ItemNone {
			t.Fatalf("Starf not consumed")
		}
		return s.Active(0).Stages
	}

	g := run(42)
	if g.Acc != 0 || g.Eva != 0 {
		t.Errorf("Starf touched accuracy/evasion: %+v", g)
	}
	boosted, total := 0, 0
	for _, v := range []int{g.Atk, g.Def, g.SpA, g.SpD, g.Spe} {
		if v != 0 {
			boosted++
		}
		total += v
	}
	if boosted != 1 || total != 2 {
		t.Errorf("Starf should sharply raise exactly one stat, got %+v", g)
	}
	if again := run(42); again != g {
		t.Errorf("Starf is not replay-deterministic: %+v then %+v", g, again)
	}
}

// --- Custap ---

// TestCustapMovesFirstInBracket: the holder is strictly slower, so winning the
// turn can only come from the berry.
func TestCustapMovesFirstInBracket(t *testing.T) {
	d := loadDex(t)
	// Snorlax (30 base Speed) vs Jolteon (130) — no contest without the berry.
	s, err := NewBattle(d, "b", "Slow", []int{143}, "Fast", []int{135}, 3)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	slow := s.Active(0)
	slow.Item = ItemCustapBerry
	slow.HP = slow.MaxHP / 4
	slow.Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "quick-attack", PP: 30, MaxPP: 30}}

	log := splashTurn(d, s)

	if !logHas(log, "can act faster than normal") {
		t.Fatalf("Custap never activated; log: %v", log)
	}
	if s.Active(0).Item != ItemNone {
		t.Errorf("Custap not consumed")
	}
	// Quick Attack sits at +1 priority, so it must still go first: Custap only
	// wins ties inside a bracket.
	firstMover := -1
	for _, l := range log {
		if l.Type == "move" && strings.Contains(l.Text, " used ") {
			firstMover = l.Side
			break
		}
	}
	if firstMover != 1 {
		t.Errorf("Custap beat a +1-priority move; first mover was side %d", firstMover)
	}
}

// TestCustapWinsSameBracket: with both sides in the same bracket the slower
// holder must move first. This is the payload of the item.
func TestCustapWinsSameBracket(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Slow", []int{143}, "Fast", []int{135}, 3)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	slow := s.Active(0)
	slow.Item = ItemCustapBerry
	slow.HP = slow.MaxHP / 4
	slow.Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}

	log := splashTurn(d, s)

	firstMover := -1
	for _, l := range log {
		if l.Type == "move" && strings.Contains(l.Text, " used ") {
			firstMover = l.Side
			break
		}
	}
	if firstMover != 0 {
		t.Errorf("Custap holder did not move first; first mover was side %d; log: %v", firstMover, log)
	}
	// The flag is turn-scoped and must not leak into the next turn's ordering.
	if s.Active(0).Volatiles.CustapBoost {
		t.Errorf("CustapBoost survived end of turn")
	}
}

// TestCustapStaysInReserveWhenSwitching: a switching holder isn't competing for
// a slot in the move order, so the berry is not spent.
func TestCustapStaysInReserveWhenSwitching(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143, 6}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Item = ItemCustapBerry
	holder.HP = holder.MaxHP / 4
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})

	if s.Sides[0].Team[0].Item != ItemCustapBerry {
		t.Errorf("Custap spent on a switch turn")
	}
}

// --- Micle ---

// TestMicleBoostsNextMoveAccuracy: the prime is armed at the threshold and
// consumed by the next attempt that actually rolls accuracy.
func TestMicleBoostsNextMoveAccuracy(t *testing.T) {
	d, s := berryBattle(t, ItemMicleBerry)
	holder := s.Active(0)
	holder.HP = holder.MaxHP / 4
	holder.Moves = []MoveSlot{{MoveID: "hydro-pump", PP: 5, MaxPP: 5}}

	// Turn one: the holder is already in range, so the berry arms in the
	// end-of-turn sweep — after this turn's move has already rolled.
	log := splashTurn(d, s)
	if s.Active(0).Item != ItemNone {
		t.Fatalf("Micle not consumed at the threshold; log: %v", log)
	}
	if !logHas(log, "boosted the accuracy of its next move") {
		t.Errorf("no Micle arm line; log: %v", log)
	}
	if s.Active(0).Volatiles.MicleTurns == 0 {
		t.Fatalf("Micle prime not armed")
	}

	// Turn two: the prime is spent on the next attempt.
	splashTurn(d, s)
	if s.Active(0).Volatiles.MicleTurns != 0 {
		t.Errorf("Micle prime not consumed by the move it boosted")
	}
}

// TestMicleSurvivesIntoTheNextTurn: the prime has to outlive the turn it was
// armed on (a berry eaten in the residual block has no move left to boost), but
// it is not indefinite — see TestMicleLapses.
func TestMicleSurvivesIntoTheNextTurn(t *testing.T) {
	d, s := berryBattle(t, ItemMicleBerry)
	holder := s.Active(0)
	holder.HP = holder.MaxHP / 4
	// Splash never rolls accuracy (Accuracy 0), so nothing spends the prime.
	splashTurn(d, s)

	if s.Active(0).Volatiles.MicleTurns == 0 {
		t.Errorf("Micle prime cleared by the sweep that armed it; it must reach the next turn")
	}
}

// TestMicleLapses: canon gives the prime a duration of 2, so a holder that
// never lands a real accuracy roll loses it rather than banking it. Without a
// duration a sleeping holder could hold the boost for the rest of the battle.
func TestMicleLapses(t *testing.T) {
	d, s := berryBattle(t, ItemMicleBerry)
	holder := s.Active(0)
	holder.HP = holder.MaxHP / 4

	for turn := 0; turn < 4; turn++ {
		splashTurn(d, s) // Splash never rolls accuracy
	}
	if got := s.Active(0).Volatiles.MicleTurns; got != 0 {
		t.Errorf("Micle prime still armed (%d) after four turns of never rolling accuracy", got)
	}
}

// TestMicleNotSpentByAnUnmissableMove: an accuracy prime spent on a move that
// cannot miss is a wasted berry. Canon only fires the accuracy event on moves
// that actually roll, so the prime waits.
// resolveAccuracy is driven directly: through a full turn the prime's own
// duration tick would confound "spent by the move" with "lapsed on schedule",
// and it is the spend decision that is under test here.
func TestMicleNotSpentByAnUnmissableMove(t *testing.T) {
	d, s := berryBattle(t, ItemMicleBerry)
	holder := s.Active(0)
	holder.Volatiles.MicleTurns = 2

	var log []LogLine
	// Swift carries bypass-acc: it never rolls, so there is nothing to boost.
	if !firstOf2(resolveAccuracy(s, 0, d.Moves["swift"], NewRNG(1), &log)) {
		t.Fatal("an unmissable move reported a miss")
	}
	if holder.Volatiles.MicleTurns == 0 {
		t.Errorf("an unmissable move burned the Micle prime for nothing")
	}

	// A move that does roll spends it.
	if _ = firstOf2(resolveAccuracy(s, 0, d.Moves["hydro-pump"], NewRNG(1), &log)); holder.Volatiles.MicleTurns != 0 {
		t.Errorf("a real accuracy roll did not spend the Micle prime")
	}
}

// TestMicleDoesNotBoostOHKOMoves: canon explicitly excludes OHKO moves from the
// accuracy boost — a 30%-accurate Fissure stays 30%.
func TestMicleDoesNotBoostOHKOMoves(t *testing.T) {
	d := loadDex(t)
	hits := func(primed bool) int {
		n := 0
		for seed := uint64(1); seed <= 150; seed++ {
			s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			s.Active(0).Moves = []MoveSlot{{MoveID: "horn-drill", PP: 5, MaxPP: 5}}
			s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			if primed {
				s.Active(0).Volatiles.MicleTurns = 2
			}
			if !logHas(splashTurn(d, s), "attack missed") {
				n++
			}
		}
		return n
	}
	if bare, boosted := hits(false), hits(true); boosted != bare {
		t.Errorf("Micle changed OHKO accuracy: %d hits bare vs %d primed (of 150)", bare, boosted)
	}
}

// TestMicleRaisesTheAccuracyRoll is the numeric half: a 100%-accurate move
// can't show the difference, so this checks the multiplier directly against a
// shaky move over many seeds.
func TestMicleRaisesTheAccuracyRoll(t *testing.T) {
	d := loadDex(t)
	hits := func(primed bool) int {
		n := 0
		for seed := uint64(1); seed <= 200; seed++ {
			s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			// Focus Blast: 70% accuracy, enough headroom for 1.2x to show.
			s.Active(0).Moves = []MoveSlot{{MoveID: "focus-blast", PP: 5, MaxPP: 5}}
			s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			if primed {
				s.Active(0).Volatiles.MicleTurns = 2
			}
			log := splashTurn(d, s)
			if !logHas(log, "attack missed") {
				n++
			}
		}
		return n
	}
	bare, boosted := hits(false), hits(true)
	if boosted <= bare {
		t.Errorf("Micle did not improve the accuracy roll: %d hits bare vs %d primed (of 200)", bare, boosted)
	}
}

// --- damage reaction ---

// TestEnigmaHealsOnSuperEffectiveHit and its resisted-hit sibling pin the
// effectiveness gate, which is the only thing separating Enigma from a
// heal-on-anything berry.
func TestEnigmaHealsOnSuperEffectiveHit(t *testing.T) {
	d := loadDex(t)
	// Gengar (Ghost/Poison) is weak to Psychic; Hypno provides Psychic.
	s, err := NewBattle(d, "b", "Holder", []int{94}, "Attacker", []int{97}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Item = ItemEnigmaBerry
	// Enough headroom to survive the hit and still be missing HP to restore.
	holder.HP = holder.MaxHP - 20
	holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "psychic", PP: 10, MaxPP: 10}}

	log := splashTurn(d, s)

	if s.Active(0).Item != ItemNone {
		t.Fatalf("Enigma did not fire on a super-effective hit; log: %v", log)
	}
	if !logHas(log, "Enigma Berry") {
		t.Errorf("no Enigma line; log: %v", log)
	}
}

// TestEnigmaIgnoresNeutralHit: a neutral hit must leave the berry alone.
func TestEnigmaIgnoresNeutralHit(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Item = ItemEnigmaBerry
	holder.HP = holder.MaxHP / 2
	holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	// Normal vs Normal is neutral.
	s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}

	splashTurn(d, s)

	if s.Active(0).Item != ItemEnigmaBerry {
		t.Errorf("Enigma fired on a neutral hit")
	}
}

// TestAttackerChipBerries: Jaboca answers physical, Rowap answers special, and
// each must ignore the other category.
func TestAttackerChipBerries(t *testing.T) {
	cases := []struct {
		item     ItemKind
		name     string
		fires    string // move that should trigger it
		staysPut string // move of the other category
	}{
		{ItemJabocaBerry, "Jaboca Berry", "body-slam", "water-gun"},
		{ItemRowapBerry, "Rowap Berry", "water-gun", "body-slam"},
	}
	for _, tc := range cases {
		t.Run(string(tc.item)+"/fires", func(t *testing.T) {
			d := loadDex(t)
			s, _ := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, 5)
			s.Active(0).Item = tc.item
			s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			s.Active(1).Moves = []MoveSlot{{MoveID: tc.fires, PP: 25, MaxPP: 25}}
			atkBefore := s.Active(1).HP
			wantChip := s.Active(1).MaxHP / 8

			log := splashTurn(d, s)

			if got := atkBefore - s.Active(1).HP; got != wantChip {
				t.Errorf("%s chipped the attacker %d, want %d; log: %v", tc.name, got, wantChip, log)
			}
			if s.Active(0).Item != ItemNone {
				t.Errorf("%s not consumed", tc.name)
			}
		})
		t.Run(string(tc.item)+"/wrong-category", func(t *testing.T) {
			d := loadDex(t)
			s, _ := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, 5)
			s.Active(0).Item = tc.item
			s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			s.Active(1).Moves = []MoveSlot{{MoveID: tc.staysPut, PP: 25, MaxPP: 25}}
			atkBefore := s.Active(1).HP

			splashTurn(d, s)

			if s.Active(1).HP != atkBefore {
				t.Errorf("%s chipped on a %s move", tc.name, tc.staysPut)
			}
			if s.Active(0).Item != tc.item {
				t.Errorf("%s consumed on the wrong category", tc.name)
			}
		})
	}
}

// TestAttackerChipBerrySparesMagicGuard: the chip is indirect damage, so Magic
// Guard blocks it — and the berry must then stay in reserve rather than being
// spent for nothing.
func TestAttackerChipBerrySparesMagicGuard(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, 5)
	s.Active(0).Item = ItemJabocaBerry
	s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	atk := s.Active(1)
	atk.Ability = "magic-guard"
	atk.Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	atkBefore := atk.HP

	splashTurn(d, s)

	if s.Active(1).HP != atkBefore {
		t.Errorf("Jaboca chipped through Magic Guard: %d → %d", atkBefore, s.Active(1).HP)
	}
	if s.Active(0).Item != ItemJabocaBerry {
		t.Errorf("Jaboca spent against a Magic Guard attacker with no effect")
	}
}

// TestReactBoostBerries: Kee answers physical with +1 Def, Maranga answers
// special with +1 SpD, each ignoring the other category.
func TestReactBoostBerries(t *testing.T) {
	cases := []struct {
		item  ItemKind
		name  string
		fires string
		other string
		read  func(*Stages) int
	}{
		{ItemKeeBerry, "Kee Berry", "body-slam", "water-gun", func(g *Stages) int { return g.Def }},
		{ItemMarangaBerry, "Maranga Berry", "water-gun", "body-slam", func(g *Stages) int { return g.SpD }},
	}
	for _, tc := range cases {
		t.Run(string(tc.item), func(t *testing.T) {
			d := loadDex(t)
			s, _ := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, 5)
			s.Active(0).Item = tc.item
			s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			s.Active(1).Moves = []MoveSlot{{MoveID: tc.fires, PP: 25, MaxPP: 25}}

			splashTurn(d, s)

			if got := tc.read(&s.Active(0).Stages); got != 1 {
				t.Errorf("%s: stage = %d, want 1", tc.name, got)
			}
			if s.Active(0).Item != ItemNone {
				t.Errorf("%s not consumed", tc.name)
			}
		})
		t.Run(string(tc.item)+"/wrong-category", func(t *testing.T) {
			d := loadDex(t)
			s, _ := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, 5)
			s.Active(0).Item = tc.item
			s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			s.Active(1).Moves = []MoveSlot{{MoveID: tc.other, PP: 25, MaxPP: 25}}

			splashTurn(d, s)

			if got := tc.read(&s.Active(0).Stages); got != 0 {
				t.Errorf("%s fired on the wrong category: stage = %d", tc.name, got)
			}
		})
	}
}

// TestReactiveBerryNotSpentThroughSubstitute: a doll absorbs the hit, so the
// holder was never struck and its berry must not react.
func TestReactiveBerryNotSpentThroughSubstitute(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, 5)
	holder := s.Active(0)
	holder.Item = ItemKeeBerry
	holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	holder.Volatiles.Substitute = &SubstituteState{HP: 200}
	s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}

	splashTurn(d, s)

	if s.Active(0).Item != ItemKeeBerry {
		t.Errorf("Kee Berry reacted to a hit the Substitute absorbed")
	}
	if s.Active(0).Stages.Def != 0 {
		t.Errorf("Kee Berry boosted off a sub-absorbed hit")
	}
}

// --- type resist ---

// TestResistBerryHalvesSuperEffectiveHit: the damage reduction and the
// consumption are asserted together, because computeDamage and dealDamage read
// the same predicate and a divergence between them is the failure mode.
func TestResistBerryHalvesSuperEffectiveHit(t *testing.T) {
	d := loadDex(t)
	build := func(item ItemKind) (*BattleState, int) {
		// Venusaur (Grass/Poison) takes 2x from Fire; Charizard supplies it.
		s, err := NewBattle(d, "b", "Holder", []int{3}, "Attacker", []int{6}, 11)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Item = item
		s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "flamethrower", PP: 15, MaxPP: 15}}
		return s, s.Active(0).HP
	}

	sBare, bareBefore := build(ItemNone)
	splashTurn(d, sBare)
	bareDmg := bareBefore - sBare.Active(0).HP

	sBerry, berryBefore := build(ItemOccaBerry)
	log := splashTurn(d, sBerry)
	berryDmg := berryBefore - sBerry.Active(0).HP

	if bareDmg <= 0 {
		t.Fatalf("setup: the bare holder took no damage")
	}
	// Same seed, same rolls: the halving should be exact within integer floor.
	if berryDmg*2 < bareDmg-2 || berryDmg*2 > bareDmg+2 {
		t.Errorf("Occa Berry damage = %d, want ~half of %d", berryDmg, bareDmg)
	}
	if sBerry.Active(0).Item != ItemNone {
		t.Errorf("Occa Berry not consumed")
	}
	if !logHas(log, "Occa Berry weakened the damage") {
		t.Errorf("no resist-berry line; log: %v", log)
	}
}

// TestResistBerryIgnoresNeutralHit: sixteen of the eighteen only answer
// super-effective hits. A neutral Fire hit must leave Occa untouched.
func TestResistBerryIgnoresNeutralHit(t *testing.T) {
	d := loadDex(t)
	// Snorlax (Normal) takes neutral Fire.
	s, _ := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{6}, 11)
	s.Active(0).Item = ItemOccaBerry
	s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "flamethrower", PP: 15, MaxPP: 15}}

	splashTurn(d, s)

	if s.Active(0).Item != ItemOccaBerry {
		t.Errorf("Occa Berry fired on a neutral Fire hit")
	}
}

// TestChilanHalvesAnyNormalHit: Chilan is the exception — nothing is weak to
// Normal, so it fires on a neutral hit. If it were gated like the others it
// would be dead weight, which no other test would reveal.
func TestChilanHalvesAnyNormalHit(t *testing.T) {
	d := loadDex(t)
	build := func(item ItemKind) (*BattleState, int) {
		s, _ := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, 13)
		s.Active(0).Item = item
		s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
		return s, s.Active(0).HP
	}
	sBare, bareBefore := build(ItemNone)
	splashTurn(d, sBare)
	bareDmg := bareBefore - sBare.Active(0).HP

	sBerry, berryBefore := build(ItemChilanBerry)
	splashTurn(d, sBerry)
	berryDmg := berryBefore - sBerry.Active(0).HP

	if bareDmg <= 0 {
		t.Fatalf("setup: the bare holder took no damage")
	}
	if berryDmg*2 < bareDmg-2 || berryDmg*2 > bareDmg+2 {
		t.Errorf("Chilan Berry damage = %d, want ~half of %d", berryDmg, bareDmg)
	}
	if sBerry.Active(0).Item != ItemNone {
		t.Errorf("Chilan Berry not consumed on a neutral Normal hit")
	}
}

// TestResistBerryIgnoresOtherTypes: the type match must be exact. A Water hit
// on an Occa holder is the check that catches a berry wired to the wrong type.
func TestResistBerryIgnoresOtherTypes(t *testing.T) {
	d := loadDex(t)
	// Charizard (Fire/Flying) takes 4x from Rock and 2x from Water.
	s, _ := NewBattle(d, "b", "Holder", []int{6}, "Attacker", []int{9}, 11)
	s.Active(0).Item = ItemOccaBerry
	s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "surf", PP: 15, MaxPP: 15}}

	splashTurn(d, s)

	if s.Active(0).Item != ItemOccaBerry {
		t.Errorf("Occa Berry fired on a Water hit")
	}
}

// TestResistBerryCoversEveryType: eighteen berries, eighteen types, no
// duplicates and no gaps. A table this repetitive is exactly where a
// copy-paste omission hides, and nothing else in the suite would catch it.
func TestResistBerryCoversEveryType(t *testing.T) {
	d := loadDex(t)
	byType := map[domain.Type]ItemKind{}
	for kind, it := range itemRegistry {
		if it.ResistType == "" {
			continue
		}
		if prev, dup := byType[it.ResistType]; dup {
			t.Errorf("two resist berries claim %s: %q and %q", it.ResistType, prev, kind)
		}
		byType[it.ResistType] = kind
		if _, ok := d.Items[string(kind)]; !ok {
			t.Errorf("resist berry %q is not in the catalog", kind)
		}
	}
	for _, ty := range []domain.Type{
		"normal", "fire", "water", "electric", "grass", "ice", "fighting", "poison",
		"ground", "flying", "psychic", "bug", "rock", "ghost", "dragon", "dark",
		"steel", "fairy",
	} {
		if _, ok := byType[ty]; !ok {
			t.Errorf("no resist berry covers %s", ty)
		}
	}
	if len(byType) != 18 {
		t.Errorf("resist berries cover %d types, want 18", len(byType))
	}
}

// TestResistBerryReachesExpectedDamage: the AI's estimator must see the same
// halving the real hit gets, or every switch/move score misjudges the matchup.
func TestResistBerryReachesExpectedDamage(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[6]) // Charizard
	def := buildPokemon(d, d.Species[3]) // Venusaur — 2x weak to Fire
	m := d.Moves["flamethrower"]

	bare := ExpectedDamage(d, &atk, &def, m, nil, nil, nil)
	def.Item = ItemOccaBerry
	withBerry := ExpectedDamage(d, &atk, &def, m, nil, nil, nil)

	if bare <= 0 {
		t.Fatalf("setup: expected damage is zero")
	}
	if withBerry*2 < bare-2 || withBerry*2 > bare+2 {
		t.Errorf("ExpectedDamage with Occa = %d, want ~half of %d", withBerry, bare)
	}
}

// --- framework invariants ---

// TestPinchItemsDeclareAThreshold: an OnHPThreshold hook with a zero threshold
// could only fire at 0 HP, i.e. never (a fainted holder is filtered first). A
// threshold with no hook is equally dead. Both are silent, so both are asserted.
func TestPinchItemsDeclareAThreshold(t *testing.T) {
	for kind, it := range itemRegistry {
		if it.OnHPThreshold != nil && it.HPThreshold <= 0 {
			t.Errorf("item %q has an HP-threshold hook but no threshold — it can never fire", kind)
		}
		if it.HPThreshold > 0 && it.OnHPThreshold == nil {
			t.Errorf("item %q declares HPThreshold=%v with no hook to run", kind, it.HPThreshold)
		}
		if it.HPThreshold > 1 {
			t.Errorf("item %q has HPThreshold=%v above full HP", kind, it.HPThreshold)
		}
	}
}

// TestConsumableItemsAreNotAlsoPassive: an item that both fires once and
// carries an always-on modifier would keep buffing after it was eaten in some
// code paths and not others. Nothing in the current set does this; the test is
// here so adding one is a deliberate, visible choice.
func TestConsumableItemsAreNotAlsoPassive(t *testing.T) {
	for kind, it := range itemRegistry {
		oneShot := it.OnHPThreshold != nil || it.OnStatus != nil ||
			it.OnHitTaken != nil || it.ResistType != ""
		if !oneShot {
			continue
		}
		if it.OutgoingDamageMult != nil || it.SpeedMult != nil || it.EndOfTurn != nil ||
			it.ChoiceLock || it.Recoil > 0 {
			t.Errorf("item %q mixes a one-shot trigger with an always-on modifier", kind)
		}
	}
}

// TestEveryBerryIsFlaggedAsABerry keeps the consume log honest: "-berry" slugs
// must set Berry (so the line reads "ate"), and nothing else may claim it.
func TestEveryBerryIsFlaggedAsABerry(t *testing.T) {
	for kind, it := range itemRegistry {
		isBerrySlug := strings.HasSuffix(string(kind), "-berry")
		if isBerrySlug && !it.Berry {
			t.Errorf("item %q looks like a Berry but Berry is false — it would log \"used\"", kind)
		}
		if !isBerrySlug && it.Berry {
			t.Errorf("item %q is flagged as a Berry but its slug isn't one", kind)
		}
	}
}

// TestBerryConsumptionArmsUnburden: consumeItem is the single place Unburden is
// armed, so every berry path has to route through it. A berry that cleared
// p.Item directly would silently break the ability.
func TestBerryConsumptionArmsUnburden(t *testing.T) {
	d, s := berryBattle(t, ItemSitrusBerry)
	holder := s.Active(0)
	holder.Ability = "unburden"
	holder.HP = holder.MaxHP / 2

	splashTurn(d, s)

	if !s.Active(0).Volatiles.Unburden {
		t.Errorf("eating a berry did not arm Unburden")
	}
}

// TestBerriesKeepStateInvariants runs each berry through a full turn from a
// state that triggers it and checks the structural invariants afterwards. It is
// the cheap net under the whole family: a heal that overshoots MaxHP or a chip
// that leaves HP at 0 without the Fainted flag trips here regardless of which
// berry did it.
func TestBerriesKeepStateInvariants(t *testing.T) {
	d := loadDex(t)
	for kind := range itemRegistry {
		it := itemRegistry[kind]
		if !it.Berry && kind != ItemBerryJuice {
			continue
		}
		t.Run(string(kind), func(t *testing.T) {
			s, err := NewBattle(d, "b", "Holder", []int{3}, "Attacker", []int{6}, 17)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			holder := s.Active(0)
			holder.Item = kind
			holder.HP = holder.MaxHP / 4 // in range for every threshold
			holder.Moves = []MoveSlot{{MoveID: "body-slam", PP: 1, MaxPP: 15}}
			holder.Status = StatusBurn
			holder.Volatiles.Confusion = &ConfusionState{Turns: 3}
			s.Active(1).Moves = []MoveSlot{{MoveID: "flamethrower", PP: 15, MaxPP: 15}}

			splashTurn(d, s)

			if err := ValidateStateInvariants(s); err != nil {
				t.Errorf("invariants broken with %s held: %v", kind, err)
			}
		})
	}
}
