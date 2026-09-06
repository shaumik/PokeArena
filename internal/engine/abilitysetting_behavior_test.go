package engine

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// abilitysetting_behavior_test.go covers the four moves that rewrite an ability
// in place. Everything here goes through NewBattle / ResolveTurn rather than
// calling setAbilityInPlace directly, because the defect these moves had was
// never in the write — it was that no battle ever performed one. All four were
// registered in the dataset, resolved as status moves with no payload, and
// logged a clean success. A unit test on the setter would have passed against
// the broken engine.

// asBattle builds a two-Pokémon battle with both sides stripped of items and
// holding nothing but the moves the test hands them, so the only live mechanic
// is the ability exchange under test.
func asBattle(t *testing.T, d *domain.Dex, team0, team1 []int) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "as", "P1", team0, "P2", team1, 7)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			p.Item = ItemNone
			p.Ability = AbilityNone
			p.Moves = cbmMoves("splash")
		}
	}
	return s
}

// TestWorrySeedBattleReplacesTheAbilityAndDropsWhatItWasHolding: the move's
// whole point is to take an ability away mid-battle, and the Flash Fire charge
// is the case that proves the removal is a tear-down and not just a field
// write. A charged Flash Fire holder that loses the ability must lose the
// charge with it, or Skill Swapping the ability back later would hand it a
// boost it spent the intervening turns not having.
func TestWorrySeedBattleReplacesTheAbilityAndDropsWhatItWasHolding(t *testing.T) {
	d := loadDex(t)
	s := asBattle(t, d, []int{143}, []int{143})
	s.Active(0).Moves = cbmMoves("worry-seed")
	def := s.Active(1)
	def.Ability = "flash-fire"
	def.Volatiles.FlashFireCharged = true

	cbmTurn(d, s, 0, 0)
	if def.Ability != "insomnia" {
		t.Errorf("Worry Seed should have set Insomnia, got %q", def.Ability)
	}
	if def.Volatiles.FlashFireCharged {
		t.Error("the Flash Fire charge belongs to the ability and should have gone with it")
	}
	if def.BaseAbility != "flash-fire" {
		t.Errorf("the real ability should be remembered for the switch-out revert, got %q", def.BaseAbility)
	}
}

// TestWorrySeedBattleWakesASleepingTarget: Insomnia refuses sleep, so gaining
// it clears the sleep already there. Canon reaches this through the ability's
// own onUpdate rather than through the move, which is why the engine's version
// asks the same BlocksStatus predicate inflictStatus asks — the two can't drift.
func TestWorrySeedBattleWakesASleepingTarget(t *testing.T) {
	d := loadDex(t)
	s := asBattle(t, d, []int{143}, []int{143})
	s.Active(0).Moves = cbmMoves("worry-seed")
	def := s.Active(1)
	def.Status = StatusSleep
	def.SleepTurns = 3

	cbmTurn(d, s, 0, 0)
	if def.Status != StatusNone {
		t.Errorf("gaining Insomnia should have woken the target, got %q", def.Status)
	}
	if def.SleepTurns != 0 {
		t.Errorf("the sleep counter should be cleared too, got %d", def.SleepTurns)
	}
}

// TestWorrySeedBattleFailsAgainstInsomnia: the refusal is visible. A move that
// silently no-ops against the one ability it cannot overwrite is the failure
// mode this whole file exists to prevent.
func TestWorrySeedBattleFailsAgainstInsomnia(t *testing.T) {
	d := loadDex(t)
	s := asBattle(t, d, []int{143}, []int{143})
	s.Active(0).Moves = cbmMoves("worry-seed")
	s.Active(1).Ability = "insomnia"

	log := cbmTurn(d, s, 0, 0)
	if !logHas(log, "But it failed!") {
		t.Errorf("Worry Seed into Insomnia should announce a failure, got %v", logTexts(log))
	}
}

// TestSimpleBeamBattleDoublesTheTargetsLaterBoosts: Simple is on no species in
// this dex, so the only way to observe it is to hand it out — which makes this
// the test that the ability is registered at all and not just named.
func TestSimpleBeamBattleDoublesTheTargetsLaterBoosts(t *testing.T) {
	d := loadDex(t)
	s := asBattle(t, d, []int{143}, []int{143})
	s.Active(0).Moves = cbmMoves("simple-beam")
	def := s.Active(1)
	def.Moves = cbmMoves("swords-dance")

	cbmTurn(d, s, 0, 0)
	if def.Ability != AbilitySimple {
		t.Fatalf("Simple Beam should have set Simple, got %q", def.Ability)
	}
	if got := def.Stages.Atk; got != 4 {
		t.Errorf("Swords Dance under Simple should be +4, got %d", got)
	}
}

