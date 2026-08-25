package engine

import "testing"

// customhp_behavior_test.go covers the moves whose damage or healing is an
// arithmetic expression over the two Pokémon's HP rather than the damage
// formula's output.
//
// Six of them came off the data-sync denylist together (Belly Drum, Pain Split,
// Endeavor, Super Fang, Final Gambit, Memento). Three more — Sonic Boom, Dragon
// Rage and Psywave — had been shipping all along with the same mechanic missing
// and nothing to notice it: their amounts live in Showdown's static `damage`
// field, which refresh-upstream does not capture, so they arrived at power 0
// and dealt the damage formula's one-point floor. A move that deals 1 damage is
// not inert, so TestNoCuratedMoveIsInert has no opinion about it. These tests
// are the opinion.
//
// The numbers are asserted exactly wherever canon is exact, because that is the
// whole content of the group: an off-by-one in a halving or a rounding choice is
// the only way any of these can be wrong.

// TestSuperFangHalvesCurrentHP: floor(target.hp / 2), from the *current* HP
// rather than the maximum, and never below 1 — clampIntRange(hp / 2, 1).
func TestSuperFangHalvesCurrentHP(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "super-fang")
	teachMoves(t, d, s.Active(1), "splash")

	foe := s.Active(1)
	foe.HP = 101
	playTurn(d, s, 0, 0)
	if foe.HP != 51 {
		t.Errorf("Super Fang from 101 HP should leave 51 (101 - floor(101/2)), got %d", foe.HP)
	}

	// A target on 1 HP still loses its last point: the minimum is 1, not 0.
	foe.HP = 1
	playTurn(d, s, 0, 0)
	if !foe.Fainted {
		t.Errorf("Super Fang should finish a target on 1 HP, left it on %d", foe.HP)
	}
}

// TestSuperFangRespectsTypeImmunity: the amount short-circuits the formula but
// not the type chart. Canon runs runImmunity above the whole getDamage prologue,
// so a Ghost is untouched by the Normal-type bite.
func TestSuperFangRespectsTypeImmunity(t *testing.T) {
	d := loadDex(t)
	// 94 is Gengar, the dex's only Ghost.
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{94, 65})
	teachMoves(t, d, s.Active(0), "super-fang")
	teachMoves(t, d, s.Active(1), "splash")

	foe := s.Active(1)
	before := foe.HP
	log := playTurn(d, s, 0, 0)
	if foe.HP != before {
		t.Errorf("a Ghost should be immune to Super Fang, took %d", before-foe.HP)
	}
	if !logHas(log, "doesn't affect") {
		t.Errorf("the immunity should be announced, got %v", logTexts(log))
	}
}

// TestEndeavorLevelsHPDown: damage = target.hp - user.hp, so the target ends on
// the user's HP exactly.
func TestEndeavorLevelsHPDown(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "endeavor")
	teachMoves(t, d, s.Active(1), "splash")

	user, foe := s.Active(0), s.Active(1)
	user.HP, foe.HP = 20, 180
	playTurn(d, s, 0, 0)
	if foe.HP != 20 {
		t.Errorf("Endeavor should drag the target down to the user's 20 HP, got %d", foe.HP)
	}
}

// TestEndeavorRefusesATargetItCannotDragDown: canon states the gate as an
// onTryImmunity — `return pokemon.hp < target.hp` — so it announces as an
// immunity rather than a failure, and it is strict: equal HP refuses. It also
// sits above the accuracy roll, which is why an Endeavor with nothing to do can
// never be recorded as a miss.
func TestEndeavorRefusesATargetItCannotDragDown(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		name             string
		userHP, foeHP    int
		wantFoeUnchanged bool
	}{
		{"equal HP", 100, 100, true},
		{"user higher", 180, 100, true},
		{"user lower", 20, 100, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
			teachMoves(t, d, s.Active(0), "endeavor")
			teachMoves(t, d, s.Active(1), "splash")
			user, foe := s.Active(0), s.Active(1)
			user.HP, foe.HP = c.userHP, c.foeHP

			log := playTurn(d, s, 0, 0)
			unchanged := foe.HP == c.foeHP
			if unchanged != c.wantFoeUnchanged {
				t.Errorf("target HP %d → %d; wanted unchanged=%v", c.foeHP, foe.HP, c.wantFoeUnchanged)
			}
			if c.wantFoeUnchanged {
				if !logHas(log, "doesn't affect") {
					t.Errorf("the refusal should read as an immunity, got %v", logTexts(log))
				}
				if logHas(log, "attack missed") {
					t.Errorf("the refusal happens above the accuracy roll, so it must not read as a miss: %v", logTexts(log))
				}
			}
		})
	}
}

