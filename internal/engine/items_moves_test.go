package engine

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
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

// TestFlingAndNaturalGiftCoverTheCatalog is the drift guard for the two tables.
// They are keyed by ItemKind rather than synced from data/items.json, so a new
// catalog entry would otherwise silently make Fling fail on it.
func TestFlingAndNaturalGiftCoverTheCatalog(t *testing.T) {
	d := loadDex(t)
	for _, row := range ItemCatalog(d) {
		kind := ItemKind(row.ID)
		if bp, ok := flingPower[kind]; !ok || bp <= 0 {
			t.Errorf("%s has no Fling base power — Fling would fail while holding it", row.ID)
		}
		it := itemRegistry[kind]
		if it == nil || !it.Berry {
			continue
		}
		if e, ok := naturalGift[kind]; !ok || e.Power <= 0 || e.Type == "" {
			t.Errorf("berry %s has no Natural Gift entry — the move would fail while holding it", row.ID)
		}
	}
	// Spot-check the two ends of the Fling table against canon so a
	// regenerate-from-upstream can't silently flatten it.
	if flingPower[ItemIronBall] != 130 {
		t.Errorf("Iron Ball Fling = %d, want 130", flingPower[ItemIronBall])
	}
	if flingPower[ItemSitrusBerry] != 10 {
		t.Errorf("Sitrus Berry Fling = %d, want 10", flingPower[ItemSitrusBerry])
	}
}

// TestFlingThrowsTheItemAndSpendsIt.
func TestFlingThrowsTheItemAndSpendsIt(t *testing.T) {
	t.Run("power-comes-from-the-item", func(t *testing.T) {
		dmg := func(item ItemKind) int {
			d, s := moveBattle(t, "fling", item, "splash", ItemNone)
			before := s.Active(1).HP
			splashTurn(d, s)
			return before - s.Active(1).HP
		}
		// Iron Ball is 130, Leftovers is 10 — the heaviest and near-lightest.
		heavy, light := dmg(ItemIronBall), dmg(ItemLeftovers)
		if heavy <= light {
			t.Errorf("Fling dealt %d with an Iron Ball vs %d with Leftovers; the base power "+
				"comes from the item (130 vs 10)", heavy, light)
		}
	})
	t.Run("spent-on-use", func(t *testing.T) {
		d, s := moveBattle(t, "fling", ItemIronBall, "splash", ItemNone)
		log := splashTurn(d, s)
		if s.Active(0).Item != ItemNone {
			t.Errorf("Fling did not spend the thrown item; log: %v", log)
		}
		// Consumed, not lost — Recycle can hand it back.
		if s.Active(0).LastConsumedItem != ItemIronBall {
			t.Errorf("a flung item was not recorded as consumed (%q)", s.Active(0).LastConsumedItem)
		}
	})
	t.Run("fails-empty-handed", func(t *testing.T) {
		d, s := moveBattle(t, "fling", ItemNone, "splash", ItemNone)
		before := s.Active(1).HP
		log := splashTurn(d, s)
		if s.Active(1).HP != before {
			t.Errorf("Fling with nothing to throw dealt damage; log: %v", log)
		}
		if !logHas(log, "But it failed!") {
			t.Errorf("Fling with an empty slot should fail; log: %v", log)
		}
	})
	t.Run("thrown-berry-is-eaten-by-the-target", func(t *testing.T) {
		d, s := moveBattle(t, "fling", ItemCheriBerry, "splash", ItemNone)
		s.Active(1).Status = StatusParalysis
		log := splashTurn(d, s)
		if s.Active(1).Status != StatusNone {
			t.Errorf("a thrown Cheri Berry did not cure the target's paralysis; log: %v", log)
		}
	})
}

