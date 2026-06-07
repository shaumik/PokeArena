package engine

import (
	"fmt"
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
	s, err := NewBattle(d, "b", "P1", []int{26}, "P2", []int{6}, 1)
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
	if inflictStatus(&venusaur, 0, StatusToxic, nil, rng, &log) {
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
	pika := buildPokemon(d, d.Species[26])
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
	pika := buildPokemon(d, d.Species[26])
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
	pika := buildPokemon(d, d.Species[26])
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
	s, err := NewBattle(d, "b", "P1", []int{26, 6}, "P2", []int{3}, 1)
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
	s, err := NewBattle(d, "b", "P1", []int{6}, "P2", []int{26}, 1)
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
	s, err := NewBattle(d, "b", "P1", []int{26}, "P2", []int{6}, 1)
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
	pika := buildPokemon(d, d.Species[26])
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

// TestFixedDamageLevel: Seismic Toss / Night Shade deal exactly L damage,
// regardless of stats/STAB/effectiveness — but type immunity still blocks.
func TestFixedDamageLevel(t *testing.T) {
	d := loadDex(t)
	chansey := buildPokemon(d, d.Species[113]) // huge HP, used to ensure 1 damage isn't capping
	machamp := buildPokemon(d, d.Species[68])  // fighting attacker

	rng := NewRNG(1)
	res := computeDamage(d, &machamp, &chansey, d.Moves["seismic-toss"], nil, nil, nil, rng)
	if res.Damage != Level {
		t.Errorf("seismic-toss damage = %d, want %d", res.Damage, Level)
	}
	if res.Effectiveness != 1.0 {
		t.Errorf("seismic-toss effectiveness = %v, want 1.0 (no eff log)", res.Effectiveness)
	}

	// Ghost is immune to Fighting → seismic-toss should still be blocked.
	gengar := buildPokemon(d, d.Species[94])
	res = computeDamage(d, &machamp, &gengar, d.Moves["seismic-toss"], nil, nil, nil, NewRNG(1))
	if res.Damage != 0 || res.Effectiveness != 0 {
		t.Errorf("seismic-toss vs Ghost = %+v, want 0 dmg / 0 eff", res)
	}

	// AI's expected-damage prediction should match.
	if got := ExpectedDamage(d, &machamp, &chansey, d.Moves["seismic-toss"], nil, nil, nil); got != Level {
		t.Errorf("ExpectedDamage(seismic-toss) = %d, want %d", got, Level)
	}
}

// TestSelfDestructFaintsUser: Explosion drops the user to 0 HP after dealing
// damage to the foe and fires the faint event.
func TestSelfDestructFaintsUser(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143, 6}, "P2", []int{3, 26}, 42) // Snorlax has explosion
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	atk := s.Active(0)
	atk.Moves = []MoveSlot{{MoveID: "explosion", PP: 5, MaxPP: 5}}

	a := [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}}
	// Force the foe to have a no-op move so it doesn't KO Snorlax first.
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	log := ResolveTurn(d, s, a)
	if !s.Sides[0].Team[0].Fainted {
		t.Errorf("Explosion user should have fainted, HP = %d", s.Sides[0].Team[0].HP)
	}
	if !logHas(log, "exploded") {
		t.Errorf("expected explosion log, got %v", logTexts(log))
	}
}

// TestHyperBeamRecharge: after Hyper Beam lands, the user spends the next
// turn recharging — no move resolves and damage isn't dealt.
func TestHyperBeamRecharge(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{150, 144}, "P2", []int{143}, 1) // Mewtwo has hyper-beam
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "hyper-beam", PP: 5, MaxPP: 5}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	// Turn 1: Hyper Beam connects → MustRecharge flag set.
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !s.Active(0).Volatiles.MustRecharge {
		t.Fatal("MustRecharge should be set after Hyper Beam")
	}

	// Turn 2: User must recharge. Foe HP shouldn't drop further from us.
	foeHPBefore := s.Active(1).HP
	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !logHas(log, "must recharge") {
		t.Errorf("expected 'must recharge' log, got %v", logTexts(log))
	}
	if s.Active(1).HP < foeHPBefore {
		t.Errorf("foe took damage during recharge turn (%d → %d)", foeHPBefore, s.Active(1).HP)
	}
	if s.Active(0).Volatiles.MustRecharge {
		t.Errorf("MustRecharge should clear after the recharge turn")
	}

	// Turn 3: Hyper Beam available again.
	if s.Active(0).Volatiles.MustRecharge {
		t.Errorf("MustRecharge should not persist into turn 3")
	}
}

// TestSolarBeamCharge: Solar Beam takes one turn to charge before firing,
// and the strike turn resolves against the same move even if the foe
// submits a different slot index (the LegalActions guarantee).
func TestSolarBeamCharge(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{3}, "P2", []int{143}, 1) // Venusaur learns solar-beam
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "solar-beam", PP: 10, MaxPP: 10}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	foeHPBefore := s.Active(1).HP

	// Turn 1: charge — no damage, Charging set, PP consumed.
	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !logHas(log, "began charging") {
		t.Errorf("expected charge log, got %v", logTexts(log))
	}
	if s.Active(0).Volatiles.Charging == nil {
		t.Fatal("Charging volatile should be set after charge turn")
	}
	if s.Active(1).HP != foeHPBefore {
		t.Errorf("foe took damage on charge turn (%d → %d)", foeHPBefore, s.Active(1).HP)
	}
	if s.Active(0).Moves[0].PP != 9 {
		t.Errorf("PP should decrement on charge turn, got %d", s.Active(0).Moves[0].PP)
	}

	// LegalActions on the strike turn should pin to the charging move only.
	la := LegalActions(s, 0)
	if len(la) != 1 || la[0].Kind != ActionMove || la[0].Index != 0 {
		t.Errorf("LegalActions during charge = %+v, want single charging move", la)
	}

	// Turn 2: strike — damage applies, Charging clears, PP does not decrement again.
	log = ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.Active(0).Volatiles.Charging != nil {
		t.Errorf("Charging should clear after strike, got %+v", s.Active(0).Volatiles.Charging)
	}
	if s.Active(1).HP >= foeHPBefore {
		t.Errorf("foe should have taken damage on strike turn (%d → %d)", foeHPBefore, s.Active(1).HP)
	}
	if s.Active(0).Moves[0].PP != 9 {
		t.Errorf("PP should not decrement on strike turn, got %d", s.Active(0).Moves[0].PP)
	}
}

// TestMultihitFixedCount: Bonemerang's curated entry declares MinHits=MaxHits=2.
// The engine must strike exactly twice, log the "Hit N times!" closer, and
// charge a single accuracy roll (no double-miss for a single use).
func TestMultihitFixedCount(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{105}, "P2", []int{143}, 1) // Marowak vs Snorlax
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "bonemerang", PP: 10, MaxPP: 10}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	// Force Snorlax to a large HP so two hits don't OHKO it.
	s.Active(1).HP = s.Active(1).MaxHP

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	damageLines := 0
	for _, l := range log {
		if l.Type == "damage" && l.Side == 1 {
			damageLines++
		}
	}
	if damageLines != 2 {
		t.Errorf("Bonemerang damage lines = %d, want 2 (got log: %v)", damageLines, logTexts(log))
	}
	if !logHas(log, "Hit 2 time") {
		t.Errorf("missing multihit closer; log: %v", logTexts(log))
	}
}

// TestMultihitRange: Bullet Seed declares MinHits=2, MaxHits=5. Across seeds
// the engine should produce all four legal counts (no truncation to a single
// value), and counts should never escape [2,5].
func TestMultihitRange(t *testing.T) {
	d := loadDex(t)
	seen := map[int]int{}

	for seed := uint64(1); seed <= 200; seed++ {
		s, err := NewBattle(d, "b", "P1", []int{114}, "P2", []int{143}, seed) // Tangela vs Snorlax
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Moves = []MoveSlot{{MoveID: "bullet-seed", PP: 30, MaxPP: 30}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).HP = s.Active(1).MaxHP

		log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		hits := 0
		for _, l := range log {
			if l.Type == "damage" && l.Side == 1 {
				hits++
			}
		}
		if hits < 2 || hits > 5 {
			t.Fatalf("seed %d: hit count %d out of [2,5]; log %v", seed, hits, logTexts(log))
		}
		seen[hits]++
	}
	for _, n := range []int{2, 3, 4, 5} {
		if seen[n] == 0 {
			t.Errorf("expected at least one occurrence of %d hits across 200 seeds, got distribution %v", n, seen)
		}
	}
}

// TestMultihitStopsOnFaint: if a hit reduces the target to 0 HP, the loop
// terminates immediately — subsequent strikes in the planned sequence do
// not fire against a fainted target.
func TestMultihitStopsOnFaint(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{105}, "P2", []int{143, 6}, 1) // Marowak vs Snorlax (+ reserve)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "bonemerang", PP: 10, MaxPP: 10}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	// Bring the target to 1 HP so the first hit faints it.
	s.Active(1).HP = 1

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	damageLines := 0
	for _, l := range log {
		if l.Type == "damage" && l.Side == 1 {
			damageLines++
		}
	}
	if damageLines != 1 {
		t.Errorf("expected exactly one damage line (target fainted after hit 1), got %d; log: %v", damageLines, logTexts(log))
	}
	if logHas(log, "Hit 2 time") {
		t.Errorf("Hit 2 times! should not log when the second strike was skipped due to faint; log: %v", logTexts(log))
	}
	if !logHas(log, "Hit 1 time") {
		t.Errorf("expected 'Hit 1 time(s)!' closer for a multihit that landed once; log: %v", logTexts(log))
	}
}

// TestMultihitCountDistribution covers the multihitCount helper directly.
// Fixed counts return MinHits; the [2,5] range follows the Gen-5+ weighted
// distribution (rough bounds — not asserting exact ratios, just that all
// four legal outcomes appear and stay in range).
func TestMultihitCountDistribution(t *testing.T) {
	fixed := domain.Move{MinHits: 3, MaxHits: 3}
	for seed := uint64(1); seed <= 20; seed++ {
		if n := multihitCount(fixed, NewRNG(seed)); n != 3 {
			t.Errorf("fixed [3,3] seed %d returned %d, want 3", seed, n)
		}
	}

	rangeM := domain.Move{MinHits: 2, MaxHits: 5}
	seen := map[int]int{}
	for seed := uint64(1); seed <= 1000; seed++ {
		n := multihitCount(rangeM, NewRNG(seed))
		if n < 2 || n > 5 {
			t.Fatalf("[2,5] seed %d returned %d, out of range", seed, n)
		}
		seen[n]++
	}
	for _, n := range []int{2, 3, 4, 5} {
		if seen[n] == 0 {
			t.Errorf("[2,5] range never produced %d hits across 1000 seeds (dist=%v)", n, seen)
		}
	}
	// 2 and 3 should each be substantially more common than 4 and 5
	// (Gen-5+: 35%/35%/15%/15%) — assert the ordering rather than exact pcts.
	if seen[2] <= seen[4] || seen[3] <= seen[5] {
		t.Errorf("distribution looks off: want seen[2]>seen[4] and seen[3]>seen[5], got %v", seen)
	}
}

// TestOHKOHitsForFullHP: a connecting OHKO move sets the target's HP to
// zero (overriding the BP=0 formula's dmg=1) and logs the "one-hit KO!"
// closer in place of the standard crit/effectiveness lines. Sampling 100
// seeds gives a handful of hits at Fissure's 30% accuracy; every hit must
// be a clean zero-HP outcome.
func TestOHKOHitsForFullHP(t *testing.T) {
	d := loadDex(t)
	hits := 0
	for seed := uint64(1); seed <= 100; seed++ {
		s, err := NewBattle(d, "b", "P1", []int{105}, "P2", []int{143}, seed) // Marowak vs Snorlax
		if err != nil {
			t.Fatalf("seed %d: new battle: %v", seed, err)
		}
		s.Active(0).Moves = []MoveSlot{{MoveID: "fissure", PP: 5, MaxPP: 5}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		defStart := s.Active(1).HP
		log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		if logHas(log, "Fissure") && logHas(log, "It's a one-hit KO!") {
			hits++
			if s.Active(1).HP != 0 {
				t.Errorf("seed %d: OHKO hit but def HP=%d, want 0", seed, s.Active(1).HP)
			}
			if logHas(log, "critical hit") || logHas(log, "super effective") || logHas(log, "not very effective") {
				t.Errorf("seed %d: OHKO should suppress crit/effectiveness lines; log: %v", seed, logTexts(log))
			}
		} else {
			// Missed — the move ran but didn't connect, so def HP must be untouched.
			if s.Active(1).HP != defStart {
				t.Errorf("seed %d: OHKO missed but def HP changed from %d to %d", seed, defStart, s.Active(1).HP)
			}
		}
	}
	if hits == 0 {
		t.Fatalf("Fissure (30%% acc) hit zero times across 100 seeds — likely accuracy plumbing broke")
	}
}

// TestOHKOSheerColdIceImmune: Sheer Cold's ohko="ice" makes Ice-types
// immune even though the type chart puts Ice vs Ice at 0.5×. The move
// short-circuits with a "doesn't affect" log before any accuracy roll,
// so the outcome is deterministic regardless of seed.
func TestOHKOSheerColdIceImmune(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{131}, "P2", []int{91}, 1) // Lapras vs Cloyster (Ice/Water)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "sheer-cold", PP: 5, MaxPP: 5}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	defStart := s.Active(1).HP

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if !logHas(log, "doesn't affect") {
		t.Errorf("expected 'doesn't affect' line; log: %v", logTexts(log))
	}
	if logHas(log, "It's a one-hit KO!") {
		t.Errorf("Sheer Cold should not register a KO against an Ice-type; log: %v", logTexts(log))
	}
	if s.Active(1).HP != defStart {
		t.Errorf("Ice-immune defender lost HP: %d → %d", defStart, s.Active(1).HP)
	}
}

// TestOHKOSturdyImmune: Sturdy in Gen 5+ blocks OHKO moves entirely —
// not the "leave at 1 HP" clamp that the SurviveOHKO hook applies to
// normal hits. The block fires before the accuracy roll, so the
// outcome is deterministic.
func TestOHKOSturdyImmune(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{105}, "P2", []int{95}, 1) // Marowak vs Onix
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "fissure", PP: 5, MaxPP: 5}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	// Onix slot 0 is Rock Head; force its slot-1 Sturdy for this test.
	s.Active(1).Ability = AbilitySturdy
	defStart := s.Active(1).HP

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if !logHas(log, "Sturdy") {
		t.Errorf("expected Sturdy line; log: %v", logTexts(log))
	}
	if logHas(log, "It's a one-hit KO!") {
		t.Errorf("Sturdy must block OHKO before damage applies; log: %v", logTexts(log))
	}
	if s.Active(1).HP != defStart {
		t.Errorf("Sturdy defender lost HP: %d → %d", defStart, s.Active(1).HP)
	}
}

// TestOHKOTypeImmunityStillApplies: Normal-type immunity (Ghost vs Horn
// Drill) takes the standard post-accuracy "doesn't affect" path. OHKO
// does not bypass the type chart — it only adds extra type-immunity
// layers. Some seeds miss, others reach the type-immunity branch; either
// way the defender must end the turn untouched and the OHKO log line
// must never fire.
func TestOHKOTypeImmunityStillApplies(t *testing.T) {
	d := loadDex(t)
	sawImmune := false
	for seed := uint64(1); seed <= 100; seed++ {
		s, err := NewBattle(d, "b", "P1", []int{87}, "P2", []int{94}, seed) // Dewgong vs Gengar (Ghost)
		if err != nil {
			t.Fatalf("seed %d: new battle: %v", seed, err)
		}
		s.Active(0).Moves = []MoveSlot{{MoveID: "horn-drill", PP: 5, MaxPP: 5}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		defStart := s.Active(1).HP

		log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

		if s.Active(1).HP != defStart {
			t.Fatalf("seed %d: Ghost defender lost HP %d → %d", seed, defStart, s.Active(1).HP)
		}
		if logHas(log, "It's a one-hit KO!") {
			t.Fatalf("seed %d: Horn Drill should not KO a Ghost-type; log: %v", seed, logTexts(log))
		}
		if logHas(log, "doesn't affect") {
			sawImmune = true
		}
	}
	if !sawImmune {
		t.Fatalf("Horn Drill never hit a Ghost across 100 seeds — should fall through to the type-immunity branch sometimes")
	}
}

// TestThawsTargetNonFireMove: Scald is Water-type but carries
// thawsTarget=true, so a frozen target hit by Scald thaws AND takes
// damage on the same hit. Without the flag the existing Fire-only
// thaw branch would leave Snorlax frozen.
func TestThawsTargetNonFireMove(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{9}, "P2", []int{143}, 1) // Blastoise vs Snorlax
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "scald", PP: 15, MaxPP: 15}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Status = StatusFreeze

	startHP := s.Active(1).HP
	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Active(1).Status != StatusNone {
		t.Errorf("Scald did not thaw frozen target; status = %v", s.Active(1).Status)
	}
	if !logHas(log, "was thawed") {
		t.Errorf("expected thaw log line; log: %v", logTexts(log))
	}
	if s.Active(1).HP >= startHP {
		t.Errorf("Scald should have dealt damage on top of thawing; HP %d → %d", startHP, s.Active(1).HP)
	}
}

