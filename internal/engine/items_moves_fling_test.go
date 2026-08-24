package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// items_moves_fling_test.go covers the rider a thrown item carries beyond its
// base power. Before these existed a thrown Flame Orb logged "used up its Flame
// Orb!", dealt 30 base power and inflicted nothing — the move's entire reason
// for existing on an orb user quietly absent.

func flingBattle(t *testing.T, d *domain.Dex, item ItemKind) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "fl", "P1", []int{143}, "P2", []int{143}, 9)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		p := &s.Sides[i].Team[0]
		p.Item, p.Ability = ItemNone, AbilityNone
		p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	}
	s.Active(0).Item = item
	s.Active(0).Moves = []MoveSlot{{MoveID: "fling", PP: 10, MaxPP: 10}}
	return s
}

// TestFlingDeliversTheStatusRiders: the three orbs and the barb.
func TestFlingDeliversTheStatusRiders(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		item ItemKind
		want StatusCond
	}{
		{ItemFlameOrb, StatusBurn},
		{ItemToxicOrb, StatusToxic},
		{ItemPoisonBarb, StatusPoison},
	} {
		s := flingBattle(t, d, c.item)
		playTurn(d, s, 0, 0)
		if got := s.Active(1).Status; got != c.want {
			t.Errorf("a thrown %s should leave the target %q, got %q", c.item, c.want, got)
		}
		if s.Active(0).Item != ItemNone {
			t.Errorf("%s: the thrown item should be gone", c.item)
		}
	}
}

// TestFlingDeliversTheFlinchRiders: King's Rock and Razor Fang. A flinch is
// only worth anything against a target that has yet to move, so the thrower is
// an Alakazam and the flinch is measured by the Swords Dance that never
// happens — reading the volatile after the turn would find it already swept.
func TestFlingDeliversTheFlinchRiders(t *testing.T) {
	d := loadDex(t)
	for _, item := range []ItemKind{ItemKingsRock, ItemRazorFang} {
		s, err := NewBattle(d, "fl", "P1", []int{65}, "P2", []int{143}, 9)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		for i := range s.Sides {
			p := &s.Sides[i].Team[0]
			p.Item, p.Ability = ItemNone, AbilityNone
		}
		s.Active(0).Item = item
		s.Active(0).Moves = []MoveSlot{{MoveID: "fling", PP: 10, MaxPP: 10}}
		foe := s.Active(1)
		foe.Moves = []MoveSlot{{MoveID: "swords-dance", PP: 20, MaxPP: 20}}

		log := playTurn(d, s, 0, 0)
		if !logHas(log, "flinched") {
			t.Errorf("a thrown %s should make the target flinch, got %v", item, logTexts(log))
		}
		if foe.Stages.Atk != 0 {
			t.Errorf("a flinched target should not have got its Swords Dance off, got %+v",
				foe.Stages)
		}
	}
}

// TestFlingStatusRidersAreSecondaries: upstream pushes them onto
// move.secondaries rather than running them as an on-hit effect, so Shield Dust
// refuses them. Getting this wrong would make Fling the one way to put a burn
// through a Shield Dust holder.
func TestFlingStatusRidersAreSecondaries(t *testing.T) {
	d := loadDex(t)
	s := flingBattle(t, d, ItemFlameOrb)
	s.Active(1).Ability = "shield-dust"
	playTurn(d, s, 0, 0)
	if got := s.Active(1).Status; got != StatusNone {
		t.Errorf("Shield Dust should refuse a thrown Flame Orb's burn, got %q", got)
	}
}

// TestFlingHerbsLandOnTheTarget: the herbs are not secondaries, and they apply
// to whoever was hit. Throwing a White Herb at a foe clears the *foe's* drops.
// That is a genuinely bad play and it is what canon does — upstream assigns
// move.onHit directly from item.fling.effect, and onHit's first argument is the
// target.
func TestFlingHerbsLandOnTheTarget(t *testing.T) {
	d := loadDex(t)
	s := flingBattle(t, d, ItemWhiteHerb)
	foe := s.Active(1)
	foe.Stages = Stages{Atk: -2, Def: -1}
	playTurn(d, s, 0, 0)
	if foe.Stages != (Stages{}) {
		t.Errorf("a thrown White Herb should clear the target's drops, got %+v", foe.Stages)
	}

	m := flingBattle(t, d, ItemMentalHerb)
	tgt := m.Active(1)
	tgt.Volatiles.Taunt = &TauntState{Turns: 3}
	playTurn(d, m, 0, 0)
	if tgt.Volatiles.Taunt != nil {
		t.Error("a thrown Mental Herb should free the target from its Taunt")
	}
}

// TestFlingWithoutARiderJustHits: the ordinary case still has to work. An item
// with no rider deals its base power and nothing else.
func TestFlingWithoutARiderJustHits(t *testing.T) {
	d := loadDex(t)
	s := flingBattle(t, d, ItemIronBall)
	foe := s.Active(1)
	before := foe.HP
	playTurn(d, s, 0, 0)
	if foe.HP >= before {
		t.Error("a thrown Iron Ball should still deal damage")
	}
	if foe.Status != StatusNone {
		t.Errorf("an Iron Ball carries no status rider, got %q", foe.Status)
	}
}
