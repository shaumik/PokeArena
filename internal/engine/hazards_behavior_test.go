package engine

import (
	"strings"
	"testing"

	"pokearena/internal/domain"
)

// hazards_behavior_test.go plays real battles through the public entry
// points (NewBattle / ResolveTurn / ResolveReplace) for the field state
// that outlives the Pokémon standing on it: entry hazards and their
// removal (Rapid Spin, Defog), the slot conditions (Wish, Healing Wish),
// and the one-turn guard flags (Quick Guard, Wide Guard).
//
// The mechanics themselves already had tests that call the handlers
// directly. Those pin the arithmetic but not the wiring, and every one of
// these mechanics is a wiring bug waiting to happen: a hazard clear that
// never reaches the switch-in hook, a Wish that fires on the caster
// instead of the slot, a Healing Wish consumed by the wrong Pokémon, a
// guard flag that never ticks off. Each test below therefore asserts the
// mechanic the way a player would see it — HP on a Pokémon that walked in
// two turns later, a log line, a move that no longer fails.

// --- shared fixture -------------------------------------------------

// logHasOnSide is logHas narrowed to one side's lines, so a "But it
// failed!" belonging to the opponent can't be mistaken for the one under
// test.
func logHasOnSide(log []LogLine, side int, substr string) bool {
	for _, l := range log {
		if l.Side == side && strings.Contains(l.Text, substr) {
			return true
		}
	}
	return false
}

// --- hazard removal -------------------------------------------------

// TestBattleRapidSpinClearsTheFloorItStandsOn pins the whole Rapid Spin
// loop as a player experiences it: hazards go down, a switch-in pays for
// them, the spin sweeps them, and the NEXT switch-in walks in free.
//
// Canon rule: Rapid Spin removes every entry hazard from the user's OWN
// side, and only that side. Both halves matter. A clear that never
// reaches the switch-in hook is the bug this guards — a test that reads
// the Hazards struct straight after the handler passes even if the struct
// the switch-in path consults is a different one; only a real switch-in
// proves the floor the incoming Pokémon walks onto is the floor that was
// swept. The "own side only" half is the other classic slip: a spin that
// also wipes the foe's rocks hands the spinner's team a free Defog and
// quietly deletes hazard stacking from the format.
func TestBattleRapidSpinClearsTheFloorItStandsOn(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143, 9, 131}, []int{143, 9})
	teachMoves(t, d, &s.Sides[0].Team[0], "stealth-rock", "splash") // Snorlax
	teachMoves(t, d, &s.Sides[0].Team[1], "rapid-spin", "splash")   // Blastoise
	teachMoves(t, d, &s.Sides[0].Team[2], "splash")                 // Lapras
	teachMoves(t, d, &s.Sides[1].Team[0], "stealth-rock", "spikes", "splash")
	teachMoves(t, d, &s.Sides[1].Team[1], "splash")

	// Turn 1: both sides lay Stealth Rock on the opponent.
	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	// Turn 2: the foe adds a layer of Spikes to side 0's floor.
	ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(1)})

	spinner := &s.Sides[0].Team[1]
	fullHP := spinner.MaxHP
	// Turn 3: the spinner walks in and pays the toll. Blastoise is pure
	// Water, so Stealth Rock is neutral (1/8) and one Spikes layer is
	// another 1/8.
	log := ResolveTurn(d, s, [2]Action{switchTo(1), moveAt(2)})
	wantChip := fullHP/8 + fullHP/8
	if got := fullHP - spinner.HP; got != wantChip {
		t.Fatalf("switch-in onto rocks+spikes lost %d HP, want %d; log: %v", got, wantChip, logTexts(log))
	}
	if !logHas(log, "Pointed stones dug into") || !logHas(log, "hurt by the spikes") {
		t.Fatalf("expected both hazard lines on the switch-in; log: %v", logTexts(log))
	}

	// Turn 4: Rapid Spin.
	log = ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(2)})
	if !logHas(log, "blew away the hazards") {
		t.Fatalf("Rapid Spin did not announce the sweep; log: %v", logTexts(log))
	}
	if h := s.Sides[0].Conditions.Hazards; h.StealthRock || h.Spikes != 0 || h.ToxicSpikes != 0 {
		t.Errorf("Rapid Spin left hazards on the spinner's own side: %+v", h)
	}
	if h := s.Sides[1].Conditions.Hazards; !h.StealthRock {
		t.Errorf("Rapid Spin swept the FOE's side too: %+v", h)
	}

	// Turn 5: a fresh teammate walks onto the swept floor and pays nothing.
	fresh := &s.Sides[0].Team[2]
	log = ResolveTurn(d, s, [2]Action{switchTo(2), moveAt(2)})
	if fresh.HP != fresh.MaxHP {
		t.Errorf("switch-in after Rapid Spin lost %d HP; the floor should be clean",
			fresh.MaxHP-fresh.HP)
	}
	if logHas(log, "Pointed stones dug into") || logHas(log, "hurt by the spikes") {
		t.Errorf("hazard lines fired after Rapid Spin; log: %v", logTexts(log))
	}

	// Turn 6: the foe switches, and its own untouched rocks still bite —
	// the proof that the sweep was one-sided rather than global.
	foeIn := &s.Sides[1].Team[1]
	log = ResolveTurn(d, s, [2]Action{moveAt(0), switchTo(1)})
	if got := foeIn.MaxHP - foeIn.HP; got != foeIn.MaxHP/8 {
		t.Errorf("foe switch-in lost %d HP, want %d from the rocks Rapid Spin must not have touched; log: %v",
			got, foeIn.MaxHP/8, logTexts(log))
	}
}

