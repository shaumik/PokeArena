package engine

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// statswapmoves_behavior_test.go covers the twelve moves the inert audit found:
// the ones that narrated a hit and changed nothing. Each is reached through a
// real turn rather than by calling its handler, because the defect was never in
// the handler — there wasn't one — it was that no battle did anything at all.

// inertBattle is the audit's fixture, reused: both sides stripped to nothing,
// so the only live mechanic is the move under test.
func inertBattle(t *testing.T, d *domain.Dex, moveID, foeMove string) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "sw", "P1", []int{143, 65}, "P2", []int{143, 65}, 7)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			p.Item = ItemNone
			p.Ability = AbilityNone
			p.Moves = []MoveSlot{{MoveID: foeMove, PP: 40, MaxPP: 40}}
		}
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 40, MaxPP: 40}}
	return s
}

// TestGuardSwapAndPowerSwapExchangeTheRightStages: each takes exactly two
// stages and leaves the others where they are. Getting the pair wrong would
// leave both moves "working" in a way no test that only checked for movement
// would catch.
func TestGuardSwapAndPowerSwapExchangeTheRightStages(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		move      string
		userAfter Stages
		foeAfter  Stages
		what      string
	}{
		{
			move:      "guard-swap",
			userAfter: Stages{Atk: 2, Def: 3, SpA: 0, SpD: 1, Spe: 1},
			foeAfter:  Stages{Atk: -2, Def: -1, SpA: 1, SpD: -3, Spe: 0},
			what:      "Defense and Sp. Def",
		},
		{
			move:      "power-swap",
			userAfter: Stages{Atk: -2, Def: -1, SpA: 1, SpD: -3, Spe: 1},
			foeAfter:  Stages{Atk: 2, Def: 3, SpA: 0, SpD: 1, Spe: 0},
			what:      "Attack and Sp. Atk",
		},
	} {
		s := inertBattle(t, d, c.move, "splash")
		user, foe := s.Active(0), s.Active(1)
		user.Stages = Stages{Atk: 2, Def: -1, SpA: 0, SpD: -3, Spe: 1}
		foe.Stages = Stages{Atk: -2, Def: 3, SpA: 1, SpD: 1, Spe: 0}

		playTurn(d, s, 0, 0)
		if user.Stages != c.userAfter {
			t.Errorf("%s: user stages = %+v, want %+v (%s only)", c.move, user.Stages, c.userAfter, c.what)
		}
		if foe.Stages != c.foeAfter {
			t.Errorf("%s: foe stages = %+v, want %+v (%s only)", c.move, foe.Stages, c.foeAfter, c.what)
		}
	}
}

// TestSpeedSwapExchangesStatsNotStages: the distinction is the move. Swapping
// stages would be a far weaker effect, and the slow bulky Pokémon that run
// Speed Swap run it precisely to borrow a fast body's legs.
func TestSpeedSwapExchangesStatsNotStages(t *testing.T) {
	d := loadDex(t)
	s := inertBattle(t, d, "speed-swap", "splash")
	// Alakazam in for the foe: its Speed is nothing like Snorlax's, so the
	// exchange is unmistakable.
	s.Sides[1].Active = 1
	user, foe := s.Active(0), s.Active(1)
	foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	userSpe, foeSpe := user.Stats.Spe, foe.Stats.Spe
	if userSpe == foeSpe {
		t.Fatalf("setup: the two need different Speed to measure a swap")
	}

	playTurn(d, s, 0, 0)
	if user.Stats.Spe != foeSpe || foe.Stats.Spe != userSpe {
		t.Errorf("Speed should have changed hands: user %d (want %d), foe %d (want %d)",
			user.Stats.Spe, foeSpe, foe.Stats.Spe, userSpe)
	}
	if user.Stages.Spe != 0 || foe.Stages.Spe != 0 {
		t.Errorf("Speed Swap moves stats, not stages: %d / %d", user.Stages.Spe, foe.Stages.Spe)
	}
}

// TestPowerSplitAveragesTheOffenses: both sides end on the same Attack and the
// same Sp. Atk, and it is the mean of what they brought.
func TestPowerSplitAveragesTheOffenses(t *testing.T) {
	d := loadDex(t)
	s := inertBattle(t, d, "power-split", "splash")
	s.Sides[1].Active = 1 // Alakazam: high Sp. Atk, low Attack
	user, foe := s.Active(0), s.Active(1)
	foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	wantAtk := (user.Stats.Atk + foe.Stats.Atk) / 2
	wantSpA := (user.Stats.SpA + foe.Stats.SpA) / 2

	playTurn(d, s, 0, 0)
	if user.Stats.Atk != wantAtk || foe.Stats.Atk != wantAtk {
		t.Errorf("Attack should be shared at %d, got %d / %d", wantAtk, user.Stats.Atk, foe.Stats.Atk)
	}
	if user.Stats.SpA != wantSpA || foe.Stats.SpA != wantSpA {
		t.Errorf("Sp. Atk should be shared at %d, got %d / %d", wantSpA, user.Stats.SpA, foe.Stats.SpA)
	}
}

