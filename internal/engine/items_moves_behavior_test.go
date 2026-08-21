package engine

import (
	"strings"
	"testing"

	"pokearena/internal/domain"
)

// items_moves_behavior_test.go pins the item-manipulation move family from the
// outside: every case here builds a battle, hands both sides a move, and calls
// ResolveTurn. Nothing reaches into the engine's internals, so each test states
// a rule a player could verify from the battle log alone — which is the whole
// point, because these rules have to survive being re-implemented rather than
// re-typed.
//
// The family is Trick / Switcheroo, Bestow, Corrosive Gas, Recycle, Fling,
// Natural Gift, and the berry eaters (Pluck / Bug Bite / Incinerate). What they
// share is that the *item* is the payload, so every one of them has three ways
// to be wrong: the wrong slot ends up holding it, the wrong side gets its
// effect, or a failure case silently succeeds. Each test below checks the belt,
// the effect and the announcement together for that reason.

// itemMoveDuel builds a Snorlax-versus-Charizard 1v1 with explicit movesets and
// belts on both leads and no abilities, so the item move under test is the only
// live mechanic. Both sides carry a bench slot so a faint cannot end the battle
// mid-test.
//
// The defender is Charizard rather than a second Snorlax so the type chart has
// somewhere to show: Fire/Flying resists Fire and is weak to Water, which is
// how Natural Gift's type can be measured without reading the move struct.
func itemMoveDuel(t *testing.T, seed uint64, atkItem ItemKind, atkMoves []string,
	defItem ItemKind, defMoves []string,
) (*domain.Dex, *BattleState) {
	t.Helper()
	d := loadDex(t)
	for _, id := range append(append([]string{}, atkMoves...), defMoves...) {
		if _, ok := d.Moves[id]; !ok {
			t.Skipf("%s not in the curated move set", id)
		}
	}
	s, err := NewBattle(d, "b", "A", []int{143, 6}, "B", []int{6, 143}, seed)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	s.Active(0).Item, s.Active(1).Item = atkItem, defItem
	s.Active(0).Moves = itemMoveSlots(atkMoves)
	s.Active(1).Moves = itemMoveSlots(defMoves)
	return d, s
}

func itemMoveSlots(ids []string) []MoveSlot {
	out := make([]MoveSlot, 0, len(ids))
	for _, id := range ids {
		out = append(out, MoveSlot{MoveID: id, PP: 16, MaxPP: 16})
	}
	return out
}

// itemMoveTurn plays one whole turn with an explicit choice for each side.
func itemMoveTurn(d *domain.Dex, s *BattleState, atkIdx, defIdx int) []LogLine {
	return playTurn(d, s, atkIdx, defIdx)
}

func countLogLines(log []LogLine, substr string) int {
	n := 0
	for _, l := range log {
		if strings.Contains(l.Text, substr) {
			n++
		}
	}
	return n
}

// TestBattleTrickSwapsBothBeltsAndNarratesEachSide pins Trick's two shapes and
// its announcement. Canon prints one "switched items" line and then one
// "obtained" line per side that actually ended up holding something — an empty
// slot says nothing. The narration is worth pinning because it is the only
// thing that tells the opponent what it was just handed, and a one-sided swap
// that printed "obtained one " for the empty half would be a visible bug.
func TestBattleTrickSwapsBothBeltsAndNarratesEachSide(t *testing.T) {
	t.Run("two-way", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemChoiceScarf, []string{"trick"}, ItemLeftovers, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(0).Item != ItemLeftovers || s.Active(1).Item != ItemChoiceScarf {
			t.Fatalf("Trick did not exchange the belts: user=%q target=%q; log: %v",
				s.Active(0).Item, s.Active(1).Item, logTexts(log))
		}
		if !logHas(log, "switched items with") {
			t.Errorf("no swap line in the log: %v", logTexts(log))
		}
		if !logHas(log, "obtained one Leftovers") || !logHas(log, "obtained one Choice Scarf") {
			t.Errorf("a two-way swap must name what each side ended up with; log: %v", logTexts(log))
		}
	})

	t.Run("one-sided-says-nothing-for-the-empty-half", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemChoiceScarf, []string{"trick"}, ItemNone, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(0).Item != ItemNone || s.Active(1).Item != ItemChoiceScarf {
			t.Fatalf("Trick did not hand the scarf over: user=%q target=%q; log: %v",
				s.Active(0).Item, s.Active(1).Item, logTexts(log))
		}
		if got := countLogLines(log, "obtained one"); got != 1 {
			t.Errorf("a one-sided swap printed %d \"obtained\" lines, want 1 — the side left "+
				"empty-handed has nothing to announce; log: %v", got, logTexts(log))
		}
		if !logHas(log, "obtained one Choice Scarf") {
			t.Errorf("the receiving side was not told what it got; log: %v", logTexts(log))
		}
	})
}

