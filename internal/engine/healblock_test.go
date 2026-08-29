package engine

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// healblock_test.go covers the Heal Block volatile, which arrives in this
// dataset by exactly one route: Psychic Noise's 100%-chance secondary. Heal
// Block itself is not curated — no species learns it — so the five-turn
// duration is unreachable and only Psychic Noise's two turns can be observed.
//
// The reason this needed eight guard sites rather than one is that five heal
// paths never touch healPokemon: the item heals, the ability heals, Regenerator,
// Grassy Terrain, the Leech Seed drain and the ring heals all add HP directly.
// Guarding the choke point alone would have left a blocked Pokémon quietly
// topping itself up on Leftovers.

func hbBattle(t *testing.T, d *domain.Dex) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "hb", "P1", []int{143, 65}, "P2", []int{143, 65}, 17)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			p.Item, p.Ability = ItemNone, AbilityNone
			p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		}
	}
	return s
}

// TestPsychicNoiseBlocksHealing: the ported case, end to end. The volatile
// arrives through the move's secondary, which means it comes through the data
// pipeline rather than a hard-coded branch — and therefore Shield Dust and a
// Covert Cloak refuse it, like any other foe-facing secondary.
func TestPsychicNoiseBlocksHealing(t *testing.T) {
	d := loadDex(t)
	s := hbBattle(t, d)
	s.Active(0).Moves = []MoveSlot{{MoveID: "psychic-noise", PP: 10, MaxPP: 10}}
	foe := s.Active(1)
	foe.HP = foe.MaxHP / 2

	playTurn(d, s, 0, 0)
	if foe.Volatiles.HealBlock == nil {
		t.Fatalf("Psychic Noise should have applied Heal Block")
	}
	// One tick has already run: the block is applied during the turn and the
	// end-of-turn sweep decrements it in the same turn, which is what canon's
	// Residual event does to a duration set earlier in the turn. So the two
	// turns are "the turn it lands and the one after".
	if got, want := foe.Volatiles.HealBlock.Turns, psychicNoiseHealBlockTurns-1; got != want {
		t.Errorf("after the applying turn the block should have %d turns left, got %d", want, got)
	}
}

func TestPsychicNoiseIsRefusedByShieldDust(t *testing.T) {
	d := loadDex(t)
	s := hbBattle(t, d)
	s.Active(0).Moves = []MoveSlot{{MoveID: "psychic-noise", PP: 10, MaxPP: 10}}
	s.Active(1).Ability = "shield-dust"

	playTurn(d, s, 0, 0)
	if s.Active(1).Volatiles.HealBlock != nil {
		t.Error("Heal Block rides a secondary, so Shield Dust should refuse it")
	}
}

