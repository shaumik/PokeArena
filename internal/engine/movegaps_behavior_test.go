package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// movegaps_behavior_test.go covers ten moves that shipped with a mechanic
// missing from the engine entirely. They have nothing in common except that
// each is a single hook upstream encodes in JS, which is why they were caught
// one at a time by the port rather than as a cluster.

func mgBattle(t *testing.T, d *domain.Dex, userMove string, userDex, foeDex int) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "mg", "P1", []int{userDex}, "P2", []int{foeDex}, 11)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		p := &s.Sides[i].Team[0]
		p.Item, p.Ability = ItemNone, AbilityNone
		p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: userMove, PP: 20, MaxPP: 20}}
	return s
}

// TestBrickBreakShattersScreensBeforeItHits: the ordering is the mechanic.
// Canon puts the removal in the move's own onTryHit, which fires above the
// substitute redirect and above the damage calculation — so the hit is not
// halved by the Reflect it is breaking.
func TestBrickBreakShattersScreensBeforeItHits(t *testing.T) {
	d := loadDex(t)
	s := mgBattle(t, d, "brick-break", 143, 143)
	s.Sides[1].Conditions.Reflect = &ScreenState{TurnsLeft: 5}
	s.Sides[1].Conditions.LightScreen = &ScreenState{TurnsLeft: 5}

	log := playTurn(d, s, 0, 0)
	if s.Sides[1].Conditions.Reflect != nil || s.Sides[1].Conditions.LightScreen != nil {
		t.Error("Brick Break should have shattered both screens")
	}
	if !logHas(log, "shattered the screens") {
		t.Errorf("the shatter should be announced, got %v", logTexts(log))
	}
}

// TestBrickBreakDoesNotShatterWhatItCannotTouch: a Ghost is immune to
// Fighting, and canon's type-immunity step runs above the move's own onTryHit.
func TestBrickBreakDoesNotShatterWhatItCannotTouch(t *testing.T) {
	d := loadDex(t)
	s := mgBattle(t, d, "brick-break", 143, 94) // Gengar
	s.Sides[1].Conditions.Reflect = &ScreenState{TurnsLeft: 5}

	playTurn(d, s, 0, 0)
	if s.Sides[1].Conditions.Reflect == nil {
		t.Error("a Ghost's screens survive a Brick Break that cannot reach it")
	}
}

// TestBrickBreakShattersThroughASubstitute: the flip side of the same
// ordering. The doll is what the damage hits; the screens come down anyway.
func TestBrickBreakShattersThroughASubstitute(t *testing.T) {
	d := loadDex(t)
	s := mgBattle(t, d, "brick-break", 143, 143)
	s.Sides[1].Conditions.Reflect = &ScreenState{TurnsLeft: 5}
	s.Active(1).Volatiles.Substitute = &SubstituteState{HP: 200, MaxHP: 200}

	playTurn(d, s, 0, 0)
	if s.Sides[1].Conditions.Reflect != nil {
		t.Error("a Substitute does not protect the screens from Brick Break")
	}
}

// TestJumpKicksCrashOnAMiss and its Rock Head sibling. The crash is attributed
// to a condition rather than to recoil upstream, which is precisely why Rock
// Head does not cover it and Magic Guard does — a distinction that only exists
// because of how the damage is labeled.
func TestJumpKicksCrashWhenTheyMiss(t *testing.T) {
	d := loadDex(t)
	// A Ghost is immune to Fighting, so the miss is deterministic.
	for _, id := range []string{"high-jump-kick", "jump-kick"} {
		s := mgBattle(t, d, id, 143, 94)
		atk := s.Active(0)
		before := atk.HP
		playTurn(d, s, 0, 0)
		if atk.HP != before-atk.MaxHP/2 {
			t.Errorf("%s should crash for half the user's max HP: %d -> %d of %d",
				id, before, atk.HP, atk.MaxHP)
		}
	}
}

func TestRockHeadDoesNotBlockCrashDamageButMagicGuardDoes(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		ability AbilityKind
		crashes bool
	}{
		{"rock-head", true},
		{"magic-guard", false},
		{AbilityNone, true},
	} {
		s := mgBattle(t, d, "high-jump-kick", 143, 94)
		atk := s.Active(0)
		atk.Ability = c.ability
		before := atk.HP
		playTurn(d, s, 0, 0)
		crashed := atk.HP < before
		if crashed != c.crashes {
			t.Errorf("%q: crashed = %v, want %v — Rock Head only intercepts recoil, "+
				"and the crash is not recoil", c.ability, crashed, c.crashes)
		}
	}
}

// TestIceSpinnerSweepsTheTerrain, and does not when the spinner dies on the way
// in: canon runs the wipe from onAfterHit, which fires only if the user still
// has HP and only after the DamagingHit event a Rocky Helmet hangs off.
func TestIceSpinnerSweepsTheTerrain(t *testing.T) {
	d := loadDex(t)
	s := mgBattle(t, d, "ice-spinner", 143, 143)
	s.Terrain = &TerrainState{Kind: TerrainElectric, TurnsLeft: 5}

	playTurn(d, s, 0, 0)
	if s.Terrain != nil {
		t.Errorf("Ice Spinner should have swept the terrain, got %+v", s.Terrain)
	}
}

func TestIceSpinnerLeavesTheTerrainIfTheSpinnerDies(t *testing.T) {
	d := loadDex(t)
	s := mgBattle(t, d, "ice-spinner", 143, 143)
	s.Terrain = &TerrainState{Kind: TerrainElectric, TurnsLeft: 5}
	atk := s.Active(0)
	atk.HP = 1
	s.Active(1).Item = "rocky-helmet"

	playTurn(d, s, 0, 0)
	if !atk.Fainted {
		t.Fatalf("setup: the Rocky Helmet should have killed the 1 HP spinner")
	}
	if s.Terrain == nil {
		t.Error("a spinner that dies on the way in does not get to sweep")
	}
}