// TestBattleSwitcherooIsTrickUnderAnotherName: the two moves are the same
// effect with different names and are dispatched from the same place, so the
// guard that matters is that they cannot drift apart.
func TestBattleSwitcherooIsTrickUnderAnotherName(t *testing.T) {
	belts := func(move string) (ItemKind, ItemKind, []LogLine) {
		d, s := itemMoveDuel(t, 1, ItemChoiceScarf, []string{move}, ItemLeftovers, []string{"splash"})
		log := itemMoveTurn(d, s, 0, 0)
		return s.Active(0).Item, s.Active(1).Item, log
	}
	tu, tt, _ := belts("trick")
	su, st, slog := belts("switcheroo")
	if su != tu || st != tt {
		t.Errorf("Switcheroo left user=%q target=%q but Trick left user=%q target=%q; "+
			"they are the same move; log: %v", su, st, tu, tt, logTexts(slog))
	}
	if su != ItemLeftovers || st != ItemChoiceScarf {
		t.Errorf("neither move swapped at all: user=%q target=%q", su, st)
	}
}

// TestBattleTrickFailsOnlyWhenNeitherSideHoldsAnything pins the exact failure
// gate. Canon fails Trick when there is nothing to trade — both belts empty —
// and *not* when only one side is holding, because handing a Choice Scarf to a
// wall is the move's whole competitive purpose. A gate that read "either slot
// empty" would delete that use entirely while still passing a both-empty test.
func TestBattleTrickFailsOnlyWhenNeitherSideHoldsAnything(t *testing.T) {
	t.Run("both-empty-fails", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemNone, []string{"trick"}, ItemNone, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if !logHas(log, "But it failed!") {
			t.Errorf("Trick with two empty belts should announce a failure; log: %v", logTexts(log))
		}
		if s.Active(0).Item != ItemNone || s.Active(1).Item != ItemNone {
			t.Errorf("a failed Trick conjured an item: user=%q target=%q",
				s.Active(0).Item, s.Active(1).Item)
		}
	})
	t.Run("one-side-holding-succeeds", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemNone, []string{"trick"}, ItemLeftovers, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if logHas(log, "But it failed!") {
			t.Errorf("Trick failed while the target was holding Leftovers; log: %v", logTexts(log))
		}
		if s.Active(0).Item != ItemLeftovers || s.Active(1).Item != ItemNone {
			t.Errorf("Trick did not take the target's item: user=%q target=%q",
				s.Active(0).Item, s.Active(1).Item)
		}
	})
}

// TestBattleTrickIsStoppedByASubstitute: Trick carries neither the sound flag
// nor a sub bypass, so the doll is what it reaches. Item moves that ignored
// Substitute would make the standard "sub up, then swap the scarf back" line of
// play impossible to defend against.
func TestBattleTrickIsStoppedByASubstitute(t *testing.T) {
	d, s := itemMoveDuel(t, 1, ItemChoiceScarf, []string{"trick"}, ItemLeftovers, []string{"splash"})
	s.Active(1).Volatiles.Substitute = &SubstituteState{HP: 40}

	log := itemMoveTurn(d, s, 0, 0)

	if s.Active(0).Item != ItemChoiceScarf || s.Active(1).Item != ItemLeftovers {
		t.Errorf("Trick swapped through a Substitute: user=%q target=%q; log: %v",
			s.Active(0).Item, s.Active(1).Item, logTexts(log))
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("a Trick blocked by a Substitute should announce its failure; log: %v", logTexts(log))
	}
}

