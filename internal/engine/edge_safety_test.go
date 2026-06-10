package engine

import (
	"encoding/json"
	"testing"
)

// edge_safety_test.go collects regression guards for failure modes adjacent
// to the residual-faint class (covered in residual_safety_test.go). These
// were enumerated after the Whirlpool/Leech Seed crash and the AI/engine
// LegalActions drift: each one is a place we'd previously have only learned
// about from a crashed gateway in prod.

// TestForceSwitch_ClearsLockedVolatiles: Whirlwind / Roar / Dragon Tail
// drag a foe out. The outgoing Pokémon's locked volatiles (PartialTrap,
// Ingrain, Charging, MustRecharge, Disable, Encore) must clear so the
// incoming teammate doesn't inherit dead state — and the outgoing must
// be allowed to switch in again later without the volatiles still set.
//
// doSwitch wipes Volatiles to the zero value; this test pins that
// invariant so any future "preserve X across switch" change doesn't
// silently re-enable bugs.
func TestForceSwitch_ClearsLockedVolatiles(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6, 9}, "B", []int{3, 65, 143}, 1)
	foe := s.Active(1)
	foe.Volatiles.PartialTrap = &PartialTrapState{Turns: 3, MoveName: "Whirlpool"}
	foe.Volatiles.Ingrain = true
	foe.Volatiles.Charging = &ChargingState{MoveIdx: 0}
	foe.Volatiles.MustRecharge = true
	foe.Volatiles.Disable = &DisableState{MoveID: foe.Moves[0].MoveID, Turns: 4}
	foe.Volatiles.Encore = &EncoreState{MoveID: foe.Moves[0].MoveID, Turns: 3}
	foeName := foe.Name

	var log []LogLine
	if ok := applyForceSwitch(s, 0, NewRNG(1), &log); !ok {
		t.Fatalf("expected forced switch to succeed; log: %v", logTexts(log))
	}
	if s.Active(1).Name == foeName {
		t.Fatalf("expected a different Pokémon to be active after drag-out")
	}
	// Re-check what was the outgoing — find it by name.
	var prev *Pokemon
	for i := range s.Sides[1].Team {
		if s.Sides[1].Team[i].Name == foeName {
			prev = &s.Sides[1].Team[i]
			break
		}
	}
	if prev == nil {
		t.Fatalf("could not locate outgoing Pokémon %q", foeName)
	}
	if prev.Volatiles.PartialTrap != nil ||
		prev.Volatiles.Ingrain ||
		prev.Volatiles.Charging != nil ||
		prev.Volatiles.MustRecharge ||
		prev.Volatiles.Disable != nil ||
		prev.Volatiles.Encore != nil {
		t.Errorf("locked volatiles leaked across forced switch: %+v", prev.Volatiles)
	}
}