// TestNaturalGiftTakesTypeAndPowerFromTheBerry.
func TestNaturalGiftTakesTypeAndPowerFromTheBerry(t *testing.T) {
	t.Run("type-comes-from-the-berry", func(t *testing.T) {
		// Chilan is Normal, Occa is Fire. The defender leads Charizard
		// (Fire/Flying): Normal is neutral, Fire is resisted 0.5x.
		dmg := func(item ItemKind) int {
			d, s := moveBattle(t, "natural-gift", item, "splash", ItemNone)
			before := s.Active(1).HP
			splashTurn(d, s)
			return before - s.Active(1).HP
		}
		normal, fire := dmg(ItemChilanBerry), dmg(ItemOccaBerry)
		if normal <= 0 || fire <= 0 {
			t.Fatalf("setup: Natural Gift dealt no damage (normal=%d fire=%d)", normal, fire)
		}
		if fire >= normal {
			t.Errorf("Natural Gift dealt %d as Fire vs %d as Normal into a Fire/Flying target; "+
				"the berry sets the move's type", fire, normal)
		}
	})
	t.Run("fails-without-a-berry", func(t *testing.T) {
		d, s := moveBattle(t, "natural-gift", ItemLeftovers, "splash", ItemNone)
		before := s.Active(1).HP
		log := splashTurn(d, s)
		if s.Active(1).HP != before {
			t.Errorf("Natural Gift worked off a non-berry; log: %v", log)
		}
		if s.Active(0).Item != ItemLeftovers {
			t.Errorf("a failed Natural Gift still spent the item")
		}
	})
	t.Run("spends-the-berry", func(t *testing.T) {
		d, s := moveBattle(t, "natural-gift", ItemChilanBerry, "splash", ItemNone)
		splashTurn(d, s)
		if s.Active(0).Item != ItemNone {
			t.Errorf("Natural Gift did not spend the berry")
		}
	})
}

// TestPluckAndBugBiteEatTheTargetsBerry, and Incinerate destroys it.
func TestPluckAndBugBiteEatTheTargetsBerry(t *testing.T) {
	for _, move := range []string{"pluck", "bug-bite"} {
		t.Run(move, func(t *testing.T) {
			d, s := moveBattle(t, move, ItemNone, "splash", ItemSitrusBerry)
			atk := s.Active(0)
			atk.HP = atk.MaxHP / 2
			before := atk.HP

			log := splashTurn(d, s)

			if s.Active(1).Item != ItemNone {
				t.Errorf("%s did not take the berry; log: %v", move, log)
			}
			if atk.HP <= before {
				t.Errorf("%s ate a Sitrus Berry but the eater did not heal (%d -> %d); log: %v",
					move, before, atk.HP, log)
			}
			if atk.Item != ItemNone {
				t.Errorf("%s left the eaten berry in the attacker's slot (%q)", move, atk.Item)
			}
		})
	}
	t.Run("incinerate", func(t *testing.T) {
		d, s := moveBattle(t, "incinerate", ItemNone, "splash", ItemSitrusBerry)
		atk := s.Active(0)
		atk.HP = atk.MaxHP / 2
		before := atk.HP

		log := splashTurn(d, s)

		if s.Active(1).Item != ItemNone {
			t.Errorf("Incinerate did not destroy the berry; log: %v", log)
		}
		if atk.HP != before {
			t.Errorf("Incinerate healed its user — it burns the berry, it does not eat it")
		}
	})
	t.Run("non-berry-is-left-alone", func(t *testing.T) {
		d, s := moveBattle(t, "pluck", ItemNone, "splash", ItemLeftovers)
		splashTurn(d, s)
		if s.Active(1).Item != ItemLeftovers {
			t.Errorf("Pluck took a non-berry item")
		}
	})
}

// --- item suppression: Embargo, Magic Room, Klutz ---

// TestEmbargoSuppressesTheHoldersItem: the volatile existed and ticked, with a
// comment saying the gameplay hook was "meaningless until items are modeled".
// Items are modeled.
func TestEmbargoSuppressesTheHoldersItem(t *testing.T) {
	d := loadDex(t)
	// Leftovers is the cleanest probe: a visible heal every turn, or not.
	heal := func(embargoed bool) int {
		_, s := berryBattle(t, ItemLeftovers)
		p := s.Active(0)
		p.HP = p.MaxHP / 2
		if embargoed {
			p.Volatiles.Embargo = &EmbargoState{Turns: 5}
		}
		before := p.HP
		splashTurn(d, s)
		return p.HP - before
	}
	if got := heal(false); got <= 0 {
		t.Fatalf("setup: Leftovers did not heal (%d)", got)
	}
	if got := heal(true); got != 0 {
		t.Errorf("Leftovers healed %d through an Embargo — the item is held but must do nothing", got)
	}
}