// TestBattleBestowHandsTheItemOverOneWayOnly pins Bestow as a gift rather than
// a trade: the user's item moves to the target and the user is left with
// nothing back. Both failure gates matter — an empty-handed user has nothing to
// give, and a target with full hands cannot receive — because overwriting the
// target's item would be a way to delete a held item that canon does not have.
func TestBattleBestowHandsTheItemOverOneWayOnly(t *testing.T) {
	t.Run("gift-lands", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemLeftovers, []string{"bestow"}, ItemNone, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(1).Item != ItemLeftovers {
			t.Errorf("Bestow did not deliver the item (target holds %q); log: %v",
				s.Active(1).Item, logTexts(log))
		}
		if s.Active(0).Item != ItemNone {
			t.Errorf("Bestow duplicated the item — the giver still holds %q", s.Active(0).Item)
		}
		if !logHas(log, "received Leftovers from") {
			t.Errorf("the handover was not announced; log: %v", logTexts(log))
		}
	})
	t.Run("target-with-full-hands-refuses", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemLeftovers, []string{"bestow"}, ItemChoiceBand, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(1).Item != ItemChoiceBand || s.Active(0).Item != ItemLeftovers {
			t.Errorf("Bestow overwrote a held item: user=%q target=%q; log: %v",
				s.Active(0).Item, s.Active(1).Item, logTexts(log))
		}
		if !logHas(log, "But it failed!") {
			t.Errorf("Bestow into full hands should fail out loud; log: %v", logTexts(log))
		}
	})
	t.Run("empty-handed-user-fails", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemNone, []string{"bestow"}, ItemNone, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(1).Item != ItemNone {
			t.Errorf("Bestow gave away an item the user did not have (target holds %q)", s.Active(1).Item)
		}
		if !logHas(log, "But it failed!") {
			t.Errorf("Bestow with nothing to give should fail; log: %v", logTexts(log))
		}
	})
}

// TestBattleBestowIgnoresStickyHold is the one member of the family Sticky Hold
// does not touch. The ability refuses to let go of what the holder is carrying;
// Bestow takes nothing away, it puts something in an empty hand. Filing Bestow
// under the same "is this item removable" gate as Knock Off and Trick would be
// the natural mistake, and it would silently break a legal play.
func TestBattleBestowIgnoresStickyHold(t *testing.T) {
	d, s := itemMoveDuel(t, 1, ItemLeftovers, []string{"bestow"}, ItemNone, []string{"splash"})
	s.Active(1).Ability = AbilityKind("sticky-hold")

	log := itemMoveTurn(d, s, 0, 0)

	if s.Active(1).Item != ItemLeftovers {
		t.Errorf("Sticky Hold refused a Bestow (target holds %q) — nothing was being taken "+
			"from it; log: %v", s.Active(1).Item, logTexts(log))
	}
	if s.Active(0).Item != ItemNone {
		t.Errorf("the giver kept its item as well (%q)", s.Active(0).Item)
	}
}

// TestBattleCorrosiveGasMeltsTheItemAndNobodyGetsItBack pins the destruction.
// Corrosive Gas is not theft and not consumption: the attacker must not end up
// holding the item, and the victim must not be able to Recycle it, because a
// destroyed item that counted as "consumed" would be laundered straight back
// onto the belt it was just melted off.
func TestBattleCorrosiveGasMeltsTheItemAndNobodyGetsItBack(t *testing.T) {
	d, s := itemMoveDuel(t, 1, ItemNone, []string{"corrosive-gas", "splash"},
		ItemLeftovers, []string{"splash", "recycle"})

	log := itemMoveTurn(d, s, 0, 0)

	if s.Active(1).Item != ItemNone {
		t.Fatalf("Corrosive Gas did not destroy the item (target holds %q); log: %v",
			s.Active(1).Item, logTexts(log))
	}
	if s.Active(0).Item != ItemNone {
		t.Errorf("Corrosive Gas handed the item to its user (%q) — it destroys, it does not steal",
			s.Active(0).Item)
	}
	if !logHas(log, "was corroded away") {
		t.Errorf("the destruction was not announced; log: %v", logTexts(log))
	}

	// Second turn: the victim tries to Recycle what was melted off it.
	log2 := itemMoveTurn(d, s, 1, 1)
	if s.Active(1).Item != ItemNone {
		t.Errorf("Recycle handed back an item Corrosive Gas destroyed (holds %q); a melted "+
			"item was never consumed by its holder; log: %v", s.Active(1).Item, logTexts(log2))
	}
	if !logHas(log2, "But it failed!") {
		t.Errorf("Recycle with nothing consumed should fail; log: %v", logTexts(log2))
	}
}