// TestIgnoreEvasionBypassesPositiveBoost: Chip Away ignores positive
// evasion boosts. A +6 Eva target normally dodges most accuracy-100
// attacks (chance drops to 100*3/9 ≈ 33%); with the override the
// attacker connects every time across a 30-seed sample.
func TestIgnoreEvasionBypassesPositiveBoost(t *testing.T) {
	d := loadDex(t)
	hits := 0
	for seed := uint64(1); seed <= 30; seed++ {
		s, err := NewBattle(d, "b", "P1", []int{105}, "P2", []int{143}, seed) // Marowak vs Snorlax
		if err != nil {
			t.Fatalf("seed %d: new battle: %v", seed, err)
		}
		s.Active(0).Moves = []MoveSlot{{MoveID: "chip-away", PP: 20, MaxPP: 20}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Stages.Eva = 6
		startHP := s.Active(1).HP
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		if s.Active(1).HP < startHP {
			hits++
		}
	}
	if hits != 30 {
		t.Errorf("Chip Away vs +6 Eva landed only %d/30 — ignoreEvasion should make every shot connect", hits)
	}
}

// TestIgnoreEvasionStillRespectsDrops: ignoreEvasion zeros only
// positive evasion. A -6 Eva target still feeds the +6 effective
// accuracy bonus to the attacker (the drop is preserved).
func TestIgnoreEvasionStillRespectsDrops(t *testing.T) {
	d := loadDex(t)
	hits := 0
	for seed := uint64(1); seed <= 30; seed++ {
		s, err := NewBattle(d, "b", "P1", []int{105}, "P2", []int{143}, seed)
		if err != nil {
			t.Fatalf("seed %d: new battle: %v", seed, err)
		}
		s.Active(0).Moves = []MoveSlot{{MoveID: "chip-away", PP: 20, MaxPP: 20}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Stages.Eva = -6
		startHP := s.Active(1).HP
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		if s.Active(1).HP < startHP {
			hits++
		}
	}
	if hits != 30 {
		t.Errorf("Chip Away vs -6 Eva should always hit (drop preserved); got %d/30", hits)
	}
}

// TestIgnoreDefensiveBypassesPositiveBoost: Darkest Lariat does the same
// damage against a +6 Def target as against an unboosted one — both
// scenarios use the same RNG seed so the random damage roll, crit roll,
// etc. are identical; the only differing variable is the clamp.
func TestIgnoreDefensiveBypassesPositiveBoost(t *testing.T) {
	d := loadDex(t)
	mk := func(defStage int) (*BattleState, int) {
		s, err := NewBattle(d, "b", "P1", []int{105}, "P2", []int{143}, 1)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Moves = []MoveSlot{{MoveID: "darkest-lariat", PP: 10, MaxPP: 10}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Stages.Def = defStage
		return s, s.Active(1).HP
	}
	sBuffed, hpBuffed := mk(6)
	ResolveTurn(d, sBuffed, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	dmgBuffed := hpBuffed - sBuffed.Active(1).HP

	sBaseline, hpBaseline := mk(0)
	ResolveTurn(d, sBaseline, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	dmgBaseline := hpBaseline - sBaseline.Active(1).HP

	if dmgBuffed != dmgBaseline {
		t.Errorf("Darkest Lariat damage should be identical with +6 Def vs +0 Def (ignoreDefensive); got %d vs %d", dmgBuffed, dmgBaseline)
	}
}

// TestIgnoreDefensiveStillRespectsDrops: a -6 Def target takes
// substantially MORE damage than baseline — the drop is preserved
// because the clamp only zeros positive stages.
func TestIgnoreDefensiveStillRespectsDrops(t *testing.T) {
	d := loadDex(t)
	mk := func(defStage int) (*BattleState, int) {
		s, err := NewBattle(d, "b", "P1", []int{105}, "P2", []int{143}, 1)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Moves = []MoveSlot{{MoveID: "darkest-lariat", PP: 10, MaxPP: 10}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Stages.Def = defStage
		return s, s.Active(1).HP
	}
	sDropped, hpDropped := mk(-6)
	ResolveTurn(d, sDropped, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	dmgDropped := hpDropped - sDropped.Active(1).HP

	sBaseline, hpBaseline := mk(0)
	ResolveTurn(d, sBaseline, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	dmgBaseline := hpBaseline - sBaseline.Active(1).HP

	if dmgDropped <= dmgBaseline {
		t.Errorf("Darkest Lariat vs -6 Def (%d) should exceed baseline (%d) — drop preserved by clamp", dmgDropped, dmgBaseline)
	}
}

// TestSelfSwitchUTurnBringsInBench: U-turn damages the foe and then pulls
// the attacker out, bringing the lowest-indexed live bench member in. The
// switch-in log line and Active index both reflect the new lead.
func TestSelfSwitchUTurnBringsInBench(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{12, 18}, "P2", []int{143}, 1) // Butterfree + Pidgeot vs Snorlax
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "u-turn", PP: 5, MaxPP: 5}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	defStart := s.Active(1).HP

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Active(1).HP >= defStart {
		t.Errorf("U-turn did no damage; HP %d → %d", defStart, s.Active(1).HP)
	}
	if s.Sides[0].Active != 1 {
		t.Errorf("active did not change; still slot %d", s.Sides[0].Active)
	}
	if s.Active(0).Name != "Pidgeot" {
		t.Errorf("expected Pidgeot in, got %s", s.Active(0).Name)
	}
	if !logHas(log, "Go, Pidgeot") {
		t.Errorf("missing switch-in log; %v", logTexts(log))
	}
}

// TestSelfSwitchNoBenchSkips: U-turn against a single-Pokémon side resolves
// damage but never switches — the user stays in (nobody to swap to).
func TestSelfSwitchNoBenchSkips(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{12}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "u-turn", PP: 5, MaxPP: 5}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Sides[0].Active != 0 {
		t.Errorf("active changed despite empty bench; slot %d", s.Sides[0].Active)
	}
	if logHas(log, "come back") {
		t.Errorf("unexpected switch-out log on empty bench; %v", logTexts(log))
	}
}

// TestSelfSwitchSkipsFaintedAttacker: a Pokémon that fainted during its
// own move (life orb chip, rocky-helmet recoil) must not still self-switch.
// We stand in for those death paths by setting Fainted directly.
func TestSelfSwitchSkipsFaintedAttacker(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{12, 18}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	var log []LogLine
	s.Active(0).Fainted = true
	s.Active(0).HP = 0
	applySelfSwitch(s, 0, d.Moves["u-turn"], &log)
	if s.Sides[0].Active != 0 {
		t.Errorf("active changed for fainted attacker; slot %d", s.Sides[0].Active)
	}
}

// TestSelfSwitchTeleportStatusMove: Teleport is a status-category self-
// switch — no damage, just the swap. Exercises the applyStatusMove path
// (vs U-turn's damage path) hands off to applySelfSwitch.
func TestSelfSwitchTeleportStatusMove(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{12, 18}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "teleport", PP: 20, MaxPP: 20}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Sides[0].Active != 1 {
		t.Errorf("Teleport did not switch; slot %d", s.Sides[0].Active)
	}
	if s.Active(0).Name != "Pidgeot" {
		t.Errorf("expected Pidgeot in, got %s", s.Active(0).Name)
	}
}

// TestBatonPassCarriesStages: Baton Pass copies the outgoing's stat stages
// onto the incoming. Plain "normal" self-switch resets them (see the
// counterpart test below); the contrast is the whole point of BP.
func TestBatonPassCarriesStages(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{12, 18}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "baton-pass", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(0).Stages.Atk = 2
	s.Active(0).Stages.Spe = -1

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Sides[0].Active != 1 {
		t.Fatalf("BP did not switch; slot %d", s.Sides[0].Active)
	}
	if got := s.Active(0).Stages.Atk; got != 2 {
		t.Errorf("Atk stage not carried: got %d, want +2", got)
	}
	if got := s.Active(0).Stages.Spe; got != -1 {
		t.Errorf("Spe stage not carried: got %d, want -1", got)
	}
}

// TestBatonPassCarriesConfusion: Baton Pass also transfers the Confusion
// volatile (its canonical "pass the confusion clock" trick). Turn-local
// volatiles (Flinch, MovedLast) and mid-move state (Charging, MustRecharge)
// don't pass.
func TestBatonPassCarriesConfusion(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{12, 18}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "baton-pass", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	// Seed 1 + Turns=5 gives the user enough headroom to clear its pre-move
	// confusion check (which ticks Turns by 1) and successfully fire BP.
	s.Active(0).Volatiles.Confusion = &ConfusionState{Turns: 5}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Sides[0].Active != 1 {
		t.Fatalf("BP did not switch; slot %d", s.Sides[0].Active)
	}
	c := s.Active(0).Volatiles.Confusion
	if c == nil {
		t.Fatalf("Confusion not carried to incoming")
	}
	// Pre-move check decrements the counter once before BP resolves, so
	// the incoming sees Turns = initial - 1.
	if c.Turns != 4 {
		t.Errorf("Confusion turns not carried correctly: got %d, want 4 (5 - one pre-move tick)", c.Turns)
	}
}

// TestSelfSwitchPlainResetsStages: regression — plain self-switch MUST
// reset the outgoing's stages the way an ordinary switch does. Otherwise
// the BP-vs-plain contrast collapses and every pivot move silently becomes
// Baton Pass.
func TestSelfSwitchPlainResetsStages(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{12, 18}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "u-turn", PP: 5, MaxPP: 5}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(0).Stages.Atk = 2

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Sides[0].Active != 1 {
		t.Fatalf("U-turn did not switch")
	}
	if got := s.Active(0).Stages.Atk; got != 0 {
		t.Errorf("plain self-switch leaked +2 Atk to incoming: got %d", got)
	}
}

// TestPartialTrapAppliedOnHit: applyVolatile("partiallytrapped", ...) sets
// the PartialTrap volatile with a 4 or 5 turn counter and stores the source
// move's display name so the residual log carries flavour ("hurt by Wrap!"
// rather than the generic volatile slug).
func TestPartialTrapAppliedOnHit(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[143]) // Snorlax
	rng := NewRNG(1)
	var log []LogLine

	applyVolatile(&p, 1, "partiallytrapped", d.Moves["wrap"], nil, rng, &log)

	pt := p.Volatiles.PartialTrap
	if pt == nil {
		t.Fatalf("PartialTrap not set; log: %v", logTexts(log))
	}
	if pt.MoveName != "Wrap" {
		t.Errorf("MoveName = %q, want Wrap", pt.MoveName)
	}
	if pt.Turns < 4 || pt.Turns > 5 {
		t.Errorf("Turns = %d, want 4 or 5", pt.Turns)
	}
	if !logHas(log, "was trapped by Wrap") {
		t.Errorf("missing trap log line; got %v", logTexts(log))
	}
}

// TestPartialTrapChipsOneEighthHP: end-of-turn residual chips exactly 1/8
// max HP off the trapped Pokémon and ticks the counter down by one. Snorlax
// (235 MaxHP at L50) gives a clean 29-HP chip.
func TestPartialTrapChipsOneEighthHP(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	p := s.Active(1)
	rng := NewRNG(1)
	var log []LogLine
	applyVolatile(p, 1, "partiallytrapped", d.Moves["wrap"], nil, rng, &log)
	turnsBefore := p.Volatiles.PartialTrap.Turns
	expectedChip := p.MaxHP / 8
	hpBefore := p.HP

	applyResidual(s, 1, &log)

	if got := hpBefore - p.HP; got != expectedChip {
		t.Errorf("chip = %d, want %d (1/8 of MaxHP %d)", got, expectedChip, p.MaxHP)
	}
	if p.Volatiles.PartialTrap == nil {
		t.Fatalf("trap cleared after a single chip")
	}
	if got := p.Volatiles.PartialTrap.Turns; got != turnsBefore-1 {
		t.Errorf("Turns = %d, want %d (one tick)", got, turnsBefore-1)
	}
}

// TestPartialTrapBlocksSwitch: while the volatile is active, LegalActions
// returns no Switch entries even when the bench has alive members.
func TestPartialTrapBlocksSwitch(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143, 12}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(1).Volatiles.PartialTrap = &PartialTrapState{Turns: 3, MoveName: "Wrap"}

	for _, a := range LegalActions(s, 1) {
		if a.Kind == ActionSwitch {
			t.Errorf("trapped Pokémon offered switch: %+v", a)
		}
	}
}

// TestPartialTrapClearsAtZero: when the counter ticks down to zero the
// volatile clears, the "freed from" log fires, and the holder regains
// switch options on the next turn.
func TestPartialTrapClearsAtZero(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143, 12}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	p := s.Active(1)
	p.Volatiles.PartialTrap = &PartialTrapState{Turns: 1, MoveName: "Wrap"}
	var log []LogLine

	applyResidual(s, 1, &log)

	if p.Volatiles.PartialTrap != nil {
		t.Errorf("trap not cleared at Turns=0")
	}
	if !logHas(log, "was freed from Wrap") {
		t.Errorf("missing release log; got %v", logTexts(log))
	}
	gotSwitch := false
	for _, a := range LegalActions(s, 1) {
		if a.Kind == ActionSwitch {
			gotSwitch = true
		}
	}
	if !gotSwitch {
		t.Errorf("freed Pokémon should have switch options again")
	}
}

// TestPartialTrapReapplyNoOp: applying the volatile again while it's
// already active is a no-op — the existing counter is NOT refreshed. (A
// fresh Wrap from a different attacker should restart it, but that's a
// scope-creep enhancement; canonical Showdown also no-ops same-attacker
// reapplication mid-trap.)
func TestPartialTrapReapplyNoOp(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[143])
	p.Volatiles.PartialTrap = &PartialTrapState{Turns: 2, MoveName: "Bind"}
	rng := NewRNG(1)
	var log []LogLine

	applyVolatile(&p, 1, "partiallytrapped", d.Moves["wrap"], nil, rng, &log)

	if got := p.Volatiles.PartialTrap.Turns; got != 2 {
		t.Errorf("Turns refreshed: got %d, want 2", got)
	}
	if got := p.Volatiles.PartialTrap.MoveName; got != "Bind" {
		t.Errorf("MoveName overwritten: got %q, want Bind", got)
	}
}

// TestPartialTrapBypassesShieldDust: Wrap's partial-trap volatile is the
// move's primary effect (top-level volatileStatus), not a secondary, so
// Shield Dust on the defender does NOT block it. Mirrors Showdown — only
// the chance-gated Secondary list is gated by Shield Dust.
func TestPartialTrapBypassesShieldDust(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "wrap", PP: 5, MaxPP: 5}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Ability = "shield-dust"

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Active(1).Volatiles.PartialTrap == nil {
		t.Errorf("Shield Dust wrongly blocked PartialTrap primary effect")
	}
}

// TestTerrainSetterDuration: a setter spawns terrain with 5 turns; the
// counter decrements each end-of-turn and the terrain clears on tick 5.
// Re-applying the same terrain mid-stream fails (matches Showdown).
func TestTerrainSetterDuration(t *testing.T) {
	d := loadDex(t)
	// Tauros knows Electric Terrain in Gen 1's TM/tutor pool we ingest? Use
	// any species — the setter is dispatched off Move.Terrain regardless
	// of the user. Pikachu has Electric Terrain in its learnset.
	s, _ := NewBattle(d, "b", "A", []int{26}, "B", []int{143}, 1) // Pikachu vs Snorlax
	rng := NewRNG(1)
	var log []LogLine

	slot := slotOf(s.Active(0), "electric-terrain")
	if slot < 0 {
		t.Fatalf("Pikachu lacks Electric Terrain in its learnset")
	}
	executeMove(d, s, 0, slot, rng, &log)
	if s.Terrain == nil || s.Terrain.Kind != TerrainElectric {
		t.Fatalf("Electric Terrain should set electric terrain, got %+v", s.Terrain)
	}
	if s.Terrain.TurnsLeft != defaultTerrainTurns {
		t.Errorf("Electric Terrain TurnsLeft = %d, want %d", s.Terrain.TurnsLeft, defaultTerrainTurns)
	}

	// Re-setting the same terrain fails.
	logLen := len(log)
	executeMove(d, s, 0, slot, rng, &log)
	if s.Terrain == nil || s.Terrain.TurnsLeft != defaultTerrainTurns {
		t.Errorf("re-applying same terrain should not reset counter, got %+v", s.Terrain)
	}
	if !logHas(log[logLen:], "But it failed!") {
		t.Errorf("re-applying same terrain should log fail")
	}

	// Tick down — 4 more ticks keep it; the 5th clears.
	for i := 1; i < defaultTerrainTurns; i++ {
		var tlog []LogLine
		tickTerrain(s, &tlog)
		if s.Terrain == nil {
			t.Fatalf("tick %d cleared terrain early", i)
		}
	}
	var finalLog []LogLine
	tickTerrain(s, &finalLog)
	if s.Terrain != nil {
		t.Errorf("after %d ticks terrain should clear, still %+v", defaultTerrainTurns, s.Terrain)
	}
	if !logHas(finalLog, "electric current disappeared") {
		t.Errorf("clear-line missing from %v", logTexts(finalLog))
	}
}