// TestRewrittenStatsRevertOnSwitchOut: canon keeps these edits on storedStats,
// which clearVolatile throws away by re-running setSpecies, so the change lasts
// exactly as long as the Pokémon is on the field. Without the revert a Speed
// Swap would permanently rewrite a team.
func TestRewrittenStatsRevertOnSwitchOut(t *testing.T) {
	d := loadDex(t)
	s := inertBattle(t, d, "speed-swap", "splash")
	user := s.Active(0)
	before := user.Stats.Spe
	s.Sides[1].Active = 1
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	playTurn(d, s, 0, 0)
	if user.Stats.Spe == before {
		t.Fatalf("setup: the swap should have changed the user's Speed")
	}
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
	if got := s.Sides[0].Team[0].Stats.Spe; got != before {
		t.Errorf("leaving the field should restore the real spread: Speed = %d, want %d", got, before)
	}
	if s.Sides[0].Team[0].BaseStats != nil {
		t.Error("the memo should be spent on the way out")
	}
}

// TestHealPulseHealsTheOpponent: in singles the only legal target is the foe,
// so this move restores the opposing Pokémon. That reads like a bug and is not
// one — Heal Pulse is a doubles support move, and canon's "any" target excludes
// the user. Pinned because it is exactly the kind of thing a later reader
// "fixes".
func TestHealPulseHealsTheOpponent(t *testing.T) {
	d := loadDex(t)
	s := inertBattle(t, d, "heal-pulse", "splash")
	user, foe := s.Active(0), s.Active(1)
	foe.HP = foe.MaxHP / 4
	userHP := user.HP

	playTurn(d, s, 0, 0)
	want := foe.MaxHP/4 + (foe.MaxHP+1)/2
	if foe.HP != want {
		t.Errorf("Heal Pulse should restore half the target's max HP: %d, want %d", foe.HP, want)
	}
	if user.HP != userHP {
		t.Errorf("the user heals nothing: %d, want %d", user.HP, userHP)
	}

	// Full HP is a visible failure, not a silent no-op.
	foe.HP = foe.MaxHP
	if log := playTurn(d, s, 0, 0); !logHas(log, "But it failed!") {
		t.Errorf("Heal Pulse into a full-HP target should fail loudly, got %v", logTexts(log))
	}
}

// TestRefreshCuresOnlyWhatItCanCure: burn, poison and paralysis go; nothing at
// all is a visible refusal.
func TestRefreshCuresOnlyWhatItCanCure(t *testing.T) {
	d := loadDex(t)
	for _, st := range []StatusCond{StatusPoison, StatusBurn, StatusParalysis} {
		s := inertBattle(t, d, "refresh", "splash")
		user := s.Active(0)
		user.Status = st
		playTurn(d, s, 0, 0)
		if user.Status != StatusNone {
			t.Errorf("Refresh should cure %q, still %q", st, user.Status)
		}
	}
	s := inertBattle(t, d, "refresh", "splash")
	if log := playTurn(d, s, 0, 0); !logHas(log, "But it failed!") {
		t.Errorf("Refresh with nothing to cure should fail loudly, got %v", logTexts(log))
	}
}

// TestRefreshRefusesSleepAndFreeze: the refusal that makes Refresh something
// other than a worse Rest. Reached through the handler rather than through a
// turn, and deliberately so — a sleeping user never gets to move, and a frozen
// one may thaw on its own, so the battle path cannot observe this cleanly. The
// engine being *right* about both of those is what makes the direct call the
// only honest way to test the rule.
func TestRefreshRefusesSleepAndFreeze(t *testing.T) {
	d := loadDex(t)
	for _, st := range []StatusCond{StatusSleep, StatusFreeze} {
		s := inertBattle(t, d, "refresh", "splash")
		user := s.Active(0)
		user.Status = st
		user.SleepTurns = 3
		var log []LogLine
		applyRefresh(s, 0, &log)
		if user.Status != st {
			t.Errorf("Refresh must not cure %q, but the status became %q", st, user.Status)
		}
		if !logHas(log, "But it failed!") {
			t.Errorf("Refresh against %q should fail loudly, got %v", st, logTexts(log))
		}
	}
}