// TestBattleDefogSweepsBothFloorsAndScreens is the Defog counterpart:
// one move, both sides of the field, hazards and screens alike.
//
// Canon rule (Gen 6+): Defog drops the target's evasion one stage and
// then clears entry hazards AND Reflect / Light Screen / Aurora Veil from
// BOTH sides — including the user's own. That last part is what makes
// Defog a cost as well as a tool, and it is the half most easily lost: an
// implementation that only sweeps the opponent turns Defog into a
// strictly-better Rapid Spin. The screens are checked through
// ExpectedDamage as well as by reading the field, so the test fails if a
// screen is "cleared" in a struct the damage path doesn't read; the
// hazards are checked by switching a Pokémon in on each side afterwards,
// for the same reason.
func TestBattleDefogSweepsBothFloorsAndScreens(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143, 131}, []int{143, 9})
	teachMoves(t, d, &s.Sides[0].Team[0], "stealth-rock", "reflect", "defog", "splash")
	teachMoves(t, d, &s.Sides[0].Team[1], "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "stealth-rock", "light-screen", "splash")
	teachMoves(t, d, &s.Sides[1].Team[1], "splash")

	// Turn 1: rocks on both floors. Turn 2: a screen on each side.
	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(1)})
	if s.Sides[0].Conditions.Reflect == nil || s.Sides[1].Conditions.LightScreen == nil {
		t.Fatalf("setup: screens did not go up (reflect=%v lightscreen=%v)",
			s.Sides[0].Conditions.Reflect, s.Sides[1].Conditions.LightScreen)
	}
	if !s.Sides[0].Conditions.Hazards.StealthRock || !s.Sides[1].Conditions.Hazards.StealthRock {
		t.Fatalf("setup: rocks did not go up on both sides")
	}
	// Aurora Veil needs hail to be cast, which is not the mechanic under
	// test — arrange it on the field directly so Defog's third screen
	// branch is exercised too.
	s.Sides[1].Conditions.AuroraVeil = &ScreenState{TurnsLeft: 5}

	surf := d.Moves["surf"]
	if surf.Category != domain.CatSpecial {
		t.Fatalf("setup: expected a special move to measure Light Screen with, got %q", surf.Category)
	}
	before := ExpectedDamage(d, s.Active(0), s.Active(1), surf, nil, nil, &s.Sides[1].Conditions)

	// Turn 3: Defog.
	log := ResolveTurn(d, s, [2]Action{moveAt(2), moveAt(2)})
	if !logHas(log, "All field effects were swept away!") {
		t.Fatalf("Defog did not announce the field wipe; log: %v", logTexts(log))
	}
	if got := s.Active(1).Stages.Eva; got != -1 {
		t.Errorf("Defog should drop the foe's evasion to -1, got %d", got)
	}
	for side := 0; side < 2; side++ {
		c := s.Sides[side].Conditions
		if c.Reflect != nil || c.LightScreen != nil || c.AuroraVeil != nil {
			t.Errorf("side %d still has screens after Defog: reflect=%v lightscreen=%v auroraveil=%v",
				side, c.Reflect, c.LightScreen, c.AuroraVeil)
		}
	}
	after := ExpectedDamage(d, s.Active(0), s.Active(1), surf, nil, nil, &s.Sides[1].Conditions)
	if after <= before {
		t.Errorf("a special move should hit harder once Defog removed the foe's screens: %d -> %d", before, after)
	}

	// Turn 4: both sides switch. Neither replacement should find a hazard.
	in0, in1 := &s.Sides[0].Team[1], &s.Sides[1].Team[1]
	log = ResolveTurn(d, s, [2]Action{switchTo(1), switchTo(1)})
	if logHas(log, "Pointed stones dug into") {
		t.Errorf("rocks survived Defog; log: %v", logTexts(log))
	}
	if in0.HP != in0.MaxHP {
		t.Errorf("side 0's replacement lost %d HP walking onto a Defogged floor", in0.MaxHP-in0.HP)
	}
	if in1.HP != in1.MaxHP {
		t.Errorf("side 1's replacement lost %d HP walking onto a Defogged floor", in1.MaxHP-in1.HP)
	}
}