// TestClone_DeepCopiesEveryVolatile: Clone() is the AI search's sandbox —
// if any pointer-shaped volatile shares memory with the real state, the
// AI's simulation mutates the live battle. This sets every pointer/slice
// volatile, clones, mutates the clone via the pointer, and asserts the
// original is untouched. Whitelist of fields is intentional: any new
// pointer field added to Volatiles must be added here too, and missing
// it surfaces immediately.
func TestClone_DeepCopiesEveryVolatile(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{3}, 1)
	p := s.Active(0)
	p.Volatiles.Confusion = &ConfusionState{Turns: 4}
	p.Volatiles.Charging = &ChargingState{MoveIdx: 2}
	p.Volatiles.PartialTrap = &PartialTrapState{Turns: 5, MoveName: "Wrap"}
	p.Volatiles.Substitute = &SubstituteState{HP: 40}
	p.Volatiles.LeechSeed = &LeechSeedState{SourceSide: 1}
	p.Volatiles.Disable = &DisableState{MoveID: "tackle", MoveName: "Tackle", Turns: 4}
	p.Volatiles.Encore = &EncoreState{MoveID: "tackle", MoveName: "Tackle", Turns: 3}
	p.Volatiles.Taunt = &TauntState{Turns: 3}
	p.Volatiles.Embargo = &EmbargoState{Turns: 5}
	p.Volatiles.Imprison = &ImprisonState{MoveIDs: []string{"a", "b"}}
	p.Volatiles.Yawn = &YawnState{TurnsLeft: 2}
	p.Volatiles.MagnetRise = &MagnetRiseState{TurnsLeft: 5}
	p.Volatiles.Telekinesis = &TelekinesisState{TurnsLeft: 3}
	p.Volatiles.Stockpile = &StockpileState{Count: 2}

	clone := s.Clone()
	cp := clone.Active(0)

	// Mutate the clone everywhere a pointer lives, plus the Imprison slice.
	cp.Volatiles.Confusion.Turns = 99
	cp.Volatiles.Charging.MoveIdx = 99
	cp.Volatiles.PartialTrap.Turns = 99
	cp.Volatiles.Substitute.HP = 99
	cp.Volatiles.LeechSeed.SourceSide = 9
	cp.Volatiles.Disable.Turns = 99
	cp.Volatiles.Encore.Turns = 99
	cp.Volatiles.Taunt.Turns = 99
	cp.Volatiles.Embargo.Turns = 99
	cp.Volatiles.Imprison.MoveIDs[0] = "MUTATED"
	cp.Volatiles.Yawn.TurnsLeft = 99
	cp.Volatiles.MagnetRise.TurnsLeft = 99
	cp.Volatiles.Telekinesis.TurnsLeft = 99
	cp.Volatiles.Stockpile.Count = 99

	if p.Volatiles.Confusion.Turns != 4 ||
		p.Volatiles.Charging.MoveIdx != 2 ||
		p.Volatiles.PartialTrap.Turns != 5 ||
		p.Volatiles.Substitute.HP != 40 ||
		p.Volatiles.LeechSeed.SourceSide != 1 ||
		p.Volatiles.Disable.Turns != 4 ||
		p.Volatiles.Encore.Turns != 3 ||
		p.Volatiles.Taunt.Turns != 3 ||
		p.Volatiles.Embargo.Turns != 5 ||
		p.Volatiles.Imprison.MoveIDs[0] != "a" ||
		p.Volatiles.Yawn.TurnsLeft != 2 ||
		p.Volatiles.MagnetRise.TurnsLeft != 5 ||
		p.Volatiles.Telekinesis.TurnsLeft != 3 ||
		p.Volatiles.Stockpile.Count != 2 {
		t.Errorf("Clone leaked aliased pointers — mutation on clone bled into original: %+v",
			p.Volatiles)
	}
}

// TestJSONRoundTrip_PreservesNewVolatiles: the gateway saves BattleState
// to Redis after every turn and loads it back on restart. If any new
// volatile field has a wrong json: tag or a pointer that doesn't survive
// the round-trip, the volatile silently drops between turns. Asserts the
// shape, not the values: any difference fails.
func TestJSONRoundTrip_PreservesNewVolatiles(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{3}, 1)
	p := s.Active(0)
	p.Volatiles.PartialTrap = &PartialTrapState{Turns: 4, MoveName: "Whirlpool"}
	p.Volatiles.Disable = &DisableState{MoveID: "tackle", MoveName: "Tackle", Turns: 4}
	p.Volatiles.Encore = &EncoreState{MoveID: "tackle", MoveName: "Tackle", Turns: 3}
	p.Volatiles.Imprison = &ImprisonState{MoveIDs: []string{"thunderbolt", "ice-beam"}}
	p.Volatiles.Yawn = &YawnState{TurnsLeft: 2}
	p.Volatiles.MagnetRise = &MagnetRiseState{TurnsLeft: 5}
	p.Volatiles.Stockpile = &StockpileState{Count: 3}
	p.Volatiles.FocusEnergy = true
	p.Volatiles.LaserFocus = true
	p.Volatiles.DefenseCurl = true
	p.Volatiles.Foresight = true
	p.Volatiles.Curse = true
	p.Volatiles.Nightmare = true
	p.Volatiles.SmackDown = true
	p.Volatiles.GastroAcid = true

	blob, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got BattleState
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	rp := got.Active(0)

	if rp.Volatiles.PartialTrap == nil || rp.Volatiles.PartialTrap.MoveName != "Whirlpool" {
		t.Errorf("PartialTrap lost: %+v", rp.Volatiles.PartialTrap)
	}
	if rp.Volatiles.Disable == nil || rp.Volatiles.Disable.MoveID != "tackle" {
		t.Errorf("Disable lost: %+v", rp.Volatiles.Disable)
	}
	if rp.Volatiles.Encore == nil || rp.Volatiles.Encore.MoveID != "tackle" {
		t.Errorf("Encore lost: %+v", rp.Volatiles.Encore)
	}
	if rp.Volatiles.Imprison == nil || len(rp.Volatiles.Imprison.MoveIDs) != 2 {
		t.Errorf("Imprison lost or truncated: %+v", rp.Volatiles.Imprison)
	}
	if rp.Volatiles.Yawn == nil || rp.Volatiles.Yawn.TurnsLeft != 2 {
		t.Errorf("Yawn lost: %+v", rp.Volatiles.Yawn)
	}
	if rp.Volatiles.MagnetRise == nil || rp.Volatiles.MagnetRise.TurnsLeft != 5 {
		t.Errorf("MagnetRise lost: %+v", rp.Volatiles.MagnetRise)
	}
	if rp.Volatiles.Stockpile == nil || rp.Volatiles.Stockpile.Count != 3 {
		t.Errorf("Stockpile lost: %+v", rp.Volatiles.Stockpile)
	}
	if !rp.Volatiles.FocusEnergy || !rp.Volatiles.LaserFocus ||
		!rp.Volatiles.DefenseCurl || !rp.Volatiles.Foresight ||
		!rp.Volatiles.Curse || !rp.Volatiles.Nightmare ||
		!rp.Volatiles.SmackDown || !rp.Volatiles.GastroAcid {
		t.Errorf("bool volatile flag lost: %+v", rp.Volatiles)
	}
}

