package engine

import "testing"

// The Showdown port's second wave: effects that were reached from outside the
// path that guards them, or that reversed something they never applied. Each
// test names the upstream case it stands in for; the port itself lives behind
// the `showdown` build tag, which ordinary CI never compiles, so these are the
// checks that actually run.

// TestBatonPassCarriesTheWholeVolatileBag stands in for "Baton Pass: should
// switch the user out, passing with it a variety of effects".
//
// batonCarry was an allow-list of three — Stages, Confusion, Substitute — where
// canon's copyVolatileFrom copies every volatile except the ones flagged
// noCopy. An allow-list gets this wrong every time a volatile is added and
// nobody widens it, which is how Focus Energy, Leech Seed and Ingrain came to
// be silently dropped. The carry is a deny-list now, so this asserts both
// directions: what must travel, and what must not.
func TestBatonPassCarriesTheWholeVolatileBag(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143, 9}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "baton-pass", "ingrain", "focus-energy")
	teachMoves(t, d, &s.Sides[0].Team[1], "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")

	out := s.Active(0)
	ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)}) // Ingrain
	ResolveTurn(d, s, [2]Action{moveAt(2), moveAt(0)}) // Focus Energy
	out.Stages.Atk = 2
	// Two things that must NOT travel: a noCopy debuff and turn-scheduling
	// state that would let the pass launder the turn.
	out.Volatiles.Torment = true
	out.Volatiles.DefenseCurl = true
	out.Volatiles.MoveActions = 5
	if !out.Volatiles.Ingrain || !out.Volatiles.FocusEnergy {
		t.Fatalf("setup: the passer should be rooted and focused, got %+v", out.Volatiles)
	}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0, SwitchTarget: intp(1)}, moveAt(0)})
	in := s.Active(0)
	if in.Name == out.Name {
		t.Fatalf("Baton Pass did not switch; still %s", in.Name)
	}
	if in.Stages.Atk != 2 {
		t.Errorf("stages should pass: atk %d, want 2", in.Stages.Atk)
	}
	if !in.Volatiles.Ingrain {
		t.Errorf("Ingrain should pass — it is not noCopy upstream, and this is what the " +
			"upstream Ingrain heal case measures")
	}
	if !in.Volatiles.FocusEnergy {
		t.Errorf("Focus Energy should pass")
	}
	if in.Volatiles.Torment {
		t.Errorf("Torment is noCopy upstream and must not pass — a debuff the receiver never earned")
	}
	if in.Volatiles.DefenseCurl {
		t.Errorf("Defense Curl is noCopy upstream and must not pass")
	}
	if in.Volatiles.MoveActions != 0 {
		t.Errorf("MoveActions is canon's activeMoveActions, which a switch zeroes: got %d — "+
			"a receiver that inherits it arrives unable to use Fake Out", in.Volatiles.MoveActions)
	}
	// And the passer's own bag is gone, so nothing is aliased between the two.
	if out.Volatiles.Ingrain || out.Volatiles.FocusEnergy {
		t.Errorf("the passer should have been wiped on the way out, got %+v", out.Volatiles)
	}
}

// intp is a pointer to an int literal, for Action.SwitchTarget.
func intp(i int) *int { return &i }

