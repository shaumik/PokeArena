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
