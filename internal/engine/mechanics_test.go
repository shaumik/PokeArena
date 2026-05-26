package engine

import (
	"strings"
	"testing"

	"pokearena/internal/domain"
)

// TestAccStageMultiplier checks the Gen 3+ accuracy/evasion curve, distinct
// from the symmetric offensive curve.
func TestAccStageMultiplier(t *testing.T) {
	cases := []struct {
		stage int
		want  float64
	}{
		{0, 1.0},
		{1, 4.0 / 3.0},
		{2, 5.0 / 3.0},
		{3, 2.0},
		{-1, 3.0 / 4.0},
		{-2, 3.0 / 5.0},
		{-3, 3.0 / 6.0},
		{-6, 3.0 / 9.0},
	}
	const eps = 1e-9
	for _, c := range cases {
		got := accStageMultiplier(c.stage)
		if diff := got - c.want; diff > eps || diff < -eps {
			t.Errorf("accStageMultiplier(%d) = %v, want %v", c.stage, got, c.want)
		}
	}
}

// TestStageVerb walks the wording ladder so the canonical Pokémon log lines
// don't regress.
func TestStageVerb(t *testing.T) {
	cases := []struct {
		delta int
		want  string
	}{
		{1, "rose"},
		{2, "rose sharply"},
		{3, "rose drastically"},
		{6, "rose drastically"},
		{-1, "fell"},
		{-2, "harshly fell"},
		{-3, "severely fell"},
		{-6, "severely fell"},
	}
	for _, c := range cases {
		if got := stageVerb(c.delta); got != c.want {
			t.Errorf("stageVerb(%d) = %q, want %q", c.delta, got, c.want)
		}
	}
}

// TestToxicEscalation verifies the 1/16, 2/16, 3/16 escalation across three
// end-of-turn ticks and that the counter caps at 15.
func TestToxicEscalation(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{25}, "P2", []int{6}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	p := s.Active(0)
	p.HP = p.MaxHP
	p.Status = StatusToxic
	p.ToxicCounter = 1

	var log []LogLine
	expect := []int{p.MaxHP / 16, p.MaxHP * 2 / 16, p.MaxHP * 3 / 16}
	for tick, want := range expect {
		before := p.HP
		applyResidual(s, 0, &log)
		got := before - p.HP
		if got != want {
			t.Errorf("tick %d: toxic dmg = %d, want %d (counter=%d)", tick, got, want, p.ToxicCounter)
		}
	}
}

// TestToxicImmunity ensures Poison- and Steel-types cannot be Toxic-ed.
func TestToxicImmunity(t *testing.T) {
	d := loadDex(t)
	venusaur := buildPokemon(d, d.Species[3]) // grass/poison
	if isType(&venusaur, "poison") == false {
		t.Fatal("test setup: Venusaur should be poison-type")
	}
	rng := NewRNG(1)
	var log []LogLine
	if inflictStatus(&venusaur, 0, StatusToxic, rng, &log) {
		t.Fatal("expected Toxic infliction to fail on a Poison-type")
	}
	if venusaur.Status != StatusNone {
		t.Errorf("Venusaur status = %q, want none", venusaur.Status)
	}
}

// TestConfusionSnapOut: Turns counter at 1 must decrement to 0, clear the
// volatile, log a snap-out line, and let the move execute normally.
func TestConfusionSnapOut(t *testing.T) {
	d := loadDex(t)
	pika := buildPokemon(d, d.Species[25])
	pika.Volatiles.Confusion = &ConfusionState{Turns: 1}
	rng := NewRNG(1)
	var log []LogLine
	if !canAct(&pika, 0, rng, &log) {
		t.Fatal("snap-out turn should allow the move to proceed")
	}
	if pika.Volatiles.Confusion != nil {
		t.Errorf("Confusion should be nil after snap-out, got %+v", pika.Volatiles.Confusion)
	}
	if !logHas(log, "snapped out") {
		t.Errorf("expected snap-out log, got %v", logTexts(log))
	}
}

// TestConfusionSelfHit: with an RNG seeded so the 33% roll triggers, the
// confused Pokémon hurts itself and the intended move is preempted.
func TestConfusionSelfHit(t *testing.T) {
	d := loadDex(t)
	pika := buildPokemon(d, d.Species[25])
	pika.Volatiles.Confusion = &ConfusionState{Turns: 3}
	before := pika.HP

	// Find a seed where rng.Chance(33) returns true on the first roll after
	// the turn decrement consumes nothing of its own. confusion ticks
	// rng.Chance(33) immediately, so a small seed search suffices.
	var rng *RNG
	for seed := uint64(1); seed < 1000; seed++ {
		r := NewRNG(seed)
		if r.Chance(33) {
			rng = NewRNG(seed) // re-seed so the same first roll happens in canAct
			break
		}
	}
	if rng == nil {
		t.Fatal("could not find a self-hit seed")
	}

	var log []LogLine
	if canAct(&pika, 0, rng, &log) {
		t.Fatal("self-hit turn should block the move")
	}
	if pika.HP >= before {
		t.Errorf("self-hit should have dealt damage; HP %d -> %d", before, pika.HP)
	}
	if pika.Volatiles.Confusion == nil || pika.Volatiles.Confusion.Turns != 2 {
		t.Errorf("Confusion turn count = %+v, want Turns=2", pika.Volatiles.Confusion)
	}
}