// TestBattleCorrosiveGasFailsAndIsRefused covers the two paths that leave the
// belt alone: an empty-handed target (nothing to melt, so the move announces
// its own failure) and Sticky Hold (the ability speaks up, which is
// information the attacker just paid a turn for).
func TestBattleCorrosiveGasFailsAndIsRefused(t *testing.T) {
	t.Run("empty-handed-target", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemNone, []string{"corrosive-gas"}, ItemNone, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if !logHas(log, "But it failed!") {
			t.Errorf("Corrosive Gas against an empty belt should fail; log: %v", logTexts(log))
		}
	})
	t.Run("sticky-hold-refuses", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemNone, []string{"corrosive-gas"}, ItemLeftovers, []string{"splash"})
		s.Active(1).Ability = AbilityKind("sticky-hold")

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(1).Item != ItemLeftovers {
			t.Errorf("Corrosive Gas melted a Sticky Hold holder's item; log: %v", logTexts(log))
		}
		if !logHas(log, "kept hold of its item") {
			t.Errorf("the refusal was not announced — the attacker acted on that "+
				"information; log: %v", logTexts(log))
		}
	})
}

// TestBattleRecycleReturnsTheBerryTheUserAteOnce pins the memory Recycle reads
// and the fact that it is spent on use. Canon restores the item the user itself
// consumed, once: the Recycle + Sitrus stall is supposed to cost a Recycle per
// berry, and a memory that survived the restore would make one berry an
// infinite heal.
func TestBattleRecycleReturnsTheBerryTheUserAteOnce(t *testing.T) {
	d, s := itemMoveDuel(t, 1, ItemSitrusBerry, []string{"splash", "recycle"}, ItemNone, []string{"splash"})
	p := s.Active(0)
	p.HP = p.MaxHP / 4 // under the Sitrus threshold, so turn one eats it

	itemMoveTurn(d, s, 0, 0)
	if p.Item != ItemNone {
		t.Fatalf("setup: the berry was not eaten (still holds %q)", p.Item)
	}

	// Full HP first: a restored berry still in pinch range would be eaten again
	// on the spot — canon, but it would hide whether the restore worked at all.
	p.HP = p.MaxHP
	log := itemMoveTurn(d, s, 1, 0)
	if p.Item != ItemSitrusBerry {
		t.Fatalf("Recycle did not return the eaten berry (holds %q); log: %v", p.Item, logTexts(log))
	}
	if !logHas(log, "found one Sitrus Berry") {
		t.Errorf("the restore was not announced; log: %v", logTexts(log))
	}

	// Empty the slot by hand — nothing new was consumed — and ask again.
	p.Item = ItemNone
	log2 := itemMoveTurn(d, s, 1, 0)
	if p.Item != ItemNone {
		t.Errorf("Recycle produced a second copy of a berry eaten once (holds %q); the "+
			"memory is spent on use; log: %v", p.Item, logTexts(log2))
	}
	if !logHas(log2, "But it failed!") {
		t.Errorf("a second Recycle off one berry should fail; log: %v", logTexts(log2))
	}
}

