package engine

import "testing"

// items_singles_behavior_test.go covers the last four items of the pass: Mail,
// the terrain seeds, Room Service and Mirror Herb.
//
// They have nothing in common mechanically. What they share is that each one's
// interesting behavior lives somewhere other than the Item struct's hook
// surface — Mail in a removability predicate, the seeds and Room Service in two
// firing points rather than one, Mirror Herb in a record kept on the wrong
// Pokémon on purpose — so each is a place a plausible implementation quietly
// does half the job.

// --- Mail ---

// TestMailIsTakenOnlyByTheThreeMovesThatMay. Canon's onTakeItem is not a
// deny-list: everything is refused except Knock Off, Thief and Covet, and the
// first line of the handler refuses anything that is not a move at all.
func TestMailIsTakenOnlyByTheThreeMovesThatMay(t *testing.T) {
	for _, tc := range []struct {
		move string
		want bool
	}{
		{"knock-off", true},
		{"thief", true},
		{"covet", true},
		{"trick", false},
		{"fling", false},
		{"corrosive-gas", false},
		{"bug-bite", false},
		{"", false}, // not a move at all — canon's `if (!this.activeMove) return false`
	} {
		t.Run(tc.move, func(t *testing.T) {
			d := loadDex(t)
			s := neutralBattle(t, d, 11, []int{143}, []int{143})
			holder := s.Active(1)
			holder.Item = ItemMail

			if got := itemIsRemovable(s, holder, tc.move); got != tc.want {
				t.Errorf("Mail removable by %q = %v, want %v", tc.move, got, tc.want)
			}
		})
	}
}

// TestMailIsRemovedByKnockOffEndToEnd, so the predicate above is actually
// consulted on the path that matters.
func TestMailIsRemovedByKnockOffEndToEnd(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143}, []int{143})
	mine, foe := s.Active(0), s.Active(1)
	foe.Item = ItemMail
	teachMoves(t, d, mine, "knock-off")
	teachMoves(t, d, foe, "splash")

	playTurn(d, s, 0, 0)
	if foe.Item != ItemNone {
		t.Errorf("Knock Off is one of the three that may take Mail, still holding %q", foe.Item)
	}
}

// TestMailCannotBeFlung. Canon runs the TakeItem event before Fling throws
// anything, and Mail's handler refuses because the active move is not one of
// its three.
func TestMailCannotBeFlung(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143}, []int{143})
	mine, foe := s.Active(0), s.Active(1)
	mine.Item = ItemMail
	teachMoves(t, d, mine, "fling")
	teachMoves(t, d, foe, "splash")

	before := foe.HP
	playTurn(d, s, 0, 0)
	if foe.HP != before {
		t.Errorf("a Mail that cannot be thrown should deal nothing, dealt %d", before-foe.HP)
	}
	if mine.Item != ItemMail {
		t.Error("and the Mail should still be held")
	}
}

// --- terrain seeds and Room Service ---

// TestSeedsFireOnBothOfCanonsTriggers. The switch-in half is the one an
// implementation gets for free; the terrain-change half is the one it drops,
// and it is the commoner case in play.
func TestSeedsFireOnBothOfCanonsTriggers(t *testing.T) {
	d := loadDex(t)

	t.Run("terrain rises under a holder already out", func(t *testing.T) {
		s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
		mine, foe := s.Active(0), s.Active(1)
		mine.Item = ItemGrassySeed
		teachMoves(t, d, mine, "splash")
		teachMoves(t, d, foe, "grassy-terrain")

		playTurn(d, s, 0, 0)
		if mine.Stages.Def != 1 {
			t.Errorf("the seed should have paid out when the terrain went up, Def is %+d", mine.Stages.Def)
		}
		if mine.Item != ItemNone {
			t.Errorf("and been consumed, still holding %q", mine.Item)
		}
	})

	t.Run("holder arrives to a terrain already up", func(t *testing.T) {
		s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
		for i := range s.Sides[0].Team {
			teachMoves(t, d, &s.Sides[0].Team[i], "splash")
		}
		for i := range s.Sides[1].Team {
			teachMoves(t, d, &s.Sides[1].Team[i], "splash")
		}
		s.Sides[0].Team[1].Item = ItemElectricSeed
		s.Terrain = &TerrainState{Kind: TerrainElectric, TurnsLeft: 5}

		ResolveTurn(d, s, [2]Action{switchTo(1), moveAt(0)})
		arrived := s.Active(0)
		if arrived.Stages.Def != 1 {
			t.Errorf("a seed holder arriving to its terrain should pay out, Def is %+d", arrived.Stages.Def)
		}
	})
}

// TestSeedIgnoresTheWrongTerrain.
func TestSeedIgnoresTheWrongTerrain(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	mine, foe := s.Active(0), s.Active(1)
	mine.Item = ItemGrassySeed
	teachMoves(t, d, mine, "splash")
	teachMoves(t, d, foe, "electric-terrain")

	playTurn(d, s, 0, 0)
	if mine.Item != ItemGrassySeed {
		t.Error("a Grassy Seed has no business firing on Electric Terrain")
	}
}

