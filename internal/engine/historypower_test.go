package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// historypower_test.go covers the moves whose power depends on something that
// already happened — to the target, to the user, or across the whole battle.
//
// They were all inert together, and for one reason: none of them is difficult,
// and none of them could work until something wrote the state down. Every one
// shipped as a flat base power that the hand-coded block in executeMove had no
// case for, so the engine measured the second Fury Cutter at 106% of the first.

func hpBattle(t *testing.T, d *domain.Dex, userMove, foeMove string) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "hp", "P1", []int{143, 65}, "P2", []int{143, 65}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			p.Item, p.Ability = ItemNone, AbilityNone
			p.Moves = []MoveSlot{{MoveID: foeMove, PP: 40, MaxPP: 40}}
		}
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: userMove, PP: 40, MaxPP: 40}}
	return s
}

// hpPower reads a move's resolved base power through the same path executeMove
// uses, so the assertions are about the rule rather than about a damage roll.
func hpPower(s *BattleState, d *domain.Dex, moveID string) int {
	return applyCallbackPower(s, s.Active(0), s.Active(1), d.Moves[moveID]).Power
}

// TestAssuranceDoublesOnAnyDamageTheTargetTook: "hurt this turn" is broader
// than "hit by a move this turn", and the difference is the whole row. The
// ported case measures a target that damaged *itself* with Wild Charge's
// recoil, which DamagedThisTurn deliberately excludes — it exists to answer
// Revenge and Focus Punch, which ask about being hit by the foe.
func TestAssuranceDoublesOnAnyDamageTheTargetTook(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "assurance", "splash")
	if got, want := hpPower(s, d, "assurance"), 60; got != want {
		t.Errorf("untouched target: power = %d, want %d", got, want)
	}
	s.Active(1).Volatiles.HurtThisTurn = true
	if got, want := hpPower(s, d, "assurance"), 120; got != want {
		t.Errorf("already-hurt target: power = %d, want %d", got, want)
	}
}

// TestEveryKindOfDamageCountsAsBeingHurt: the breadth is the point. Assurance
// reads canon's hurtThisTurn, which spreadDamage sets on any nonzero damage,
// and the ported case measures a target that chipped *itself* with Wild
// Charge's recoil. DamagedThisTurn deliberately excludes that — it exists to
// answer Revenge and Focus Punch, which ask about being hit by the foe.
//
// Checked at the helper rather than through a turn because the flag is
// per-turn: the end-of-turn sweep clears it before ResolveTurn returns, so
// nothing outside the turn can observe it.
func TestEveryKindOfDamageCountsAsBeingHurt(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "assurance", "splash")
	p := s.Active(1)
	var log []LogLine

	applySelfDamage(p, 1, 10, &log) // recoil
	if !p.Volatiles.HurtThisTurn {
		t.Error("recoil should count as being hurt")
	}
	if p.Volatiles.DamagedThisTurn {
		t.Error("recoil is not being hit by a move, and the narrower flag must " +
			"stay unset — the gap between the two is the whole point")
	}

	p.Volatiles.HurtThisTurn = false
	applyResidual(s, 1, &log) // no residual armed: nothing should fire
	if p.Volatiles.HurtThisTurn {
		t.Error("a turn with no residual damage should leave the flag alone")
	}
}

// TestRageFistCountsHitsAcrossTheWholeBattle: +50 per hit taken, capped at 350
// — and the counter is not a volatile. A Rage Fist user that pivots out under
// pressure and comes back keeps every hit, which is exactly the play the move
// exists for.
func TestRageFistCountsHitsAcrossTheWholeBattle(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "rage-fist", "tackle")
	user := s.Active(0)

	if got, want := hpPower(s, d, "rage-fist"), 50; got != want {
		t.Errorf("never hit: power = %d, want %d", got, want)
	}
	playTurn(d, s, 0, 0)
	if user.TimesAttacked != 1 {
		t.Fatalf("setup: one Tackle should have counted once, got %d", user.TimesAttacked)
	}
	if got, want := hpPower(s, d, "rage-fist"), 100; got != want {
		t.Errorf("after one hit: power = %d, want %d", got, want)
	}

	// Out and back: the count survives, unlike everything in Volatiles.
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 0}, {Kind: ActionMove, Index: 0}})
	if got := s.Sides[0].Team[0].TimesAttacked; got < 1 {
		t.Errorf("the hit count must survive a switch, got %d", got)
	}

	// And it caps.
	s.Sides[0].Team[0].TimesAttacked = 99
	s.Sides[0].Active = 0
	if got, want := hpPower(s, d, "rage-fist"), 350; got != want {
		t.Errorf("capped: power = %d, want %d", got, want)
	}
}