// TestBattleRecycleFailsWhileTheUserIsHoldingSomething: canon needs an empty
// hand. Without the gate a holder could overwrite its live item with a copy of
// whatever it ate earlier, which duplicates items out of nothing.
func TestBattleRecycleFailsWhileTheUserIsHoldingSomething(t *testing.T) {
	d, s := itemMoveDuel(t, 1, ItemSitrusBerry, []string{"splash", "recycle"}, ItemNone, []string{"splash"})
	p := s.Active(0)
	p.HP = p.MaxHP / 4
	itemMoveTurn(d, s, 0, 0) // eats the Sitrus
	if p.LastConsumedItem != ItemSitrusBerry {
		t.Fatalf("setup: the berry was not recorded as consumed (%q)", p.LastConsumedItem)
	}
	p.HP, p.Item = p.MaxHP, ItemLeftovers

	log := itemMoveTurn(d, s, 1, 0)

	if p.Item != ItemLeftovers {
		t.Errorf("Recycle overwrote the item the user was already holding (now %q); log: %v",
			p.Item, logTexts(log))
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("Recycle with a full hand should fail; log: %v", logTexts(log))
	}
}

// TestBattleFlingBasePowerComesFromTheThrownItem: Fling has no power of its
// own — the item supplies it, from a fixed table, and an empty hand means the
// move fails outright rather than hitting for nothing. An Iron Ball (130) and
// Leftovers (10) are the two ends of that table, so the ordering has to hold on
// every seed; a Fling that ignored the table would deal the same damage twice.
func TestBattleFlingBasePowerComesFromTheThrownItem(t *testing.T) {
	dmg := func(seed uint64, item ItemKind) int {
		d, s := itemMoveDuel(t, seed, item, []string{"fling"}, ItemNone, []string{"splash"})
		before := s.Active(1).HP
		itemMoveTurn(d, s, 0, 0)
		return before - s.Active(1).HP
	}
	for seed := uint64(1); seed <= 6; seed++ {
		heavy, light := dmg(seed, ItemIronBall), dmg(seed, ItemLeftovers)
		if light <= 0 {
			t.Fatalf("seed %d: Fling dealt no damage at all with Leftovers", seed)
		}
		if heavy <= light {
			t.Errorf("seed %d: Fling dealt %d with an Iron Ball and %d with Leftovers; the "+
				"base power is the item's (130 vs 10)", seed, heavy, light)
		}
	}

	t.Run("nothing-to-throw", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemNone, []string{"fling"}, ItemNone, []string{"splash"})
		before := s.Active(1).HP

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(1).HP != before {
			t.Errorf("Fling with an empty hand dealt %d damage; log: %v",
				before-s.Active(1).HP, logTexts(log))
		}
		if !logHas(log, "But it failed!") {
			t.Errorf("Fling with nothing to throw should fail; log: %v", logTexts(log))
		}
	})
}

// TestBattleFlingFeedsTheThrownBerryToTheTargetOffThreshold is the trap that
// makes Fling a bad idea with a berry: the target eats it, and it activates
// even though the target's own trigger condition is not met. A thrown Sitrus
// heals a target that is nowhere near half HP, and a thrown Liechi genuinely
// hands the opponent +1 Attack. Gating the delivery on the berry's usual
// threshold would look like a fix and would quietly delete a real rule.
func TestBattleFlingFeedsTheThrownBerryToTheTargetOffThreshold(t *testing.T) {
	t.Run("heal-berry-off-threshold", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemSitrusBerry, []string{"fling"}, ItemNone, []string{"splash"})
		def := s.Active(1)
		def.HP = def.MaxHP * 7 / 10 // well above Sitrus's own half-HP trigger
		before := def.HP

		log := itemMoveTurn(d, s, 0, 0)

		if !logHas(log, "ate the thrown Sitrus Berry") {
			t.Fatalf("the target was never fed the thrown berry; log: %v", logTexts(log))
		}
		if def.HP <= before {
			t.Errorf("the target went %d -> %d after being fed a Sitrus Berry; a thrown berry "+
				"activates regardless of the holder's usual threshold", before, def.HP)
		}
		if def.Item != ItemNone {
			t.Errorf("the thrown berry ended up in the target's item slot (%q) — it is eaten, "+
				"not handed over", def.Item)
		}
	})

	t.Run("stat-berry-boosts-the-victim", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemLiechiBerry, []string{"fling"}, ItemNone, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(1).Stages.Atk != 1 {
			t.Errorf("a thrown Liechi Berry left the target at Atk stage %d, want +1; the "+
				"target eats what you throw at it, boost and all; log: %v",
				s.Active(1).Stages.Atk, logTexts(log))
		}
		if s.Active(0).Stages.Atk != 0 {
			t.Errorf("the thrower boosted itself off the berry it threw (Atk stage %d)",
				s.Active(0).Stages.Atk)
		}
	})

	t.Run("status-berry-cures-the-victim", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemCheriBerry, []string{"fling"}, ItemNone, []string{"splash"})
		s.Active(1).Status = StatusParalysis

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(1).Status != StatusNone {
			t.Errorf("a thrown Cheri Berry left the target paralyzed (%q); the victim eats the "+
				"berry and gets its cure; log: %v", s.Active(1).Status, logTexts(log))
		}
	})

	t.Run("non-berry-feeds-nobody", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemIronBall, []string{"fling"}, ItemNone, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if logHas(log, "ate the thrown") {
			t.Errorf("the target ate a thrown Iron Ball; only berries are eaten; log: %v",
				logTexts(log))
		}
	})
}