// TestVenomDrenchNeedsAPoisonedTarget: three drops on a poisoned target, a
// visible refusal on anything else.
func TestVenomDrenchNeedsAPoisonedTarget(t *testing.T) {
	d := loadDex(t)
	s := inertBattle(t, d, "venom-drench", "splash")
	foe := s.Active(1)
	foe.Status = StatusToxic

	playTurn(d, s, 0, 0)
	if got := (Stages{Atk: -1, SpA: -1, Spe: -1}); foe.Stages != got {
		t.Errorf("Venom Drench should drop Attack, Sp. Atk and Speed: %+v, want %+v", foe.Stages, got)
	}

	clean := inertBattle(t, d, "venom-drench", "splash")
	log := playTurn(d, clean, 0, 0)
	if clean.Active(1).Stages != (Stages{}) {
		t.Errorf("an unpoisoned target keeps its stats, got %+v", clean.Active(1).Stages)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("Venom Drench into a clean target should fail loudly, got %v", logTexts(log))
	}
}

// TestAcupressureRaisesOneStatByTwo: across a sweep of seeds rather than on a
// chosen one, so the assertion is about the rule and not about splitmix64. Both
// halves matter — exactly one stat moves, and it moves by two.
func TestAcupressureRaisesOneStatByTwo(t *testing.T) {
	d := loadDex(t)
	seen := map[string]bool{}
	for seed := uint64(1); seed <= 40; seed++ {
		s, err := NewBattle(d, "acu", "P1", []int{143}, "P2", []int{143}, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		for i := range s.Sides {
			p := &s.Sides[i].Team[0]
			p.Item, p.Ability = ItemNone, AbilityNone
			p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		}
		user := s.Active(0)
		user.Moves = []MoveSlot{{MoveID: "acupressure", PP: 30, MaxPP: 30}}

		playTurn(d, s, 0, 0)
		moved, total := 0, 0
		for name, v := range map[string]int{
			"attack": user.Stages.Atk, "defense": user.Stages.Def,
			"spatk": user.Stages.SpA, "spdef": user.Stages.SpD,
			"speed": user.Stages.Spe, "accuracy": user.Stages.Acc,
			"evasion": user.Stages.Eva,
		} {
			if v != 0 {
				moved++
				total = v
				seen[name] = true
			}
		}
		if moved != 1 || total != 2 {
			t.Fatalf("seed %d: wanted exactly one stat at +2, got %+v", seed, user.Stages)
		}
	}
	if len(seen) < 2 {
		t.Errorf("the stat should be chosen at random, but only %v ever came up", seen)
	}
}

// TestAcupressureFailsWithNothingLeftToRaise: every stat already maxed is a
// visible refusal, not a silent success.
func TestAcupressureFailsWithNothingLeftToRaise(t *testing.T) {
	d := loadDex(t)
	s := inertBattle(t, d, "acupressure", "splash")
	s.Active(0).Stages = Stages{Atk: 6, Def: 6, SpA: 6, SpD: 6, Spe: 6, Acc: 6, Eva: 6}
	if log := playTurn(d, s, 0, 0); !logHas(log, "But it failed!") {
		t.Errorf("Acupressure with nothing left to raise should fail loudly, got %v", logTexts(log))
	}
}

// TestRototillerNeedsAGroundedGrassTypeOrSomethingAirborne: the failure
// condition is a genuine canon quirk — an airborne Pokémon alone makes the move
// "work" even though it boosts nobody, because announcing the immunity counts.
func TestRototillerNeedsAGroundedGrassTypeOrSomethingAirborne(t *testing.T) {
	d := loadDex(t)
	// Neither side grounded-and-Grass, nobody airborne: refuses.
	s := inertBattle(t, d, "rototiller", "splash")
	if log := playTurn(d, s, 0, 0); !logHas(log, "But it failed!") {
		t.Errorf("no Grass, nothing airborne: should fail loudly, got %v", logTexts(log))
	}

	// A grounded Grass-type on the field gets the boost — from either side,
	// since canon's target is "all".
	g, err := NewBattle(d, "rt", "P1", []int{143}, "P2", []int{3}, 7) // Venusaur
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range g.Sides {
		p := &g.Sides[i].Team[0]
		p.Item, p.Ability = ItemNone, AbilityNone
		p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	}
	g.Active(0).Moves = []MoveSlot{{MoveID: "rototiller", PP: 10, MaxPP: 10}}
	playTurn(d, g, 0, 0)
	if got := g.Active(1).Stages; got.Atk != 1 || got.SpA != 1 {
		t.Errorf("a grounded Grass-type should gain +1 Attack and +1 Sp. Atk, got %+v", got)
	}
}

// TestCelebrateSaysSomethingAndDoesNothing: the one move whose canonical
// behavior really is nothing at all. It still belongs in the engine, because
// "nothing, silently" and "nothing, out loud" are different behaviors and only
// the second is Celebrate.
func TestCelebrateSaysSomethingAndDoesNothing(t *testing.T) {
	d := loadDex(t)
	s := inertBattle(t, d, "celebrate", "splash")
	before := s.Active(0).Stages
	log := playTurn(d, s, 0, 0)
	if !logHas(log, "Congratulations") {
		t.Errorf("Celebrate should announce itself, got %v", logTexts(log))
	}
	if s.Active(0).Stages != before {
		t.Error("Celebrate changes nothing")
	}
}

// TestMagneticFluxRefusesWithoutPlusOrMinus: neither ability is on any species
// in this dex, so the move always refuses today. The refusal is a fact about
// the roster rather than about the move, which is why the handler tests the
// abilities instead of returning a hard-coded failure.
func TestMagneticFluxRefusesWithoutPlusOrMinus(t *testing.T) {
	d := loadDex(t)
	s := inertBattle(t, d, "magnetic-flux", "splash")
	log := playTurn(d, s, 0, 0)
	if !logHas(log, "But it failed!") {
		t.Errorf("Magnetic Flux with no Plus or Minus holder should fail loudly, got %v", logTexts(log))
	}

	// And it is genuinely gated on the ability, not on nothing: hand the user
	// Plus and the boost lands.
	s2 := inertBattle(t, d, "magnetic-flux", "splash")
	s2.Active(0).Ability = "plus"
	playTurn(d, s2, 0, 0)
	if got := s2.Active(0).Stages; got.Def != 1 || got.SpD != 1 {
		t.Errorf("a Plus holder should gain +1 Defense and +1 Sp. Def, got %+v", got)
	}
}

// TestNaturePowerBecomesTheTerrainsMove: the move is a name for another move.
// Tri Attack on bare ground, one move per terrain otherwise — and the
// substitution is complete enough that the called move announces itself and
// carries its own type.
func TestNaturePowerBecomesTheTerrainsMove(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		terrain TerrainKind
		want    string
	}{
		{"", "Tri Attack"},
		{TerrainElectric, "Thunderbolt"},
		{TerrainGrassy, "Energy Ball"},
		{TerrainMisty, "Moonblast"},
		{TerrainPsychic, "Psychic"},
	} {
		s := inertBattle(t, d, "nature-power", "splash")
		if c.terrain != "" {
			s.Terrain = &TerrainState{Kind: c.terrain, TurnsLeft: 5}
		}
		foe := s.Active(1)
		before := foe.HP
		log := playTurn(d, s, 0, 0)
		if !logHas(log, "used "+c.want+"!") {
			t.Errorf("terrain %q: wanted %q, got %v", c.terrain, c.want, logTexts(log))
		}
		if foe.HP >= before {
			t.Errorf("terrain %q: the called move should have dealt damage", c.terrain)
		}
	}
}