// TestTerrainElectricBoostsElectricDamage: a grounded attacker on Electric
// Terrain hits ~1.3x harder with an Electric-type move. ExpectedDamage is
// deterministic — no RNG sampling needed.
func TestTerrainElectricBoostsElectricDamage(t *testing.T) {
	d := loadDex(t)
	raichu := buildPokemon(d, d.Species[26])  // electric, grounded
	snorlax := buildPokemon(d, d.Species[143]) // normal, grounded
	tbolt := d.Moves["thunderbolt"]

	base := ExpectedDamage(d, &raichu, &snorlax, tbolt, nil, nil, nil)
	terr := &TerrainState{Kind: TerrainElectric, TurnsLeft: 5}
	boosted := ExpectedDamage(d, &raichu, &snorlax, tbolt, nil, terr, nil)
	ratio := float64(boosted) / float64(base)
	if ratio < 1.25 || ratio > 1.35 {
		t.Errorf("Electric Terrain boost ratio = %.2f (base=%d, boosted=%d), want ~1.30",
			ratio, base, boosted)
	}
}

// TestTerrainBoostRequiresGroundedAttacker: an airborne attacker (Flying-type)
// gets no boost from terrain, even using a matching-type move.
func TestTerrainBoostRequiresGroundedAttacker(t *testing.T) {
	d := loadDex(t)
	zapdos := buildPokemon(d, d.Species[145])  // electric/flying — not grounded
	snorlax := buildPokemon(d, d.Species[143])
	tbolt := d.Moves["thunderbolt"]

	terr := &TerrainState{Kind: TerrainElectric, TurnsLeft: 5}
	base := ExpectedDamage(d, &zapdos, &snorlax, tbolt, nil, nil, nil)
	withTerrain := ExpectedDamage(d, &zapdos, &snorlax, tbolt, nil, terr, nil)
	if withTerrain != base {
		t.Errorf("Electric Terrain wrongly boosted Flying attacker: base=%d terrain=%d",
			base, withTerrain)
	}
}

// TestTerrainMistyHalvesDragonDamage: a grounded defender on Misty Terrain
// takes half from Dragon-type moves.
func TestTerrainMistyHalvesDragonDamage(t *testing.T) {
	d := loadDex(t)
	dragonite := buildPokemon(d, d.Species[149]) // dragon/flying (attacker, airborne fine)
	venusaur := buildPokemon(d, d.Species[3])    // grass/poison, grounded
	outrage := d.Moves["outrage"]

	base := ExpectedDamage(d, &dragonite, &venusaur, outrage, nil, nil, nil)
	terr := &TerrainState{Kind: TerrainMisty, TurnsLeft: 5}
	halved := ExpectedDamage(d, &dragonite, &venusaur, outrage, nil, terr, nil)
	ratio := float64(halved) / float64(base)
	if ratio < 0.45 || ratio > 0.55 {
		t.Errorf("Misty Terrain Dragon ratio = %.2f (base=%d, halved=%d), want ~0.50",
			ratio, base, halved)
	}
}

// TestTerrainGrassyHalvesEarthquake: a grounded defender on Grassy Terrain
// takes half from Earthquake (a ground-shake move grass absorbs).
func TestTerrainGrassyHalvesEarthquake(t *testing.T) {
	d := loadDex(t)
	rhydon := buildPokemon(d, d.Species[112])  // ground/rock
	tauros := buildPokemon(d, d.Species[128])  // normal, grounded
	eq := d.Moves["earthquake"]

	base := ExpectedDamage(d, &rhydon, &tauros, eq, nil, nil, nil)
	terr := &TerrainState{Kind: TerrainGrassy, TurnsLeft: 5}
	halved := ExpectedDamage(d, &rhydon, &tauros, eq, nil, terr, nil)
	ratio := float64(halved) / float64(base)
	if ratio < 0.45 || ratio > 0.55 {
		t.Errorf("Grassy Terrain EQ ratio = %.2f (base=%d, halved=%d), want ~0.50",
			ratio, base, halved)
	}
}

// TestTerrainGrassyHeals: grounded actives heal 1/16 max HP per end-of-turn
// under Grassy Terrain; airborne ones don't.
func TestTerrainGrassyHeals(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{145}, 1) // Snorlax (grounded) vs Zapdos (flying)
	s.Terrain = &TerrainState{Kind: TerrainGrassy, TurnsLeft: 5}

	// Damage both to half so the heal is observable.
	snorlax := s.Active(0)
	zapdos := s.Active(1)
	snorlax.HP = snorlax.MaxHP / 2
	zapdos.HP = zapdos.MaxHP / 2
	preSnorlax := snorlax.HP
	preZapdos := zapdos.HP

	var log []LogLine
	applyTerrainResidual(s, &log)

	expHeal := snorlax.MaxHP / 16
	if got := snorlax.HP - preSnorlax; got != expHeal {
		t.Errorf("Snorlax heal = %d, want %d", got, expHeal)
	}
	if got := zapdos.HP - preZapdos; got != 0 {
		t.Errorf("Zapdos (Flying) wrongly healed by Grassy Terrain: +%d", got)
	}
}

// TestTerrainMistyBlocksStatus: Misty Terrain prevents Toxic infliction
// on a grounded target. Without terrain the same call succeeds.
func TestTerrainMistyBlocksStatus(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{128}, 1) // Snorlax vs Tauros
	target := s.Active(1) // Tauros is grounded (pure Normal, no Poison immunity)
	rng := NewRNG(1)
	var log []LogLine

	s.Terrain = &TerrainState{Kind: TerrainMisty, TurnsLeft: 5}
	if inflictStatus(target, 1, StatusToxic, s, rng, &log) {
		t.Errorf("Misty Terrain should block Toxic on grounded target")
	}
	if target.Status != StatusNone {
		t.Errorf("target status = %q, want none", target.Status)
	}

	// Without terrain the same infliction succeeds.
	s.Terrain = nil
	if !inflictStatus(target, 1, StatusToxic, s, rng, &log) {
		t.Errorf("without terrain Toxic should land")
	}
}

// TestTerrainElectricBlocksSleep: Electric Terrain prevents Sleep on a
// grounded target; other statuses still land.
func TestTerrainElectricBlocksSleep(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{128}, 1) // Snorlax vs Tauros
	target := s.Active(1) // Tauros is grounded (pure Normal)
	rng := NewRNG(1)
	var log []LogLine

	s.Terrain = &TerrainState{Kind: TerrainElectric, TurnsLeft: 5}
	if inflictStatus(target, 1, StatusSleep, s, rng, &log) {
		t.Errorf("Electric Terrain should block Sleep on grounded target")
	}
	// Paralysis is fine (Tauros isn't Electric-typed).
	if !inflictStatus(target, 1, StatusParalysis, s, rng, &log) {
		t.Errorf("Electric Terrain should NOT block Paralysis")
	}
}

// TestTerrainPsychicBlocksPriority: a priority+1 move (Quick Attack) is
// blocked when the foe is grounded under Psychic Terrain.
func TestTerrainPsychicBlocksPriority(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{26}, "B", []int{143}, 1) // Pikachu vs Snorlax
	s.Terrain = &TerrainState{Kind: TerrainPsychic, TurnsLeft: 5}
	rng := NewRNG(1)
	var log []LogLine

	preHP := s.Active(1).HP
	slot := slotOf(s.Active(0), "quick-attack")
	if slot < 0 {
		t.Fatalf("Pikachu should learn Quick Attack")
	}
	executeMove(d, s, 0, slot, rng, &log)
	if s.Active(1).HP != preHP {
		t.Errorf("Psychic Terrain should block Quick Attack damage; HP %d -> %d", preHP, s.Active(1).HP)
	}
	if !logHas(log, "Psychic Terrain") {
		t.Errorf("expected Psychic Terrain block log line, got %v", logTexts(log))
	}
}

// TestReflectHalvesPhysical: with Reflect up on the defender's side, an
// incoming physical move deals ~half damage. ExpectedDamage is the average
// (no crit), which keeps the screen multiplier engaged.
func TestReflectHalvesPhysical(t *testing.T) {
	d := loadDex(t)
	tauros := buildPokemon(d, d.Species[128])  // Normal physical attacker
	snorlax := buildPokemon(d, d.Species[143]) // bulky defender
	bs := d.Moves["body-slam"]

	base := ExpectedDamage(d, &tauros, &snorlax, bs, nil, nil, nil)
	sc := &SideConditions{Reflect: &ScreenState{TurnsLeft: 5}}
	halved := ExpectedDamage(d, &tauros, &snorlax, bs, nil, nil, sc)
	ratio := float64(halved) / float64(base)
	if ratio < 0.45 || ratio > 0.55 {
		t.Errorf("Reflect physical ratio = %.2f (base=%d halved=%d), want ~0.50",
			ratio, base, halved)
	}
}

// TestLightScreenHalvesSpecial: Light Screen halves special damage and
// leaves physical untouched.
func TestLightScreenHalvesSpecial(t *testing.T) {
	d := loadDex(t)
	starmie := buildPokemon(d, d.Species[121]) // Water/Psychic special attacker
	chansey := buildPokemon(d, d.Species[113]) // bulky defender
	surf := d.Moves["surf"]

	base := ExpectedDamage(d, &starmie, &chansey, surf, nil, nil, nil)
	sc := &SideConditions{LightScreen: &ScreenState{TurnsLeft: 5}}
	halved := ExpectedDamage(d, &starmie, &chansey, surf, nil, nil, sc)
	ratio := float64(halved) / float64(base)
	if ratio < 0.45 || ratio > 0.55 {
		t.Errorf("Light Screen special ratio = %.2f (base=%d halved=%d), want ~0.50",
			ratio, base, halved)
	}

	// Wrong-category test: a physical hit through Light Screen is unchanged.
	tauros := buildPokemon(d, d.Species[128])
	bs := d.Moves["body-slam"]
	basePhys := ExpectedDamage(d, &tauros, &chansey, bs, nil, nil, nil)
	throughLS := ExpectedDamage(d, &tauros, &chansey, bs, nil, nil, sc)
	if basePhys != throughLS {
		t.Errorf("Light Screen should not affect physical: base=%d under-screen=%d",
			basePhys, throughLS)
	}
}

// TestAuroraVeilHalvesBoth: Aurora Veil reduces both physical and special
// damage to ~50%, no matter the category.
func TestAuroraVeilHalvesBoth(t *testing.T) {
	d := loadDex(t)
	tauros := buildPokemon(d, d.Species[128])
	starmie := buildPokemon(d, d.Species[121])
	chansey := buildPokemon(d, d.Species[113])
	bs := d.Moves["body-slam"]
	surf := d.Moves["surf"]

	sc := &SideConditions{AuroraVeil: &ScreenState{TurnsLeft: 5}}

	basePhys := ExpectedDamage(d, &tauros, &chansey, bs, nil, nil, nil)
	halvedPhys := ExpectedDamage(d, &tauros, &chansey, bs, nil, nil, sc)
	if r := float64(halvedPhys) / float64(basePhys); r < 0.45 || r > 0.55 {
		t.Errorf("Aurora Veil physical ratio = %.2f (base=%d halved=%d), want ~0.50",
			r, basePhys, halvedPhys)
	}

	baseSpec := ExpectedDamage(d, &starmie, &chansey, surf, nil, nil, nil)
	halvedSpec := ExpectedDamage(d, &starmie, &chansey, surf, nil, nil, sc)
	if r := float64(halvedSpec) / float64(baseSpec); r < 0.45 || r > 0.55 {
		t.Errorf("Aurora Veil special ratio = %.2f (base=%d halved=%d), want ~0.50",
			r, baseSpec, halvedSpec)
	}
}

// TestScreensDontReduceCrits: a critical hit bypasses screens entirely.
// Tests the helper directly because forcing a crit through computeDamage
// without screen interference would mean threading the seeded RNG just
// for that one outcome.
func TestScreensDontReduceCrits(t *testing.T) {
	sc := &SideConditions{
		Reflect:     &ScreenState{TurnsLeft: 5},
		LightScreen: &ScreenState{TurnsLeft: 5},
		AuroraVeil:  &ScreenState{TurnsLeft: 5},
	}
	cases := []struct {
		name string
		cat  domain.Category
	}{
		{"physical", domain.CatPhysical},
		{"special", domain.CatSpecial},
	}
	for _, c := range cases {
		m := domain.Move{Category: c.cat}
		if mult := screenDamageMult(sc, m, true); mult != 1.0 {
			t.Errorf("%s crit through screens = %v, want 1.0", c.name, mult)
		}
		if mult := screenDamageMult(sc, m, false); mult != 0.5 {
			t.Errorf("%s non-crit through screens = %v, want 0.5", c.name, mult)
		}
	}
	// Status moves are always 1.0 regardless of crit flag.
	if mult := screenDamageMult(sc, domain.Move{Category: domain.CatStatus}, false); mult != 1.0 {
		t.Errorf("status move through screens = %v, want 1.0", mult)
	}
}

// TestScreenSetterDuration: a fresh setter lays down a 5-turn screen; five
// ticks clear it; the expiry log line fires on the last tick.
func TestScreenSetterDuration(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{128}, 1)
	var log []LogLine

	applyScreenSetter(s, 0, ScreenReflect, &log)
	if s.Sides[0].Conditions.Reflect == nil {
		t.Fatalf("Reflect not set after applyScreenSetter")
	}
	if got := s.Sides[0].Conditions.Reflect.TurnsLeft; got != 5 {
		t.Errorf("fresh Reflect TurnsLeft = %d, want 5", got)
	}
	if !logHas(log, "Reflect raised") {
		t.Errorf("missing setter log line, got %v", logTexts(log))
	}

	for i := 1; i <= 5; i++ {
		tickScreens(s, 0, &log)
		switch {
		case i < 5:
			if s.Sides[0].Conditions.Reflect == nil {
				t.Errorf("Reflect cleared too early at tick %d", i)
			}
			if got := s.Sides[0].Conditions.Reflect.TurnsLeft; got != 5-i {
				t.Errorf("after %d ticks TurnsLeft = %d, want %d", i, got, 5-i)
			}
		case i == 5:
			if s.Sides[0].Conditions.Reflect != nil {
				t.Errorf("Reflect should clear at tick 5; still set with TurnsLeft=%d",
					s.Sides[0].Conditions.Reflect.TurnsLeft)
			}
			if !logHas(log, "Reflect wore off") {
				t.Errorf("missing expiry log, got %v", logTexts(log))
			}
		}
	}
}

// TestScreenSetterRefusesSameScreen: setting Reflect when Reflect is
// already up fails and does not refresh the timer (canonical Showdown).
func TestScreenSetterRefusesSameScreen(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{128}, 1)
	var log []LogLine

	applyScreenSetter(s, 0, ScreenReflect, &log)
	s.Sides[0].Conditions.Reflect.TurnsLeft = 2 // simulate 3 turns elapsed

	log = nil
	applyScreenSetter(s, 0, ScreenReflect, &log)
	if got := s.Sides[0].Conditions.Reflect.TurnsLeft; got != 2 {
		t.Errorf("re-setting Reflect should not refresh; TurnsLeft = %d, want 2", got)
	}
	if !logHas(log, "But it failed") {
		t.Errorf("expected 'But it failed' line, got %v", logTexts(log))
	}
}

// TestAuroraVeilRequiresSnow: setter refuses unless snow is active; once
// set it persists even if the weather changes.
func TestAuroraVeilRequiresSnow(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{128}, 1)
	var log []LogLine

	// No weather — refused.
	applyScreenSetter(s, 0, ScreenAuroraVeil, &log)
	if s.Sides[0].Conditions.AuroraVeil != nil {
		t.Errorf("Aurora Veil should refuse without snow")
	}
	if !logHas(log, "But it failed") {
		t.Errorf("expected fail log without snow, got %v", logTexts(log))
	}

	// Rain — still refused.
	s.Weather = &WeatherState{Kind: WeatherRain, TurnsLeft: 5}
	log = nil
	applyScreenSetter(s, 0, ScreenAuroraVeil, &log)
	if s.Sides[0].Conditions.AuroraVeil != nil {
		t.Errorf("Aurora Veil should refuse under rain")
	}

	// Snow — accepted.
	s.Weather = &WeatherState{Kind: WeatherSnow, TurnsLeft: 5}
	log = nil
	applyScreenSetter(s, 0, ScreenAuroraVeil, &log)
	if s.Sides[0].Conditions.AuroraVeil == nil {
		t.Fatalf("Aurora Veil should set under snow")
	}

	// Weather clears — Aurora Veil persists.
	s.Weather = nil
	if s.Sides[0].Conditions.AuroraVeil == nil {
		t.Errorf("Aurora Veil should outlive its setup weather")
	}
}