// TestRoomServiceDropsSpeedWhenTrickRoomGoesUp, and only on the way up: canon
// hangs it on onAnyPseudoWeatherChange, and re-using the setter *clears* Trick
// Room rather than re-setting it, which pays nobody.
func TestRoomServiceDropsSpeedWhenTrickRoomGoesUp(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	mine, foe := s.Active(0), s.Active(1)
	mine.Item = ItemRoomService
	teachMoves(t, d, mine, "splash")
	teachMoves(t, d, foe, "trick-room")

	playTurn(d, s, 0, 0)
	if mine.Stages.Spe != -1 {
		t.Errorf("Room Service should have cost the holder a stage of Speed, got %+d", mine.Stages.Spe)
	}
	if mine.Item != ItemNone {
		t.Errorf("and been consumed, still holding %q", mine.Item)
	}
}

// TestRoomServiceIsSelfInflictedAndNotGuarded. It is the holder's own item
// spending itself, so Mist and Clear Body have nothing to say — which is also
// why the ported case asserts it does not wake a Defiant.
func TestRoomServiceIsSelfInflictedAndNotGuarded(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	mine, foe := s.Active(0), s.Active(1)
	mine.Item = ItemRoomService
	mine.Ability = "clear-body"
	teachMoves(t, d, mine, "splash")
	teachMoves(t, d, foe, "trick-room")
	seedAbilitySuppression(s)

	playTurn(d, s, 0, 0)
	if mine.Stages.Spe != -1 {
		t.Errorf("Clear Body guards against a foe's drops, not the holder's own item: Spe %+d", mine.Stages.Spe)
	}
}

// --- Mirror Herb ---

// TestMirrorHerbCopiesTheFoesRaise, and only the raise.
func TestMirrorHerbCopiesTheFoesRaise(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	mine, foe := s.Active(0), s.Active(1)
	mine.Item = ItemMirrorHerb
	teachMoves(t, d, mine, "splash")
	teachMoves(t, d, foe, "swords-dance")

	playTurn(d, s, 0, 0)
	if mine.Stages.Atk != 2 {
		t.Errorf("the herb should have copied the foe's +2 Attack, got %+d", mine.Stages.Atk)
	}
	if mine.Item != ItemNone {
		t.Errorf("and been consumed, still holding %q", mine.Item)
	}
	if foe.Stages.Atk != 2 {
		t.Errorf("fixture: the foe keeps its own boost, got %+d", foe.Stages.Atk)
	}
}

// TestMirrorHerbIgnoresDrops. Canon's filter is `if (boost[i]! > 0)`, which is
// why a Superpower's own drops are not a gift to the other side.
func TestMirrorHerbIgnoresDrops(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	mine, foe := s.Active(0), s.Active(1)
	mine.Item = ItemMirrorHerb
	teachMoves(t, d, mine, "splash")
	teachMoves(t, d, foe, "leer")

	playTurn(d, s, 0, 0)
	if mine.Item != ItemMirrorHerb {
		t.Error("a drop is not something to copy; the herb should be unspent")
	}
}

// TestMirrorHerbCopiesOnlyWhatTheClampAllowed. A foe already at +5 gains one
// stage from a Swords Dance, so that is what the herb takes — canon trims its
// boost object the same way before the herb ever sees it.
func TestMirrorHerbCopiesOnlyWhatTheClampAllowed(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	mine, foe := s.Active(0), s.Active(1)
	mine.Item = ItemMirrorHerb
	foe.Stages.Atk = 5
	teachMoves(t, d, mine, "splash")
	teachMoves(t, d, foe, "swords-dance")

	playTurn(d, s, 0, 0)
	if mine.Stages.Atk != 1 {
		t.Errorf("only the stage that actually landed is copied, got %+d", mine.Stages.Atk)
	}
}

// TestGainedBoostsDoNotOutliveTheirWindow. The record is kept on the Pokémon
// that received the boost, so it has to be drained on the herb's own schedule —
// otherwise a herb picked up later copies something from two turns ago.
func TestGainedBoostsDoNotOutliveTheirWindow(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	mine, foe := s.Active(0), s.Active(1)
	teachMoves(t, d, mine, "splash")
	teachMoves(t, d, foe, "swords-dance")

	playTurn(d, s, 0, 0)
	if foe.Volatiles.GainedBoosts != nil {
		t.Fatalf("the record should be drained at the end of the turn, got %+v", foe.Volatiles.GainedBoosts)
	}
	mine.Item = ItemMirrorHerb
	teachMoves(t, d, foe, "splash")
	playTurn(d, s, 0, 0)

	if mine.Stages.Atk != 0 {
		t.Errorf("a herb acquired afterwards must not copy last turn's boost, got %+d", mine.Stages.Atk)
	}
	if mine.Item != ItemMirrorHerb {
		t.Error("and it should still be held")
	}
}
