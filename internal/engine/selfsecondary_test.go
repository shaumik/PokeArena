package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// selfBoost is a synthetic 100%-chance secondary aimed at the user, so the
// tests below don't depend on any particular move's roll.
func selfBoostMove(stat string) domain.Move {
	return domain.Move{
		Name: "test-self-boost", Type: "normal", Category: domain.CatPhysical,
		Power: 50, Accuracy: 100, Target: domain.TargetFoe,
		Secondaries: []domain.Effect{{Self: true, Chance: 100, Boosts: map[string]int{stat: 1}}},
	}
}

// TestSelfSecondaryBoostsTheUser is the core of the fix: a secondary carrying
// Self applies to the attacker, not the foe. Before it shipped, the payload
// was dropped in the data pipeline and the effect rolled into nothing.
func TestSelfSecondaryBoostsTheUser(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{3}, 1)
	var log []LogLine
	applyDamageEffects(s, 0, selfBoostMove("speed"), 1, NewRNG(1), &log)

	if got := s.Active(0).Stages.Spe; got != 1 {
		t.Errorf("user Speed stage = %d, want 1", got)
	}
	if got := s.Active(1).Stages.Spe; got != 0 {
		t.Errorf("foe Speed stage = %d, want 0 — the boost went to the wrong side", got)
	}
}

// TestRapidSpinRaisesSpeed: the reported symptom. Rapid Spin's +1 Speed rides
// its 100% self-secondary; hazard removal is separate and hand-coded.
func TestRapidSpinRaisesSpeed(t *testing.T) {
	d := loadDex(t)
	m, ok := d.Moves["rapid-spin"]
	if !ok {
		t.Skip("rapid-spin not in dataset")
	}
	if len(m.Secondaries) != 1 || !m.Secondaries[0].Self || m.Secondaries[0].Boosts["speed"] != 1 {
		t.Fatalf("rapid-spin should carry a self +1 Speed secondary, got %+v", m.Secondaries)
	}
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{3}, 1)
	var log []LogLine
	applyDamageEffects(s, 0, m, 1, NewRNG(1), &log)
	if got := s.Active(0).Stages.Spe; got != 1 {
		t.Errorf("Rapid Spin should leave the user at +1 Speed, got %d", got)
	}
}

// TestSelfSecondaryDatasetCoverage: every curated move whose upstream
// definition carries a self-targeted secondary has to arrive with a payload.
// A bare {"chance": N} is the exact shape the bug produced — it rolls and
// then does nothing, which reads as working in a battle log.
func TestSelfSecondaryDatasetCoverage(t *testing.T) {
	d := loadDex(t)
	want := map[string]string{
		"rapid-spin":     "speed",
		"power-up-punch": "attack",
		"flame-charge":   "speed",
		"trailblaze":     "speed",
		"charge-beam":    "spatk",
		"metal-claw":     "attack",
		"meteor-mash":    "attack",
		"steel-wing":     "defense",
		"ancient-power":  "attack",
		"silver-wind":    "attack",
		"ominous-wind":   "attack",
	}
	for id, stat := range want {
		m, ok := d.Moves[id]
		if !ok {
			continue // not every move survives the curation filter
		}
		if len(m.Secondaries) == 0 {
			t.Errorf("%s: no secondaries at all", id)
			continue
		}
		found := false
		for _, sec := range m.Secondaries {
			if sec.Self && sec.Boosts[stat] > 0 {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: expected a self secondary boosting %s, got %+v", id, stat, m.Secondaries)
		}
	}
}

// TestSelfSecondarySurvivesShieldDust: Shield Dust and Covert Cloak refuse
// added effects aimed *at the holder*. A boost the attacker points at itself
// is none of their business — canon filters on the flag, not on the move.
func TestSelfSecondarySurvivesShieldDust(t *testing.T) {
	d := loadDex(t)
	for _, blocker := range []struct {
		name  string
		apply func(*Pokemon)
	}{
		{"shield-dust", func(p *Pokemon) { p.Ability = "shield-dust" }},
		{"covert-cloak", func(p *Pokemon) { p.Item = "covert-cloak" }},
	} {
		t.Run(blocker.name, func(t *testing.T) {
			s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{3}, 1)
			blocker.apply(s.Active(1))
			var log []LogLine
			applyDamageEffects(s, 0, selfBoostMove("attack"), 1, NewRNG(1), &log)
			if got := s.Active(0).Stages.Atk; got != 1 {
				t.Errorf("%s should not reach the attacker's own boost; user Attack stage = %d, want 1", blocker.name, got)
			}
		})
	}
}

// TestShieldDustStillBlocksFoeSecondaries: the per-entry gate must not have
// let foe-targeted secondaries through as a side effect.
func TestShieldDustStillBlocksFoeSecondaries(t *testing.T) {
	d := loadDex(t)
	burnRider := domain.Move{
		Name: "test-burn-rider", Type: "fire", Category: domain.CatSpecial, Power: 50, Accuracy: 100,
		Secondaries: []domain.Effect{{Chance: 100, Status: "burn"}},
	}
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{3}, 1)
	s.Active(1).Ability = "shield-dust"
	var log []LogLine
	applyDamageEffects(s, 0, burnRider, 1, NewRNG(1), &log)
	if s.Active(1).Status == StatusBurn {
		t.Error("Shield Dust should still refuse a foe-targeted burn secondary")
	}
}

// TestSheerForceSuppressesSelfSecondary: Sheer Force trades away *every*
// secondary for its damage boost, the user's own included. That's the one
// blocker that does reach a self secondary.
func TestSheerForceSuppressesSelfSecondary(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{3}, 1)
	s.Active(0).Ability = "sheer-force"
	var log []LogLine
	applyDamageEffects(s, 0, selfBoostMove("attack"), 1, NewRNG(1), &log)
	if got := s.Active(0).Stages.Atk; got != 0 {
		t.Errorf("Sheer Force should suppress the user's own secondary too; Attack stage = %d, want 0", got)
	}
}

// TestSelfSecondarySkipsFaintedUser: a contact-reactive item (Rocky Helmet,
// Jaboca Berry) can KO the attacker inside dealDamage, before the secondaries
// run. Boosting a corpse leaves stages on a fainted Pokémon.
func TestSelfSecondarySkipsFaintedUser(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{3}, 1)
	s.Active(0).HP = 0
	s.Active(0).Fainted = true
	var log []LogLine
	applyDamageEffects(s, 0, selfBoostMove("attack"), 1, NewRNG(1), &log)
	if got := s.Active(0).Stages.Atk; got != 0 {
		t.Errorf("a fainted user should not take its own boost; Attack stage = %d", got)
	}
}

// TestSelfSecondaryIgnoresFoeSubstitute: the doll sits between the attacker
// and the foe, not between the attacker and itself.
func TestSelfSecondaryIgnoresFoeSubstitute(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{3}, 1)
	s.Active(1).Volatiles.Substitute = &SubstituteState{HP: 50}
	var log []LogLine
	applyDamageEffects(s, 0, selfBoostMove("attack"), 1, NewRNG(1), &log)
	if got := s.Active(0).Stages.Atk; got != 1 {
		t.Errorf("the foe's Substitute should not block the attacker's own boost; Attack stage = %d, want 1", got)
	}
}