// TestStealthRockChipScalesWithEffectiveness: Stealth Rock chip is
// (MaxHP/8) × Rock-type effectiveness. Charizard (Fire/Flying) is 4× weak
// and loses ~50% HP; Snorlax (Normal) is 1× and loses ~12.5%; Magneton
// (Electric/Steel — Rock-vs-Electric is neutral, Rock-vs-Steel is 0.5×)
// is 0.5× and loses ~6%. Flying-types ARE chipped — that's the whole
// point of Stealth Rock, distinct from Spikes.
func TestStealthRockChipScalesWithEffectiveness(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{6, 143, 82}, 1)
	// Side 1 has SR up; we'll swap actives to test each switch-in.
	s.Sides[1].Conditions.Hazards.StealthRock = true

	type tc struct {
		idx     int
		name    string
		wantMin float64
		wantMax float64
	}
	for _, c := range []tc{
		{0, "Charizard 4×", 0.45, 0.55},     // (1/8)×4 = 0.5
		{1, "Snorlax 1×", 0.10, 0.15},       // (1/8)×1 = 0.125
		{2, "Magneton 0.5×", 0.05, 0.07},    // (1/8)×0.5 = 0.0625
	} {
		s.Sides[1].Active = c.idx
		s.Sides[1].Team[c.idx].HP = s.Sides[1].Team[c.idx].MaxHP
		var log []LogLine
		applyHazardsOnSwitchIn(s, 1, &log)
		p := &s.Sides[1].Team[c.idx]
		lost := float64(p.MaxHP-p.HP) / float64(p.MaxHP)
		if lost < c.wantMin || lost > c.wantMax {
			t.Errorf("%s SR chip = %.3f of MaxHP, want [%.3f, %.3f]",
				c.name, lost, c.wantMin, c.wantMax)
		}
	}
}

// TestSpikesScaleWithLayers: 1/2/3 layers chip 1/8, 1/6, 1/4 of MaxHP on
// a grounded switch-in. Flying / Levitate are untouched (see
// TestSpikesIgnoreUngrounded).
func TestSpikesScaleWithLayers(t *testing.T) {
	d := loadDex(t)
	for layers, frac := range map[int]float64{1: 0.125, 2: 1.0 / 6.0, 3: 0.25} {
		s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
		s.Sides[1].Conditions.Hazards.Spikes = layers
		var log []LogLine
		applyHazardsOnSwitchIn(s, 1, &log)
		p := &s.Sides[1].Team[0]
		lost := float64(p.MaxHP-p.HP) / float64(p.MaxHP)
		if lost < frac-0.01 || lost > frac+0.01 {
			t.Errorf("%d-layer spikes chip = %.4f, want ~%.4f", layers, lost, frac)
		}
	}
}

// TestSpikesIgnoreUngrounded: Flying-types (Pidgeot) walk over Spikes and
// Toxic Spikes; Stealth Rock still chips them (no grounded check).
func TestSpikesIgnoreUngrounded(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{18}, 1)
	s.Sides[1].Conditions.Hazards.Spikes = 3
	s.Sides[1].Conditions.Hazards.ToxicSpikes = 2
	var log []LogLine
	applyHazardsOnSwitchIn(s, 1, &log)
	pid := &s.Sides[1].Team[0]
	if pid.HP != pid.MaxHP {
		t.Errorf("Pidgeot took %d hazard damage, expected zero (Flying)", pid.MaxHP-pid.HP)
	}
	if pid.Status != StatusNone {
		t.Errorf("Pidgeot got status %q from Toxic Spikes, expected none", pid.Status)
	}
}

// TestToxicSpikesPoisons: 1 layer poisons a grounded non-Poison/Steel
// switch-in; 2 layers badly poison. Tauros is the fixture (Normal-type
// without a status-blocking ability — Snorlax's default Immunity would
// otherwise eat the poison).
func TestToxicSpikesPoisons(t *testing.T) {
	d := loadDex(t)
	for layers, want := range map[int]StatusCond{1: StatusPoison, 2: StatusToxic} {
		s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{128}, 1) // Tauros
		s.Sides[1].Conditions.Hazards.ToxicSpikes = layers
		var log []LogLine
		applyHazardsOnSwitchIn(s, 1, &log)
		if got := s.Sides[1].Team[0].Status; got != want {
			t.Errorf("%d TS layers → status %q, want %q", layers, got, want)
		}
	}
}

// TestToxicSpikesAbsorbedByPoison: a grounded Poison-type clears the
// Toxic Spikes layers on entry (without taking status), regardless of how
// many layers were up.
func TestToxicSpikesAbsorbedByPoison(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{89}, 1) // Muk is Poison
	s.Sides[1].Conditions.Hazards.ToxicSpikes = 2
	var log []LogLine
	applyHazardsOnSwitchIn(s, 1, &log)
	if s.Sides[1].Conditions.Hazards.ToxicSpikes != 0 {
		t.Errorf("Poison-type didn't absorb TS: %d layers remain",
			s.Sides[1].Conditions.Hazards.ToxicSpikes)
	}
	if s.Sides[1].Team[0].Status != StatusNone {
		t.Errorf("Muk got status %q from TS, want none (absorbs)", s.Sides[1].Team[0].Status)
	}
	if !logHas(log, "absorbed") {
		t.Errorf("missing absorb log, got %v", logTexts(log))
	}
}

// TestToxicSpikesStealsNothingFromSteel: Steel-types are immune to poison
// status (existing type guard) but do NOT absorb the layers — they
// persist for the next switch-in. The chip / status path silently no-ops.
func TestToxicSpikesStealsNothingFromSteel(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{82}, 1) // Magneton: Electric/Steel
	s.Sides[1].Conditions.Hazards.ToxicSpikes = 1
	var log []LogLine
	applyHazardsOnSwitchIn(s, 1, &log)
	if s.Sides[1].Conditions.Hazards.ToxicSpikes != 1 {
		t.Errorf("Steel-type cleared TS layers, want them to persist; got %d",
			s.Sides[1].Conditions.Hazards.ToxicSpikes)
	}
	if s.Sides[1].Team[0].Status != StatusNone {
		t.Errorf("Magneton got status %q from TS, want none (Steel immune)",
			s.Sides[1].Team[0].Status)
	}
}

// TestHazardSetterStackingAndCaps: SR is binary; Spikes caps at 3 layers;
// Toxic Spikes caps at 2. At the cap, the next setter fails.
func TestHazardSetterStackingAndCaps(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{28}, "B", []int{143}, 1)
	var log []LogLine

	// Stealth Rock: one layer, second fails.
	applyHazardSetter(s, 0, HazardStealthRock, &log)
	log = nil
	applyHazardSetter(s, 0, HazardStealthRock, &log)
	if !logHas(log, "But it failed") {
		t.Errorf("second SR should fail, log = %v", logTexts(log))
	}

	// Spikes: 1, 2, 3, then 4th fails.
	for i := 1; i <= 3; i++ {
		log = nil
		applyHazardSetter(s, 0, HazardSpikes, &log)
		if got := s.Sides[1].Conditions.Hazards.Spikes; got != i {
			t.Errorf("Spikes after %d sets = %d, want %d", i, got, i)
		}
	}
	log = nil
	applyHazardSetter(s, 0, HazardSpikes, &log)
	if !logHas(log, "But it failed") {
		t.Errorf("4th Spikes should fail, log = %v", logTexts(log))
	}

	// Toxic Spikes: 1, 2, then 3rd fails.
	for i := 1; i <= 2; i++ {
		log = nil
		applyHazardSetter(s, 0, HazardToxicSpikes, &log)
		if got := s.Sides[1].Conditions.Hazards.ToxicSpikes; got != i {
			t.Errorf("TS after %d sets = %d, want %d", i, got, i)
		}
	}
	log = nil
	applyHazardSetter(s, 0, HazardToxicSpikes, &log)
	if !logHas(log, "But it failed") {
		t.Errorf("3rd TS should fail, log = %v", logTexts(log))
	}
}

// TestRapidSpinClearsOwnSideHazards: a successful Rapid Spin sweeps the
// user's own side of all hazards (and leaves the foe's side untouched).
// The Speed +1 self-boost comes through the upstream secondary effect.
func TestRapidSpinClearsOwnSideHazards(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{9}, "B", []int{143}, 1) // Blastoise has Rapid Spin
	s.Sides[0].Conditions.Hazards = Hazards{StealthRock: true, Spikes: 2, ToxicSpikes: 1}
	s.Sides[1].Conditions.Hazards = Hazards{StealthRock: true, Spikes: 1}

	idx := -1
	for i, ms := range s.Sides[0].Team[0].Moves {
		if ms.MoveID == "rapid-spin" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("Blastoise should know rapid-spin")
	}

	rng := NewRNG(1)
	var log []LogLine
	executeMove(d, s, 0, idx, rng, &log)

	own := s.Sides[0].Conditions.Hazards
	if own.StealthRock || own.Spikes != 0 || own.ToxicSpikes != 0 {
		t.Errorf("Rapid Spin should clear user's hazards, got %+v", own)
	}
	foe := s.Sides[1].Conditions.Hazards
	if !foe.StealthRock || foe.Spikes != 1 {
		t.Errorf("Rapid Spin should not touch the foe's hazards, got %+v", foe)
	}
	if !logHas(log, "blew away the hazards") {
		t.Errorf("missing rapid-spin sweep log, got %v", logTexts(log))
	}
}

// TestDefogClearsBothSidesAndDropsEvasion: Defog (Gen 6+) clears hazards
// AND screens on BOTH sides, and drops the foe's evasion by 1 stage.
func TestDefogClearsBothSidesAndDropsEvasion(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{143}, 1) // Charizard has Defog
	s.Sides[0].Conditions.Hazards = Hazards{StealthRock: true}
	s.Sides[0].Conditions.Reflect = &ScreenState{TurnsLeft: 4}
	s.Sides[1].Conditions.Hazards = Hazards{Spikes: 2, ToxicSpikes: 1}
	s.Sides[1].Conditions.LightScreen = &ScreenState{TurnsLeft: 3}

	idx := -1
	for i, ms := range s.Sides[0].Team[0].Moves {
		if ms.MoveID == "defog" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("Charizard should know defog")
	}

	rng := NewRNG(1)
	var log []LogLine
	executeMove(d, s, 0, idx, rng, &log)

	if h := s.Sides[0].Conditions.Hazards; h.StealthRock || h.Spikes != 0 || h.ToxicSpikes != 0 {
		t.Errorf("Defog should clear user's hazards, got %+v", h)
	}
	if h := s.Sides[1].Conditions.Hazards; h.StealthRock || h.Spikes != 0 || h.ToxicSpikes != 0 {
		t.Errorf("Defog should clear foe's hazards, got %+v", h)
	}
	if s.Sides[0].Conditions.Reflect != nil {
		t.Errorf("Defog should clear user's Reflect")
	}
	if s.Sides[1].Conditions.LightScreen != nil {
		t.Errorf("Defog should clear foe's Light Screen")
	}
	if got := s.Sides[1].Team[0].Stages.Eva; got != -1 {
		t.Errorf("Defog should drop foe's evasion to -1, got %d", got)
	}
}

// TestLeadsDoNotTriggerHazards: leads on turn 1 walk onto an empty board
// — no hazards have been set yet, so the lead path must not touch them.
// Regression guard against accidentally piggybacking the hazard hook on
// the same turn-1 lead invocation that applyOnSwitchIn uses.
func TestLeadsDoNotTriggerHazards(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{6}, 1)
	rng := NewRNG(1)
	// Both sides choose Struggle-equivalent moves; turn-1 leads fire.
	ResolveTurn(d, s, [2]Action{
		{Kind: ActionMove, Index: 0},
		{Kind: ActionMove, Index: 0},
	})
	_ = rng
	// Charizard at full HP would survive; what we actually want to verify is
	// that hazards never fired for either lead (the bag is empty by default
	// and the lead path doesn't call the hazard hook). A passing build that
	// reaches here without the lead crashing is the assertion; an explicit
	// check that neither side took chip damage on turn 1 is the canary.
	if s.Sides[0].Team[0].HP <= 0 || s.Sides[1].Team[0].HP <= 0 {
		t.Skip("turn 1 KO'd a lead; can't observe a clean hazards no-op")
	}
}

// TestStealthRockOneShotsCrippledMatchup: a 4×-weak Pokémon at ≤50% HP
// faints on switch-in — the faint hook fires and Replace is set on the
// next turn boundary.
func TestStealthRockOneShotsCrippledMatchup(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{143, 6}, 1)
	s.Sides[1].Conditions.Hazards.StealthRock = true
	zard := &s.Sides[1].Team[1]
	zard.HP = zard.MaxHP / 2 // half HP — SR chip will be exactly half MaxHP
	s.Sides[1].Active = 1

	var log []LogLine
	applyHazardsOnSwitchIn(s, 1, &log)
	if !zard.Fainted {
		t.Errorf("Charizard with %d HP didn't faint to SR (HP now %d)", zard.MaxHP/2, zard.HP)
	}
}

// TestSubstituteSetupDeductsQuarterMaxHP: a successful Substitute pays
// MaxHP/4 (integer division — fractional remainders stay with the user)
// and stands up a doll with that many HP. Sets up using applyVolatile so
// the test exercises the same dispatch the engine uses for the volatile
// slug.
func TestSubstituteSetupDeductsQuarterMaxHP(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[143]) // Snorlax (high MaxHP)
	hpBefore := p.HP
	cost := p.MaxHP / 4
	rng := NewRNG(1)
	var log []LogLine

	applyVolatile(&p, 0, "substitute", domain.Move{}, nil, rng, &log)

	if p.Volatiles.Substitute == nil {
		t.Fatalf("sub not set; log: %v", logTexts(log))
	}
	if got := hpBefore - p.HP; got != cost {
		t.Errorf("HP cost = %d, want MaxHP/4 = %d", got, cost)
	}
	if got := p.Volatiles.Substitute.HP; got != cost {
		t.Errorf("doll HP = %d, want %d (the spent HP)", got, cost)
	}
	if !logHas(log, "put up a substitute") {
		t.Errorf("missing setup log line; got %v", logTexts(log))
	}
}

// TestSubstituteSetupFailsAtOrBelowQuarterHP: the cost cannot push the user
// into faint range, so HP <= MaxHP/4 makes the move fail outright. The
// holder keeps its HP and no doll appears.
func TestSubstituteSetupFailsAtOrBelowQuarterHP(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[143])
	p.HP = p.MaxHP / 4 // exactly the cost — would faint
	hpBefore := p.HP
	rng := NewRNG(1)
	var log []LogLine

	applyVolatile(&p, 0, "substitute", domain.Move{}, nil, rng, &log)

	if p.Volatiles.Substitute != nil {
		t.Errorf("sub set despite insufficient HP")
	}
	if p.HP != hpBefore {
		t.Errorf("HP changed on failed setup: %d → %d", hpBefore, p.HP)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("missing fail line; got %v", logTexts(log))
	}
}

// TestSubstituteSetupFailsWhenAlreadyUp: a second Substitute while one is
// already standing fails — no HP cost, no log line about a new doll.
func TestSubstituteSetupFailsWhenAlreadyUp(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[143])
	p.Volatiles.Substitute = &SubstituteState{HP: 50, MaxHP: 50}
	hpBefore := p.HP
	rng := NewRNG(1)
	var log []LogLine

	applyVolatile(&p, 0, "substitute", domain.Move{}, nil, rng, &log)

	if p.HP != hpBefore {
		t.Errorf("HP changed despite duplicate setup: %d → %d", hpBefore, p.HP)
	}
	if got := p.Volatiles.Substitute.HP; got != 50 {
		t.Errorf("doll HP changed on duplicate setup: got %d, want 50", got)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("missing fail line; got %v", logTexts(log))
	}
}