// TestBatonPassAndTeleportFailWithNoBench stands in for the two "should fail to
// switch the user out if no Pokemon can be switched in" cases.
//
// Canon draws this line move by move, not by category: Baton Pass carries an
// onHit that emits -fail and Teleport an onTry, because neither has any other
// effect, so "announce and do nothing" is indistinguishable from working. The
// damaging pivots carry neither hook — that asymmetry is deliberate and the
// second half of this test is what keeps it.
func TestBatonPassAndTeleportFailWithNoBench(t *testing.T) {
	for _, move := range []string{"baton-pass", "teleport"} {
		t.Run(move, func(t *testing.T) {
			d := loadDex(t)
			s := neutralBattle(t, d, 1, []int{143}, []int{143})
			teachMoves(t, d, &s.Sides[0].Team[0], move)
			teachMoves(t, d, &s.Sides[1].Team[0], "splash")
			log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
			if !logHas(log, "But it failed") {
				t.Errorf("%s with an empty bench should fail loudly; log: %v", move, logTexts(log))
			}
		})
	}
	t.Run("u-turn stays silent", func(t *testing.T) {
		d := loadDex(t)
		s := neutralBattle(t, d, 1, []int{143}, []int{143})
		teachMoves(t, d, &s.Sides[0].Team[0], "u-turn")
		teachMoves(t, d, &s.Sides[1].Team[0], "splash")
		before := s.Active(1).HP
		log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		if logHas(log, "But it failed") {
			t.Errorf("U-turn on a last Pokemon deals its damage and quietly does not switch — "+
				"it must not print a failure; log: %v", logTexts(log))
		}
		if s.Active(1).HP >= before {
			t.Errorf("U-turn should still have dealt its damage")
		}
	})
}

// TestPsychUpTakesTheCritVolatilesAndDropsItsOwn stands in for "Psych Up:
// should copy the opponent's crit ratio". The removal is the half that makes
// Psych Up a read rather than a free steal: canon's first loop strips
// focusenergy and laserfocus from the *user* before the second loop copies the
// target's, so a Focus Energy user that Psychs Up a foe with none loses it.
func TestPsychUpTakesTheCritVolatilesAndDropsItsOwn(t *testing.T) {
	d := loadDex(t)
	build := func(foeFocused bool) (*Pokemon, *Pokemon) {
		s := neutralBattle(t, d, 1, []int{143}, []int{143})
		teachMoves(t, d, &s.Sides[0].Team[0], "psych-up", "focus-energy")
		teachMoves(t, d, &s.Sides[1].Team[0], "focus-energy", "splash")
		foeMove := 1
		if foeFocused {
			foeMove = 0
		}
		ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(foeMove)}) // user focuses; foe maybe
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(1)})       // Psych Up
		return s.Active(0), s.Active(1)
	}
	user, foe := build(true)
	if !foe.Volatiles.FocusEnergy || !user.Volatiles.FocusEnergy {
		t.Errorf("Psych Up should copy the foe's Focus Energy: user=%v foe=%v",
			user.Volatiles.FocusEnergy, foe.Volatiles.FocusEnergy)
	}
	user, foe = build(false)
	if foe.Volatiles.FocusEnergy {
		t.Fatalf("setup: the foe should not be focused")
	}
	if user.Volatiles.FocusEnergy {
		t.Errorf("Psych Up should have stripped the user's own Focus Energy — copying a foe " +
			"that has none means ending up with none")
	}
}

// TestStockpileGivesBackOnlyWhatItTook stands in for "Stockpile: should keep
// track of how many boosts to each defense stat were successful". A Stockpile
// used at +6 Defense stacks and boosts nothing, so releasing it must not drop
// the Defense it never raised.
func TestStockpileGivesBackOnlyWhatItTook(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "stockpile", "spit-up")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")

	me := s.Active(0)
	me.Stages.Def = 6 // already capped: the Def half of the stockpile cannot land
	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if me.Stages.Def != 6 || me.Stages.SpD != 1 {
		t.Fatalf("setup: one Stockpile at +6 Def should leave Def=6 SpD=1, got Def=%d SpD=%d",
			me.Stages.Def, me.Stages.SpD)
	}
	if st := me.Volatiles.Stockpile; st == nil || st.Count != 1 || st.Def != 0 || st.SpD != 1 {
		t.Fatalf("the tally should record one landed SpD boost and no Def boost, got %+v", st)
	}
	ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)}) // Spit Up spends it
	if me.Stages.Def != 6 {
		t.Errorf("Spit Up gave back a Defense stage the stockpile never took: Def=%d, want 6",
			me.Stages.Def)
	}
	if me.Stages.SpD != 0 {
		t.Errorf("Spit Up should have taken back the Sp. Def stage it granted: SpD=%d, want 0",
			me.Stages.SpD)
	}
}

