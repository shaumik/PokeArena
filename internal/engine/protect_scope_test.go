package engine

import "testing"

// Protect used to block everything that was not self-targeted, listing the
// escapes — where canon blocks only what carries Showdown's `protect` flag and
// lists nothing else. The data pipeline made that worse by marking the entry
// hazards as foe-targeting, so the standard answer to a hazard lead became
// "press Protect".
//
// These are the untagged half of the evidence for the fix. The flag itself is
// data, so the assertions double as a check that the sync kept it.

// TestProtectBlocksOnlyFlaggedMoves walks the four shapes: a damaging move
// (blocked), a flagged status move (blocked), a hazard (through), and a
// side-support move (through, and onto the caster's own side).
func TestProtectBlocksOnlyFlaggedMoves(t *testing.T) {
	cases := []struct {
		move    string
		blocked bool
		why     string
	}{
		{"tackle", true, "a damaging move carries the protect flag"},
		{"thunder-wave", true, "Thunder Wave carries the protect flag"},
		{"toxic", true, "Toxic carries the protect flag"},
		{"stealth-rock", false, "hazards are foeSide upstream and carry no protect flag"},
		{"spikes", false, "hazards are foeSide upstream and carry no protect flag"},
		{"toxic-spikes", false, "hazards are foeSide upstream and carry no protect flag"},
		{"roar", false, "Roar carries no protect flag"},
		{"reflect", false, "a side-support move never touches the foe's shield"},
		{"sunny-day", false, "a field move never touches the foe's shield"},
	}
	for _, c := range cases {
		t.Run(c.move, func(t *testing.T) {
			d, s := duel(t, 3,
				[]TeamPick{mon(dexSnorlax, "", "", "protect")},
				[]TeamPick{mon(dexGengar, "", "", c.move), mon(dexGengar, "", "", "splash")})
			log := ResolveTurn(d, s, slots(0, 0))
			got := logHas(log, "protected itself")
			if got != c.blocked {
				t.Errorf("Protect vs %s: blocked=%v, want %v — %s\nlog: %v",
					c.move, got, c.blocked, c.why, logTexts(log))
			}
		})
	}
}

// TestHazardsLandThroughProtect is the same rule stated as the consequence
// that matters: the shield does not keep rocks off the board.
func TestHazardsLandThroughProtect(t *testing.T) {
	d, s := duel(t, 5,
		[]TeamPick{mon(dexSnorlax, "", "", "protect")},
		[]TeamPick{mon(dexGengar, "", "", "stealth-rock", "spikes", "toxic-spikes")})
	for i := range 3 {
		ResolveTurn(d, s, slots(0, i))
	}
	h := s.Sides[0].Conditions.Hazards
	if !h.StealthRock || h.Spikes == 0 || h.ToxicSpikes == 0 {
		t.Errorf("hazards set into a Protect: rocks=%v spikes=%d tspikes=%d, want all laid",
			h.StealthRock, h.Spikes, h.ToxicSpikes)
	}
}

// TestAllyFacingMovesTargetTheirUser: Showdown's `allies` and `allySide`
// targets resolve to the user in singles, and the transform's old `default:
// foe` sent them to the opponent instead — so Howl and Life Dew were free gifts
// and Reflect only escaped because it rides a dedicated side-condition handler.
func TestAllyFacingMovesTargetTheirUser(t *testing.T) {
	t.Run("howl boosts its user", func(t *testing.T) {
		d, s := duel(t, 7,
			[]TeamPick{mon(dexSnorlax, "", "", "splash")},
			[]TeamPick{mon(dexGengar, "", "", "howl")})
		ResolveTurn(d, s, slots(0, 0))
		if got := s.Active(1).Stages.Atk; got != 1 {
			t.Errorf("Howl's user atk stage = %d, want +1", got)
		}
		if got := s.Active(0).Stages.Atk; got != 0 {
			t.Errorf("Howl moved the opponent's atk stage to %d; it should not touch it", got)
		}
	})
	t.Run("life dew heals its user", func(t *testing.T) {
		d, s := duel(t, 9,
			[]TeamPick{mon(dexSnorlax, "", "", "splash")},
			[]TeamPick{mon(dexGengar, "", "", "life-dew")})
		s.Active(1).HP = s.Active(1).MaxHP / 2
		s.Active(0).HP = s.Active(0).MaxHP / 2
		before0, before1 := s.Active(0).HP, s.Active(1).HP
		ResolveTurn(d, s, slots(0, 0))
		if s.Active(1).HP <= before1 {
			t.Errorf("Life Dew did not heal its user (%d -> %d)", before1, s.Active(1).HP)
		}
		if s.Active(0).HP != before0 {
			t.Errorf("Life Dew healed the opponent (%d -> %d)", before0, s.Active(0).HP)
		}
	})
}

// TestAdjacentAllyMovesAreNotShipped: Coaching and Hold Hands have no singles
// meaning at all — upstream's getRandomTarget returns null and the move fails —
// so mapping them either way fabricates a rule. They are denylisted, which is
// the same call the pipeline already made for Helping Hand and Dragon Cheer.
func TestAdjacentAllyMovesAreNotShipped(t *testing.T) {
	d := loadDex(t)
	for _, id := range []string{"coaching", "hold-hands"} {
		if _, ok := d.Moves[id]; ok {
			t.Errorf("%s is in the dataset; it has no singles behavior and should be denylisted", id)
		}
	}
	// The two that were already denylisted, as the control.
	for _, id := range []string{"helping-hand", "dragon-cheer"} {
		if _, ok := d.Moves[id]; ok {
			t.Errorf("control: %s should still be denylisted", id)
		}
	}
}