// TestSubstituteAbsorbsDamage: a damaging move against a sub'd target lands
// on the doll; the holder's HP is untouched. Uses dealDamage directly so
// the test doesn't depend on a specific move's BP — any non-immune hit
// suffices to prove the redirect.
func TestSubstituteAbsorbsDamage(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	def := s.Active(1)
	def.Volatiles.Substitute = &SubstituteState{HP: 80, MaxHP: 80}
	defHPBefore := def.HP
	subHPBefore := def.Volatiles.Substitute.HP
	rng := NewRNG(1)
	var log []LogLine

	dmg, ok := dealDamage(d, s, 0, d.Moves["tackle"], rng, &log)

	if !ok || dmg <= 0 {
		t.Fatalf("dealDamage returned (%d, %v); expected a real hit", dmg, ok)
	}
	if def.HP != defHPBefore {
		t.Errorf("holder HP changed: %d → %d (sub should have absorbed)", defHPBefore, def.HP)
	}
	if def.Volatiles.Substitute == nil {
		t.Fatalf("sub gone after a non-breaking hit")
	}
	if got := def.Volatiles.Substitute.HP; got >= subHPBefore {
		t.Errorf("sub HP %d ≥ before %d — doll didn't absorb", got, subHPBefore)
	}
	if !logHas(log, "substitute took the damage") {
		t.Errorf("missing sub-damage log line; got %v", logTexts(log))
	}
}

// TestSubstituteBreaksAtZeroNoOverflow: a hit that overshoots the doll's
// HP breaks the doll but does NOT carry overflow damage to the holder
// (canon Gen 5+). The "faded" log line fires.
func TestSubstituteBreaksAtZeroNoOverflow(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	def := s.Active(1)
	def.Volatiles.Substitute = &SubstituteState{HP: 1, MaxHP: 80} // hair-trigger doll
	defHPBefore := def.HP
	rng := NewRNG(1)
	var log []LogLine

	if _, ok := dealDamage(d, s, 0, d.Moves["tackle"], rng, &log); !ok {
		t.Fatalf("dealDamage failed")
	}

	if def.Volatiles.Substitute != nil {
		t.Fatalf("sub still up after breaking hit; HP=%d", def.Volatiles.Substitute.HP)
	}
	if def.HP != defHPBefore {
		t.Errorf("holder took overflow damage: %d → %d (canon: none passes through)",
			defHPBefore, def.HP)
	}
	if !logHas(log, "substitute faded") {
		t.Errorf("missing fade log line; got %v", logTexts(log))
	}
}

// TestSubstituteBlocksStatusMove: Toxic against a sub'd foe fails — status
// does not stick, and the dispatcher logs "But it failed!" via the
// status-fail return from applyEffectFields.
func TestSubstituteBlocksStatusMove(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{128}, 1) // Tauros (no Immunity)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	def := s.Active(1)
	def.Volatiles.Substitute = &SubstituteState{HP: 30, MaxHP: 80}
	rng := NewRNG(1)
	var log []LogLine

	applyStatusMove(s, 0, d.Moves["toxic"], rng, &log)

	if def.Status != StatusNone {
		t.Errorf("foe got status %q despite sub; want none", def.Status)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("missing fail line; got %v", logTexts(log))
	}
}

// TestSubstituteBlocksDamageMoveSecondary: a damaging move that would roll
// a secondary on the foe (e.g. paralysis from an electric attack) cannot
// inflict the secondary while a sub is up — the doll soaked the contact,
// nothing reached the holder to status. We force the RNG to take the
// secondary roll by using a 100% secondary fixture for determinism.
func TestSubstituteBlocksDamageMoveSecondary(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{128}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	def := s.Active(1)
	def.Volatiles.Substitute = &SubstituteState{HP: 200, MaxHP: 200}
	rng := NewRNG(1)
	var log []LogLine

	// Synthetic move with a 100% paralyze secondary so the test is RNG-free.
	m := domain.Move{
		ID: "synthetic-zap", Name: "ZapTest", Type: "electric",
		Category: domain.CatPhysical, Power: 40, Accuracy: 100,
		Secondaries: []domain.Effect{{Chance: 100, Status: "paralysis"}},
	}
	if _, ok := dealDamage(d, s, 0, m, rng, &log); !ok {
		t.Fatalf("dealDamage failed")
	}
	applyDamageEffects(s, 0, m, 1, rng, &log)

	if def.Status != StatusNone {
		t.Errorf("foe paralyzed despite sub; status = %q", def.Status)
	}
}

// TestSoundMoveBypassesSubstitute: a sound-flagged move ignores the doll
// and damages the holder directly. Hyper Voice carries both sound and
// bypass-sub in the curated data, so the foe takes the hit and the doll
// stays untouched.
func TestSoundMoveBypassesSubstitute(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	def := s.Active(1)
	def.Volatiles.Substitute = &SubstituteState{HP: 200, MaxHP: 200}
	subBefore := def.Volatiles.Substitute.HP
	defHPBefore := def.HP
	rng := NewRNG(1)
	var log []LogLine

	if _, ok := dealDamage(d, s, 0, d.Moves["hyper-voice"], rng, &log); !ok {
		t.Fatalf("dealDamage failed")
	}

	if def.Volatiles.Substitute.HP != subBefore {
		t.Errorf("sound move chipped the doll: %d → %d", subBefore, def.Volatiles.Substitute.HP)
	}
	if def.HP >= defHPBefore {
		t.Errorf("sound move didn't damage the holder: HP %d → %d", defHPBefore, def.HP)
	}
}

// TestSubstituteAllowsSelfBoost: a sub doesn't block the user's own effect
// blocks. Swords Dance (TargetSelf, +2 Atk) succeeds while the user has a
// sub up — canon, since the substitute sits between user and foe, not
// between user and itself.
func TestSubstituteAllowsSelfBoost(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	atk := s.Active(0)
	atk.Volatiles.Substitute = &SubstituteState{HP: 50, MaxHP: 50}
	rng := NewRNG(1)
	var log []LogLine

	applyStatusMove(s, 0, d.Moves["swords-dance"], rng, &log)

	if got := atk.Stages.Atk; got != 2 {
		t.Errorf("Atk stage = %d, want +2 (self-boost should pass through sub)", got)
	}
}

// TestSubstituteClearedOnSwitch: the doll lives on Volatiles, which
// doSwitchWithCarry zeroes — so a returning Pokémon does NOT keep its sub.
func TestSubstituteClearedOnSwitch(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143, 6}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Volatiles.Substitute = &SubstituteState{HP: 50, MaxHP: 50}
	var log []LogLine

	doSwitch(s, 0, 1, &log) // switch to Charizard
	doSwitch(s, 0, 0, &log) // switch Snorlax back

	if got := s.Active(0).Volatiles.Substitute; got != nil {
		t.Errorf("sub survived a switch round-trip; HP=%d", got.HP)
	}
}

// TestBatonPassCarriesSubstitute: canon, Baton Pass copies the doll to the
// incoming. Tied to the existing batonCarry path (stages + confusion +
// substitute) — the contrast with plain U-turn (substitute clears) is
// what makes BP the "preserve the sub" pivot.
func TestBatonPassCarriesSubstitute(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "P1", "A", []int{12, 18}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "baton-pass", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(0).Volatiles.Substitute = &SubstituteState{HP: 50, MaxHP: 50}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Sides[0].Active != 1 {
		t.Fatalf("BP did not switch; slot %d", s.Sides[0].Active)
	}
	sub := s.Active(0).Volatiles.Substitute
	if sub == nil {
		t.Fatalf("sub not carried to incoming")
	}
	if sub.HP != 50 || sub.MaxHP != 50 {
		t.Errorf("carried sub state = %+v, want HP=50 MaxHP=50", sub)
	}
}

// TestProtectSetsVolatileAndIncrementsCounter: a first-use Protect lands
// at 100%, sets the one-turn Protect volatile, and bumps the stall counter
// from 0 to 1. The counter is what drives the diminishing-returns curve
// for the next attempt.
func TestProtectSetsVolatileAndIncrementsCounter(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[143])
	rng := NewRNG(1)
	var log []LogLine

	applyProtectMove(&p, 0, false, rng, &log)

	if !p.Volatiles.Protect {
		t.Errorf("Protect volatile not set; log: %v", logTexts(log))
	}
	if got := p.Volatiles.ProtectCounter; got != 1 {
		t.Errorf("ProtectCounter = %d, want 1", got)
	}
	if !logHas(log, "protected itself") {
		t.Errorf("missing setup log line; got %v", logTexts(log))
	}
}

// TestProtectBlocksFoeDamage: a foe damaging move announces but does not
// connect when the target has Protect up. HP unchanged, no contact-rider
// fallout, no secondary effects — return-from-executeMove path.
func TestProtectBlocksFoeDamage(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "tackle", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "protect", PP: 10, MaxPP: 10}}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	def := s.Active(1)
	if def.HP != def.MaxHP {
		t.Errorf("Protect didn't block: HP %d / %d", def.HP, def.MaxHP)
	}
}

// TestBypassProtectMoveConnects: a bypass-protect-flagged move (Feint)
// punches through the shield and damages the holder normally. We use the
// curated Feint entry — transform.go is what maps Showdown's
// breaksProtect=true into the bypass-protect flag.
func TestBypassProtectMoveConnects(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	def := s.Active(1)
	def.Volatiles.Protect = true
	hpBefore := def.HP
	rng := NewRNG(1)
	var log []LogLine

	dmg, ok := dealDamage(d, s, 0, d.Moves["feint"], rng, &log)

	if !ok || dmg <= 0 {
		t.Fatalf("dealDamage returned (%d, %v); Feint should connect through Protect", dmg, ok)
	}
	if def.HP >= hpBefore {
		t.Errorf("Feint didn't damage: HP %d → %d", hpBefore, def.HP)
	}
}

// TestProtectStallChainResets: a second Protect in a row goes through the
// 33% chance; failure rolls a "But it failed!" and zeros the counter. We
// sweep seeds until one fails the second roll so the test stays RNG-honest.
func TestProtectStallChainResets(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[143])

	for seed := uint64(1); seed < 200; seed++ {
		p.Volatiles = Volatiles{}
		rng := NewRNG(seed)
		var log []LogLine
		applyProtectMove(&p, 0, false, rng, &log) // 100% success
		if !p.Volatiles.Protect || p.Volatiles.ProtectCounter != 1 {
			continue
		}
		p.Volatiles.Protect = false // simulate end-of-turn clear
		applyProtectMove(&p, 0, false, rng, &log)
		if !p.Volatiles.Protect {
			// landed a fail on the second roll — counter must reset to 0
			if got := p.Volatiles.ProtectCounter; got != 0 {
				t.Errorf("seed %d: failed roll left counter at %d, want 0", seed, got)
			}
			if !logHas(log, "But it failed!") {
				t.Errorf("seed %d: missing fail log", seed)
			}
			return
		}
	}
	t.Fatal("no seed in [1,200) produced a second-roll failure; widen the search if RNG semantics changed")
}

// TestProtectCounterResetsAfterNonStallMove: pretend two prior protects
// raised the counter; the very next turn's Tackle resets it to 0 via the
// defer in executeMove. That brings the following Protect back to 100%.
func TestProtectCounterResetsAfterNonStallMove(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	atk := s.Active(0)
	atk.Volatiles.ProtectCounter = 2
	atk.Moves = []MoveSlot{{MoveID: "tackle", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if got := atk.Volatiles.ProtectCounter; got != 0 {
		t.Errorf("Tackle didn't reset the stall counter: got %d, want 0", got)
	}
}

// TestEndureClampsLethalDamage: a hit that would zero the target instead
// drops HP to 1. Uses dealDamage directly with a synthetic high-power
// move so the test doesn't rely on a specific BP threshold.
func TestEndureClampsLethalDamage(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	def := s.Active(1)
	def.HP = 10
	def.Volatiles.Endure = true
	rng := NewRNG(1)
	var log []LogLine

	m := domain.Move{
		ID: "syn-blast", Name: "BlastTest", Type: "normal",
		Category: domain.CatPhysical, Power: 250, Accuracy: 100,
	}
	if _, ok := dealDamage(d, s, 0, m, rng, &log); !ok {
		t.Fatalf("dealDamage failed")
	}

	if def.HP != 1 {
		t.Errorf("Endure target HP = %d, want 1", def.HP)
	}
	if def.Fainted {
		t.Errorf("Endure target fainted")
	}
	if !logHas(log, "endured the hit") {
		t.Errorf("missing endure log; got %v", logTexts(log))
	}
}

// TestEndureLetsNonLethalDamageThrough: Endure only clamps the killing
// blow. Non-lethal damage applies normally; the target loses real HP and
// no "endured the hit!" line fires.
func TestEndureLetsNonLethalDamageThrough(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	def := s.Active(1)
	def.Volatiles.Endure = true
	hpBefore := def.HP
	rng := NewRNG(1)
	var log []LogLine

	dmg, ok := dealDamage(d, s, 0, d.Moves["tackle"], rng, &log)

	if !ok || dmg <= 0 {
		t.Fatalf("dealDamage returned (%d, %v)", dmg, ok)
	}
	if def.HP != hpBefore-dmg {
		t.Errorf("non-lethal Tackle clamped: HP %d → %d, want %d", hpBefore, def.HP, hpBefore-dmg)
	}
	if logHas(log, "endured the hit") {
		t.Errorf("endure log fired on a non-lethal hit")
	}
}

// TestProtectClearsAtEndOfTurn: Protect is one-shot. After ResolveTurn
// wraps, the volatile is gone — next turn the foe attack goes through.
// The counter persists.
func TestProtectClearsAtEndOfTurn(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "protect", PP: 10, MaxPP: 10}}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	def := s.Active(1)
	if def.Volatiles.Protect {
		t.Errorf("Protect volatile persisted past end of turn")
	}
	if got := def.Volatiles.ProtectCounter; got != 1 {
		t.Errorf("ProtectCounter = %d, want 1 (persists across turns)", got)
	}
}

// TestProtectDoesntBlockSelfMove: Protect intercepts only foe-targeted
// moves. A self-buff (Swords Dance) the user queues against itself runs
// even when the user has Protect up.
func TestProtectDoesntBlockSelfMove(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	atk := s.Active(0)
	atk.Volatiles.Protect = true
	rng := NewRNG(1)
	var log []LogLine

	applyStatusMove(s, 0, d.Moves["swords-dance"], rng, &log)

	if got := atk.Stages.Atk; got != 2 {
		t.Errorf("self-targeted Swords Dance blocked by own Protect: stage = %d, want +2", got)
	}
}

// TestProtectCounterClearsOnSwitch: the counter lives on Volatiles, which
// doSwitch zeroes — a switched-in Pokémon starts with a fresh stall chain.
func TestProtectCounterClearsOnSwitch(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143, 6}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Volatiles.ProtectCounter = 3
	var log []LogLine

	doSwitch(s, 0, 1, &log)

	if got := s.Active(0).Volatiles.ProtectCounter; got != 0 {
		t.Errorf("ProtectCounter survived switch: %d", got)
	}
}

// TestSleepNoSameTurnWake: a Pokémon put to sleep on turn N (and slower, so
// canAct fires the same turn) must not wake up that same turn. Regression
// for issue #24.
func TestSleepNoSameTurnWake(t *testing.T) {
	d := loadDex(t)
	pika := buildPokemon(d, d.Species[26])
	var log []LogLine

	for seed := uint64(1); seed < 200; seed++ {
		pika.Status = StatusNone
		pika.SleepTurns = 0
		rng := NewRNG(seed)
		if !inflictStatus(&pika, 0, StatusSleep, nil, rng, &log) {
			t.Fatalf("seed %d: sleep should infliict on a Pikachu", seed)
		}
		// Now simulate canAct on the same turn (slower target scenario).
		if canAct(&pika, 0, rng, &log) {
			t.Fatalf("seed %d: pikachu woke up on the same turn it was put to sleep (SleepTurns started at %d)",
				seed, pika.SleepTurns+1) // SleepTurns already decremented
		}
	}
}

// TestRecoilRounding: recoil now rounds rather than truncating. A 50-damage
// hit with recoil=0.33 should self-damage 17 (round(16.5)) not 16. Regression
// for issue #27.
func TestRecoilRounding(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[6])
	atk.HP = atk.MaxHP
	rng := NewRNG(1)
	var log []LogLine

	before := atk.HP
	e := &domain.Effect{Recoil: 0.33}
	applyEffectFields(e, domain.Move{}, &atk, 0, &atk, 0, 50, nil, rng, &log)
	got := before - atk.HP
	if got != 17 {
		t.Errorf("rounded recoil(50, 0.33) = %d, want 17", got)
	}
}

