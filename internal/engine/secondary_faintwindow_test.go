// Regression: a foe-targeted secondary must not land on a target the same hit
// already reduced to 0 HP. applyDamageEffects runs inside turn.go's faint
// window, where the victim still has Fainted == false and HP == 0, so the
// guard has to test the HP via isDown() — checking the flag alone is what let
// this through. Found by the tournament's referee agents in two separate
// matches; before the fix this reported 42 paralysis applications out of 400
// seeds, exactly Thunderbolt's 10% secondary rate.

package engine

import (
	"strings"
	"testing"
)

func TestSecondaryDoesNotLandOnDyingTarget(t *testing.T) {
	d := loadDex(t)
	fired, kills := 0, 0
	for seed := 1; seed <= 400; seed++ {
		s, err := NewBattle(d, "b", "A", []int{135, 6}, "B", []int{36, 143}, uint64(seed))
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item, s.Active(1).Item = ItemNone, ItemNone
		s.Active(0).Moves = []MoveSlot{{MoveID: "thunderbolt", PP: 20, MaxPP: 20}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).HP = 1 // guarantees the Thunderbolt is lethal
		log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		var txt []string
		for _, l := range log {
			txt = append(txt, l.Text)
		}
		j := strings.Join(txt, " | ")
		if strings.Contains(j, "fainted") {
			kills++
		}
		if strings.Contains(j, "paralyzed") {
			fired++
			if fired == 1 {
				t.Logf("seed %d: %s", seed, j)
			}
		}
	}
	if kills != 400 {
		t.Fatalf("probe is not exercising the path: %d/400 lethal hits", kills)
	}
	if fired > 0 {
		t.Errorf("secondary fired on a target killed by the same hit (%d/400)", fired)
	}
}