// TestSkillSwapBattleExchangesAndRunsTheIncomingEntryEffect: the swap is not a
// pair of field writes. Canon runs the incoming ability's Start event on each
// side, so a swapped-in Drought sets the sun on the spot rather than waiting
// for a switch that may never come.
func TestSkillSwapBattleExchangesAndRunsTheIncomingEntryEffect(t *testing.T) {
	d := loadDex(t)
	s := asBattle(t, d, []int{143}, []int{143})
	user := s.Active(0)
	user.Moves = cbmMoves("skill-swap")
	user.Ability = "insomnia"
	foe := s.Active(1)
	foe.Ability = "drought"

	cbmTurn(d, s, 0, 0)
	if user.Ability != "drought" || foe.Ability != "insomnia" {
		t.Fatalf("abilities should have changed hands, got %q / %q", user.Ability, foe.Ability)
	}
	if s.Weather == nil || s.Weather.Kind != WeatherSun {
		t.Errorf("the swapped-in Drought should have set the sun, got %+v", s.Weather)
	}
}

// TestSkillSwapBattleCuresAStatusTheIncomingAbilityRefuses: the Showdown case
// this closes. Immunity cannot merely refuse a *new* poison — receiving it
// clears the poison already there.
func TestSkillSwapBattleCuresAStatusTheIncomingAbilityRefuses(t *testing.T) {
	d := loadDex(t)
	s := asBattle(t, d, []int{143}, []int{143})
	user := s.Active(0)
	user.Moves = cbmMoves("skill-swap")
	user.Ability = "immunity"
	foe := s.Active(1)
	foe.Ability = "thick-fat"
	foe.Status = StatusToxic
	foe.ToxicCounter = 3

	cbmTurn(d, s, 0, 0)
	if foe.Ability != "immunity" {
		t.Fatalf("the foe should have received Immunity, got %q", foe.Ability)
	}
	if foe.Status != StatusNone {
		t.Errorf("receiving Immunity should have cleared the poison, got %q", foe.Status)
	}
	if foe.ToxicCounter != 0 {
		t.Errorf("the toxic ladder should reset with the status, got %d", foe.ToxicCounter)
	}
}

// TestSkillSwapBattleRefusesNeutralizingGas: the gas is the one ability in this
// dex canon marks unswappable, and the reason is mechanical rather than
// flavor — a swap would hand it to a Pokémon it then suppresses while turning
// it off on the holder that was paying for it.
func TestSkillSwapBattleRefusesNeutralizingGas(t *testing.T) {
	d := loadDex(t)
	s := asBattle(t, d, []int{143}, []int{143})
	user := s.Active(0)
	user.Moves = cbmMoves("skill-swap")
	user.Ability = "insomnia"
	foe := s.Active(1)
	foe.Ability = AbilityNeutralizingGas

	log := cbmTurn(d, s, 0, 0)
	if user.Ability != "insomnia" || foe.Ability != AbilityNeutralizingGas {
		t.Errorf("nothing should have moved, got %q / %q", user.Ability, foe.Ability)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("the refusal should be announced, got %v", logTexts(log))
	}
}

// TestRolePlayBattleCopiesTheFoeAndFailsOnAMatch: the copy is one-way, and the
// no-op case is a failure rather than a success with nothing to show.
func TestRolePlayBattleCopiesTheFoeAndFailsOnAMatch(t *testing.T) {
	d := loadDex(t)
	s := asBattle(t, d, []int{143}, []int{143})
	user := s.Active(0)
	user.Moves = cbmMoves("role-play")
	user.Ability = "insomnia"
	foe := s.Active(1)
	foe.Ability = "thick-fat"

	cbmTurn(d, s, 0, 0)
	if user.Ability != "thick-fat" {
		t.Fatalf("Role Play should have copied Thick Fat, got %q", user.Ability)
	}
	if foe.Ability != "thick-fat" {
		t.Errorf("the target keeps its own ability, got %q", foe.Ability)
	}

	log := cbmTurn(d, s, 0, 0) // both are Thick Fat now
	if !logHas(log, "But it failed!") {
		t.Errorf("copying an ability the user already has should fail, got %v", logTexts(log))
	}
}

// TestAbilitySettingRevertsOnSwitchOut: every one of these changes is
// field-scoped. A Pokémon Worry Seeded and then Skill Swapped comes back with
// what it was built with, not with whatever it was wearing when it left —
// which is what the first-writer-wins rule on BaseAbility buys.
func TestAbilitySettingRevertsOnSwitchOut(t *testing.T) {
	d := loadDex(t)
	s := asBattle(t, d, []int{143}, []int{143, 65})
	s.Active(0).Moves = cbmMoves("worry-seed")
	def := s.Active(1)
	def.Ability = "thick-fat"

	cbmTurn(d, s, 0, 0)
	if def.Ability != "insomnia" {
		t.Fatalf("setup: Worry Seed should have landed, got %q", def.Ability)
	}
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionSwitch, Index: 1}})
	if got := s.Sides[1].Team[0].Ability; got != "thick-fat" {
		t.Errorf("leaving the field should restore the real ability, got %q", got)
	}
	if got := s.Sides[1].Team[0].BaseAbility; got != "" {
		t.Errorf("the memo should be spent on the way out, got %q", got)
	}
}