// TestMagicRoomSuppressesBothSides, and the mirror stays in sync across a
// switch and an expiry. The mirror is the risky part of this design, so the
// invariant checker is asserted directly alongside the behavior.
func TestMagicRoomSuppressesBothSides(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143, 6}, "B", []int{143, 6}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := 0; i < 2; i++ {
		s.Active(i).Ability = AbilityNone
		s.Active(i).Item = ItemLeftovers
		s.Active(i).HP = s.Active(i).MaxHP / 2
		s.Active(i).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	}

	var log []LogLine
	applyMagicRoomSetter(s, 0, &log)
	if err := ValidateStateInvariants(s); err != nil {
		t.Fatalf("mirror out of sync right after the setter: %v", err)
	}

	before := [2]int{s.Active(0).HP, s.Active(1).HP}
	splashTurn(d, s)
	for i := 0; i < 2; i++ {
		if s.Active(i).HP != before[i] {
			t.Errorf("side %d healed from Leftovers inside Magic Room (%d -> %d)",
				i, before[i], s.Active(i).HP)
		}
	}

	// A fresh switch-in arrives with Volatiles zeroed and must still be
	// suppressed — the room belongs to the field, not to the Pokémon.
	doSwitch(s, 0, 1, NewRNG(1), &log)
	if err := ValidateStateInvariants(s); err != nil {
		t.Fatalf("mirror out of sync after a switch-in: %v", err)
	}
	in := s.Active(0)
	in.Item, in.HP = ItemLeftovers, in.MaxHP/2
	in.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	inBefore := in.HP
	splashTurn(d, s)
	if in.HP != inBefore {
		t.Errorf("a Pokémon that switched into Magic Room still healed from Leftovers (%d -> %d)",
			inBefore, in.HP)
	}

	// Dismissing the room hands the items back, and the mirror follows.
	applyMagicRoomSetter(s, 0, &log)
	if err := ValidateStateInvariants(s); err != nil {
		t.Fatalf("mirror out of sync after dismissing the room: %v", err)
	}
	outBefore := s.Active(0).HP
	splashTurn(d, s)
	if s.Active(0).HP <= outBefore {
		t.Errorf("Leftovers stayed suppressed after Magic Room ended (%d -> %d)",
			outBefore, s.Active(0).HP)
	}
}

// TestSuppressedItemIsStillHeld: suppression is not removal. The slot still
// counts for Acrobatics and Unburden, and the item can still be knocked off.
func TestSuppressedItemIsStillHeld(t *testing.T) {
	d := loadDex(t)
	t.Run("acrobatics-does-not-double", func(t *testing.T) {
		if _, ok := d.Moves["acrobatics"]; !ok {
			t.Skip("acrobatics not in the curated move set")
		}
		dmg := func(embargoed bool) int {
			d2, s := moveBattle(t, "acrobatics", ItemLeftovers, "splash", ItemNone)
			if embargoed {
				s.Active(0).Volatiles.Embargo = &EmbargoState{Turns: 5}
			}
			before := s.Active(1).HP
			splashTurn(d2, s)
			return before - s.Active(1).HP
		}
		if plain, emb := dmg(false), dmg(true); emb != plain {
			t.Errorf("Acrobatics dealt %d under an Embargo vs %d without; it reads the slot, "+
				"and a suppressed item still fills it", emb, plain)
		}
	})
	t.Run("knock-off-still-removes", func(t *testing.T) {
		d2, s := moveBattle(t, "knock-off", ItemNone, "splash", ItemLeftovers)
		s.Active(1).Volatiles.Embargo = &EmbargoState{Turns: 5}
		splashTurn(d2, s)
		if s.Active(1).Item != ItemNone {
			t.Errorf("Knock Off could not remove a suppressed item — suppression is not removal")
		}
	})
	t.Run("unburden-stays-slow", func(t *testing.T) {
		_, s := berryBattle(t, ItemLeftovers)
		p := s.Active(0)
		p.Ability = AbilityKind("unburden")
		p.Volatiles.Unburden = true
		p.Volatiles.Embargo = &EmbargoState{Turns: 5}
		if got := effectiveSpeed(p, s.Weather); got != p.Stats.Spe {
			t.Errorf("Unburden doubled Speed for a holder whose item is merely suppressed: "+
				"%d, want %d", got, p.Stats.Spe)
		}
	})
}