// TestTrumpCardHitsHardestOnItsLastPP: the inverted curve. Read off the slot
// after the PP for this use has been paid, which is the figure canon's
// basePowerCallback sees.
func TestTrumpCardHitsHardestOnItsLastPP(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "trump-card", "splash")
	for _, c := range []struct{ pp, power int }{
		{5, 40}, {4, 40}, {3, 50}, {2, 60}, {1, 80}, {0, 200},
	} {
		s.Active(0).Moves[0].PP = c.pp
		if got := hpPower(s, d, "trump-card"); got != c.power {
			t.Errorf("%d PP left: power = %d, want %d", c.pp, got, c.power)
		}
	}
}

// TestStompingTantrumDoublesAfterAFailedMove: strictly after a *failed* move.
// A move that resolved and merely accomplished little does not arm it, which is
// why this reads the engine's own move-failure signal rather than anything
// derived from the log. Spit Up with no stockpile is the clean failure — it
// refuses before it can do anything at all.
func TestStompingTantrumDoublesAfterAFailedMove(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "stomping-tantrum", "splash")
	s.Active(0).Moves = []MoveSlot{
		{MoveID: "stomping-tantrum", PP: 10, MaxPP: 10},
		{MoveID: "spit-up", PP: 10, MaxPP: 10},
	}
	log := playTurn(d, s, 1, 0)
	if !logHas(log, "But it failed!") {
		t.Fatalf("setup: Spit Up with no stockpile should fail, got %v", logTexts(log))
	}
	if !s.Active(0).Volatiles.MoveLastTurnFailed {
		t.Fatalf("the failure should have been recorded for next turn")
	}
	if got, want := hpPower(s, d, "stomping-tantrum"), 150; got != want {
		t.Errorf("after a failed move: power = %d, want %d", got, want)
	}

	// A move that lands leaves it at base.
	c := hpBattle(t, d, "stomping-tantrum", "splash")
	c.Active(0).Moves = []MoveSlot{
		{MoveID: "stomping-tantrum", PP: 10, MaxPP: 10},
		{MoveID: "tackle", PP: 35, MaxPP: 35},
	}
	playTurn(d, c, 1, 0)
	if c.Active(0).Volatiles.MoveLastTurnFailed {
		t.Fatalf("setup: a connecting Tackle is not a failure")
	}
	if got, want := hpPower(c, d, "stomping-tantrum"), 75; got != want {
		t.Errorf("after a move that landed: power = %d, want %d", got, want)
	}
}

// TestStompingTantrumForgetsAFailureAfterOneTurn: "last turn" means last turn.
// A record that never cleared would leave the move permanently doubled from the
// first time anything went wrong.
func TestStompingTantrumForgetsAFailureAfterOneTurn(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "stomping-tantrum", "splash")
	s.Active(0).Moves = []MoveSlot{
		{MoveID: "stomping-tantrum", PP: 10, MaxPP: 10},
		{MoveID: "spit-up", PP: 10, MaxPP: 10},
	}
	playTurn(d, s, 1, 0) // fails
	playTurn(d, s, 0, 0) // Stomping Tantrum lands, doubled
	if got, want := hpPower(s, d, "stomping-tantrum"), 75; got != want {
		t.Errorf("a turn later the doubling should be gone: power = %d, want %d", got, want)
	}
}

// TestLashOutDoublesAfterTheUsersStatsFell: the drop is read on the *user*, and
// any source counts — a foe's String Shot arms it just as the user's own Close
// Combat would.
//
// Measured as damage over a sweep of seeds rather than by reading the flag,
// because the flag is per-turn: it is set and cleared inside the same
// ResolveTurn, so nothing outside the turn can observe it. The drop therefore
// has to land *before* Lash Out in the same turn, which is why the foe is a
// Weezing — it outspeeds Snorlax, so its String Shot resolves first.
//
// Two details keep the measurement honest. String Shot rather than Growl or
// Leer: it drops Speed, which changes neither the damage Lash Out deals nor the
// defense it is dealt against, so the only difference between the two arms is
// the flag. And the target sits at +6 Defense in both arms, because a Weezing
// that faints would have the sweep measuring its HP rather than the damage.
func TestLashOutDoublesAfterTheUsersStatsFell(t *testing.T) {
	d := loadDex(t)
	sweep := func(foeMove string) int {
		total := 0
		for seed := uint64(1); seed <= 25; seed++ {
			s, err := NewBattle(d, "lo", "P1", []int{143}, "P2", []int{110}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			for i := range s.Sides {
				p := &s.Sides[i].Team[0]
				p.Item, p.Ability = ItemNone, AbilityNone
			}
			s.Active(0).Moves = []MoveSlot{{MoveID: "lash-out", PP: 10, MaxPP: 10}}
			foe := s.Active(1)
			foe.Moves = []MoveSlot{{MoveID: foeMove, PP: 40, MaxPP: 40}}
			foe.Stages.Def = 6
			before := foe.HP
			playTurn(d, s, 0, 0)
			if foe.Fainted {
				t.Fatalf("seed %d: the target must survive for the sweep to measure damage", seed)
			}
			total += before - foe.HP
		}
		return total
	}
	plain, lowered := sweep("splash"), sweep("string-shot")
	if plain == 0 {
		t.Fatalf("control: Lash Out should have dealt damage")
	}
	if lowered < plain*3/2 {
		t.Errorf("Lash Out after a stat drop should hit at roughly double power: "+
			"%d lowered vs %d plain", lowered, plain)
	}
}

// TestStatDirectionFlagsClearBetweenTurns: both flags are per-turn, and the
// sweep runs at the end of the turn they were set on. Left standing they would
// arm Lash Out and Burning Jealousy off something several turns old.
func TestStatDirectionFlagsClearBetweenTurns(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "splash", "growl")
	playTurn(d, s, 0, 0)
	if s.Active(0).Stages.Atk != -1 {
		t.Fatalf("setup: Growl should have dropped the user's Attack")
	}
	if s.Active(0).Volatiles.StatsLoweredThisTurn {
		t.Error("the drop happened this turn, so the flag must not survive it")
	}
	if s.Active(0).Volatiles.StatsRaisedThisTurn {
		t.Error("nothing raised a stat, so the raise flag should be unset")
	}
}

