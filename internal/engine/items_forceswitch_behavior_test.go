package engine

import "testing"

// items_forceswitch_behavior_test.go covers Eject Button, Red Card and Eject
// Pack.
//
// The thing worth testing hardest is the switch/drag distinction, because the
// three items look identical from the outside and behave differently the
// moment anything refuses. Ingrain is the cleanest probe: it stops a drag and
// does nothing to a switch, so the same board gives opposite answers for a Red
// Card and an Eject Button, and an implementation that routed both through one
// helper would pass neither pair.

// switchItemBattle builds a two-deep mirror with the named item on side 0.
func switchItemBattle(t *testing.T, kind ItemKind, foeMove string) (*BattleState, *Pokemon, *Pokemon) {
	t.Helper()
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	mine, foe := s.Active(0), s.Active(1)
	mine.Item = kind
	teachMoves(t, d, mine, "splash")
	teachMoves(t, d, foe, foeMove)
	for i := range s.Sides[0].Team {
		teachMoves(t, d, &s.Sides[0].Team[i], "splash")
	}
	return s, mine, foe
}

// --- Eject Button ---

// TestEjectButtonSwitchesTheHolderOutOnADamagingHit.
func TestEjectButtonSwitchesTheHolderOutOnADamagingHit(t *testing.T) {
	d := loadDex(t)
	s, mine, _ := switchItemBattle(t, ItemEjectButton, "tackle")

	playTurn(d, s, 0, 0)
	if s.Sides[0].Active == 0 {
		t.Error("the button should have taken the holder off the field")
	}
	if mine.Item != ItemNone {
		t.Errorf("and consumed itself, still holding %q", mine.Item)
	}
}

// TestEjectButtonIgnoresStatusMoves. Canon's guard is
// `move.category !== 'Status'`, so a Growl is not a hit.
func TestEjectButtonIgnoresStatusMoves(t *testing.T) {
	d := loadDex(t)
	s, mine, _ := switchItemBattle(t, ItemEjectButton, "growl")

	playTurn(d, s, 0, 0)
	if s.Sides[0].Active != 0 {
		t.Error("a status move should not pop the button")
	}
	if mine.Item != ItemEjectButton {
		t.Error("and the button should still be held")
	}
}

// TestEjectButtonDoesNothingWithNobodyToBringIn, and stays held. Canon checks
// canSwitch before it sets the flag and puts the item back if useItem fails.
func TestEjectButtonDoesNothingWithNobodyToBringIn(t *testing.T) {
	d := loadDex(t)
	s, mine, _ := switchItemBattle(t, ItemEjectButton, "tackle")
	s.Sides[0].Team[1].Fainted = true
	s.Sides[0].Team[1].HP = 0

	playTurn(d, s, 0, 0)
	if s.Sides[0].Active != 0 {
		t.Error("there was nobody to bring in; the holder should still be out")
	}
	if mine.Item != ItemEjectButton {
		t.Error("and an unspent button is still held")
	}
}

// TestEjectButtonIsNotADragAndIgnoresIngrain. This is the distinction the whole
// file is about: the holder is leaving under its own power, so nothing that
// refuses a *drag* has any say.
func TestEjectButtonIsNotADragAndIgnoresIngrain(t *testing.T) {
	d := loadDex(t)
	s, _, _ := switchItemBattle(t, ItemEjectButton, "tackle")
	s.Active(0).Volatiles.Ingrain = true

	playTurn(d, s, 0, 0)
	if s.Sides[0].Active == 0 {
		t.Error("Ingrain blocks a drag, not the holder's own departure")
	}
}

// TestEjectButtonCancelsTheAttackersPivot. Upstream clears
// `source.switchFlag` from inside the button's handler: one move, one switch,
// and the button's holder is the one that gets it.
func TestEjectButtonCancelsTheAttackersPivot(t *testing.T) {
	d := loadDex(t)
	s, _, foe := switchItemBattle(t, ItemEjectButton, "u-turn")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "u-turn")
	}

	playTurn(d, s, 0, 0)
	if s.Sides[0].Active == 0 {
		t.Fatal("fixture: the button should have fired")
	}
	if s.Sides[1].Active != 0 {
		t.Errorf("the U-turn user should have been held in place, active is now %d (%s)",
			s.Sides[1].Active, foe.Name)
	}
}

// --- Red Card ---

// TestRedCardThrowsTheAttackerOut, which is a drag: the replacement is the
// engine's random pick rather than the attacking side's choice.
func TestRedCardThrowsTheAttackerOut(t *testing.T) {
	d := loadDex(t)
	s, mine, _ := switchItemBattle(t, ItemRedCard, "tackle")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "tackle")
	}

	playTurn(d, s, 0, 0)
	if s.Sides[1].Active == 0 {
		t.Error("the card should have thrown the attacker off the field")
	}
	if s.Sides[0].Active != 0 {
		t.Error("and left its own holder standing")
	}
	if mine.Item != ItemNone {
		t.Errorf("and consumed itself, still holding %q", mine.Item)
	}
}