// TestBattleFlingSpendsTheThrownItemAsConsumed: the item leaves the belt when
// the move is used, and it leaves as *consumed* rather than destroyed — canon
// lets Recycle hand a flung item back, which is the whole Fling + Recycle line.
func TestBattleFlingSpendsTheThrownItemAsConsumed(t *testing.T) {
	d, s := itemMoveDuel(t, 1, ItemIronBall, []string{"fling", "recycle"}, ItemNone, []string{"splash"})

	log := itemMoveTurn(d, s, 0, 0)

	if s.Active(0).Item != ItemNone {
		t.Fatalf("Fling did not spend the thrown item (still holds %q); log: %v",
			s.Active(0).Item, logTexts(log))
	}
	if !logHas(log, "used up its Iron Ball") {
		t.Errorf("spending the thrown item was not announced; log: %v", logTexts(log))
	}

	log2 := itemMoveTurn(d, s, 1, 0)
	if s.Active(0).Item != ItemIronBall {
		t.Errorf("Recycle could not return a flung item (holds %q); Fling consumes, it does "+
			"not destroy; log: %v", s.Active(0).Item, logTexts(log2))
	}
}

// TestBattleNaturalGiftTakesTypeAndPowerFromTheHeldBerry: the berry decides
// what the move *is*. Passho and Occa have the same Natural Gift power, so the
// only thing separating them into a Fire/Flying target is the type — Water is
// double, Fire is halved. Losing the type lookup would leave a move that still
// deals damage and still spends the berry, which no damage-only test catches.
func TestBattleNaturalGiftTakesTypeAndPowerFromTheHeldBerry(t *testing.T) {
	dmg := func(seed uint64, berry ItemKind) (int, []LogLine) {
		d, s := itemMoveDuel(t, seed, berry, []string{"natural-gift"}, ItemNone, []string{"splash"})
		before := s.Active(1).HP
		log := itemMoveTurn(d, s, 0, 0)
		return before - s.Active(1).HP, log
	}
	for seed := uint64(1); seed <= 6; seed++ {
		water, _ := dmg(seed, ItemPasshoBerry)
		fire, _ := dmg(seed, ItemOccaBerry)
		if fire <= 0 {
			t.Fatalf("seed %d: Natural Gift dealt no damage at all", seed)
		}
		if water <= fire {
			t.Errorf("seed %d: Natural Gift dealt %d off a Passho Berry and %d off an Occa "+
				"Berry into a Fire/Flying target; the berry sets the move's type (Water is "+
				"super effective, Fire is resisted)", seed, water, fire)
		}
	}

	t.Run("berry-is-spent-but-nobody-eats-it", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemPasshoBerry, []string{"natural-gift"}, ItemNone, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(0).Item != ItemNone {
			t.Errorf("Natural Gift did not spend the berry (still holds %q)", s.Active(0).Item)
		}
		if logHas(log, "ate the thrown") {
			t.Errorf("Natural Gift fed the berry to the target; it converts the berry to "+
				"energy rather than throwing it; log: %v", logTexts(log))
		}
		if s.Active(1).Item != ItemNone {
			t.Errorf("the target ended up holding the berry (%q)", s.Active(1).Item)
		}
	})

	t.Run("non-berry-fails-and-keeps-the-item", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemLeftovers, []string{"natural-gift"}, ItemNone, []string{"splash"})
		before := s.Active(1).HP

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(1).HP != before {
			t.Errorf("Natural Gift worked off a non-berry, dealing %d; log: %v",
				before-s.Active(1).HP, logTexts(log))
		}
		if s.Active(0).Item != ItemLeftovers {
			t.Errorf("a failed Natural Gift still spent the item (holds %q)", s.Active(0).Item)
		}
	})
}