// TestSynchronoiseNeedsASharedType: the immunity sits above the accuracy roll,
// where canon's hitStepTryImmunity is.
func TestSynchronoiseNeedsASharedType(t *testing.T) {
	d := loadDex(t)
	// Snorlax is Normal; Gengar is Ghost/Poison — nothing in common.
	s := mgBattle(t, d, "synchronoise", 143, 94)
	foe := s.Active(1)
	before := foe.HP
	log := playTurn(d, s, 0, 0)
	if foe.HP != before {
		t.Error("Synchronoise should not touch a target with no shared type")
	}
	if !logHas(log, "doesn't affect") {
		t.Errorf("the immunity should be announced, got %v", logTexts(log))
	}

	// Snorlax into Snorlax shares Normal.
	m := mgBattle(t, d, "synchronoise", 143, 143)
	tgt := m.Active(1)
	before = tgt.HP
	playTurn(d, m, 0, 0)
	if tgt.HP >= before {
		t.Error("Synchronoise should hit a target that shares a type")
	}
}

// TestTheStatusCuringSlaps: each doubles against the status it then removes,
// which is the joke — the doubled hit is the last one that gets the bonus.
func TestTheStatusCuringSlaps(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		move   string
		status StatusCond
	}{
		{"smelling-salts", StatusParalysis},
		{"sparkling-aria", StatusBurn},
		{"wake-up-slap", StatusSleep},
	} {
		s := mgBattle(t, d, c.move, 143, 143)
		foe := s.Active(1)
		foe.Status = c.status
		if c.status == StatusSleep {
			foe.SleepTurns = 3
		}
		playTurn(d, s, 0, 0)
		if foe.Status != StatusNone {
			t.Errorf("%s should have cured %q, still %q", c.move, c.status, foe.Status)
		}

		// And it leaves an unrelated status alone.
		o := mgBattle(t, d, c.move, 143, 143)
		other := o.Active(1)
		other.Status = StatusToxic
		playTurn(d, o, 0, 0)
		if c.status != StatusToxic && other.Status != StatusToxic {
			t.Errorf("%s should only cure %q, but it cleared a poison", c.move, c.status)
		}
	}
}

// TestSmellingSaltsDoublesAgainstParalysis: the power half, measured through
// the same path executeMove uses.
func TestSmellingSaltsDoublesAgainstParalysis(t *testing.T) {
	d := loadDex(t)
	s := mgBattle(t, d, "smelling-salts", 143, 143)
	base := d.Moves["smelling-salts"].Power
	if got := applyCallbackPower(s, s.Active(0), s.Active(1), d.Moves["smelling-salts"], "").Power; got != base {
		t.Errorf("healthy target: power = %d, want %d", got, base)
	}
	s.Active(1).Status = StatusParalysis
	if got := applyCallbackPower(s, s.Active(0), s.Active(1), d.Moves["smelling-salts"], "").Power; got != base*2 {
		t.Errorf("paralyzed target: power = %d, want %d", got, base*2)
	}
}

// TestBelchNeedsABerryFirst: the latch, and the two gates. It is kept off the
// menu *and* refused at resolution, and the menu gate is a real gate rather
// than a tidy — unlike Taunt or Gravity, the rule keys on the move's ID, which
// the slot carries, so it runs on the dex-less path too.
func TestBelchNeedsABerryFirst(t *testing.T) {
	d := loadDex(t)
	s := mgBattle(t, d, "belch", 143, 143)

	for _, a := range LegalActionsDex(d, s, 0) {
		if a.Kind == ActionMove && a.Index == 0 {
			t.Error("Belch should not be offered before its user has eaten a berry")
		}
	}
	if log := playTurn(d, s, 0, 0); !logHas(log, "But it failed!") {
		t.Errorf("Belch before a berry should fail loudly, got %v", logTexts(log))
	}

	// Eat one, and it works.
	s.Active(0).Item = "sitrus-berry"
	s.Active(0).HP = s.Active(0).MaxHP / 4
	var lg []LogLine
	applyItemHPTrigger(s, 0, NewRNG(1), &lg)
	if !s.Active(0).AteBerry {
		t.Fatalf("setup: the Sitrus should have been eaten, log %v", logTexts(lg))
	}
	foe := s.Active(1)
	before := foe.HP
	playTurn(d, s, 0, 0)
	if foe.HP >= before {
		t.Error("Belch should connect once its user has eaten a berry")
	}
}

// TestBelchLatchSurvivesSwitchingOut: canon never clears ateBerry, so it is a
// field on the Pokémon rather than a volatile. A Belch user that eats its berry
// and pivots out keeps the right to use the move when it comes back.
func TestBelchLatchSurvivesSwitchingOut(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "bl", "P1", []int{143, 65}, "P2", []int{143}, 11)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			p.Item, p.Ability = ItemNone, AbilityNone
			p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		}
	}
	eater := s.Active(0)
	eater.Item = "sitrus-berry"
	eater.HP = eater.MaxHP / 4
	var lg []LogLine
	applyItemHPTrigger(s, 0, NewRNG(1), &lg)
	if !eater.AteBerry {
		t.Fatalf("setup: the berry should have been eaten")
	}

	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
	if !s.Sides[0].Team[0].AteBerry {
		t.Error("the latch is a Pokémon field, not a volatile — it must survive a switch")
	}
}