// TestFinalGambitTradesTheUserForItsRemainingHP: damage equals the user's whole
// HP bar, read before the user pays it, and the user faints.
func TestFinalGambitTradesTheUserForItsRemainingHP(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "final-gambit")
	teachMoves(t, d, s.Active(1), "splash")

	user, foe := s.Active(0), s.Active(1)
	user.HP = 77
	foeBefore := foe.HP
	playTurn(d, s, 0, 0)

	if got := foeBefore - foe.HP; got != 77 {
		t.Errorf("Final Gambit should deal the user's 77 HP, dealt %d", got)
	}
	if !user.Fainted {
		t.Errorf("Final Gambit should faint its user, left it on %d HP", user.HP)
	}
}

// TestFinalGambitDoesNotTradeTheUserForNothing is the half that separates
// selfdestruct: 'ifHit' from Explosion's 'always'. Canon faints an Explosion
// user above the hit steps, so it detonates through a type immunity; Final
// Gambit's faint is checked from inside the hit, so a Ghost walling the
// Fighting-type attack costs the user only its PP.
func TestFinalGambitDoesNotTradeTheUserForNothing(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{94, 65})
	teachMoves(t, d, s.Active(0), "final-gambit")
	teachMoves(t, d, s.Active(1), "splash")

	user, foe := s.Active(0), s.Active(1)
	user.HP = 77
	foeBefore := foe.HP
	playTurn(d, s, 0, 0)

	if foe.HP != foeBefore {
		t.Errorf("a Ghost should be immune to Final Gambit, took %d", foeBefore-foe.HP)
	}
	if user.Fainted || user.HP != 77 {
		t.Errorf("a Final Gambit that never landed must not faint its user; HP %d fainted %v", user.HP, user.Fainted)
	}
}

// TestBellyDrumCostsHalfAndMaxesAttack: the cost is floor(MaxHP/2), leaving
// ceil(MaxHP/2), and the boost is +12 clamped to +6 — twelve rather than six so
// a user already partway up still ends maxed.
func TestBellyDrumCostsHalfAndMaxesAttack(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "belly-drum")
	teachMoves(t, d, s.Active(1), "splash")

	user := s.Active(0)
	user.Stages.Atk = 2
	want := (user.MaxHP + 1) / 2
	playTurn(d, s, 0, 0)

	if user.HP != want {
		t.Errorf("Belly Drum should leave ceil(MaxHP/2) = %d HP, got %d", want, user.HP)
	}
	if user.Stages.Atk != 6 {
		t.Errorf("Belly Drum should max Attack from +2, got %+d", user.Stages.Atk)
	}
	// directDamage upstream: no damage event, so nothing that reads "was this
	// Pokémon hurt this turn" may see it. Assurance doubles off HurtThisTurn.
	if user.Volatiles.HurtThisTurn {
		t.Error("Belly Drum's self-damage is direct, so it must not set HurtThisTurn")
	}
}

// TestBellyDrumRefusals: canon states all three as one condition checked before
// any HP is spent, so a refused Belly Drum costs nothing but its PP.
func TestBellyDrumRefusals(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		name string
		// setup runs before the turn; it returns the HP the user should still
		// have afterwards.
		setup func(p *Pokemon) int
	}{
		{"below half HP", func(p *Pokemon) int { p.HP = p.MaxHP / 2; return p.HP }},
		{"exactly half HP", func(p *Pokemon) int {
			// The comparison canon makes is `hp <= maxhp / 2` on real numbers,
			// so a user sitting on exactly half refuses.
			p.HP = p.MaxHP / 2
			return p.HP
		}},
		{"already at +6 Attack", func(p *Pokemon) int { p.Stages.Atk = 6; return p.HP }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
			teachMoves(t, d, s.Active(0), "belly-drum")
			teachMoves(t, d, s.Active(1), "splash")
			user := s.Active(0)
			wantHP := c.setup(user)
			// Captured after the setup, so the "already maxed" case compares
			// against +6 rather than against the fixture's fresh zero.
			atkBefore := user.Stages.Atk

			log := playTurn(d, s, 0, 0)
			if user.HP != wantHP {
				t.Errorf("a refused Belly Drum must cost no HP: %d → %d", wantHP, user.HP)
			}
			if user.Stages.Atk != atkBefore {
				t.Errorf("a refused Belly Drum must not boost: %+d → %+d", atkBefore, user.Stages.Atk)
			}
			if !logHas(log, "But it failed!") {
				t.Errorf("the refusal should be announced, got %v", logTexts(log))
			}
		})
	}
}