// TestKlutzSuppressesItsHoldersItem. No dex species has Klutz today, so this is
// the only thing exercising it — which is exactly why it is worth writing.
func TestKlutzSuppressesItsHoldersItem(t *testing.T) {
	d := loadDex(t)
	heal := func(ability AbilityKind) int {
		_, s := berryBattle(t, ItemLeftovers)
		p := s.Active(0)
		p.Ability = ability
		p.HP = p.MaxHP / 2
		before := p.HP
		splashTurn(d, s)
		return p.HP - before
	}
	if heal(AbilityNone) <= 0 {
		t.Fatalf("setup: Leftovers did not heal")
	}
	if got := heal(AbilityKind("klutz")); got != 0 {
		t.Errorf("Leftovers healed %d for a Klutz holder", got)
	}
}

// --- ninth review pass: regressions from the suppression + Fling batch ---

// TestFaintKeepsTheMagicRoomMirror: faint() wipes Volatiles, which zeroed the
// mirror while the room was still up — making the most common event in the game
// a fourth, unsynced writer. The fainted Pokémon stays the active until the
// replace phase, so the mirror has to keep agreeing with the field until then.
func TestFaintKeepsTheMagicRoomMirror(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143, 6}, "B", []int{143, 6}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	var log []LogLine
	applyMagicRoomSetter(s, 0, &log)
	faint(s.Active(1), 1, &log)
	// A bare faint() leaves the state mid-flight — the fainted mon is still the
	// active and the phase has not moved. Advance it the way ResolveTurn would,
	// so the only thing this test can trip on is the mirror.
	s.Phase, s.Replace[1] = PhaseReplace, true

	if err := ValidateStateInvariants(s); err != nil {
		t.Errorf("fainting under Magic Room desynced the mirror: %v", err)
	}
	if !s.Active(1).Volatiles.MagicRoomHere {
		t.Errorf("the fainted active lost MagicRoomHere while the room is still up")
	}
}

// TestInvariantsReportOutOfRangeActiveInsteadOfPanicking: the mirror check
// dereferenced s.Active(i) — an unguarded index into the team slice — above the
// bounds check that exists to report exactly that corruption, so the checker
// panicked on the one input it was written to describe.
func TestInvariantsReportOutOfRangeActiveInsteadOfPanicking(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Sides[0].Active = 7

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ValidateStateInvariants panicked on an out-of-range Active (%v); it is "+
				"supposed to report that, not crash on it", r)
		}
	}()
	if err := ValidateStateInvariants(s); err == nil {
		t.Errorf("an out-of-range Active was not reported")
	}
}

// TestFlingSpendsTheItemBeforeTheThrowResolves is the big one. Deferring the
// consume left the thrown item live for the whole move: a Life Orb boosted and
// recoiled for the orb being thrown, and the pinch check at the tail of
// executeMove let the user eat the very berry it was throwing — so the target
// never received it and the user healed off its own attack.
func TestFlingSpendsTheItemBeforeTheThrowResolves(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Moves["fling"]; !ok {
		t.Skip("fling not in the curated move set")
	}

	t.Run("user-does-not-eat-the-berry-it-throws", func(t *testing.T) {
		d2, s := moveBattle(t, "fling", ItemSitrusBerry, "splash", ItemNone)
		atk := s.Active(0)
		atk.HP = atk.MaxHP * 2 / 5 // under the Sitrus threshold
		before := atk.HP

		log := splashTurn(d2, s)

		if atk.HP > before {
			t.Errorf("the user healed from the Sitrus it was throwing (%d -> %d); log: %v",
				before, atk.HP, log)
		}
		if !logHas(log, "ate the thrown Sitrus Berry") {
			t.Errorf("the target never received the thrown berry; log: %v", log)
		}
	})

	t.Run("life-orb-does-not-boost-its-own-throw", func(t *testing.T) {
		// Life Orb and Iron Ball are both non-berries; the orb's Fling power is
		// lower, so if the orb were still live the ×1.3 and the recoil would
		// both show up.
		d2, s := moveBattle(t, "fling", ItemLifeOrb, "splash", ItemNone)
		atk := s.Active(0)
		before := atk.HP

		log := splashTurn(d2, s)

		if atk.HP < before {
			t.Errorf("the user took Life Orb recoil for the orb it was throwing (%d -> %d); "+
				"log: %v", before, atk.HP, log)
		}
	})
}