// TestRefusedStatChangesDoNotArmTheFlags: a drop Clear Body refused, or one
// clamped at the floor, did not happen. Arming off the attempt would let a
// Lash Out user double its power against a target that successfully stopped it.
func TestRefusedStatChangesDoNotArmTheFlags(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "splash", "growl")
	user := s.Active(0)
	user.Stages.Atk = -6 // already at the floor: Growl can take nothing more

	playTurn(d, s, 0, 0)
	if user.Volatiles.StatsLoweredThisTurn {
		t.Error("a drop that could not apply should not arm Lash Out")
	}
}

// TestFuryCutterRampsAndBreaks: doubling per consecutive connecting use, capped
// at 4x, and back to the bottom after any turn the chain does not continue.
func TestFuryCutterRampsAndBreaks(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "fury-cutter", "splash")
	// Two slots, so the chain can be broken by choosing something else.
	s.Active(0).Moves = []MoveSlot{
		{MoveID: "fury-cutter", PP: 20, MaxPP: 20},
		{MoveID: "splash", PP: 40, MaxPP: 40},
	}
	want := []int{40, 80, 160, 160, 160}
	for i, w := range want {
		if got := hpPower(s, d, "fury-cutter"); got != w {
			t.Fatalf("use %d: power = %d, want %d", i+1, got, w)
		}
		// hpPower ticks the counter the way a real use does, so the chain has
		// to be kept alive by hand between reads.
		s.Active(0).Volatiles.FuryCutter.TurnsLeft = 2
	}

	// A turn of anything else lets it expire.
	s.Active(0).Volatiles.FuryCutter = nil
	playTurn(d, s, 0, 0) // Fury Cutter connects: chain armed
	if s.Active(0).Volatiles.FuryCutter == nil {
		t.Fatalf("setup: a connecting Fury Cutter should arm the chain")
	}
	playTurn(d, s, 1, 0) // Splash: nothing to refresh it
	if s.Active(0).Volatiles.FuryCutter != nil {
		t.Error("a turn without Fury Cutter should break the chain")
	}
}

// TestRolloutRampsForFiveTurnsThenResets: 30 BP doubling to 480 across five
// consecutive uses, then starting over. The reset is the half that makes
// Rollout a gamble rather than a win condition.
func TestRolloutRampsForFiveTurnsThenResets(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "rollout", "splash")
	want := []int{30, 60, 120, 240, 480, 30, 60}
	for i, w := range want {
		if got := hpPower(s, d, "rollout"); got != w {
			t.Fatalf("use %d: power = %d, want %d", i+1, got, w)
		}
	}
}

// TestRolloutDoublesAfterDefenseCurl and TestRolloutChainBreaks together pin
// the two things that end or amplify the ramp.
func TestRolloutDoublesAfterDefenseCurl(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "rollout", "splash")
	s.Active(0).Volatiles.DefenseCurl = true
	if got, want := hpPower(s, d, "rollout"), 60; got != want {
		t.Errorf("curled: power = %d, want %d", got, want)
	}
}

func TestRolloutChainBreaksOnAnythingElse(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "rollout", "splash")
	s.Active(0).Moves = []MoveSlot{
		{MoveID: "rollout", PP: 20, MaxPP: 20},
		{MoveID: "splash", PP: 40, MaxPP: 40},
	}
	playTurn(d, s, 0, 0)
	if s.Active(0).Volatiles.Rollout == nil {
		t.Fatalf("setup: a connecting Rollout should arm the chain")
	}
	playTurn(d, s, 1, 0) // Splash
	if s.Active(0).Volatiles.Rollout != nil {
		t.Error("a turn without Rollout should break the chain")
	}
}

