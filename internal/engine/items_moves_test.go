package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// items_moves_test.go covers the item-manipulation move family. Every case here
// is a move that shipped inert — the curated learnsets taught it and it resolved
// as plain damage or as nothing — so these are first-implementation tests rather
// than regression pins.

// moveBattle sets up a two-Pokémon fixture with explicit moves and items on both
// leads and no abilities, so the move under test is the only live mechanic.
func moveBattle(t *testing.T, atkMove string, atkItem ItemKind, defMove string, defItem ItemKind) (*domain.Dex, *BattleState) {
	t.Helper()
	d := loadDex(t)
	if _, ok := d.Moves[atkMove]; !ok {
		t.Skipf("%s not in the curated move set", atkMove)
	}
	// The defender leads with Charizard, not a second Snorlax: Normal is immune
	// to Ghost, which would make Poltergeist deal zero for a reason that has
	// nothing to do with items.
	s, err := NewBattle(d, "b", "A", []int{143, 6}, "B", []int{6, 143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	s.Active(0).Item, s.Active(1).Item = atkItem, defItem
	s.Active(0).Moves = []MoveSlot{{MoveID: atkMove, PP: 20, MaxPP: 20}}
	s.Active(1).Moves = []MoveSlot{{MoveID: defMove, PP: 40, MaxPP: 40}}
	return d, s
}

// TestKnockOffRemovesAndBoosts covers both halves at once: the ×1.5 base power
// against a target holding something, and the removal after the hit.
func TestKnockOffRemovesAndBoosts(t *testing.T) {
	dmgAgainst := func(defItem ItemKind) (int, *BattleState, []LogLine) {
		d, s := moveBattle(t, "knock-off", ItemNone, "splash", defItem)
		before := s.Active(1).HP
		log := splashTurn(d, s)
		return before - s.Active(1).HP, s, log
	}

	bare, _, _ := dmgAgainst(ItemNone)
	held, s, log := dmgAgainst(ItemLeftovers)
	if bare <= 0 {
		t.Fatalf("setup: Knock Off dealt no damage")
	}
	if held <= bare {
		t.Errorf("Knock Off dealt %d against a Leftovers holder vs %d against an empty-handed "+
			"one; canon adds 50%% base power when there is something to knock off", held, bare)
	}
	if s.Active(1).Item != ItemNone {
		t.Errorf("Knock Off did not remove the item (still %q); log: %v", s.Active(1).Item, log)
	}
	if !logHas(log, "knocked off") {
		t.Errorf("no knock-off line in the log: %v", log)
	}
	// Destroyed, not consumed: Recycle must not be able to hand it back.
	if s.Active(1).LastConsumedItem != ItemNone {
		t.Errorf("a knocked-off item was recorded as consumed (%q) — Recycle would launder it back",
			s.Active(1).LastConsumedItem)
	}
}

// TestKnockOffDoesNotStealThroughSubstitute: the doll took the hit, so nothing
// reached the target's belt.
func TestKnockOffDoesNotStealThroughSubstitute(t *testing.T) {
	d, s := moveBattle(t, "knock-off", ItemNone, "splash", ItemLeftovers)
	sub := 25
	s.Active(1).Volatiles.Substitute = &SubstituteState{HP: sub}

	log := splashTurn(d, s)

	if s.Active(1).Item != ItemLeftovers {
		t.Errorf("Knock Off removed an item through a Substitute; log: %v", log)
	}
}

// TestStickyHoldRefusesEveryRemoval walks the whole family against the one
// ability that says no. itemIsRemovable is the single predicate they share, so
// this is the test that keeps them from drifting apart.
func TestStickyHoldRefusesEveryRemoval(t *testing.T) {
	for _, move := range []string{"knock-off", "thief", "covet", "trick", "corrosive-gas"} {
		t.Run(move, func(t *testing.T) {
			d, s := moveBattle(t, move, ItemNone, "splash", ItemLeftovers)
			s.Active(1).Ability = AbilityKind("sticky-hold")

			log := splashTurn(d, s)

			if s.Active(1).Item != ItemLeftovers {
				t.Errorf("%s took an item off a Sticky Hold holder; log: %v", move, log)
			}
			if s.Active(0).Item != ItemNone {
				t.Errorf("%s gave the attacker %q off a Sticky Hold holder", move, s.Active(0).Item)
			}
		})
	}
}

// TestKnockOffStillBoostsAgainstStickyHold is the documented split: the ability
// keeps the item, the attacker still gets the bigger hit.
func TestKnockOffStillBoostsAgainstStickyHold(t *testing.T) {
	dmg := func(ability AbilityKind, item ItemKind) int {
		d, s := moveBattle(t, "knock-off", ItemNone, "splash", item)
		s.Active(1).Ability = ability
		before := s.Active(1).HP
		splashTurn(d, s)
		return before - s.Active(1).HP
	}
	bare := dmg(AbilityNone, ItemNone)
	sticky := dmg(AbilityKind("sticky-hold"), ItemLeftovers)
	if sticky <= bare {
		t.Errorf("Knock Off dealt %d into Sticky Hold vs %d into an empty slot; the boost keys "+
			"on the target holding something, not on the removal succeeding", sticky, bare)
	}
}

// TestThiefAndCovetSteal: both take the target's item, and only when the
// attacker's own hands are empty.
func TestThiefAndCovetSteal(t *testing.T) {
	for _, move := range []string{"thief", "covet"} {
		t.Run(move, func(t *testing.T) {
			d, s := moveBattle(t, move, ItemNone, "splash", ItemLeftovers)
			log := splashTurn(d, s)
			if s.Active(0).Item != ItemLeftovers {
				t.Errorf("%s did not take the item (attacker holds %q); log: %v", move, s.Active(0).Item, log)
			}
			if s.Active(1).Item != ItemNone {
				t.Errorf("%s left the item on the target as well — it was duplicated", move)
			}

			// Hands full: the move still hits, the item stays put.
			d2, s2 := moveBattle(t, move, ItemChoiceBand, "splash", ItemLeftovers)
			splashTurn(d2, s2)
			if s2.Active(0).Item != ItemChoiceBand || s2.Active(1).Item != ItemLeftovers {
				t.Errorf("%s stole while already holding an item: attacker=%q target=%q",
					move, s2.Active(0).Item, s2.Active(1).Item)
			}
		})
	}
}

// TestTrickSwapsBothWays covers the two shapes that matter: a genuine two-way
// swap, and the one-sided handoff that is how the move is actually used.
func TestTrickSwapsBothWays(t *testing.T) {
	t.Run("two-way", func(t *testing.T) {
		d, s := moveBattle(t, "trick", ItemChoiceScarf, "splash", ItemLeftovers)
		log := splashTurn(d, s)
		if s.Active(0).Item != ItemLeftovers || s.Active(1).Item != ItemChoiceScarf {
			t.Errorf("Trick did not swap: attacker=%q target=%q; log: %v",
				s.Active(0).Item, s.Active(1).Item, log)
		}
	})
	t.Run("one-sided", func(t *testing.T) {
		d, s := moveBattle(t, "trick", ItemChoiceScarf, "splash", ItemNone)
		splashTurn(d, s)
		if s.Active(0).Item != ItemNone || s.Active(1).Item != ItemChoiceScarf {
			t.Errorf("Trick did not hand the scarf over: attacker=%q target=%q",
				s.Active(0).Item, s.Active(1).Item)
		}
	})
	t.Run("nothing-to-trade", func(t *testing.T) {
		d, s := moveBattle(t, "trick", ItemNone, "splash", ItemNone)
		log := splashTurn(d, s)
		if !logHas(log, "But it failed!") {
			t.Errorf("Trick with both slots empty should fail; log: %v", log)
		}
	})
}

// TestBestowNeedsAnEmptyHandedTarget.
func TestBestowNeedsAnEmptyHandedTarget(t *testing.T) {
	d, s := moveBattle(t, "bestow", ItemLeftovers, "splash", ItemNone)
	log := splashTurn(d, s)
	if s.Active(1).Item != ItemLeftovers || s.Active(0).Item != ItemNone {
		t.Errorf("Bestow did not hand the item over: attacker=%q target=%q; log: %v",
			s.Active(0).Item, s.Active(1).Item, log)
	}

	d2, s2 := moveBattle(t, "bestow", ItemLeftovers, "splash", ItemChoiceBand)
	log2 := splashTurn(d2, s2)
	if s2.Active(1).Item != ItemChoiceBand || s2.Active(0).Item != ItemLeftovers {
		t.Errorf("Bestow overwrote a held item; log: %v", log2)
	}
}

// TestCorrosiveGasDestroys — same removal as Knock Off, status-move form.
func TestCorrosiveGasDestroys(t *testing.T) {
	d, s := moveBattle(t, "corrosive-gas", ItemNone, "splash", ItemLeftovers)
	log := splashTurn(d, s)
	if s.Active(1).Item != ItemNone {
		t.Errorf("Corrosive Gas did not remove the item; log: %v", log)
	}
	if s.Active(0).Item != ItemNone {
		t.Errorf("Corrosive Gas gave the item to the user — it destroys, it does not steal")
	}
}

// TestPoltergeistFailsAgainstAnEmptyHandedTarget.
func TestPoltergeistFailsAgainstAnEmptyHandedTarget(t *testing.T) {
	d, s := moveBattle(t, "poltergeist", ItemNone, "splash", ItemNone)
	before := s.Active(1).HP
	log := splashTurn(d, s)
	if s.Active(1).HP != before {
		t.Errorf("Poltergeist damaged an empty-handed target; log: %v", log)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("Poltergeist should announce its failure; log: %v", log)
	}

	d2, s2 := moveBattle(t, "poltergeist", ItemNone, "splash", ItemLeftovers)
	before2 := s2.Active(1).HP
	splashTurn(d2, s2)
	if s2.Active(1).HP >= before2 {
		t.Errorf("Poltergeist dealt no damage to a target holding an item")
	}
}

// TestRecycleRestoresOnlyWhatWasConsumed is the reason consumeItem and loseItem
// are separate functions. An eaten berry comes back; a stolen item does not.
func TestRecycleRestoresOnlyWhatWasConsumed(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Moves["recycle"]; !ok {
		t.Skip("recycle not in the curated move set")
	}

	t.Run("eaten-berry-returns", func(t *testing.T) {
		_, s := berryBattle(t, ItemSitrusBerry)
		p := s.Active(0)
		p.HP = p.MaxHP / 4 // under the Sitrus threshold
		splashTurn(d, s)
		if p.Item != ItemNone || p.LastConsumedItem != ItemSitrusBerry {
			t.Fatalf("setup: the berry was not eaten (item=%q last=%q)", p.Item, p.LastConsumedItem)
		}
		// Heal past the threshold first. A Recycled berry that is still in pinch
		// range is eaten again on the spot — canon, and the whole point of the
		// Recycle + Sitrus stall — but it would hide whether the restore worked.
		p.HP = p.MaxHP
		p.Moves = []MoveSlot{{MoveID: "recycle", PP: 10, MaxPP: 10}}
		log := splashTurn(d, s)
		if p.Item != ItemSitrusBerry {
			t.Errorf("Recycle did not restore the eaten berry (holds %q); log: %v", p.Item, log)
		}
		// The memory is spent on use: emptying the slot by hand (nothing was
		// consumed) leaves Recycle with nothing to find.
		p.Item = ItemNone
		log2 := splashTurn(d, s)
		if p.Item != ItemNone {
			t.Errorf("Recycle restored a berry it had already handed back once; log: %v", log2)
		}
	})

	t.Run("stolen-item-does-not", func(t *testing.T) {
		d2, s := moveBattle(t, "splash", ItemLeftovers, "thief", ItemNone)
		splashTurn(d2, s)
		victim := s.Active(0)
		if victim.Item != ItemNone {
			t.Fatalf("setup: Thief did not take the item")
		}
		victim.Moves = []MoveSlot{{MoveID: "recycle", PP: 10, MaxPP: 10}}
		log := splashTurn(d2, s)
		if victim.Item != ItemNone {
			t.Errorf("Recycle handed back an item that was stolen, not consumed (holds %q); "+
				"log: %v", victim.Item, log)
		}
	})
}

// TestSwapArmsUnburdenButTheSlotIsFull ties the family back to the Unburden
// rule: every one of these moves empties a slot, which arms the flag, but a
// Pokémon that received something in the same breath is not unburdened.
func TestSwapArmsUnburdenButTheSlotIsFull(t *testing.T) {
	d, s := moveBattle(t, "trick", ItemChoiceScarf, "splash", ItemLeftovers)
	s.Active(0).Ability = AbilityKind("unburden")

	splashTurn(d, s)

	p := s.Active(0)
	if !p.Volatiles.Unburden {
		t.Errorf("Trick did not arm Unburden — the user did lose its item")
	}
	if p.Item == ItemNone {
		t.Fatalf("setup: the swap left the user empty-handed")
	}
	if got := effectiveSpeed(p, s.Weather); got != p.Stats.Spe {
		t.Errorf("Unburden doubled Speed for a Pokémon holding a traded item: %d, want %d",
			got, p.Stats.Spe)
	}
}