// TestBattlePluckEatsTheBerryAndTheEaterGetsTheEffect: Pluck and Bug Bite take
// the target's berry and use it *as the attacker*, unconditionally — the eater
// never held the thing, so the berry's own trigger condition never applied to
// it. A healthy Snorlax eating a Rawst Berry off the foe is cured of its burn
// even though a Rawst on its own belt would have fired the moment it was
// burned. The eaten berry must also not land in the attacker's item slot.
func TestBattlePluckEatsTheBerryAndTheEaterGetsTheEffect(t *testing.T) {
	for _, move := range []string{"pluck", "bug-bite"} {
		t.Run(move, func(t *testing.T) {
			d, s := itemMoveDuel(t, 1, ItemNone, []string{move}, ItemRawstBerry, []string{"splash"})
			atk := s.Active(0)
			atk.Status = StatusBurn

			log := itemMoveTurn(d, s, 0, 0)

			if s.Active(1).Item != ItemNone {
				t.Fatalf("%s left the berry on the target (%q); log: %v",
					move, s.Active(1).Item, logTexts(log))
			}
			if atk.Status != StatusNone {
				t.Errorf("%s ate a Rawst Berry and the eater is still burned (%q); the eater "+
					"gets the berry's effect; log: %v", move, atk.Status, logTexts(log))
			}
			if atk.Item != ItemNone {
				t.Errorf("%s put the eaten berry in the attacker's slot (%q) — it is eaten, "+
					"not stolen", move, atk.Item)
			}
			if !logHas(log, "stole and ate") {
				t.Errorf("%s did not announce eating the berry; log: %v", move, logTexts(log))
			}
		})
	}

	t.Run("a-non-berry-is-left-alone", func(t *testing.T) {
		d, s := itemMoveDuel(t, 1, ItemNone, []string{"pluck"}, ItemLeftovers, []string{"splash"})

		log := itemMoveTurn(d, s, 0, 0)

		if s.Active(1).Item != ItemLeftovers {
			t.Errorf("Pluck ate a Leftovers (target now holds %q); the move only takes "+
				"berries; log: %v", s.Active(1).Item, logTexts(log))
		}
	})
}

// TestBattlePluckedBerryIsGoneForGood: the victim did not consume its own
// berry, somebody else ate it, so there is nothing for it to Recycle. Filing
// the loss as a consumption would let the victim conjure the berry straight
// back and eat it for real.
func TestBattlePluckedBerryIsGoneForGood(t *testing.T) {
	d, s := itemMoveDuel(t, 1, ItemNone, []string{"pluck", "splash"},
		ItemSitrusBerry, []string{"splash", "recycle"})

	itemMoveTurn(d, s, 0, 0)
	if s.Active(1).Item != ItemNone {
		t.Fatalf("setup: Pluck did not take the berry")
	}

	log := itemMoveTurn(d, s, 1, 1)

	if s.Active(1).Item != ItemNone {
		t.Errorf("Recycle returned a berry that was plucked off its holder (holds %q); log: %v",
			s.Active(1).Item, logTexts(log))
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("Recycle after a Pluck should fail; log: %v", logTexts(log))
	}
}