// TestHealBlockStopsEveryHealPath: one assertion per site that does not go
// through healPokemon, which is the reason the guard is not a single check.
func TestHealBlockStopsEveryHealPath(t *testing.T) {
	d := loadDex(t)

	t.Run("a healing move", func(t *testing.T) {
		s := hbBattle(t, d)
		p := s.Active(0)
		p.Moves = []MoveSlot{{MoveID: "recover", PP: 10, MaxPP: 10}}
		p.HP = p.MaxHP / 2
		p.Volatiles.HealBlock = &HealBlockState{Turns: 2}
		before := p.HP
		log := playTurn(d, s, 0, 0)
		if p.HP != before {
			t.Errorf("Recover should have been refused, %d -> %d", before, p.HP)
		}
		if !logHas(log, "Heal Block") {
			t.Errorf("the refusal should name the block, got %v", logTexts(log))
		}
		// The PP is spent, and that is canon rather than a shortcut: Heal Block
		// keeps the move off the menu through onDisableMove, and the
		// resolve-time refusal is onBeforeMove, which runs after deductPP. Same
		// as every other lock-restrict refusal in this engine.
		if got := p.Moves[0].PP; got != 9 {
			t.Errorf("a Heal Blocked move still pays its PP, got %d left", got)
		}
	})

	t.Run("Leftovers", func(t *testing.T) {
		s := hbBattle(t, d)
		p := s.Active(0)
		p.Item = "leftovers"
		p.HP = p.MaxHP / 2
		p.Volatiles.HealBlock = &HealBlockState{Turns: 2}
		before := p.HP
		var log []LogLine
		applyItemEndOfTurn(s, 0, &log)
		if p.HP != before {
			t.Errorf("Leftovers should have been refused, %d -> %d", before, p.HP)
		}
		if p.Item != "leftovers" {
			t.Error("and the item is spared, not spent — canon refuses before useItem")
		}
	})

	t.Run("an ability heal", func(t *testing.T) {
		s := hbBattle(t, d)
		p := s.Active(0)
		p.HP = p.MaxHP / 2
		p.Volatiles.HealBlock = &HealBlockState{Turns: 2}
		before := p.HP
		var log []LogLine
		healFraction(p, 0, 1.0/16.0, "Rain Dish", &log)
		if p.HP != before {
			t.Errorf("an ability heal should have been refused, %d -> %d", before, p.HP)
		}
	})

	t.Run("Grassy Terrain", func(t *testing.T) {
		s := hbBattle(t, d)
		s.Terrain = &TerrainState{Kind: TerrainGrassy, TurnsLeft: 5}
		p := s.Active(0)
		p.HP = p.MaxHP / 2
		p.Volatiles.HealBlock = &HealBlockState{Turns: 2}
		before := p.HP
		var log []LogLine
		applyTerrainResidual(s, 0, &log)
		if p.HP != before {
			t.Errorf("Grassy Terrain should have been refused, %d -> %d", before, p.HP)
		}
	})

	t.Run("Aqua Ring", func(t *testing.T) {
		s := hbBattle(t, d)
		p := s.Active(0)
		p.HP = p.MaxHP / 2
		p.Volatiles.AquaRing = true
		p.Volatiles.HealBlock = &HealBlockState{Turns: 2}
		before := p.HP
		var log []LogLine
		applyRingHeals(s, 0, &log)
		if p.HP != before {
			t.Errorf("Aqua Ring should have been refused, %d -> %d", before, p.HP)
		}
	})

	t.Run("the Leech Seed drain", func(t *testing.T) {
		s := hbBattle(t, d)
		seeded, seeder := s.Active(1), s.Active(0)
		seeded.Volatiles.LeechSeed = &LeechSeedState{SourceSide: 0}
		seeder.HP = seeder.MaxHP / 2
		seedBefore, drainBefore := seeded.HP, seeder.HP
		seeder.Volatiles.HealBlock = &HealBlockState{Turns: 2}
		var log []LogLine
		applyLeechSeedResidual(s, 1, &log)
		if seeded.HP >= seedBefore {
			t.Error("the chip still happens — only the drainer's half is refused")
		}
		if seeder.HP != drainBefore {
			t.Errorf("the seeder should not have drained, %d -> %d", drainBefore, seeder.HP)
		}
	})

	t.Run("Regenerator", func(t *testing.T) {
		s := hbBattle(t, d)
		p := s.Active(0)
		p.Ability = "regenerator"
		p.HP = p.MaxHP / 2
		p.Volatiles.HealBlock = &HealBlockState{Turns: 2}
		before := p.HP
		ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
		if s.Sides[0].Team[0].HP != before {
			t.Errorf("Regenerator runs before clearVolatile, so the block still applies: %d -> %d",
				before, s.Sides[0].Team[0].HP)
		}
	})
}

// TestHealBlockKeepsHealingMovesOffTheMenu: Gen 6+ refuses the move rather than
// letting it resolve and heal nothing.
func TestHealBlockKeepsHealingMovesOffTheMenu(t *testing.T) {
	d := loadDex(t)
	s := hbBattle(t, d)
	act := s.Active(0)
	act.Moves = []MoveSlot{
		{MoveID: "recover", PP: 10, MaxPP: 10},
		{MoveID: "tackle", PP: 35, MaxPP: 35},
	}
	act.Volatiles.HealBlock = &HealBlockState{Turns: 2}

	for _, a := range LegalActionsDex(d, s, 0) {
		if a.Kind == ActionMove && a.Index == 0 {
			t.Error("Recover should not be offered under a Heal Block")
		}
	}
}

// TestHealBlockExpires: two turns from Psychic Noise, and healing works again.
func TestHealBlockExpires(t *testing.T) {
	d := loadDex(t)
	s := hbBattle(t, d)
	p := s.Active(0)
	p.HP = p.MaxHP / 2
	p.Volatiles.HealBlock = &HealBlockState{Turns: 2}

	playTurn(d, s, 0, 0)
	if p.Volatiles.HealBlock == nil {
		t.Fatalf("a two-turn block should survive its first tick")
	}
	log := playTurn(d, s, 0, 0)
	if p.Volatiles.HealBlock != nil {
		t.Error("the block should have worn off")
	}
	if !logHas(log, "Heal Block wore off") {
		t.Errorf("the expiry should be announced, got %v", logTexts(log))
	}
}