// --- slot conditions ------------------------------------------------

// TestBattleWishHealsTheSlotNotTheCaster is the rule that makes Wish
// worth having: the heal belongs to the POSITION, not to the Pokémon that
// cast it.
//
// Canon rule: Wish restores half of the CASTER's max HP at the end of the
// following turn, to whoever occupies the slot at that moment. Cast it,
// pivot out, and the teammate that came in is the one healed — for
// exactly the caster's half-max, not its own. Two things go wrong in
// implementations that only ever get exercised on one Pokémon: the heal
// follows the caster off the field (so pivoting throws it away), and the
// amount is recomputed from whoever is standing there (so a Wish passed
// from a fat Pokémon to a frail one shrinks). Snorlax (235 max, 117 heal)
// passing to Lapras (205 max, 102 heal) separates the two.
func TestBattleWishHealsTheSlotNotTheCaster(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143, 131}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "wish", "splash")
	teachMoves(t, d, &s.Sides[0].Team[1], "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")

	caster := &s.Sides[0].Team[0]
	receiver := &s.Sides[0].Team[1]
	caster.HP = caster.MaxHP - 100
	receiver.HP = 20
	if caster.MaxHP/2 == receiver.MaxHP/2 {
		t.Fatalf("setup: caster and receiver need different half-max HP to tell the two rules apart")
	}

	// Turn 1: cast. Nothing heals this turn — a Wish that resolves
	// immediately is just a worse Recover.
	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHas(log, "made a wish") {
		t.Fatalf("Wish was not cast; log: %v", logTexts(log))
	}
	if caster.HP != caster.MaxHP-100 {
		t.Fatalf("Wish healed on the turn it was cast (HP %d/%d); it must wait a turn",
			caster.HP, caster.MaxHP)
	}

	// Turn 2: the caster pivots out and the Wish lands on the teammate
	// that took its place, at end of turn.
	log = ResolveTurn(d, s, [2]Action{switchTo(1), moveAt(0)})
	if s.Active(0) != receiver {
		t.Fatalf("setup: the switch did not put the receiver in the slot")
	}
	if !logHas(log, "Wish came true") {
		t.Fatalf("Wish did not resolve on the turn after it was cast; log: %v", logTexts(log))
	}
	if got := receiver.HP - 20; got != caster.MaxHP/2 {
		t.Errorf("Wish healed the incoming for %d, want the caster's half-max %d (the receiver's own half-max is %d)",
			got, caster.MaxHP/2, receiver.MaxHP/2)
	}
	if caster.HP != caster.MaxHP-100 {
		t.Errorf("Wish followed the caster to the bench: HP %d/%d", caster.HP, caster.MaxHP)
	}

	// Turn 3: it was a one-shot. A Wish that fires every turn is an
	// infinite heal.
	hp := receiver.HP
	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if receiver.HP != hp {
		t.Errorf("Wish healed a second time (HP %d -> %d); it should have been consumed", hp, receiver.HP)
	}
}