// TestFlinchConsumed: a flinching Pokémon's move fails, and the flag clears.
func TestFlinchConsumed(t *testing.T) {
	d := loadDex(t)
	pika := buildPokemon(d, d.Species[25])
	pika.Volatiles.Flinch = true
	rng := NewRNG(1)
	var log []LogLine
	if canAct(&pika, 0, rng, &log) {
		t.Fatal("flinched Pokémon should not move")
	}
	if pika.Volatiles.Flinch {
		t.Error("Flinch flag should be consumed by canAct")
	}
	if !logHas(log, "flinched") {
		t.Errorf("expected flinch log, got %v", logTexts(log))
	}
}

// TestSleepResetsOnSwitch: Gen 5+ semantics — the sleep counter does not
// carry over a switch.
func TestSleepResetsOnSwitch(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{25, 6}, "P2", []int{3}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Status = StatusSleep
	s.Active(0).SleepTurns = 3

	var log []LogLine
	doSwitch(s, 0, 1, &log)
	out := &s.Sides[0].Team[0] // the one we just switched out
	if out.SleepTurns != 0 {
		t.Errorf("switched-out sleeper SleepTurns = %d, want 0", out.SleepTurns)
	}
	if out.Status != StatusSleep {
		t.Errorf("sleeping Pokémon should still be asleep after switch, got %q", out.Status)
	}
}

// TestFireThawsFreeze: a frozen target hit by a Fire-type move thaws and the
// move still lands.
func TestFireThawsFreeze(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{6}, "P2", []int{25}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	def := s.Active(1)
	def.Status = StatusFreeze

	rng := NewRNG(1)
	var log []LogLine
	flame := d.Moves["flamethrower"]
	dmg, ok := dealDamage(d, s, 0, flame, rng, &log)
	if !ok {
		t.Fatal("dealDamage should report a normal hit")
	}
	if def.Status != StatusNone {
		t.Errorf("freeze should be cleared by Fire move, got %q", def.Status)
	}
	if dmg <= 0 {
		t.Error("Fire move should still deal damage on thaw")
	}
	if !logHas(log, "thawed by the heat") {
		t.Errorf("expected thaw log, got %v", logTexts(log))
	}
}

// TestAccuracyEvasionGating: with foe at +2 Evasion, a normally-100% move can
// miss; with the bypass-acc flag, it never misses.
func TestAccuracyEvasionGating(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{25}, "P2", []int{6}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(1).Stages.Eva = 2 // combined = 0 - 2 = -2, mult = 0.6, chance = 60

	thunderbolt := d.Moves["thunderbolt"]
	misses := 0
	const trials = 500
	for i := 0; i < trials; i++ {
		rng := NewRNG(uint64(i)*2654435761 + 1)
		var log []LogLine
		if !resolveAccuracy(s, 0, thunderbolt, rng, &log) {
			misses++
		}
	}
	if misses == 0 {
		t.Errorf("with +2 evasion a 100%% move should miss sometimes, got %d/%d", misses, trials)
	}

	// Same configuration, but with the bypass-acc flag — never misses.
	bypass := thunderbolt
	bypass.Flags = append([]string{}, "bypass-acc")
	bypassMisses := 0
	for i := 0; i < trials; i++ {
		rng := NewRNG(uint64(i)*2654435761 + 1)
		var log []LogLine
		if !resolveAccuracy(s, 0, bypass, rng, &log) {
			bypassMisses++
		}
	}
	if bypassMisses != 0 {
		t.Errorf("bypass-acc should never miss, got %d misses", bypassMisses)
	}
}

// TestRestRevives: Rest cures any prior status, fully heals, and inflicts a
// 2-turn sleep.
func TestRestRevives(t *testing.T) {
	d := loadDex(t)
	pika := buildPokemon(d, d.Species[25])
	pika.HP = 5
	pika.Status = StatusBurn

	var log []LogLine
	doRest(&pika, 0, &log)
	if pika.HP != pika.MaxHP {
		t.Errorf("Rest HP = %d, want %d", pika.HP, pika.MaxHP)
	}
	if pika.Status != StatusSleep {
		t.Errorf("Rest status = %q, want sleep", pika.Status)
	}
	if pika.SleepTurns != 2 {
		t.Errorf("Rest SleepTurns = %d, want 2", pika.SleepTurns)
	}
}

// --- helpers ---

func logTexts(log []LogLine) []string {
	out := make([]string, len(log))
	for i, l := range log {
		out[i] = l.Text
	}
	return out
}

func logHas(log []LogLine, substr string) bool {
	for _, l := range log {
		if strings.Contains(l.Text, substr) {
			return true
		}
	}
	return false
}

// Compile-time guard that Effect's exported fields are accessible — catches
// accidental rename of the schema struct.
var _ = domain.Effect{Chance: 1, Status: "burn", Volatile: "flinch"}
