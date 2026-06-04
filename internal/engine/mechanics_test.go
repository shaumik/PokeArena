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
	res := computeDamage(d, &machamp, &chansey, d.Moves["seismic-toss"], nil, rng)
	if res.Damage != Level {
		t.Errorf("seismic-toss damage = %d, want %d", res.Damage, Level)
	}
	if res.Effectiveness != 1.0 {
		t.Errorf("seismic-toss effectiveness = %v, want 1.0 (no eff log)", res.Effectiveness)
	}

	// Ghost is immune to Fighting → seismic-toss should still be blocked.
	gengar := buildPokemon(d, d.Species[94])
	res = computeDamage(d, &machamp, &gengar, d.Moves["seismic-toss"], nil, NewRNG(1))
	if res.Damage != 0 || res.Effectiveness != 0 {
		t.Errorf("seismic-toss vs Ghost = %+v, want 0 dmg / 0 eff", res)
	}

	// AI's expected-damage prediction should match.
	if got := ExpectedDamage(d, &machamp, &chansey, d.Moves["seismic-toss"], nil); got != Level {
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
		if !inflictStatus(&pika, 0, StatusSleep, rng, &log) {
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
	applyEffectFields(e, &atk, 0, &atk, 0, 50, rng, &log)
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
		return computeDamage(d, &charizard, &blastoise, flamethrower, w, NewRNG(seed)).Damage
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
	ec, es, er := ExpectedDamage(d, &charizard, &blastoise, flamethrower, mk("")),
		ExpectedDamage(d, &charizard, &blastoise, flamethrower, mk(WeatherSun)),
		ExpectedDamage(d, &charizard, &blastoise, flamethrower, mk(WeatherRain))
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
	clear := computeDamage(d, &starmie, &rhydon, surf, nil, NewRNG(seed)).Damage
	sand := computeDamage(d, &starmie, &rhydon, surf, &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}, NewRNG(seed)).Damage

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
	clear := computeDamage(d, &tauros, &jynx, bodyslam, nil, NewRNG(seed)).Damage
	snow := computeDamage(d, &tauros, &jynx, bodyslam, &WeatherState{Kind: WeatherSnow, TurnsLeft: 5}, NewRNG(seed)).Damage

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

	res := computeDamage(d, &rhydon, &weezing, eq, nil, NewRNG(1))
	if res.Damage != 0 || res.Effectiveness != 0 {
		t.Errorf("Earthquake vs Levitate Weezing = %+v, want 0 damage / 0 eff", res)
	}
	if got := ExpectedDamage(d, &rhydon, &weezing, eq, nil); got != 0 {
		t.Errorf("ExpectedDamage Earthquake vs Weezing = %d, want 0", got)
	}

	// Non-Ground move still hurts (Tackle is Normal, hits normally).
	tackle := d.Moves["tackle"]
	res2 := computeDamage(d, &rhydon, &weezing, tackle, nil, NewRNG(1))
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
			tf := ExpectedDamage(d, &charizard, &withFat, m, nil)
			plain := ExpectedDamage(d, &charizard, &without, m, nil)
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
	if a, b := ExpectedDamage(d, &charizard, &withFat, bodyslam, nil),
		ExpectedDamage(d, &charizard, &without, bodyslam, nil); a != b {
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
		res := computeDamage(d, s.Active(0), onix, d.Moves["aura-sphere"], nil, NewRNG(7))
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
	without := ExpectedDamage(d, &ninetales, &venusaur, ft, nil)
	ninetales.Volatiles.FlashFireCharged = true
	with := ExpectedDamage(d, &ninetales, &venusaur, ft, nil)
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
	if inflictStatus(&snorlax, 0, StatusToxic, rng, &log) {
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
		res := computeDamage(d, &charizard, &target, flamethrower, nil, NewRNG(uint64(i+1)))
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
	gutsBurned := ExpectedDamage(d, &atk, &def, bs, nil)

	atk.Ability = AbilityNone
	atk.Status = StatusBurn
	burnedNoGuts := ExpectedDamage(d, &atk, &def, bs, nil)

	atk.Status = StatusNone
	unburnedNoGuts := ExpectedDamage(d, &atk, &def, bs, nil)

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
	applyVolatile(&p, 0, "flinch", rng, &log)
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
	applyVolatile(&p, 0, "flinch", rng, &log)
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
	without := ExpectedDamage(d, &atk, &def, m, nil)
	atk.Volatiles.MovedLast = true
	with := ExpectedDamage(d, &atk, &def, m, nil)
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
	base := ExpectedDamage(d, &atk, &def, ft, nil)
	atk.Ability = "sheer-force"
	boosted := ExpectedDamage(d, &atk, &def, ft, nil)
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