// TestBattleWishRefusesToStackWhilePending pins the overlap rule.
//
// Canon rule: a side may hold only one pending Wish. A second cast while
// one is in the air fails outright ("But it failed!") — it does not
// re-arm the timer, does not queue a second heal, and does not overwrite
// the amount. Without the guard, using Wish on consecutive turns yields a
// heal every turn instead of every other turn. The last turn here is the
// other half of the rule: once the pending Wish has resolved, casting is
// legal again, so the guard has to be "one pending", not "one per
// battle".
func TestBattleWishRefusesToStackWhilePending(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "wish", "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")

	user := s.Active(0)
	user.HP = user.MaxHP - 120
	start := user.HP

	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})

	// Turn 2: the second cast is refused, and the first Wish resolves at
	// end of turn regardless.
	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHasOnSide(log, 0, "But it failed!") {
		t.Errorf("a second Wish while one is pending should fail; log: %v", logTexts(log))
	}
	if got := user.HP - start; got != user.MaxHP/2 {
		t.Fatalf("the pending Wish healed %d, want %d; log: %v", got, user.MaxHP/2, logTexts(log))
	}

	// Turn 3: nothing is pending any more, so Wish is castable again.
	log = ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if logHasOnSide(log, 0, "But it failed!") {
		t.Errorf("Wish should be castable again once the previous one resolved; log: %v", logTexts(log))
	}
	if !logHas(log, "made a wish") {
		t.Errorf("the third-turn Wish was not cast; log: %v", logTexts(log))
	}
}

// TestBattleHealingWishHealsBeforeTheHazardChip walks the full sacrifice: the
// user dies, the battle enters the replace phase, and the Pokémon the player
// sends in is topped up on the way through the rocks.
//
// Canon rule: Healing Wish faints its user, and the next Pokémon to enter that
// slot is restored to full HP with its non-volatile status cleared. The
// ordering against entry hazards is the part worth pinning, and this test used
// to pin it the other way round — heal last, so the arrival landed at full "
// regardless. That reads well and is the wrong game. Showdown runs one SwitchIn
// field event and sorts its handlers by subOrder, where a slot condition is 3
// and a side condition — the entry hazards — is 4; and the [Gen 4] case in
// test/sim/moves/healingwish.js is titled "…after hazards" and asserts the
// arrival *faints*, which is only a meaningful contrast if the modern rule is
// heal-then-chip. So a Pokémon sent in on a Healing Wish under Stealth Rock
// arrives at full minus the rocks, not at full.
//
// The flag must also be spent — a Healing Wish that stays armed heals every
// later switch-in for the rest of the battle.
func TestBattleHealingWishHealsBeforeTheHazardChip(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143, 9}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "healing-wish", "splash")
	teachMoves(t, d, &s.Sides[0].Team[1], "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "stealth-rock", "splash")

	// Turn 1: the foe lays rocks on the side that is about to switch.
	ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})
	if !s.Sides[0].Conditions.Hazards.StealthRock {
		t.Fatalf("setup: rocks did not go up on side 0")
	}

	user := s.Active(0)
	bench := &s.Sides[0].Team[1]
	bench.HP = bench.MaxHP / 2
	bench.Status = StatusPoison

	// Turn 2: the sacrifice.
	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(1)})
	if !logHas(log, "is calling on the spirit of the past") {
		t.Fatalf("Healing Wish did not announce; log: %v", logTexts(log))
	}
	if !user.Fainted {
		t.Fatalf("Healing Wish must faint its user; HP %d/%d", user.HP, user.MaxHP)
	}
	if s.Phase != PhaseReplace || !s.Replace[0] {
		t.Fatalf("the fainted user should force a replacement, phase=%v replace=%v", s.Phase, s.Replace)
	}

	// The replacement walks through the rocks and still arrives whole.
	in := switchTo(1)
	rlog := ResolveReplace(s, [2]*Action{&in, nil})
	if !logHas(rlog, "Pointed stones dug into") {
		t.Errorf("the replacement should still take its hazard chip on entry; log: %v", logTexts(rlog))
	}
	if !logHas(rlog, "healing wish came true") {
		t.Errorf("Healing Wish did not fire for the replacement; log: %v", logTexts(rlog))
	}
	// Healed to full, then the rocks take their cut of the restored total.
	// Blastoise is neutral to Rock, so that is exactly MaxHP/8.
	wantHP := bench.MaxHP - bench.MaxHP/8
	if bench.HP != wantHP {
		t.Errorf("the replacement should be healed to full and then pay the rocks: got %d/%d, want %d",
			bench.HP, bench.MaxHP, wantHP)
	}
	if bench.HP <= bench.MaxHP/2 {
		t.Errorf("the heal should still have happened — it arrived on half HP and is now %d/%d",
			bench.HP, bench.MaxHP)
	}
	if bench.Status != StatusNone {
		t.Errorf("Healing Wish should clear status, got %q", bench.Status)
	}
	if s.Sides[0].SlotConditions.HealingWish {
		t.Errorf("the Healing Wish flag should be spent once it has healed someone")
	}
}