// TestMissedFlingDoesNotFeedTheTarget: the item is spent on a miss (canon —
// the classic way to waste a Choice Scarf), but a berry cannot reach a target
// the move never touched.
func TestMissedFlingDoesNotFeedTheTarget(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Moves["fling"]; !ok {
		t.Skip("fling not in the curated move set")
	}
	missed := false
	for seed := uint64(1); seed <= 80 && !missed; seed++ {
		d2, s := moveBattle(t, "fling", ItemSalacBerry, "splash", ItemNone)
		s.RNGState, s.Seed = seed, seed
		s.Active(1).Stages.Eva = 6 // force whiffs

		log := splashTurn(d2, s)
		if !logHas(log, "attack missed") {
			continue
		}
		missed = true
		if logHas(log, "ate the thrown") {
			t.Errorf("a missed Fling still fed the target its berry; log: %v", log)
		}
		if s.Active(1).Stages.Spe != 0 {
			t.Errorf("a missed Fling handed the target a Salac boost (Spe = %d)", s.Active(1).Stages.Spe)
		}
		if s.Active(0).Item != ItemNone {
			t.Errorf("a missed Fling did not spend the item — canon spends it on the attempt")
		}
	}
	if !missed {
		t.Skip("no miss in 80 seeds; nothing proven")
	}
}

// TestFlingDoesNotReachThroughASubstitute — same rule the theft moves follow.
func TestFlingDoesNotReachThroughASubstitute(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Moves["fling"]; !ok {
		t.Skip("fling not in the curated move set")
	}
	d2, s := moveBattle(t, "fling", ItemSalacBerry, "splash", ItemNone)
	s.Active(1).Volatiles.Substitute = &SubstituteState{HP: 80}

	log := splashTurn(d2, s)

	if logHas(log, "ate the thrown") {
		t.Errorf("a thrown berry reached through a Substitute; log: %v", log)
	}
	if s.Active(1).Stages.Spe != 0 {
		t.Errorf("a berry thrown into a Substitute still boosted the holder (Spe = %d)",
			s.Active(1).Stages.Spe)
	}
}

// TestFlingAndNaturalGiftRespectSuppression: canon gates both on ignoringItem,
// so an Embargoed, Magic-Roomed or Klutzed holder cannot throw what it cannot
// use. This is the one member of the family that consults suppression — the
// theft moves read the raw slot on purpose, because a suppressed item is still
// there to be taken.
func TestFlingAndNaturalGiftRespectSuppression(t *testing.T) {
	d := loadDex(t)
	for _, move := range []string{"fling", "natural-gift"} {
		if _, ok := d.Moves[move]; !ok {
			t.Skipf("%s not in the curated move set", move)
		}
		item := ItemIronBall
		if move == "natural-gift" {
			item = ItemChilanBerry
		}
		t.Run(move+"/embargo", func(t *testing.T) {
			d2, s := moveBattle(t, move, item, "splash", ItemNone)
			s.Active(0).Volatiles.Embargo = &EmbargoState{Turns: 5}
			before := s.Active(1).HP
			log := splashTurn(d2, s)
			if s.Active(1).HP != before {
				t.Errorf("%s dealt damage under an Embargo; log: %v", move, log)
			}
			if s.Active(0).Item != item {
				t.Errorf("%s consumed the item under an Embargo", move)
			}
		})
		t.Run(move+"/klutz", func(t *testing.T) {
			d2, s := moveBattle(t, move, item, "splash", ItemNone)
			s.Active(0).Ability = AbilityKind("klutz")
			before := s.Active(1).HP
			splashTurn(d2, s)
			if s.Active(1).HP != before || s.Active(0).Item != item {
				t.Errorf("%s worked for a Klutz holder", move)
			}
		})
	}
}