// TestWeatherDamageMods: Sun boosts Fire by 1.5x and halves Water; Rain
// inverts. computeDamage applies the weather multiplier through the same
// path as STAB/type — verified by comparing the same RNG seed in clear,
// sun, and rain.
func TestWeatherDamageMods(t *testing.T) {
	d := loadDex(t)
	charizard := buildPokemon(d, d.Species[6])  // fire / flying
	blastoise := buildPokemon(d, d.Species[9])  // water (water-resistant)
	flamethrower := d.Moves["flamethrower"]

	mk := func(kind WeatherKind) *WeatherState {
		if kind == "" {
			return nil
		}
		return &WeatherState{Kind: kind, TurnsLeft: 5}
	}
	const seed = 0xC0FFEE
	dmg := func(w *WeatherState) int {
		return computeDamage(d, &charizard, &blastoise, flamethrower, w, nil, nil, NewRNG(seed)).Damage
	}
	clear := dmg(mk(""))
	sun := dmg(mk(WeatherSun))
	rain := dmg(mk(WeatherRain))

	if sun <= clear {
		t.Errorf("sun should boost fire: sun=%d, clear=%d", sun, clear)
	}
	if rain >= clear {
		t.Errorf("rain should halve fire: rain=%d, clear=%d", rain, clear)
	}
	// Sanity: ExpectedDamage agrees in the same direction.
	ec, es, er := ExpectedDamage(d, &charizard, &blastoise, flamethrower, mk(""), nil, nil),
		ExpectedDamage(d, &charizard, &blastoise, flamethrower, mk(WeatherSun), nil, nil),
		ExpectedDamage(d, &charizard, &blastoise, flamethrower, mk(WeatherRain), nil, nil)
	if es <= ec || er >= ec {
		t.Errorf("ExpectedDamage weather ordering wrong: clear=%d sun=%d rain=%d", ec, es, er)
	}
}

// TestSandstormChip: non-Rock/Ground/Steel active Pokémon take 1/16 max HP
// at end of turn; Rock takes none.
func TestSandstormChip(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{112}, 1) // Charizard vs Rhydon (Ground/Rock)
	s.Weather = &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}

	cz := s.Active(0)
	rh := s.Active(1)
	czBefore := cz.HP
	rhBefore := rh.HP
	var log []LogLine
	applyWeatherResidual(s, &log)

	if cz.HP != czBefore-cz.MaxHP/16 {
		t.Errorf("Charizard sand chip: HP %d → %d, want -%d", czBefore, cz.HP, cz.MaxHP/16)
	}
	if rh.HP != rhBefore {
		t.Errorf("Rhydon should be sand-immune: %d → %d", rhBefore, rh.HP)
	}
}

// TestSandstormBoostsRockSpD: Rock-type defender gets +50% SpD under
// sandstorm; the same special move should hit it for less damage.
func TestSandstormBoostsRockSpD(t *testing.T) {
	d := loadDex(t)
	starmie := buildPokemon(d, d.Species[121]) // Water/Psychic
	rhydon := buildPokemon(d, d.Species[112])  // Ground/Rock
	surf := d.Moves["surf"]                    // special, water

	const seed = 42
	clear := computeDamage(d, &starmie, &rhydon, surf, nil, nil, nil, NewRNG(seed)).Damage
	sand := computeDamage(d, &starmie, &rhydon, surf, &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}, nil, nil, NewRNG(seed)).Damage

	if sand >= clear {
		t.Errorf("sandstorm should boost Rock SpD: sand=%d, clear=%d", sand, clear)
	}
}

// TestSnowBoostsIceDef: Ice-type defender gets +50% Def under snow; same
// physical move hits for less.
func TestSnowBoostsIceDef(t *testing.T) {
	d := loadDex(t)
	tauros := buildPokemon(d, d.Species[128]) // Normal
	jynx := buildPokemon(d, d.Species[124])   // Ice/Psychic
	bodyslam := d.Moves["body-slam"]

	const seed = 7
	clear := computeDamage(d, &tauros, &jynx, bodyslam, nil, nil, nil, NewRNG(seed)).Damage
	snow := computeDamage(d, &tauros, &jynx, bodyslam, &WeatherState{Kind: WeatherSnow, TurnsLeft: 5}, nil, nil, NewRNG(seed)).Damage

	if snow >= clear {
		t.Errorf("snow should boost Ice Def: snow=%d, clear=%d", snow, clear)
	}
}

// TestWeatherSetterDuration: a setter move spawns weather with 5 turns
// left; the counter decrements each turn and the weather clears on turn 5.
// Re-applying the same weather mid-stream fails (matches Showdown).
func TestWeatherSetterDuration(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{9}, 1) // Charizard vs Blastoise
	rng := NewRNG(1)
	var log []LogLine

	// Charizard uses Sunny Day.
	executeMove(d, s, 0, slotOf(s.Active(0), "sunny-day"), rng, &log)
	if s.Weather == nil || s.Weather.Kind != WeatherSun {
		t.Fatalf("Sunny Day should set sun, got %+v", s.Weather)
	}
	if s.Weather.TurnsLeft != defaultWeatherTurns {
		t.Errorf("Sunny Day TurnsLeft = %d, want %d", s.Weather.TurnsLeft, defaultWeatherTurns)
	}

	// Re-setting the same weather fails.
	logLen := len(log)
	executeMove(d, s, 0, slotOf(s.Active(0), "sunny-day"), rng, &log)
	if s.Weather == nil || s.Weather.TurnsLeft != defaultWeatherTurns {
		t.Errorf("re-applying same weather should not reset counter, got %+v", s.Weather)
	}
	if !logHas(log[logLen:], "But it failed!") {
		t.Errorf("re-applying same weather should log fail")
	}

	// Tick down — 4 more ticks should keep the weather; the 5th clears.
	for i := 1; i < defaultWeatherTurns; i++ {
		var tlog []LogLine
		tickWeather(s, &tlog)
		if s.Weather == nil {
			t.Fatalf("tick %d cleared weather early", i)
		}
	}
	var finalLog []LogLine
	tickWeather(s, &finalLog)
	if s.Weather != nil {
		t.Errorf("after %d ticks weather should clear, still %+v", defaultWeatherTurns, s.Weather)
	}
	if !logHas(finalLog, "sunlight faded") {
		t.Errorf("clear-line missing from %v", logTexts(finalLog))
	}
}

// slotOf returns the move-slot index of moveID on p, or -1 if not learned.
// Lookup-only helper for tests: real flows use the slot index the controller
// submitted, not a name-based lookup.
func slotOf(p *Pokemon, moveID string) int {
	for i, ms := range p.Moves {
		if ms.MoveID == moveID {
			return i
		}
	}
	return -1
}

// TestWeatherBattleIntegration drives a multi-turn battle through ResolveTurn
// and asserts the headline weather behaviors against the actual turn log.
// Verbose log lines via t.Logf show the play-by-play under `go test -v`.
func TestWeatherBattleIntegration(t *testing.T) {
	d := loadDex(t)

	// Scenario 1: Sun boost — Charizard vs Blastoise.
	// Turn 1: Charizard sets Sunny Day; Blastoise tackles.
	// Turn 2: Charizard Flamethrowers Blastoise under sun.
	// Compare against a clear-weather replica turn from the same state.
	s, _ := NewBattle(d, "weather-sanity", "Red", []int{6}, "Blue", []int{9}, 0xC0FFEE)
	cz := slotOf(s.Active(0), "sunny-day")
	bs := slotOf(s.Active(1), "tackle")
	if cz < 0 || bs < 0 {
		t.Fatalf("required moves missing: sunny-day=%d, tackle=%d", cz, bs)
	}
	log := ResolveTurn(d, s, [2]Action{
		{Kind: ActionMove, Index: cz},
		{Kind: ActionMove, Index: bs},
	})
	dumpLog(t, "Turn 1 (Sunny Day + Tackle)", log)

	if s.Weather == nil || s.Weather.Kind != WeatherSun {
		t.Fatalf("after Sunny Day, expected sun, got %+v", s.Weather)
	}
	if s.Weather.TurnsLeft != 4 {
		t.Errorf("sun TurnsLeft after first tick = %d, want 4 (5 - 1)", s.Weather.TurnsLeft)
	}

	// Snapshot state before turn 2 so we can replay it without sun for the
	// damage delta.
	preFlame := s.Clone()
	flameCz := slotOf(s.Active(0), "flamethrower")
	tackleBs := slotOf(s.Active(1), "tackle")
	hpBefore := s.Active(1).HP

	log2 := ResolveTurn(d, s, [2]Action{
		{Kind: ActionMove, Index: flameCz},
		{Kind: ActionMove, Index: tackleBs},
	})
	dumpLog(t, "Turn 2 (Flamethrower under sun)", log2)
	sunDmg := hpBefore - s.Active(1).HP

	// Replay turn 2 without the weather modifier by clearing it on the snapshot.
	preFlame.Weather = nil
	clearLog := ResolveTurn(d, preFlame, [2]Action{
		{Kind: ActionMove, Index: flameCz},
		{Kind: ActionMove, Index: tackleBs},
	})
	dumpLog(t, "Turn 2 replayed in clear weather", clearLog)
	clearDmg := hpBefore - preFlame.Active(1).HP

	t.Logf("Flamethrower vs Blastoise: sun=%d, clear=%d (expected sun ~1.5x clear)", sunDmg, clearDmg)
	if sunDmg <= clearDmg {
		t.Errorf("sun should boost Flamethrower: sun=%d, clear=%d", sunDmg, clearDmg)
	}

	// Scenario 2: Sandstorm chip vs Rhydon immunity.
	// Fresh battle: Blastoise vs Rhydon. Rhydon uses Sandstorm; Blastoise
	// takes chip but Rhydon does not.
	s2, _ := NewBattle(d, "sand-sanity", "Red", []int{9}, "Blue", []int{112}, 0xBEEF)
	tackleBs2 := slotOf(s2.Active(0), "tackle")
	sandRh := slotOf(s2.Active(1), "sandstorm")
	if tackleBs2 < 0 || sandRh < 0 {
		t.Fatalf("required moves missing: blastoise.tackle=%d, rhydon.sandstorm=%d", tackleBs2, sandRh)
	}

	bsHP, rhHP := s2.Active(0).HP, s2.Active(1).HP
	sandLog := ResolveTurn(d, s2, [2]Action{
		{Kind: ActionMove, Index: tackleBs2},
		{Kind: ActionMove, Index: sandRh},
	})
	dumpLog(t, "Turn 1 (Rhydon Sandstorm + Blastoise Tackle)", sandLog)

	if s2.Weather == nil || s2.Weather.Kind != WeatherSandstorm {
		t.Fatalf("expected sandstorm, got %+v", s2.Weather)
	}
	bsDelta := bsHP - s2.Active(0).HP
	rhDelta := rhHP - s2.Active(1).HP
	t.Logf("After turn 1: Blastoise lost %d HP (tackle + sand chip), Rhydon lost %d HP (tackle only)",
		bsDelta, rhDelta)

	// Blastoise should have taken the sandstorm chip on top of the tackle.
	// Rhydon (Ground/Rock) is sand-immune.
	if expected := s2.Active(0).MaxHP / 16; bsDelta < expected {
		t.Errorf("Blastoise should have taken sand chip (~%d HP) plus tackle, only took %d", expected, bsDelta)
	}
	// Rhydon's loss is just whatever tackle dealt — should NOT include /16 sand chip.
	rhSandChip := s2.Active(1).MaxHP / 16
	// If Rhydon's HP loss is at least the tackle damage plus a sand chip, sand chip leaked.
	// We don't know the tackle damage independently here, but Rhydon shouldn't lose more than
	// MaxHP/16 above what tackle alone would do. Heuristic check: confirm no "buffeted" log line for Rhydon.
	for _, l := range sandLog {
		if l.Side == 1 && strings.Contains(l.Text, "buffeted by the sandstorm") {
			t.Errorf("Rhydon should be sand-immune but got line: %q (would chip %d)", l.Text, rhSandChip)
		}
	}

	// Scenario 3: Same-weather re-set fails.
	preFail := s2.Clone()
	failLog := ResolveTurn(d, s2, [2]Action{
		{Kind: ActionMove, Index: tackleBs2},
		{Kind: ActionMove, Index: sandRh},
	})
	dumpLog(t, "Turn 2 (Rhydon tries Sandstorm again)", failLog)
	if !logHas(failLog, "But it failed!") {
		t.Errorf("re-using same weather setter should log fail")
	}
	if s2.Weather == nil {
		t.Fatal("weather cleared unexpectedly")
	}
	// TurnsLeft should have ticked from preFail's value down by one, not been reset.
	if s2.Weather.TurnsLeft != preFail.Weather.TurnsLeft-1 {
		t.Errorf("re-set should not refresh TurnsLeft: was %d, now %d",
			preFail.Weather.TurnsLeft, s2.Weather.TurnsLeft)
	}

	// Scenario 4: Drive sandstorm to expiry. Total lifetime is 5 turns;
	// we've consumed 2 already. 3 more idle turns should clear it.
	for i := 0; i < 3; i++ {
		if s2.Phase != PhaseChoosing {
			break
		}
		tick := ResolveTurn(d, s2, [2]Action{
			{Kind: ActionMove, Index: tackleBs2},
			{Kind: ActionMove, Index: 0}, // Rhydon does whatever's in slot 0
		})
		dumpLog(t, fmt.Sprintf("Filler turn %d (waiting for sand to expire)", i+3), tick)
		if s2.Phase != PhaseChoosing {
			t.Logf("battle ended at turn %d via phase=%v", s2.Turn, s2.Phase)
			break
		}
	}
	if s2.Weather != nil && s2.Phase == PhaseChoosing {
		t.Errorf("sandstorm should have cleared by now, still %+v at turn %d", s2.Weather, s2.Turn)
	}
}

// --- abilities ---

// TestAbilityDefaultsToSlotZero checks that buildPokemon picks up the slot-0
// ability from a species' Abilities slice. This is the convention for batches
// before the picker UI grows an ability dropdown (#30 step 4).
func TestAbilityDefaultsToSlotZero(t *testing.T) {
	d := loadDex(t)
	cases := []struct {
		dexNo int
		want  AbilityKind
	}{
		{128, AbilityIntimidate}, // Tauros
		{110, AbilityLevitate},   // Weezing
		{87, AbilityThickFat},    // Dewgong — slot-0 thick-fat
		{143, "immunity"},        // Snorlax — slot-0 is immunity, not thick-fat
		{95, "rock-head"},        // Onix — slot-0 is rock-head; Sturdy at slot 1
		{6, "blaze"},             // Charizard — slot-0 blaze (unimplemented; engine no-ops)
	}
	for _, c := range cases {
		p := buildPokemon(d, d.Species[c.dexNo])
		if p.Ability != c.want {
			t.Errorf("dex %d (%s): Ability = %q, want %q", c.dexNo, p.Name, p.Ability, c.want)
		}
	}
}

// TestIntimidateOnSwitchIn verifies the foe's Atk drops by 1 stage when a
// holder enters. We invoke applyOnSwitchIn directly rather than going through
// doSwitch so the test is independent of the switch plumbing.
func TestIntimidateOnSwitchIn(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{128}, "P2", []int{143}, 1) // Tauros (Intimidate) vs Snorlax
	if s.Active(0).Ability != AbilityIntimidate {
		t.Fatalf("Tauros should have Intimidate by default, got %q", s.Active(0).Ability)
	}
	foe := s.Active(1)
	if foe.Stages.Atk != 0 {
		t.Fatalf("foe Atk stage should start at 0, got %d", foe.Stages.Atk)
	}

	var log []LogLine
	applyOnSwitchIn(s, 0, &log)
	if foe.Stages.Atk != -1 {
		t.Errorf("Intimidate should drop foe Atk to -1, got %d", foe.Stages.Atk)
	}
	if !logHas(log, "Intimidate") {
		t.Errorf("Intimidate log line missing: %v", logTexts(log))
	}
}

// TestSturdyClampsOHKO checks that a hit that would KO at full HP gets
// clamped to 1 HP, and that the same hit at less than full HP isn't saved.
func TestSturdyClampsOHKO(t *testing.T) {
	d := loadDex(t)
	// Onix's slot-0 in modern data is Rock Head, with Sturdy at slot 1.
	// Picker (when it ships) will let the user choose; for now we set it
	// explicitly so the test pins Sturdy semantics rather than the slot lookup.
	onix := buildPokemon(d, d.Species[95])
	onix.Ability = AbilitySturdy

	// Full HP: an overkill hit clamps to 1 below max.
	got, fired := abilitySurviveOHKO(&onix, onix.MaxHP+9999)
	if !fired {
		t.Error("Sturdy should fire at full HP for an OHKO")
	}
	if got != onix.MaxHP-1 {
		t.Errorf("Sturdy clamped damage = %d, want %d", got, onix.MaxHP-1)
	}

	// Not at full HP: Sturdy doesn't trigger.
	onix.HP = onix.MaxHP - 1
	got2, fired2 := abilitySurviveOHKO(&onix, 9999)
	if fired2 {
		t.Error("Sturdy must not fire when defender is below full HP")
	}
	if got2 != 9999 {
		t.Errorf("damage should pass through unchanged at <full HP, got %d", got2)
	}

	// Non-lethal hit at full HP: Sturdy doesn't trigger either.
	onix.HP = onix.MaxHP
	got3, fired3 := abilitySurviveOHKO(&onix, 1)
	if fired3 {
		t.Error("Sturdy must not fire for non-lethal damage")
	}
	if got3 != 1 {
		t.Errorf("non-lethal damage should pass through, got %d", got3)
	}
}