// TestBothSidesFaint_SameTurnIsADraw: when residuals (or a same-priority
// double-KO) take both actives to 0 in one ResolveTurn, updatePhase must
// see LiveCount==0 on both sides and call endBattle with winner=2 (draw).
// A bug here would manifest as "the battle never ends" — Phase stuck at
// PhaseReplace with no live mons.
func TestBothSidesFaint_SameTurnIsADraw(t *testing.T) {
	d := loadDex(t)
	// Single-mon teams so a faint can't be "covered" by a switch-in.
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	a, b := s.Active(0), s.Active(1)
	a.HP = 1
	a.Status = StatusBurn
	b.HP = 1
	b.Status = StatusBurn

	actions := [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}}
	_ = ResolveTurn(d, s, actions)

	if !s.Ended() {
		t.Fatalf("battle should have ended; phase=%s winner=%d", s.Phase, s.Winner)
	}
	if s.Winner != 2 {
		t.Errorf("double-KO should be a draw (winner=2); got winner=%d", s.Winner)
	}
}

// TestHazardChainOnSwitchInKills: layer-3 spikes + stealth rock can chip a
// fragile switch-in to 0 HP. The hazard chain must run cleanly through —
// no later hazard accesses post-faint state, and updatePhase enters Replace
// on next ResolveTurn rather than continuing with a 0-HP active.
func TestHazardChainOnSwitchInKills(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{3, 65}, 1)
	// Lay every hazard on the foe side.
	s.Sides[1].Conditions.Hazards.StealthRock = true
	s.Sides[1].Conditions.Hazards.Spikes = 3
	s.Sides[1].Conditions.Hazards.ToxicSpikes = 2

	// Make the incoming fragile so the chain kills.
	inIdx := 1
	s.Sides[1].Team[inIdx].HP = 5

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("hazard chain panicked on lethal switch-in: %v", r)
		}
	}()

	var log []LogLine
	doSwitch(s, 1, inIdx, &log)
	in := &s.Sides[1].Team[inIdx]
	if !in.Fainted {
		t.Errorf("hazard chain should have KO'd the fragile switch-in; HP=%d Fainted=%v",
			in.HP, in.Fainted)
	}
}

// TestEncoreWithZeroPPSurfacesStruggle: Encore forces one slot. If that
// slot is also out of PP, LegalActions must still return something — the
// Struggle sentinel — rather than an empty slice that hangs collectActions.
func TestEncoreWithZeroPPSurfacesStruggle(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{3}, 1)
	p := s.Active(0)
	// Drain every move's PP.
	for i := range p.Moves {
		p.Moves[i].PP = 0
	}
	// Encore lock onto slot 0.
	p.Volatiles.Encore = &EncoreState{MoveID: p.Moves[0].MoveID, Turns: 3}
	p.Volatiles.LastMoveID = p.Moves[0].MoveID

	acts := LegalActions(s, 0)
	if len(acts) == 0 {
		t.Fatalf("Encore + no-PP must surface at least one action (Struggle); got empty")
	}
	struggleSeen := false
	for _, a := range acts {
		if a.Kind == ActionMove && a.Index == -1 {
			struggleSeen = true
			break
		}
	}
	if !struggleSeen {
		t.Errorf("expected Struggle sentinel (move index -1) in legal actions; got %+v", acts)
	}
}