// TestCuteCharmDoesNotFireThroughASubstitute: the fifth contact rider, and the
// one whose comment described firing through a doll as deliberate.
func TestCuteCharmDoesNotFireThroughASubstitute(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(1); seed <= 40; seed++ {
		s, err := NewBattle(d, "b", "Attacker", []int{143}, "Defender", []int{143}, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability = AbilityNone
		s.Active(1).Ability = AbilityKind("cute-charm")
		s.Active(1).Volatiles.Substitute = &SubstituteState{HP: 60}
		s.Active(0).Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

		log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

		if s.Active(0).Volatiles.Attract {
			t.Fatalf("Cute Charm infatuated through a Substitute (seed %d); log: %v", seed, log)
		}
	}
}

// --- tenth review pass ---

// TestFlingCanStillKOWithAHealBerry: moving the delivery call above the faint
// block put it inside the window where a killed Pokémon sits at HP 0 with
// Fainted still false. The `tgt.Fainted` guard passed, the berry healed the
// target off zero, and the faint block's `def.HP <= 0` was then false — so
// Fling with any heal berry could never KO. The victim ate the berry instead.
func TestFlingCanStillKOWithAHealBerry(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Moves["fling"]; !ok {
		t.Skip("fling not in the curated move set")
	}
	for _, berry := range []ItemKind{ItemOranBerry, ItemSitrusBerry, ItemBerryJuice} {
		t.Run(string(berry), func(t *testing.T) {
			d2, s := moveBattle(t, "fling", berry, "splash", ItemNone)
			def := s.Active(1)
			def.HP = 1

			log := splashTurn(d2, s)

			if !def.Fainted {
				t.Errorf("a 1-HP target survived a Fling because it was fed the thrown %s "+
					"(HP=%d); log: %v", berry, def.HP, log)
			}
			if logHas(log, "ate the thrown") {
				t.Errorf("a berry was delivered to a target the throw had already killed; log: %v", log)
			}
		})
	}
}

// TestThiefDoesNotLootACorpse is the same window, one call earlier — the theft
// moves run from the same pre-faint point and had the same guard shape.
func TestThiefDoesNotLootACorpse(t *testing.T) {
	for _, move := range []string{"thief", "covet", "knock-off"} {
		t.Run(move, func(t *testing.T) {
			d, s := moveBattle(t, move, ItemNone, "splash", ItemLeftovers)
			s.Active(1).HP = 1

			log := splashTurn(d, s)

			if !s.Sides[1].Team[0].Fainted {
				t.Fatalf("setup: the target survived; log: %v", log)
			}
			if s.Active(0).Item != ItemNone {
				t.Errorf("%s took an item off a target the same hit killed (holds %q); log: %v",
					move, s.Active(0).Item, log)
			}
		})
	}
}

// TestReplacementThatDiesToHazardsStaysInTheReplacePhase: ResolveReplace cleared
// the replace flag on the assumption that doSwitch installed a healthy active.
// Stealth Rock and Spikes both faint the incoming from applyHazardsOnSwitchIn,
// so the battle dropped into PhaseChoosing with a fainted active — a state
// ValidateStateInvariants rejects and LegalActions would read volatiles from.
// Pre-existing, and the one real bug behind a fuzz signature that had been
// showing up in ~12% of seeds.
func TestReplacementThatDiesToHazardsStaysInTheReplacePhase(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143, 6, 9}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Sides[1].Conditions.Hazards.StealthRock = true
	s.Sides[1].Team[1].HP = 1 // Charizard: 4x weak to rocks, dies on entry

	var log []LogLine
	faint(s.Active(1), 1, &log)
	s.Phase, s.Replace[1] = PhaseReplace, true

	log = append(log, ResolveReplace(s, [2]*Action{nil, {Kind: ActionSwitch, Index: 1}})...)

	if !s.Active(1).Fainted {
		t.Fatalf("setup: the replacement survived the hazards; log: %v", log)
	}
	if s.Phase == PhaseChoosing {
		t.Errorf("the battle moved to PhaseChoosing with a fainted active — the side still "+
			"owes a replacement; log: %v", log)
	}
	if !s.Replace[1] && s.Phase != PhaseEnded {
		t.Errorf("side 1 lost its replace flag while its active is fainted; phase=%v", s.Phase)
	}
	if err := ValidateStateInvariants(s); err != nil {
		t.Errorf("state is invalid after a hazard-killed replacement: %v", err)
	}
}