// TestSmackDownCancelsFlyAndRefusesAGroundedTarget stands in for "Stomping
// Tantrum: should not double its Base Power if the user dropped mid-Fly due to
// Smack Down". Canon's condition is a list of things the volatile can be taking
// away, and it refuses to apply when none of them is there — a permanent
// grounding handed to something already standing on the ground is free, and
// then blocks the Magnet Rise it should not have blocked.
func TestSmackDownCancelsFlyAndRefusesAGroundedTarget(t *testing.T) {
	d := loadDex(t)
	t.Run("knocks a target out of Fly", func(t *testing.T) {
		s := neutralBattle(t, d, 1, []int{143}, []int{6}) // Charizard flies
		teachMoves(t, d, &s.Sides[0].Team[0], "smack-down")
		teachMoves(t, d, &s.Sides[1].Team[0], "fly")
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		zard := s.Active(1)
		if zard.Volatiles.Charging != nil {
			t.Errorf("Smack Down should cancel a mid-air charge, got %+v", zard.Volatiles.Charging)
		}
		if !zard.Volatiles.SmackDown {
			t.Errorf("Smack Down should have grounded the flier")
		}
	})
	t.Run("refuses a target that was never in the air", func(t *testing.T) {
		s := neutralBattle(t, d, 1, []int{143}, []int{143}) // Snorlax: grounded already
		teachMoves(t, d, &s.Sides[0].Team[0], "smack-down")
		teachMoves(t, d, &s.Sides[1].Team[0], "splash")
		log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		if s.Active(1).Volatiles.SmackDown {
			t.Errorf("Smack Down should not stick to a target that was already on the ground — " +
				"the flag is a grounding, and there was nothing to ground")
		}
		if logHas(log, "fell straight down") {
			t.Errorf("nothing fell; log: %v", logTexts(log))
		}
	})
}

// TestLiquidOozeBackfiresOffLeechSeedAtFullHP stands in for "Liquid Ooze:
// should damage the target after taking damage from Leech Seed".
//
// The seeder sitting at max HP is the whole fixture: the old code returned out
// of the function on that check before there was anywhere left to ask about the
// ability. Canon runs the heal event even when nothing heals, with a comment in
// Battle#heal saying it does so for exactly this.
func TestLiquidOozeBackfiresOffLeechSeedAtFullHP(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143}, []int{73}) // Tentacruel carries Liquid Ooze
	teachMoves(t, d, &s.Sides[0].Team[0], "leech-seed", "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")
	s.Active(1).Ability = "liquid-ooze"

	seeder := s.Active(0)
	// The seed lands and its first residual runs in the same turn, with the
	// seeder untouched at full HP — which is the whole fixture: there is
	// nothing to heal, so the old code returned before it could ask about the
	// ability.
	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if seeder.HP >= seeder.MaxHP {
		t.Errorf("a Liquid Ooze holder's Leech Seed should hurt the seeder even at full HP, "+
			"got %d/%d; log: %v", seeder.HP, seeder.MaxHP, logTexts(log))
	}
	if !logHas(log, "sucked up the liquid ooze") {
		t.Errorf("the backfire should announce itself; log: %v", logTexts(log))
	}
	if logHas(log, "drained HP!") {
		t.Errorf("the seeder must not also have drained; log: %v", logTexts(log))
	}
	// And the control: without the ability the same board heals the seeder.
	s2 := neutralBattle(t, d, 1, []int{143}, []int{73})
	teachMoves(t, d, &s2.Sides[0].Team[0], "leech-seed", "splash")
	teachMoves(t, d, &s2.Sides[1].Team[0], "splash")
	s2.Active(1).Ability = AbilityNone
	s2.Active(0).HP = s2.Active(0).MaxHP / 2
	before := s2.Active(0).HP
	ResolveTurn(d, s2, [2]Action{moveAt(0), moveAt(0)})
	if s2.Active(0).HP <= before {
		t.Errorf("control: without Liquid Ooze the seeder should drain, %d -> %d",
			before, s2.Active(0).HP)
	}
}