// TestPainSplitAveragesBothBars: both end on floor((a+b)/2), each clamped to its
// own maximum. The clamp is what makes the move asymmetric — a big bar hands
// over more than it can receive back.
func TestPainSplitAveragesBothBars(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "pain-split")
	teachMoves(t, d, s.Active(1), "splash")

	user, foe := s.Active(0), s.Active(1)
	user.HP, foe.HP = 21, 180
	playTurn(d, s, 0, 0)

	const want = (21 + 180) / 2 // 100
	if user.HP != want || foe.HP != want {
		t.Errorf("Pain Split should leave both on %d, got user %d foe %d", want, user.HP, foe.HP)
	}
}

// TestPainSplitIsNotHealingAndIsNotDamage. Canon writes both bars with sethp,
// which is neither a heal event nor a damage event, and two separate rules fall
// out of that: Heal Block cannot refuse the gaining half, and the losing half
// does not arm Assurance. Upstream ships a case for the second.
func TestPainSplitIsNotHealingAndIsNotDamage(t *testing.T) {
	d := loadDex(t)

	t.Run("heal block does not refuse it", func(t *testing.T) {
		s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
		teachMoves(t, d, s.Active(0), "pain-split")
		teachMoves(t, d, s.Active(1), "splash")
		user, foe := s.Active(0), s.Active(1)
		user.HP, foe.HP = 21, 180
		user.Volatiles.HealBlock = &HealBlockState{Turns: 5}

		playTurn(d, s, 0, 0)
		if user.HP != 100 {
			t.Errorf("Pain Split carries no heal flag, so Heal Block must not stop it: got %d, want 100", user.HP)
		}
	})

	t.Run("the losing half does not set HurtThisTurn", func(t *testing.T) {
		s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
		teachMoves(t, d, s.Active(0), "pain-split")
		teachMoves(t, d, s.Active(1), "splash")
		user, foe := s.Active(0), s.Active(1)
		user.HP, foe.HP = 21, 180

		playTurn(d, s, 0, 0)
		if foe.Volatiles.HurtThisTurn {
			t.Error("Pain Split is not damage, so the target it drained must not read as hurt this turn")
		}
	})
}

// TestMementoFaintsItsUserForConnecting, not for succeeding. Canon tests whether
// the hit step reached the target and tests it before folding in whether the
// drops landed, so a target that refuses the drops still buries the user.
func TestMementoFaintsItsUserForConnecting(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "memento")
	teachMoves(t, d, s.Active(1), "splash")

	user, foe := s.Active(0), s.Active(1)
	// Both stats already at the floor: nothing for the move to accomplish.
	foe.Stages.Atk, foe.Stages.SpA = -6, -6
	playTurn(d, s, 0, 0)

	if !user.Fainted {
		t.Errorf("Memento should faint its user even when the drops accomplish nothing; HP %d", user.HP)
	}
	if s.Phase != PhaseReplace {
		t.Errorf("the fainted user should owe a replacement, phase is %q", s.Phase)
	}
}

// TestMementoDropsBothStatsAndFaints is the ordinary case: the drops ride the
// declarative payload the transform emits, and the sacrifice is the engine's.
func TestMementoDropsBothStatsAndFaints(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "memento")
	teachMoves(t, d, s.Active(1), "splash")

	user, foe := s.Active(0), s.Active(1)
	playTurn(d, s, 0, 0)

	if foe.Stages.Atk != -2 || foe.Stages.SpA != -2 {
		t.Errorf("Memento should drop both offenses by two, got atk %+d spatk %+d", foe.Stages.Atk, foe.Stages.SpA)
	}
	if !user.Fainted {
		t.Error("Memento should faint its user")
	}
}