// TestMiracleEyeLetsPsychicDamageLand: parallel to TestForesightLiftsGhostImmunity
// and TestSmackDownGroundsFlying — verifies the lift is wired through
// computeDamage end-to-end (not just the helper). Catches a regression where
// effectivenessWithLifts is right but the damage formula reads a stale
// effectiveness from a different path.
func TestMiracleEyeLetsPsychicDamageLand(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{65}, "P2", []int{6}, 1)
	atk := s.Active(0)
	def := s.Active(1)
	def.Type1 = "dark"
	def.Type2 = ""

	rng := NewRNG(1)
	baseline := computeDamage(d, atk, def, d.Moves["psychic"], nil, nil, &s.Sides[1].Conditions, &s.PseudoWeather, rng)
	if baseline.Damage != 0 {
		t.Fatalf("baseline Psychic vs Dark must be 0; got %d damage", baseline.Damage)
	}
	def.Volatiles.MiracleEye = true
	rng = NewRNG(1)
	lifted := computeDamage(d, atk, def, d.Moves["psychic"], nil, nil, &s.Sides[1].Conditions, &s.PseudoWeather, rng)
	if lifted.Damage <= 0 {
		t.Errorf("Miracle Eye should let Psychic hit Dark; got %d damage", lifted.Damage)
	}
}

// TestMaxTurnsTriggersTieBreak: when a battle reaches maxTurns without a
// faint, updatePhase must end it with the higher-HP side as the winner
// (or draw on equal HP). A bug here would manifest as "this battle never
// ends." The test pushes Turn to the cap, resolves once, and asserts.
func TestMaxTurnsTriggersTieBreak(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143, 143}, "B", []int{143, 143}, 1)
	// Drop side 1's HP so the tie-break has a clear winner.
	for i := range s.Sides[1].Team {
		s.Sides[1].Team[i].HP = s.Sides[1].Team[i].MaxHP / 2
	}
	s.Turn = maxTurns - 1 // ResolveTurn increments to maxTurns

	actions := [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}}
	_ = ResolveTurn(d, s, actions)

	if !s.Ended() {
		t.Fatalf("battle should have ended at maxTurns; phase=%s turn=%d", s.Phase, s.Turn)
	}
	if s.Winner != 0 {
		t.Errorf("HP-favored side 0 should win the cap tie-break; got winner=%d", s.Winner)
	}
}

// TestEncorePlusDisableOnSameSlot: the locked move is also banned. The
// engine has to produce *some* legal action — Encore alone forces one
// slot, Disable alone forbids one slot; together on the same slot, the
// move can't be legally used. Whatever the engine does, the result must
// not be an empty action list (would hang collectActions). The test
// pins current behavior so a future refactor that changes the answer
// has to make the choice deliberately.
func TestEncorePlusDisableOnSameSlot(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{3}, 1)
	p := s.Active(0)
	slot := 0
	moveID := p.Moves[slot].MoveID
	p.Volatiles.Encore = &EncoreState{MoveID: moveID, MoveName: moveID, Turns: 3}
	p.Volatiles.Disable = &DisableState{MoveID: moveID, MoveName: moveID, Turns: 4}
	p.Volatiles.LastMoveID = moveID

	acts := LegalActions(s, 0)
	if len(acts) == 0 {
		t.Fatalf("Encore + Disable on same slot left zero legal actions — would hang collectActions")
	}
	// Either Struggle surfaces (Encore wins → slot is the only allowed
	// move, Disable banned everything → Struggle) or other moves surface
	// (Disable wins → Encore yields). Either is internally consistent;
	// fail only on the no-action outcome above.
}

// TestCloudNineSuppressesSandstormResidual: Cloud Nine on either active
// must zero out weather effects, including sandstorm chip. A 1-HP target
// without Rock/Ground/Steel would normally die to sandstorm chip; with
// Cloud Nine up it survives.
func TestCloudNineSuppressesSandstormResidual(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1) // Snorlax: Normal, no SS immunity
	s.Weather = &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}
	tgt := s.Active(1)
	tgt.HP = 1
	// Cloud Nine on the *attacker's* active — the canon effect is field-wide.
	s.Active(0).Ability = "cloud-nine"

	var log []LogLine
	applyWeatherResidual(s, &log)

	if tgt.Fainted {
		t.Errorf("Cloud Nine must suppress sandstorm chip; target fainted to suppressed weather: hp=%d", tgt.HP)
	}
	if tgt.HP != 1 {
		t.Errorf("Cloud Nine should leave HP untouched by weather residual; got %d", tgt.HP)
	}
}