// TestBattleHealingWishWaitsForSomebodyWhoNeedsIt covers the unhappy path: the
// Pokémon sent in to collect the heal has nothing to collect.
//
// Canon gates the whole body of healingwish's onSwap on `!target.fainted &&
// (target.hp < target.maxhp || target.status)` (ps/data/moves.ts), so a
// full-HP, statusless arrival walks past the wish and it stays armed for
// whoever comes in after — and upstream has a case for exactly that,
// "should not be consumed if a switch-in is fully healed already". The failure
// this guards is a heal silently burned on a body that did not need it: the
// player sacrificed a Pokémon and got nothing, with no log line to explain it.
//
// This test used to make the same point with a replacement that *fainted* on
// entry — a 1-HP Butterfree walking into 4× Stealth Rock. That scenario is
// unreachable now, and its unreachability is the fix: with the heal running
// ahead of the hazards (see TestBattleHealingWishHealsBeforeTheHazardChip) the
// Butterfree is at full HP before the rocks land and survives them. The
// fainted-slot guard is still in the code and still correct — it is what stops
// a wish being spent on the corpse of a Pokémon that died to something else —
// but the observable case is the full-HP one.
func TestBattleHealingWishWaitsForSomebodyWhoNeedsIt(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143, 12, 131}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "healing-wish", "splash")
	teachMoves(t, d, &s.Sides[0].Team[1], "splash")
	teachMoves(t, d, &s.Sides[0].Team[2], "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "stealth-rock", "splash")

	ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})

	healthy := &s.Sides[0].Team[1] // Butterfree, arriving whole and clean
	needy := &s.Sides[0].Team[2]
	needy.HP = 60
	needy.Status = StatusBurn

	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(1)})
	if s.Phase != PhaseReplace {
		t.Fatalf("expected a replacement after the sacrifice, phase=%v", s.Phase)
	}

	// The first replacement needs nothing, so it does not spend the wish —
	// even though the rocks are about to hurt it, because canon asks the
	// question before the hazards run.
	first := switchTo(1)
	rlog := ResolveReplace(s, [2]*Action{&first, nil})
	if logHas(rlog, "healing wish came true") {
		t.Errorf("Healing Wish fired for a Pokémon that arrived whole; log: %v", logTexts(rlog))
	}
	if !s.Sides[0].SlotConditions.HealingWish {
		t.Fatalf("the Healing Wish should still be waiting after a full-HP arrival")
	}
	if healthy.HP >= healthy.MaxHP {
		t.Fatalf("setup: the arrival should still have taken its rock chip, got %d/%d",
			healthy.HP, healthy.MaxHP)
	}

	// Bring the needy one in and it collects.
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 2}, moveAt(1)})
	if !s.Sides[0].SlotConditions.HealingWish && needy.Status != StatusNone {
		t.Errorf("the waiting Healing Wish should have cleared the burn, status %q", needy.Status)
	}
	if needy.HP <= 60 {
		t.Errorf("the waiting Healing Wish should have healed the arrival, got %d/%d",
			needy.HP, needy.MaxHP)
	}
	if s.Sides[0].SlotConditions.HealingWish {
		t.Errorf("the flag should be spent once it finally healed someone")
	}
}

// TestBattleHealingWishFailsWithNoOneToReceiveIt pins the suicide guard.
//
// Canon rule: Healing Wish fails if the user has no live teammate to
// switch in, and — crucially — the user does NOT faint. Without the check
// the move is a straight self-KO that hands the opponent the last Pokémon
// of the battle for free, which is the worst possible way for a heal to
// behave.
func TestBattleHealingWishFailsWithNoOneToReceiveIt(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "healing-wish", "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")

	user := s.Active(0)
	user.HP = user.MaxHP - 40
	before := user.HP

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHasOnSide(log, 0, "But it failed!") {
		t.Errorf("Healing Wish with an empty bench should fail; log: %v", logTexts(log))
	}
	if user.Fainted || user.HP != before {
		t.Errorf("a failed Healing Wish must not cost the user anything: HP %d (was %d), fainted=%v",
			user.HP, before, user.Fainted)
	}
	if s.Sides[0].SlotConditions.HealingWish {
		t.Errorf("a failed Healing Wish must not arm the slot")
	}
	if s.Ended() {
		t.Errorf("the battle should still be running; log: %v", logTexts(log))
	}
}

// --- guards ---------------------------------------------------------