// TestBattleIncinerateBurnsTheBerryAndFeedsNobody: Incinerate destroys where
// Pluck eats. Same removal, but the attacker must gain nothing — a shared code
// path that fell through to the eating branch would quietly turn Incinerate
// into a healing move against every heal-berry holder.
func TestBattleIncinerateBurnsTheBerryAndFeedsNobody(t *testing.T) {
	d, s := itemMoveDuel(t, 1, ItemNone, []string{"incinerate"}, ItemSitrusBerry, []string{"splash"})
	atk := s.Active(0)
	atk.HP = atk.MaxHP / 2
	before := atk.HP

	log := itemMoveTurn(d, s, 0, 0)

	if s.Active(1).Item != ItemNone {
		t.Fatalf("Incinerate did not destroy the berry (target holds %q); log: %v",
			s.Active(1).Item, logTexts(log))
	}
	if !logHas(log, "was burnt up") {
		t.Errorf("the berry was removed without the burn-up line; log: %v", logTexts(log))
	}
	if logHas(log, "stole and ate") {
		t.Errorf("Incinerate ate the berry; log: %v", logTexts(log))
	}
	if atk.HP != before {
		t.Errorf("Incinerate's user went %d -> %d off the target's Sitrus Berry; it burns the "+
			"berry, it does not eat it", before, atk.HP)
	}

	t.Run("a-non-berry-is-left-alone", func(t *testing.T) {
		d2, s2 := itemMoveDuel(t, 1, ItemNone, []string{"incinerate"}, ItemLeftovers, []string{"splash"})

		log2 := itemMoveTurn(d2, s2, 0, 0)

		if s2.Active(1).Item != ItemLeftovers {
			t.Errorf("Incinerate burnt a Leftovers (target now holds %q); only berries burn; "+
				"log: %v", s2.Active(1).Item, logTexts(log2))
		}
	})
}

// TestBattleBerryEatingMovesRespectStickyHold: the berry has to leave the belt
// to be eaten or burnt, and Sticky Hold is exactly the ability that says it
// does not leave. Incinerate is included on purpose — "it is destroyed, not
// taken" is the tempting reading, and it is wrong.
func TestBattleBerryEatingMovesRespectStickyHold(t *testing.T) {
	for _, move := range []string{"pluck", "bug-bite", "incinerate"} {
		t.Run(move, func(t *testing.T) {
			d, s := itemMoveDuel(t, 1, ItemNone, []string{move}, ItemSitrusBerry, []string{"splash"})
			s.Active(1).Ability = AbilityKind("sticky-hold")
			atk := s.Active(0)
			atk.HP = atk.MaxHP / 2
			before := atk.HP

			log := itemMoveTurn(d, s, 0, 0)

			if s.Active(1).Item != ItemSitrusBerry {
				t.Errorf("%s took a berry off a Sticky Hold holder (now %q); log: %v",
					move, s.Active(1).Item, logTexts(log))
			}
			if atk.HP != before {
				t.Errorf("%s healed its user off a berry Sticky Hold kept (%d -> %d)",
					move, before, atk.HP)
			}
			if !logHas(log, "kept hold of its item") {
				t.Errorf("%s did not announce the refusal; log: %v", move, logTexts(log))
			}
		})
	}
}

// TestBattleItemStatusMovesDoNotSwallowOrdinaryOnes: the item family is
// dispatched from inside the status-move path, so a dispatcher that claimed
// every status move rather than only its own would silently blank out Growl,
// Thunder Wave and every other non-item status move in the game. This is the
// cheapest possible guard against that, and it pins the boundary of the family.
func TestBattleItemStatusMovesDoNotSwallowOrdinaryOnes(t *testing.T) {
	d, s := itemMoveDuel(t, 1, ItemNone, []string{"growl"}, ItemNone, []string{"splash"})

	log := itemMoveTurn(d, s, 0, 0)

	if s.Active(1).Stages.Atk != -1 {
		t.Errorf("Growl left the target at Atk stage %d, want -1 — a status move outside the "+
			"item family must still resolve normally; log: %v", s.Active(1).Stages.Atk,
			logTexts(log))
	}
	if logHas(log, "But it failed!") {
		t.Errorf("Growl was reported as a failure; log: %v", logTexts(log))
	}
}