// TestSideThatRunsOutDuringAReplaceEndsTheBattle is the other half of routing
// through updatePhase: the hand-rolled tail could not end a battle, so a side
// whose last Pokémon died to entry hazards had nowhere to go.
func TestSideThatRunsOutDuringAReplaceEndsTheBattle(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143, 6}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Sides[1].Conditions.Hazards.StealthRock = true
	s.Sides[1].Team[1].HP = 1

	var log []LogLine
	faint(s.Active(1), 1, &log)
	s.Phase, s.Replace[1] = PhaseReplace, true

	log = append(log, ResolveReplace(s, [2]*Action{nil, {Kind: ActionSwitch, Index: 1}})...)

	if s.Phase != PhaseEnded {
		t.Errorf("side 1's last Pokémon died to hazards and the battle did not end "+
			"(phase=%v); log: %v", s.Phase, log)
	}
	if s.Winner != 0 {
		t.Errorf("winner = %d, want 0", s.Winner)
	}
}

// TestHazardKilledReplacementsChainToTheEnd: ResolveReplace can now return
// *still* in PhaseReplace when a replacement dies to entry hazards and the side
// has more Pokémon to send. That is a new return state, and this asserts the
// engine drives it to a terminal phase in bounded rounds rather than looping —
// the bench strictly shrinks, so it must.
func TestHazardKilledReplacementsChainToTheEnd(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143, 6, 6, 6}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Sides[1].Conditions.Hazards.StealthRock = true
	for i := 1; i < len(s.Sides[1].Team); i++ {
		s.Sides[1].Team[i].HP = 1 // every Charizard dies to the rocks on entry
	}

	var log []LogLine
	faint(s.Active(1), 1, &log)
	s.Phase, s.Replace[1] = PhaseReplace, true

	rounds := 0
	for s.Phase == PhaseReplace {
		rounds++
		if rounds > len(s.Sides[1].Team)+2 {
			t.Fatalf("replace phase did not terminate after %d rounds; log: %v", rounds, log)
		}
		var sw [2]*Action
		for i := 0; i < 2; i++ {
			if !s.Replace[i] {
				continue
			}
			acts := LegalActionsDex(d, s, i)
			if len(acts) == 0 {
				// No live bench left: the engine must have ended the battle
				// rather than asking for a switch that cannot exist.
				t.Fatalf("side %d owes a replacement but has no legal action; phase=%v", i, s.Phase)
			}
			a := acts[0]
			sw[i] = &a
		}
		log = append(log, ResolveReplace(s, sw)...)
		if err := ValidateStateInvariants(s); err != nil {
			t.Fatalf("round %d: %v", rounds, err)
		}
	}

	if s.Phase != PhaseEnded {
		t.Errorf("phase = %v after every replacement died, want ended; log: %v", s.Phase, log)
	}
	wins := 0
	for _, l := range log {
		if l.Type == "win" {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("got %d win lines across the chained replaces, want exactly 1; log: %v", wins, log)
	}
}

// TestResolveReplaceMakesNoProgressOnANonSwitch documents the engine behavior
// that turned a driver's `for` loop into a livelock: ResolveReplace silently
// ignores an action that is not a switch, and updatePhase then re-derives
// PhaseReplace from the same fainted active. Identical state, forever.
//
// The engine is arguably right to ignore it — a non-switch is not an answer to
// "who comes in" — so the fix lives in the drivers, which now check ctx and bail
// when a round produces no switch. This pins the property they rely on.
func TestResolveReplaceMakesNoProgressOnANonSwitch(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{143, 6}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	var log []LogLine
	faint(s.Active(1), 1, &log)
	s.Phase, s.Replace[1] = PhaseReplace, true

	before := s.Sides[1].Active
	ResolveReplace(s, [2]*Action{nil, {Kind: ActionMove, Index: 0}})

	if s.Sides[1].Active != before || s.Phase != PhaseReplace || !s.Replace[1] {
		t.Fatalf("a non-switch replace action changed something: active %d->%d phase=%v replace=%v",
			before, s.Sides[1].Active, s.Phase, s.Replace[1])
	}
	// The point: a caller that loops on the phase without checking for progress
	// will spin here. Asserted so the property is visible rather than folklore.
	ResolveReplace(s, [2]*Action{nil, {Kind: ActionMove, Index: 0}})
	if s.Phase != PhaseReplace {
		t.Errorf("phase changed on the second identical call; the livelock premise no longer holds")
	}
}