// TestBattleQuickAndWideGuardClearAtEndOfTurn pins the lifetime of the
// two side-shield flags.
//
// Canon rule: Quick Guard and Wide Guard protect the user's side for the
// turn they are used and expire at end of turn. Neither has a live effect
// in a singles engine (there are no allies to shield and no spread moves
// to block), which is exactly why the lifetime needs a test: the only
// observable consequence of a flag that never expires is that the move
// stops working, and nothing else in the battle would ever notice. Using
// each move on two consecutive turns is the check — the second use must
// succeed, which it can only do if the first one's flag was ticked away.
func TestBattleQuickAndWideGuardClearAtEndOfTurn(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "quick-guard", "wide-guard", "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")

	cases := []struct {
		name    string
		moveIdx int
		line    string
		live    func() bool
	}{
		{"Quick Guard", 0, "Quick Guard protected P1's team!", func() bool { return s.Sides[0].Conditions.QuickGuard != nil }},
		{"Wide Guard", 1, "Wide Guard protected P1's team!", func() bool { return s.Sides[0].Conditions.WideGuard != nil }},
	}
	for _, c := range cases {
		log := ResolveTurn(d, s, [2]Action{moveAt(c.moveIdx), moveAt(0)})
		if !logHas(log, c.line) {
			t.Fatalf("%s did not go up; log: %v", c.name, logTexts(log))
		}
		if c.live() {
			t.Errorf("%s should expire at the end of the turn it was used", c.name)
		}
		// Same move again next turn: a flag that outlived its turn shows
		// up here as a failure to re-cast.
		log = ResolveTurn(d, s, [2]Action{moveAt(c.moveIdx), moveAt(0)})
		if logHasOnSide(log, 0, "But it failed!") {
			t.Errorf("%s should be usable again the next turn; log: %v", c.name, logTexts(log))
		}
		if !logHas(log, c.line) {
			t.Errorf("%s did not go up on the second turn; log: %v", c.name, logTexts(log))
		}
		if c.live() {
			t.Errorf("%s should expire again at the end of the second turn", c.name)
		}
	}
}

// TestBattleGuardTimersCountDownAndRefuseToRestack separates "the timer
// ticks" from "the flag is wiped every turn", and pins the re-cast guard.
//
// Canon rule: a guard already up cannot be re-applied ("But it failed!"),
// and the flag comes down by counting its timer to zero rather than being
// cleared wholesale at end of turn. A tick that clears unconditionally
// and a tick that counts down look identical on the one-turn duration the
// moves themselves set, so the timers are armed here with two turns to
// make the difference observable: after one turn the guard must still be
// up, after two it must be gone.
func TestBattleGuardTimersCountDownAndRefuseToRestack(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "quick-guard", "wide-guard", "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")

	s.Sides[0].Conditions.QuickGuard = &QuickGuardState{TurnsLeft: 2}
	s.Sides[0].Conditions.WideGuard = &WideGuardState{TurnsLeft: 2}

	// Turn 1: Quick Guard is already up, so casting it fails.
	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHasOnSide(log, 0, "But it failed!") {
		t.Errorf("Quick Guard should fail while one is already up; log: %v", logTexts(log))
	}
	qg, wg := s.Sides[0].Conditions.QuickGuard, s.Sides[0].Conditions.WideGuard
	if qg == nil || wg == nil {
		t.Fatalf("two-turn guards should survive one end-of-turn tick, got quick=%v wide=%v", qg, wg)
	}
	if qg.TurnsLeft != 1 || wg.TurnsLeft != 1 {
		t.Errorf("guard timers should have counted down to 1, got quick=%d wide=%d",
			qg.TurnsLeft, wg.TurnsLeft)
	}

	// Turn 2: Wide Guard is still up on the way in, so it fails too, and
	// both timers reach zero at end of turn.
	log = ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})
	if !logHasOnSide(log, 0, "But it failed!") {
		t.Errorf("Wide Guard should fail while one is already up; log: %v", logTexts(log))
	}
	if s.Sides[0].Conditions.QuickGuard != nil || s.Sides[0].Conditions.WideGuard != nil {
		t.Fatalf("both guards should be gone after their second tick, got quick=%v wide=%v",
			s.Sides[0].Conditions.QuickGuard, s.Sides[0].Conditions.WideGuard)
	}

	// Turn 3: with the field clear, the move works again.
	log = ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if logHasOnSide(log, 0, "But it failed!") {
		t.Errorf("Quick Guard should be castable once the old flag ticked away; log: %v", logTexts(log))
	}
}