// TestProtectIsNotAMoveFailure: the distinction the port caught. Canon's
// Protect returns NOT_FAIL — null, not false — and Stomping Tantrum compares
// strictly against false, so an attack that was answered is not an attack that
// botched. It is also where this record and the Metronome streak diverge: the
// streak *does* break on a Protect, because it asks a different question.
func TestProtectIsNotAMoveFailure(t *testing.T) {
	d := loadDex(t)
	s := hpBattle(t, d, "tackle", "protect")
	s.Active(0).Moves = []MoveSlot{
		{MoveID: "tackle", PP: 35, MaxPP: 35},
		{MoveID: "stomping-tantrum", PP: 10, MaxPP: 10},
	}
	log := playTurn(d, s, 0, 0)
	if !logHas(log, "protected itself") {
		t.Fatalf("setup: the foe should have protected, got %v", logTexts(log))
	}
	if s.Active(0).Volatiles.MoveLastTurnFailed {
		t.Error("hitting a Protect is not the move failing")
	}
	if got, want := hpPower(s, d, "stomping-tantrum"), 75; got != want {
		t.Errorf("after being Protected: power = %d, want %d", got, want)
	}
}

// TestStatDropOnAReplacementDoesNotCarryIntoTheNextTurn: an Intimidate on a
// Pokémon that comes in after a KO lands during ResolveReplace, which the
// end-of-turn sweep has already run past — so the flag really is still set when
// that phase returns. What must not happen is for it to still be set when the
// next turn's moves resolve. The clearing therefore lives at the top of
// ResolveTurn, the same place and for the same reason as MovedThisTurn.
//
// The second half measures damage rather than reading the flag, because by the
// time ResolveTurn returns the end-of-turn sweep has cleared it either way and
// reading it would prove nothing. The flag is set by hand rather than via
// Intimidate so the two arms differ in exactly one thing: an Intimidate would
// also cut the Attack the measurement depends on.
func TestStatDropOnAReplacementDoesNotCarryIntoTheNextTurn(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "repl", "P1", []int{143}, "P2", []int{65, 143}, 3)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	atk := s.Active(0)
	atk.Item, atk.Ability = ItemNone, AbilityNone
	atk.Moves = []MoveSlot{{MoveID: "lash-out", PP: 10, MaxPP: 10}}
	for j := range s.Sides[1].Team {
		p := &s.Sides[1].Team[j]
		p.Item = ItemNone
		p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	}
	s.Sides[1].Team[0].Ability = AbilityNone
	s.Sides[1].Team[0].HP = 1                      // dies to the first hit
	s.Sides[1].Team[1].Ability = AbilityIntimidate // and its replacement cuts Attack

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !s.Sides[1].Team[0].Fainted {
		t.Fatalf("setup: the 1 HP foe should have fainted")
	}
	ResolveReplace(s, [2]*Action{nil, {Kind: ActionSwitch, Index: 1}})
	if atk.Stages.Atk != -1 {
		t.Fatalf("setup: the replacement's Intimidate should have cut Attack, got %+v", atk.Stages)
	}
	if !atk.Volatiles.StatsLoweredThisTurn {
		t.Fatalf("setup: the replace phase runs past the end-of-turn sweep, so the " +
			"flag is expected to survive it — the fix is at the top of the next turn")
	}

	// And the next turn does not double.
	sweep := func(preset bool) int {
		total := 0
		for seed := uint64(1); seed <= 25; seed++ {
			b, err := NewBattle(d, "lo", "P1", []int{143}, "P2", []int{110}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			for i := range b.Sides {
				p := &b.Sides[i].Team[0]
				p.Item, p.Ability = ItemNone, AbilityNone
			}
			b.Active(0).Moves = []MoveSlot{{MoveID: "lash-out", PP: 10, MaxPP: 10}}
			foe := b.Active(1)
			foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			foe.Stages.Def = 6
			// A drop left over from before this turn began, exactly as the
			// replace phase would have left it.
			b.Active(0).Volatiles.StatsLoweredThisTurn = preset
			before := foe.HP
			playTurn(d, b, 0, 0)
			total += before - foe.HP
		}
		return total
	}
	stale, clean := sweep(true), sweep(false)
	if clean == 0 {
		t.Fatalf("control: Lash Out should have dealt damage")
	}
	if stale != clean {
		t.Errorf("a drop from before the turn began must not double Lash Out: "+
			"%d with a stale flag vs %d without", stale, clean)
	}
}