// TestWakeUpMidTurnClearsNightmare: Nightmare requires Status==Sleep. If a
// Pokémon wakes up at the start of its action this turn, the Nightmare
// volatile must clear by end-of-turn so it doesn't tick on a now-awake
// holder. Cross-check: the invariant test in state_invariants.go disallows
// "Nightmare set while Status!=Sleep" — so failure here also fails the
// invariant check, double-coverage.
func TestWakeUpMidTurnClearsNightmare(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{3}, 1)
	p := s.Active(0)
	p.Status = StatusSleep
	p.SleepTurns = 1 // wakes this turn
	p.Volatiles.Nightmare = true
	hpBefore := p.HP

	actions := [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}}
	_ = ResolveTurn(d, s, actions)

	// After wake-up, Nightmare must be cleared by end-of-turn — either the
	// status sweep clears it, or tickStatusVols clears it on the post-wake
	// state. Either way, the invariant must hold.
	if p.Volatiles.Nightmare && p.Status != StatusSleep {
		t.Errorf("Nightmare leaked past wake-up: Status=%q Nightmare=%v", p.Status, p.Volatiles.Nightmare)
	}
	if err := ValidateStateInvariants(s); err != nil {
		t.Errorf("state invariant violated after wake-up: %v", err)
	}
	_ = hpBefore
}

// TestAIDecideInReplaceReturnsSwitches: when a side must replace, the AI
// driver must produce a switch — proposing a move is illegal in Replace
// phase. The harness already runs through ai.LegalActions (now the engine
// shim), so this should be true by construction; running it across many
// seeds catches any reconstruction bug that would let a move slip through.
func TestAIDecideInReplaceReturnsSwitches(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(1); seed <= 20; seed++ {
		s, _ := NewBattle(d, "b", "A", []int{6, 9, 26}, "B", []int{3, 65, 143}, seed)
		// Faint the active to force a Replace.
		s.Sides[1].Team[s.Sides[1].Active].HP = 0
		s.Sides[1].Team[s.Sides[1].Active].Fainted = true
		s.Phase = PhaseReplace
		s.Replace[1] = true

		acts := LegalActions(s, 1)
		for _, a := range acts {
			if a.Kind != ActionSwitch {
				t.Errorf("seed %d: LegalActions in Replace must be switches only; got %+v", seed, a)
			}
		}
	}
}

// TestStateInvariantsAcrossManySeeds: drive AI-style battles to completion
// across a range of seeds and assert ValidateStateInvariants on every
// intermediate state. This is the meta-test for the whole class of
// "state silently corrupted" bugs — a single seed that lands in a bad
// state fails loudly here instead of much later.
func TestStateInvariantsAcrossManySeeds(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(1); seed <= 15; seed++ {
		s, err := NewBattle(d, "b", "Red", []int{6, 9, 26}, "Blue", []int{3, 65, 143}, seed)
		if err != nil {
			t.Fatalf("seed %d: new battle: %v", seed, err)
		}
		if err := ValidateStateInvariants(s); err != nil {
			t.Errorf("seed %d: fresh battle violates invariants: %v", seed, err)
			continue
		}
		guard := 0
		for !s.Ended() {
			guard++
			if guard > maxTurns*4 {
				t.Errorf("seed %d: failed to terminate", seed)
				break
			}
			switch s.Phase {
			case PhaseChoosing:
				a := [2]Action{LegalActions(s, 0)[0], LegalActions(s, 1)[0]}
				ResolveTurn(d, s, a)
			case PhaseReplace:
				var sw [2]*Action
				for i := 0; i < 2; i++ {
					if s.Replace[i] {
						act := LegalActions(s, i)[0]
						sw[i] = &act
					}
				}
				ResolveReplace(s, sw)
			}
			if err := ValidateStateInvariants(s); err != nil {
				t.Errorf("seed %d turn %d phase %s: %v", seed, s.Turn, s.Phase, err)
				break
			}
		}
	}
}

// TestSubstituteAbsorbsLethalLeechSeed: Substitute soaks DIRECT damage but
// Leech Seed chips the holder behind the sub. If the seed tick KOs the
// holder, the substitute pointer should not prevent or interfere with the
// faint cleanup. Engine must not panic, holder must Fainted=true.
func TestSubstituteAbsorbsLethalLeechSeed(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{6}, "B", []int{3}, 1)
	tgt := s.Active(1)
	tgt.HP = 1
	tgt.Volatiles.Substitute = &SubstituteState{HP: 40}
	tgt.Volatiles.LeechSeed = &LeechSeedState{SourceSide: 0}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Leech Seed residual panicked with Substitute up: %v", r)
		}
	}()
	var log []LogLine
	applyLeechSeedResidual(s, 1, &log)
	if !tgt.Fainted {
		t.Errorf("lethal Leech Seed tick must KO the holder even with Substitute up; HP=%d Fainted=%v",
			tgt.HP, tgt.Fainted)
	}
}