// TestNaturePowerCostsItsOwnPPAndNotTheCalledMoves: canon's useMove is not a
// second action. The user pays for Nature Power; Thunderbolt is free, and its
// own slot — if the user even has one — is untouched.
func TestNaturePowerCostsItsOwnPPAndNotTheCalledMoves(t *testing.T) {
	d := loadDex(t)
	s := inertBattle(t, d, "nature-power", "splash")
	s.Active(0).Moves = []MoveSlot{
		{MoveID: "nature-power", PP: 20, MaxPP: 20},
		{MoveID: "tri-attack", PP: 10, MaxPP: 10},
	}
	playTurn(d, s, 0, 0)
	if got := s.Active(0).Moves[0].PP; got != 19 {
		t.Errorf("Nature Power should have paid one PP, got %d left", got)
	}
	if got := s.Active(0).Moves[1].PP; got != 10 {
		t.Errorf("the called move is free, but Tri Attack's slot has %d left", got)
	}
}

// TestNaturePowerBreaksAFocusPunch: the reason one of the ported rows lived
// under Focus Punch rather than under Nature Power. Once the substitution makes
// it a real damaging move, the loss-of-focus check needs no special case at all.
func TestNaturePowerBreaksAFocusPunch(t *testing.T) {
	d := loadDex(t)
	// Alakazam outspeeds, so the Nature Power lands before the punch resolves.
	s, err := NewBattle(d, "np", "P1", []int{65}, "P2", []int{143}, 7)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		p := &s.Sides[i].Team[0]
		p.Item, p.Ability = ItemNone, AbilityNone
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "nature-power", PP: 20, MaxPP: 20}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "focus-punch", PP: 20, MaxPP: 20}}

	log := playTurn(d, s, 0, 0)
	if !logHas(log, "lost its focus") {
		t.Errorf("a move called by Nature Power should still break a Focus Punch, got %v",
			logTexts(log))
	}
}