// TestRedCardIsSpentEvenWhenTheDragIsRefused. Upstream says so in a comment of
// its own, and the ordering in its handler is explicit: useItem, then the
// DragOut event, then the flag.
func TestRedCardIsSpentEvenWhenTheDragIsRefused(t *testing.T) {
	d := loadDex(t)
	s, mine, foe := switchItemBattle(t, ItemRedCard, "tackle")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "tackle")
	}
	foe.Volatiles.Ingrain = true

	playTurn(d, s, 0, 0)
	if s.Sides[1].Active != 0 {
		t.Error("Ingrain should have refused the drag")
	}
	if mine.Item != ItemNone {
		t.Error("but the card is spent anyway")
	}
}

// TestRedCardDoesNothingWhenTheAttackerHasNoBench, and is not spent — canon's
// canSwitch check returns before useItem.
func TestRedCardDoesNothingWhenTheAttackerHasNoBench(t *testing.T) {
	d := loadDex(t)
	s, mine, _ := switchItemBattle(t, ItemRedCard, "tackle")
	s.Sides[1].Team[1].Fainted = true
	s.Sides[1].Team[1].HP = 0

	playTurn(d, s, 0, 0)
	if s.Sides[1].Active != 0 {
		t.Error("there was nobody to drag in")
	}
	if mine.Item != ItemRedCard {
		t.Error("and an unspent card is still held")
	}
}

// --- Eject Pack ---

// TestEjectPackFiresOnAFoeInducedDrop.
func TestEjectPackFiresOnAFoeInducedDrop(t *testing.T) {
	d := loadDex(t)
	s, mine, _ := switchItemBattle(t, ItemEjectPack, "leer")

	playTurn(d, s, 0, 0)
	if s.Sides[0].Active == 0 {
		t.Error("Leer's Defense drop should have ejected the holder")
	}
	if mine.Item != ItemNone {
		t.Errorf("and consumed the pack, still holding %q", mine.Item)
	}
}

// TestEjectPackFiresOnASelfInflictedDrop. Canon's onAfterBoost does not care
// who caused the drop, which is why the upstream file has cases for a holder
// dropping its own stats with Swallow and for one dropped by its own Moody.
func TestEjectPackFiresOnASelfInflictedDrop(t *testing.T) {
	d := loadDex(t)
	s, _, _ := switchItemBattle(t, ItemEjectPack, "splash")
	teachMoves(t, d, s.Active(0), "close-combat")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "splash")
	}

	playTurn(d, s, 0, 0)
	if s.Sides[0].Active == 0 {
		t.Error("Close Combat drops the user's own defenses; the pack should fire on that too")
	}
}

// TestEjectPackIgnoresARaise. Only a drop arms it — canon walks the boost
// object for a negative entry.
func TestEjectPackIgnoresARaise(t *testing.T) {
	d := loadDex(t)
	s, mine, _ := switchItemBattle(t, ItemEjectPack, "splash")
	teachMoves(t, d, s.Active(0), "swords-dance")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "splash")
	}

	playTurn(d, s, 0, 0)
	if s.Sides[0].Active != 0 {
		t.Error("a Swords Dance is not a stat drop and must not eject")
	}
	if mine.Volatiles.EjectPackArmed {
		t.Error("nor should it arm the pack")
	}
}

// TestEjectPackIgnoresADropThatDidNotLand. A holder already pinned at -6 has
// nothing left to lose, and canon arms off the boost that was actually applied.
func TestEjectPackIgnoresADropThatDidNotLand(t *testing.T) {
	d := loadDex(t)
	s, mine, _ := switchItemBattle(t, ItemEjectPack, "leer")
	mine.Stages.Def = -6

	playTurn(d, s, 0, 0)
	if s.Sides[0].Active != 0 {
		t.Error("a Defense drop at -6 changes nothing and must not eject")
	}
	if mine.Volatiles.EjectPackArmed {
		t.Error("nor should it arm the pack")
	}
}

// TestEjectPackIsDisarmedWhenItFires, so a second drop later in the same battle
// does not eject a holder that no longer has the item.
func TestEjectPackIsDisarmedWhenItFires(t *testing.T) {
	d := loadDex(t)
	s, mine, _ := switchItemBattle(t, ItemEjectPack, "leer")

	playTurn(d, s, 0, 0)
	if mine.Volatiles.EjectPackArmed {
		t.Error("the flag should be spent along with the item")
	}
	if mine.Item != ItemNone {
		t.Error("and the item is gone")
	}
}