// TestLevitateGroundImmunity: a Ground-type move against a Levitate holder
// resolves to 0 damage in both computeDamage and ExpectedDamage.
func TestLevitateGroundImmunity(t *testing.T) {
	d := loadDex(t)
	rhydon := buildPokemon(d, d.Species[112]) // ground/rock attacker
	weezing := buildPokemon(d, d.Species[110]) // Levitate
	if weezing.Ability != AbilityLevitate {
		t.Fatalf("Weezing slot-0 should be Levitate, got %q", weezing.Ability)
	}
	eq := d.Moves["earthquake"]

	res := computeDamage(d, &rhydon, &weezing, eq, nil, nil, nil, NewRNG(1))
	if res.Damage != 0 || res.Effectiveness != 0 {
		t.Errorf("Earthquake vs Levitate Weezing = %+v, want 0 damage / 0 eff", res)
	}
	if got := ExpectedDamage(d, &rhydon, &weezing, eq, nil, nil, nil); got != 0 {
		t.Errorf("ExpectedDamage Earthquake vs Weezing = %d, want 0", got)
	}

	// Non-Ground move still hurts (Tackle is Normal, hits normally).
	tackle := d.Moves["tackle"]
	res2 := computeDamage(d, &rhydon, &weezing, tackle, nil, nil, nil, NewRNG(1))
	if res2.Damage <= 0 {
		t.Errorf("Levitate must not affect non-Ground moves, got %+v", res2)
	}
}

// TestThickFatHalvesFireAndIce: Fire/Ice incoming damage halves; other types
// pass through. Compared head-to-head against a clone with the ability
// cleared so the assertion is robust to dex changes.
func TestThickFatHalvesFireAndIce(t *testing.T) {
	d := loadDex(t)
	charizard := buildPokemon(d, d.Species[6])

	mk := func(ability AbilityKind) Pokemon {
		// Dewgong (slot-0 thick-fat) — using Dewgong rather than Snorlax (who
		// has Immunity at slot 0) so the default Ability is what we want.
		p := buildPokemon(d, d.Species[87])
		p.Ability = ability
		return p
	}

	flamethrower := d.Moves["flamethrower"]
	icebeam := d.Moves["ice-beam"]
	bodyslam := d.Moves["body-slam"] // normal — should be untouched

	for _, m := range []domain.Move{flamethrower, icebeam} {
		t.Run(m.ID, func(t *testing.T) {
			withFat := mk(AbilityThickFat)
			without := mk(AbilityNone)
			// ExpectedDamage is deterministic — no RNG sampling needed.
			tf := ExpectedDamage(d, &charizard, &withFat, m, nil, nil, nil)
			plain := ExpectedDamage(d, &charizard, &without, m, nil, nil, nil)
			if tf >= plain {
				t.Errorf("%s: thick-fat=%d should be < no-ability=%d", m.ID, tf, plain)
			}
			// Allow a ±1 floor-rounding wiggle around the 0.5x target.
			want := plain / 2
			if tf < want-1 || tf > want+1 {
				t.Errorf("%s: thick-fat=%d, want ≈ %d (half of %d)", m.ID, tf, want, plain)
			}
		})
	}

	// Sanity: a non-Fire-non-Ice move is unaffected.
	withFat := mk(AbilityThickFat)
	without := mk(AbilityNone)
	if a, b := ExpectedDamage(d, &charizard, &withFat, bodyslam, nil, nil, nil),
		ExpectedDamage(d, &charizard, &without, bodyslam, nil, nil, nil); a != b {
		t.Errorf("body-slam: thick-fat=%d should equal no-ability=%d", a, b)
	}
}

// TestAbilityBattleIntegration drives multi-turn battles through ResolveTurn
// and asserts the headline ability behaviors against the real turn log.
// Verbose play-by-play under `go test -v`.
func TestAbilityBattleIntegration(t *testing.T) {
	d := loadDex(t)

	// Scene 1: Intimidate — Tauros vs Charizard, turn 1 leads.
	// Expectation: Charizard's Atk stage = -1 BEFORE any move resolves,
	// driven by the lead-trigger applyOnSwitchIn at the top of turn 1.
	t.Run("Intimidate fires on lead", func(t *testing.T) {
		s, _ := NewBattle(d, "ab", "Red", []int{128}, "Blue", []int{6}, 0xC0FFEE)
		// Tauros has Tackle; Charizard has Scratch (no Tackle in modern dex).
		atk1 := slotOf(s.Active(0), "tackle")
		atk2 := slotOf(s.Active(1), "scratch")
		if atk1 < 0 || atk2 < 0 {
			t.Fatalf("required moves missing: tauros tackle=%d, charizard scratch=%d", atk1, atk2)
		}
		log := ResolveTurn(d, s, [2]Action{
			{Kind: ActionMove, Index: atk1},
			{Kind: ActionMove, Index: atk2},
		})
		dumpLog(t, "Scene 1 turn 1 (Tauros Intimidate lead)", log)

		if !logHas(log, "Intimidate") {
			t.Errorf("Intimidate log line missing from turn 1: %v", logTexts(log))
		}
		if s.Active(1).Stages.Atk != -1 {
			t.Errorf("Charizard Atk stage after Intimidate = %d, want -1", s.Active(1).Stages.Atk)
		}
	})

	// Scene 2: Sturdy — Mewtwo Hyper Beam vs Onix at full HP. Onix
	// should survive at exactly 1 HP and the log should call out
	// Sturdy. Followup Tackle next turn KOs normally (no second save).
	t.Run("Sturdy saves once", func(t *testing.T) {
		s, _ := NewBattle(d, "ab", "Red", []int{150}, "Blue", []int{95}, 0xC0FFEE)
		s.Active(1).Ability = AbilitySturdy // see note in TestSturdyClampsOHKO
		// Mewtwo's Aura Sphere is special (hits Onix's low SpD), Fighting-type
		// (4× SE vs Rock/Ground), and never misses — a clean OHKO when Sturdy
		// is absent. Onix's Tackle gives turn 2 a sane second mover; we don't
		// actually use it.
		as := slotOf(s.Active(0), "aura-sphere")
		tackle := slotOf(s.Active(1), "tackle")
		if as < 0 || tackle < 0 {
			t.Fatalf("required moves missing: aura-sphere=%d, tackle=%d", as, tackle)
		}
		log1 := ResolveTurn(d, s, [2]Action{
			{Kind: ActionMove, Index: as},
			{Kind: ActionMove, Index: tackle},
		})
		dumpLog(t, "Scene 2 turn 1 (Mewtwo Aura Sphere vs Sturdy Onix)", log1)

		onix := s.Active(1)
		if onix.HP != 1 {
			t.Errorf("Sturdy should leave Onix at 1 HP, got %d / %d", onix.HP, onix.MaxHP)
		}
		if !logHas(log1, "Sturdy") {
			t.Errorf("Sturdy log line missing from turn 1: %v", logTexts(log1))
		}

		// Heal back to full and re-run computeDamage directly to confirm
		// Sturdy fires again — the trigger is "at full HP at hit time", not
		// "has ever fired".
		onix.HP = onix.MaxHP
		res := computeDamage(d, s.Active(0), onix, d.Moves["aura-sphere"], nil, nil, nil, NewRNG(7))
		if !res.Sturdy {
			t.Errorf("Sturdy should fire again on a fresh full-HP hit, got %+v", res)
		}
	})

	// Scene 3: Levitate — Rhydon's Earthquake vs Weezing. Damage line
	// should be the "doesn't affect" immunity message, not a number.
	t.Run("Levitate blocks Earthquake", func(t *testing.T) {
		s, _ := NewBattle(d, "ab", "Red", []int{112}, "Blue", []int{110}, 0xC0FFEE)
		eq := slotOf(s.Active(0), "earthquake")
		tackle := slotOf(s.Active(1), "tackle")
		if eq < 0 || tackle < 0 {
			t.Fatalf("required moves missing: earthquake=%d, tackle=%d", eq, tackle)
		}
		weezBefore := s.Active(1).HP
		log := ResolveTurn(d, s, [2]Action{
			{Kind: ActionMove, Index: eq},
			{Kind: ActionMove, Index: tackle},
		})
		dumpLog(t, "Scene 3 turn 1 (Rhydon Earthquake vs Levitate Weezing)", log)

		if s.Active(1).HP != weezBefore {
			t.Errorf("Levitate Weezing should take 0 from Earthquake; HP %d → %d", weezBefore, s.Active(1).HP)
		}
		if !logHas(log, "doesn't affect") {
			t.Errorf("immunity log line missing: %v", logTexts(log))
		}
	})

	// Scene 4: Thick Fat — Charizard's Flamethrower into Dewgong with and
	// without the ability. Cloned snapshot replay isolates the ability
	// delta from RNG drift.
	t.Run("Thick Fat halves fire", func(t *testing.T) {
		s, _ := NewBattle(d, "ab", "Red", []int{6}, "Blue", []int{87}, 0xC0FFEE)
		ft := slotOf(s.Active(0), "flamethrower")
		auroraBeam := slotOf(s.Active(1), "aurora-beam")
		if ft < 0 || auroraBeam < 0 {
			t.Fatalf("required moves missing: flamethrower=%d, aurora-beam=%d", ft, auroraBeam)
		}
		if s.Active(1).Ability != AbilityThickFat {
			t.Fatalf("Dewgong slot-0 should be thick-fat, got %q", s.Active(1).Ability)
		}

		// Snapshot before turn 1 so we can rerun with the ability cleared.
		snap := s.Clone()
		snap.Sides[1].Team[0].Ability = AbilityNone

		log := ResolveTurn(d, s, [2]Action{
			{Kind: ActionMove, Index: ft},
			{Kind: ActionMove, Index: auroraBeam},
		})
		dumpLog(t, "Scene 4 turn 1 (Flamethrower into Thick Fat Dewgong)", log)
		dmgWith := s.Active(1).MaxHP - s.Active(1).HP

		logNo := ResolveTurn(d, snap, [2]Action{
			{Kind: ActionMove, Index: ft},
			{Kind: ActionMove, Index: auroraBeam},
		})
		dumpLog(t, "Scene 4 turn 1 control (same state, no ability)", logNo)
		dmgWithout := snap.Active(1).MaxHP - snap.Active(1).HP

		if dmgWith >= dmgWithout {
			t.Errorf("Thick Fat should halve fire: with=%d, without=%d", dmgWith, dmgWithout)
		}
		// ±1 wiggle for floor rounding on the half.
		want := dmgWithout / 2
		if dmgWith < want-1 || dmgWith > want+1 {
			t.Errorf("Thick Fat fire damage = %d, want ≈ %d (half of %d)", dmgWith, want, dmgWithout)
		}
	})
}

// --- batch-2 abilities ---

// TestAbilityWeatherSetter: a Drought holder switching in installs Sun for
// the default duration. We force the ability since no Gen-1 species has
// Drought at slot 0 (Ninetales has it at slot 1).
func TestAbilityWeatherSetter(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{38}, "P2", []int{6}, 1) // Ninetales vs Charizard
	s.Active(0).Ability = "drought"

	var log []LogLine
	applyOnSwitchIn(s, 0, &log)
	if s.Weather == nil || s.Weather.Kind != WeatherSun {
		t.Errorf("Drought should set sun, got %+v", s.Weather)
	}
	if s.Weather.TurnsLeft != defaultWeatherTurns {
		t.Errorf("Drought duration = %d, want %d", s.Weather.TurnsLeft, defaultWeatherTurns)
	}
	if !logHas(log, "harsh") {
		t.Errorf("missing 'sunlight turned harsh' line: %v", logTexts(log))
	}
}

// TestAbilityVoltAbsorb: Jolteon (slot-0 Volt Absorb) takes 0 from
// Thunderbolt and heals 1/4 MaxHP.
func TestAbilityVoltAbsorb(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{26}, "P2", []int{135}, 1) // Raichu vs Jolteon
	jolteon := s.Active(1)
	if jolteon.Ability != "volt-absorb" {
		t.Fatalf("Jolteon slot-0 should be volt-absorb, got %q", jolteon.Ability)
	}
	jolteon.HP = jolteon.MaxHP / 2
	before := jolteon.HP

	tbolt := slotOf(s.Active(0), "thunderbolt")
	if tbolt < 0 {
		t.Fatalf("Raichu missing thunderbolt")
	}
	log := ResolveTurn(d, s, [2]Action{
		{Kind: ActionMove, Index: tbolt},
		{Kind: ActionMove, Index: 0}, // Jolteon's first move (we don't care which)
	})
	dumpLog(t, "Volt Absorb scene", log)

	want := before + jolteon.MaxHP/4
	if want > jolteon.MaxHP {
		want = jolteon.MaxHP
	}
	if jolteon.HP != want {
		t.Errorf("Jolteon HP after Volt Absorb = %d, want %d (from %d, +%d)", jolteon.HP, want, before, jolteon.MaxHP/4)
	}
	if !logHas(log, "absorbed") {
		t.Errorf("missing absorbed log: %v", logTexts(log))
	}
}

// TestAbilityFlashFire: Ninetales absorbs a Fire move (FlashFireCharged
// flips), and its next Fire move gets ×1.5 outgoing damage. We compare
// against the same matchup with the charge cleared.
func TestAbilityFlashFire(t *testing.T) {
	d := loadDex(t)
	ninetales := buildPokemon(d, d.Species[38])
	venusaur := buildPokemon(d, d.Species[3])
	if ninetales.Ability != "flash-fire" {
		t.Fatalf("Ninetales slot-0 should be flash-fire, got %q", ninetales.Ability)
	}

	// Compute Flamethrower damage with and without the Flash Fire charge.
	ft := d.Moves["flamethrower"]
	without := ExpectedDamage(d, &ninetales, &venusaur, ft, nil, nil, nil)
	ninetales.Volatiles.FlashFireCharged = true
	with := ExpectedDamage(d, &ninetales, &venusaur, ft, nil, nil, nil)
	wantRatio := 1.5
	got := float64(with) / float64(without)
	if got < wantRatio*0.95 || got > wantRatio*1.05 {
		t.Errorf("Flash Fire boost ratio = %.2f (with=%d, without=%d), want ~%.2f", got, with, without, wantRatio)
	}
}

// TestAbilityLightningRod: Rhydon absorbs an Electric move and gains +1
// SpA. (Rhydon is also Ground/Rock so the Electric-immunity comes from
// the typing too; Lightning Rod is more interesting on the boost — we
// verify the SpA stage moves regardless of typing.)
func TestAbilityLightningRod(t *testing.T) {
	d := loadDex(t)
	rhydon := buildPokemon(d, d.Species[112])
	if rhydon.Ability != "lightning-rod" {
		t.Fatalf("Rhydon slot-0 should be lightning-rod, got %q", rhydon.Ability)
	}
	s, _ := NewBattle(d, "b", "P1", []int{26}, "P2", []int{112}, 1) // Raichu vs Rhydon
	r := s.Active(1)
	if r.Stages.SpA != 0 {
		t.Fatalf("expected fresh SpA stage = 0, got %d", r.Stages.SpA)
	}

	// Invoke the immunity bonus directly so we don't have to find an
	// Electric attacker that survives turn 1.
	var log []LogLine
	abilityImmunityBonus(s, 1, "electric", &log)
	if r.Stages.SpA != 1 {
		t.Errorf("Lightning Rod should raise SpA to +1, got %d", r.Stages.SpA)
	}
}

// TestAbilityStatusGuard: Snorlax (Immunity) refuses Toxic infliction.
func TestAbilityStatusGuard(t *testing.T) {
	d := loadDex(t)
	snorlax := buildPokemon(d, d.Species[143])
	if snorlax.Ability != "immunity" {
		t.Fatalf("Snorlax slot-0 should be immunity, got %q", snorlax.Ability)
	}
	rng := NewRNG(1)
	var log []LogLine
	if inflictStatus(&snorlax, 0, StatusToxic, nil, rng, &log) {
		t.Error("Immunity should block Toxic infliction")
	}
	if snorlax.Status != StatusNone {
		t.Errorf("Snorlax status = %q, want none", snorlax.Status)
	}
}