// TestMementoWalledByASubstituteCostsNothing. The doll refuses the move outright
// — Memento carries no bypass-sub flag in gen 9 — so the hit never reaches the
// target and the sacrifice is never paid. This is the case that makes "faints on
// connecting" different from "faints on use".
func TestMementoWalledByASubstituteCostsNothing(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "memento")
	teachMoves(t, d, s.Active(1), "splash")

	user, foe := s.Active(0), s.Active(1)
	foe.Volatiles.Substitute = &SubstituteState{HP: 40, MaxHP: 40}
	playTurn(d, s, 0, 0)

	if user.Fainted {
		t.Error("a Memento a Substitute refused must not faint its user")
	}
	if foe.Stages.Atk != 0 || foe.Stages.SpA != 0 {
		t.Errorf("the doll should have absorbed the drops, got atk %+d spatk %+d", foe.Stages.Atk, foe.Stages.SpA)
	}
	if s.Phase == PhaseReplace {
		t.Error("no replacement should be owed")
	}
}

// TestMementoBlockedByProtectCostsNothing: Protect stops the move above the hit
// step, so — like the Substitute — there is nothing to pay for.
func TestMementoBlockedByProtectCostsNothing(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "memento")
	teachMoves(t, d, s.Active(1), "protect")

	user := s.Active(0)
	playTurn(d, s, 0, 0)

	if user.Fainted {
		t.Error("a Memento a Protect refused must not faint its user")
	}
}

// TestStaticFixedDamageMovesDealTheirStatedAmount. Sonic Boom and Dragon Rage
// carry their 20 and 40 in Showdown's static `damage` field, which the upstream
// refresh does not capture — so both shipped at power 0 and dealt the damage
// formula's one-point floor. The amount is flat: no STAB, no effectiveness
// multiplier, no roll.
func TestStaticFixedDamageMovesDealTheirStatedAmount(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		move string
		want int
	}{
		{"sonic-boom", 20},
		{"dragon-rage", 40},
	} {
		t.Run(c.move, func(t *testing.T) {
			s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
			teachMoves(t, d, s.Active(0), c.move)
			teachMoves(t, d, s.Active(1), "splash")
			foe := s.Active(1)
			before := foe.HP

			playTurn(d, s, 0, 0)
			if got := before - foe.HP; got != c.want {
				t.Errorf("%s should deal exactly %d, dealt %d", c.move, c.want, got)
			}
		})
	}
}

// TestPsywaveRollsWithinItsBand. Canon is `random(50, 151) * level / 100`, which
// at this engine's fixed level 50 is 25..75 inclusive. Asserted as a band rather
// than a number because it is the one move in the family that draws.
func TestPsywaveRollsWithinItsBand(t *testing.T) {
	d := loadDex(t)
	lo, hi := 1<<30, 0
	for seed := uint64(1); seed <= 60; seed++ {
		s := neutralBattle(t, d, seed, []int{143, 65}, []int{143, 65})
		teachMoves(t, d, s.Active(0), "psywave")
		teachMoves(t, d, s.Active(1), "splash")
		foe := s.Active(1)
		before := foe.HP
		playTurn(d, s, 0, 0)
		got := before - foe.HP
		if got < 25 || got > 75 {
			t.Fatalf("seed %d: Psywave dealt %d, outside the 25..75 band", seed, got)
		}
		if got < lo {
			lo = got
		}
		if got > hi {
			hi = got
		}
	}
	// A constant would also sit inside the band, so require the roll to move.
	if lo == hi {
		t.Errorf("Psywave dealt %d on every one of 60 seeds, so it is not rolling", lo)
	}
}

// TestFixedDamageIgnoresEffectivenessButNotImmunity. Dragon Rage into a Steel
// type — Dragon is resisted by Steel — still deals its flat 40, because canon
// returns the amount above the whole effectiveness/STAB/crit/roll block. Only a
// zero from the type chart, which sits above it, stops these moves.
func TestFixedDamageIgnoresEffectivenessButNotImmunity(t *testing.T) {
	d := loadDex(t)
	// 82 is Magneton: Electric/Steel, so Dragon is resisted.
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{82, 65})
	teachMoves(t, d, s.Active(0), "dragon-rage")
	teachMoves(t, d, s.Active(1), "splash")

	foe := s.Active(1)
	before := foe.HP
	log := playTurn(d, s, 0, 0)
	if got := before - foe.HP; got != 40 {
		t.Errorf("Dragon Rage into a Steel type should still deal 40, dealt %d", got)
	}
	if logHas(log, "not very effective") {
		t.Errorf("a fixed-damage move reports no effectiveness line, got %v", logTexts(log))
	}
}