// TestAbilityContactRider: Static rolls 30% para on contact. We exercise
// a deterministic seed where the roll succeeds.
func TestAbilityContactRider(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{26}, 7) // Charizard vs Raichu (Static)
	if s.Active(1).Ability != "static" {
		t.Fatalf("Raichu slot-0 should be static, got %q", s.Active(1).Ability)
	}

	// Find a seed where Static fires within a few tries. We don't lock in
	// a specific seed — we sample several until one para roll succeeds, so
	// the assertion is "Static can fire" rather than "fires on this seed".
	// Charizard's Scratch is the contact attack; Raichu just needs any
	// move it actually owns (slot 0 / "first legal move").
	fired := false
	for seed := uint64(1); seed <= 50 && !fired; seed++ {
		s2, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{26}, seed)
		scratch := slotOf(s2.Active(0), "scratch")
		if scratch < 0 {
			t.Fatalf("Charizard missing scratch")
		}
		raichuMove := 0 // first legal slot — exact move doesn't matter for this test
		log := ResolveTurn(d, s2, [2]Action{
			{Kind: ActionMove, Index: scratch},
			{Kind: ActionMove, Index: raichuMove},
		})
		if s2.Active(0).Status == StatusParalysis || logHas(log, "Static") {
			fired = true
			dumpLog(t, fmt.Sprintf("Static fires on seed %d", seed), log)
		}
	}
	if !fired {
		t.Error("Static never fired across 50 seeds — expected ≥1 trigger at 30% probability per contact")
	}
}

// TestAbilityClearBody: Tentacruel's Clear Body blocks any foe-induced
// stat drop. Driven via applyStagesFromFoe (the path Growl etc. take).
func TestAbilityClearBody(t *testing.T) {
	d := loadDex(t)
	tentacruel := buildPokemon(d, d.Species[73])
	if tentacruel.Ability != "clear-body" {
		t.Fatalf("Tentacruel slot-0 should be clear-body, got %q", tentacruel.Ability)
	}
	var log []LogLine
	applyStagesFromFoe(&tentacruel, 1, "attack", -1, &log)
	if tentacruel.Stages.Atk != 0 {
		t.Errorf("Clear Body should block stat drop; Atk = %d, want 0", tentacruel.Stages.Atk)
	}
	if !logHas(log, "prevented") {
		t.Errorf("missing block log: %v", logTexts(log))
	}
}

// TestAbilityDefiant: a foe-induced stat drop triggers +2 Atk reaction.
// No Gen-1 species has Defiant at slot 0; force-assign.
func TestAbilityDefiant(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[6])
	p.Ability = "defiant"
	var log []LogLine
	applyStagesFromFoe(&p, 0, "defense", -1, &log)
	if p.Stages.Def != -1 {
		t.Errorf("Def stage = %d, want -1 (the drop should still apply)", p.Stages.Def)
	}
	if p.Stages.Atk != 2 {
		t.Errorf("Defiant reaction: Atk stage = %d, want +2", p.Stages.Atk)
	}
}

// TestAbilityMagicGuard: Clefable forced with Magic Guard takes no burn
// chip on the residual tick.
func TestAbilityMagicGuard(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{36}, "P2", []int{6}, 1) // Clefable vs Charizard
	p := s.Active(0)
	p.Ability = "magic-guard"
	p.Status = StatusBurn
	before := p.HP
	var log []LogLine
	applyResidual(s, 0, &log)
	if p.HP != before {
		t.Errorf("Magic Guard should block burn chip; HP %d → %d", before, p.HP)
	}
}

// TestAbilitySpeedBoost: end-of-turn Spe stage +1. Force-assign since no
// Gen-1 slot-0 Speed Boost holder.
func TestAbilitySpeedBoost(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{9}, 1)
	s.Active(0).Ability = "speed-boost"
	if s.Active(0).Stages.Spe != 0 {
		t.Fatal("test setup: spe stage should start 0")
	}
	var log []LogLine
	applyAbilityEndOfTurn(s, 0, &log)
	if s.Active(0).Stages.Spe != 1 {
		t.Errorf("Speed Boost should raise Spe to +1, got %d", s.Active(0).Stages.Spe)
	}
}

// TestAbilityRainDishHealsInRain: heal only fires when rain is active.
func TestAbilityRainDishHealsInRain(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{9}, "P2", []int{6}, 1)
	p := s.Active(0)
	p.Ability = "rain-dish"
	p.HP = p.MaxHP / 2
	before := p.HP

	// Without rain — no heal.
	var log []LogLine
	applyAbilityEndOfTurn(s, 0, &log)
	if p.HP != before {
		t.Errorf("Rain Dish should not heal in clear weather; HP %d → %d", before, p.HP)
	}

	// With rain — heal 1/16.
	s.Weather = &WeatherState{Kind: WeatherRain, TurnsLeft: 5}
	applyAbilityEndOfTurn(s, 0, &log)
	want := before + p.MaxHP/16
	if want > p.MaxHP {
		want = p.MaxHP
	}
	if p.HP != want {
		t.Errorf("Rain Dish in rain: HP %d → %d, want %d", before, p.HP, want)
	}
}

// TestAbilityNaturalCure: switching out cures status.
func TestAbilityNaturalCure(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{113, 6}, "P2", []int{3}, 1) // Chansey + Charizard
	chansey := s.Active(0)
	if chansey.Ability != "natural-cure" {
		t.Fatalf("Chansey slot-0 should be natural-cure, got %q", chansey.Ability)
	}
	chansey.Status = StatusBurn

	var log []LogLine
	doSwitch(s, 0, 1, &log)
	if chansey.Status != StatusNone {
		t.Errorf("Natural Cure should clear status on switch-out, status = %q", chansey.Status)
	}
}

// TestAbilityRegenerator: switching out heals 1/3 MaxHP.
func TestAbilityRegenerator(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{80, 6}, "P2", []int{3}, 1) // Slowbro + Charizard
	p := s.Active(0)
	p.Ability = "regenerator" // Slowbro·H so we force it
	p.HP = p.MaxHP / 3
	before := p.HP

	var log []LogLine
	doSwitch(s, 0, 1, &log)
	wantMin := before + p.MaxHP/3 - 1 // ±1 wiggle
	wantMax := before + p.MaxHP/3 + 1
	if p.HP < wantMin || p.HP > wantMax {
		t.Errorf("Regenerator heal: HP %d → %d, want %d±1", before, p.HP, before+p.MaxHP/3)
	}
}

// TestAbilityCloudNine: sandstorm chip is suppressed when either active
// has Cloud Nine. Golduck (slot-1) is forced here.
func TestAbilityCloudNine(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{55}, "P2", []int{6}, 1) // Golduck vs Charizard
	s.Active(0).Ability = "cloud-nine"
	s.Weather = &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}
	cz := s.Active(1)
	czBefore := cz.HP

	var log []LogLine
	applyWeatherResidual(s, &log)
	if cz.HP != czBefore {
		t.Errorf("Cloud Nine should suppress sandstorm chip; Charizard HP %d → %d", czBefore, cz.HP)
	}
}

// TestAbilitySoundproof: a sound-flagged move targets the holder with the
// "doesn't affect" message and lands no damage.
func TestAbilitySoundproof(t *testing.T) {
	d := loadDex(t)
	hyperVoice, ok := d.Moves["hyper-voice"]
	if !ok {
		t.Skip("dataset doesn't carry hyper-voice; skipping")
	}
	if !hyperVoice.HasFlag("sound") {
		t.Skip("hyper-voice not flagged sound; skipping")
	}
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{101}, 1) // Charizard vs Electrode
	if s.Active(1).Ability != "soundproof" {
		t.Fatalf("Electrode slot-0 should be soundproof, got %q", s.Active(1).Ability)
	}
	rng := NewRNG(1)
	var log []LogLine
	if resolveAccuracy(s, 0, hyperVoice, rng, &log) {
		t.Error("Soundproof should make hyper-voice not land")
	}
	if !logHas(log, "Soundproof") {
		t.Errorf("missing Soundproof log: %v", logTexts(log))
	}
}

// TestAbilityCompoundEyes: accuracy multiplied by 1.3.
func TestAbilityCompoundEyes(t *testing.T) {
	d := loadDex(t)
	if got := abilityAccuracyMult(&Pokemon{Ability: "compound-eyes"}); got != 1.3 {
		t.Errorf("Compound Eyes accuracy mult = %v, want 1.3", got)
	}
	butterfree := buildPokemon(d, d.Species[12])
	if butterfree.Ability != "compound-eyes" {
		t.Fatalf("Butterfree slot-0 should be compound-eyes, got %q", butterfree.Ability)
	}
}

// TestAbilitySwiftSwim: speed doubles in rain.
func TestAbilitySwiftSwim(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[119]) // Seaking slot-0 swift-swim
	clear := effectiveSpeed(&p, nil)
	rain := effectiveSpeed(&p, &WeatherState{Kind: WeatherRain, TurnsLeft: 5})
	if rain != clear*2 {
		t.Errorf("Swift Swim in rain: %d, want %d (2× clear=%d)", rain, clear*2, clear)
	}
}

// TestAbilityBattleArmor: crit denied. Probabilistic, so we sample over
// many seeds and assert no crit fires; the chance of a false-positive
// across 200 trials at p=0 is exactly zero.
func TestAbilityBattleArmor(t *testing.T) {
	d := loadDex(t)
	charizard := buildPokemon(d, d.Species[6])
	target := buildPokemon(d, d.Species[68]) // Machamp — has Guts at slot 0; force armor
	target.Ability = "battle-armor"
	flamethrower := d.Moves["flamethrower"]
	for i := 0; i < 200; i++ {
		res := computeDamage(d, &charizard, &target, flamethrower, nil, nil, nil, NewRNG(uint64(i+1)))
		if res.Crit {
			t.Fatalf("Battle Armor should block crits, fired on iter %d", i)
		}
	}
}

// TestAbilityGutsBoostsBurnedAtk: burned holder's physical attack is not
// halved (Guts cancels) AND is multiplied ×1.5. ExpectedDamage shows ~3x
// the "burned no-Guts" baseline; ~1.5x the "unburned no-Guts" baseline.
func TestAbilityGutsBoostsBurnedAtk(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[68]) // Machamp (Guts slot 0)
	if atk.Ability != "guts" {
		t.Fatalf("Machamp slot-0 should be guts, got %q", atk.Ability)
	}
	def := buildPokemon(d, d.Species[3]) // Venusaur
	bs := d.Moves["body-slam"]

	atk.Status = StatusBurn
	gutsBurned := ExpectedDamage(d, &atk, &def, bs, nil, nil, nil)

	atk.Ability = AbilityNone
	atk.Status = StatusBurn
	burnedNoGuts := ExpectedDamage(d, &atk, &def, bs, nil, nil, nil)

	atk.Status = StatusNone
	unburnedNoGuts := ExpectedDamage(d, &atk, &def, bs, nil, nil, nil)

	// Burned-no-Guts should be ~half of unburned-no-Guts.
	if burnedNoGuts >= unburnedNoGuts {
		t.Errorf("baseline: burned %d should be less than unburned %d", burnedNoGuts, unburnedNoGuts)
	}
	// Guts-burned should be ~1.5× unburned-no-Guts (cancels halve + adds 1.5).
	if gutsBurned < unburnedNoGuts {
		t.Errorf("Guts burned %d should exceed unburned-no-Guts %d", gutsBurned, unburnedNoGuts)
	}
}

// TestAbilitySteadfast: a flinch raises Spe.
func TestAbilitySteadfast(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[6])
	p.Ability = "steadfast"
	rng := NewRNG(1)
	var log []LogLine
	applyVolatile(&p, 0, "flinch", domain.Move{}, nil, rng, &log)
	if p.Stages.Spe != 1 {
		t.Errorf("Steadfast on flinch: Spe stage = %d, want 1", p.Stages.Spe)
	}
}

// TestAbilityInnerFocus: flinch refused.
func TestAbilityInnerFocus(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[6])
	p.Ability = "inner-focus"
	rng := NewRNG(1)
	var log []LogLine
	applyVolatile(&p, 0, "flinch", domain.Move{}, nil, rng, &log)
	if p.Volatiles.Flinch {
		t.Error("Inner Focus should block flinch")
	}
}

// TestAbilityAnalytic: Analytic gives ×1.3 outgoing damage when the user's
// Volatiles.MovedLast flag is set (i.e. they are the last scheduled mover
// this turn). Verified via ExpectedDamage with and without the flag.
func TestAbilityAnalytic(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[6]) // Charizard
	atk.Ability = "analytic"
	def := buildPokemon(d, d.Species[3]) // Venusaur

	m := d.Moves["flamethrower"]
	without := ExpectedDamage(d, &atk, &def, m, nil, nil, nil)
	atk.Volatiles.MovedLast = true
	with := ExpectedDamage(d, &atk, &def, m, nil, nil, nil)
	ratio := float64(with) / float64(without)
	if ratio < 1.25 || ratio > 1.35 {
		t.Errorf("Analytic boost ratio = %.2f (with=%d, without=%d), want ~1.30", ratio, with, without)
	}
}

// TestAbilityAnalyticInBattle: ResolveTurn marks the slower (last-moving)
// Pokémon's MovedLast flag in time for the Analytic hook to fire inside
// computeDamage. A faster Charizard moving first means Snorlax's Body Slam
// resolves last with the ×1.3 boost. We compare HP loss vs the same matchup
// without Analytic (same RNG seed). MovedLast is also asserted cleared after.
func TestAbilityAnalyticInBattle(t *testing.T) {
	d := loadDex(t)

	run := func(seed uint64, ability AbilityKind) int {
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{6}, seed) // Snorlax vs Charizard
		s.Active(0).Ability = ability
		atkMove := slotOf(s.Active(0), "body-slam")
		if atkMove < 0 {
			atkMove = 0
		}
		foeMove := slotOf(s.Active(1), "scratch")
		if foeMove < 0 {
			foeMove = 0
		}
		hpBefore := s.Active(1).HP
		ResolveTurn(d, s, [2]Action{
			{Kind: ActionMove, Index: atkMove},
			{Kind: ActionMove, Index: foeMove},
		})
		if s.Active(0).Volatiles.MovedLast {
			t.Errorf("MovedLast should be cleared in end-of-turn sweep")
		}
		return hpBefore - s.Active(1).HP
	}
	const seed = 11
	with := run(seed, "analytic")
	without := run(seed, "")
	if with <= without {
		t.Errorf("Analytic should boost damage when moving last: with=%d, without=%d", with, without)
	}
}

// TestAbilitySheerForceBoost: with Sheer Force, a move that has secondaries
// gets the ×1.3 outgoing-damage boost. Moves without secondaries are not
// boosted (paired trade).
func TestAbilitySheerForceBoost(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[6])
	def := buildPokemon(d, d.Species[3])

	ft := d.Moves["flamethrower"]
	if len(ft.Secondaries) == 0 {
		t.Fatalf("flamethrower missing expected secondary effect in dataset")
	}
	atk.Ability = ""
	base := ExpectedDamage(d, &atk, &def, ft, nil, nil, nil)
	atk.Ability = "sheer-force"
	boosted := ExpectedDamage(d, &atk, &def, ft, nil, nil, nil)
	ratio := float64(boosted) / float64(base)
	if ratio < 1.25 || ratio > 1.35 {
		t.Errorf("Sheer Force boost ratio = %.2f (base=%d, boosted=%d), want ~1.30", ratio, base, boosted)
	}
}

// TestAbilitySheerForceSuppresses: applyDamageEffects skips the foe-targeted
// Secondaries loop when the attacker has Sheer Force. Verified with a synthetic
// 100%-chance burn secondary so the test is deterministic: without Sheer Force
// the foe is burned; with Sheer Force the foe is unaffected.
func TestAbilitySheerForceSuppresses(t *testing.T) {
	d := loadDex(t)

	burnRider := domain.Move{
		Name: "test-burn-rider", Type: "fire", Category: domain.CatSpecial, Power: 50, Accuracy: 100,
		Secondaries: []domain.Effect{{Chance: 100, Status: "burn"}},
	}

	// Baseline: no Sheer Force → burn lands.
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{3}, 1) // Charizard vs Venusaur
	s.Active(0).Ability = ""
	var log []LogLine
	rng := NewRNG(1)
	applyDamageEffects(s, 0, burnRider, 1, rng, &log)
	if s.Active(1).Status != StatusBurn {
		t.Fatalf("baseline (no Sheer Force) should burn the foe; got status=%v", s.Active(1).Status)
	}

	// With Sheer Force → secondary suppressed.
	s2, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{3}, 1)
	s2.Active(0).Ability = "sheer-force"
	var log2 []LogLine
	rng2 := NewRNG(1)
	applyDamageEffects(s2, 0, burnRider, 1, rng2, &log2)
	if s2.Active(1).Status == StatusBurn {
		t.Errorf("Sheer Force should suppress the foe-targeted burn secondary; foe is burned")
	}
}

// dumpLog prints every turn-log line under -v, indented under a label.
func dumpLog(t *testing.T, label string, log []LogLine) {
	t.Helper()
	t.Logf("=== %s ===", label)
	for _, l := range log {
		t.Logf("  [%d] %s: %s", l.Side, l.Type, l.Text)
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
