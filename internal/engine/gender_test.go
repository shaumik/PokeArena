package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// Dex numbers used below: Snorlax (both genders, 87.5% male), Nidoqueen
// (female only), Nidoking (male only), Mewtwo and Magneton (genderless).
const (
	nidoqueenDex = 31
	nidokingDex  = 34
	magnetonDex  = 82
	mewtwoDex    = 150
)

func attractLands(t *testing.T, d *domain.Dex, userGender, targetGender string) bool {
	t.Helper()
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Gender = userGender
	s.Active(1).Gender = targetGender
	var log []LogLine
	applyAttractVolatile(s.Active(1), 1, d.Moves["attract"], s, NewRNG(1), &log)
	return s.Active(1).Volatiles.Attract
}

// TestAttractNeedsOppositeGenders is the fix. Before it, Attract landed on
// anything — genderless legendaries, same-sex targets, everything — at 100%
// accuracy, through Substitute, on 73 of the 80 curated species. Stacked with
// paralysis and confusion that approaches a 75% do-nothing rate on the
// opponent, which would warp any competitive use of the arena.
func TestAttractNeedsOppositeGenders(t *testing.T) {
	d := loadDex(t)
	M, F, N := domain.GenderMale, domain.GenderFemale, domain.GenderGenderless
	cases := []struct {
		user, target string
		want         bool
	}{
		{M, F, true},
		{F, M, true},
		{M, M, false},
		{F, F, false},
		{N, F, false},
		{M, N, false},
		{N, N, false},
		{"", F, false},
		{M, "", false},
	}
	for _, c := range cases {
		if got := attractLands(t, d, c.user, c.target); got != c.want {
			t.Errorf("Attract from %q onto %q = %v, want %v", c.user, c.target, got, c.want)
		}
	}
}

// TestGenderlessSpeciesAreImmuneToAttract: the dataset half of the same
// rule — a genderless species is genderless without anyone setting a field.
func TestGenderlessSpeciesAreImmuneToAttract(t *testing.T) {
	d := loadDex(t)
	for _, dexNo := range []int{mewtwoDex, magnetonDex} {
		sp, ok := d.Species[dexNo]
		if !ok {
			continue
		}
		if got := sp.DefaultGender(); got != domain.GenderGenderless {
			t.Fatalf("%s should be genderless, got %q", sp.Name, got)
		}
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{dexNo}, 1)
		s.Active(0).Gender = domain.GenderFemale
		var log []LogLine
		applyAttractVolatile(s.Active(1), 1, d.Moves["attract"], s, NewRNG(1), &log)
		if s.Active(1).Volatiles.Attract {
			t.Errorf("%s is genderless and should not be infatuated", sp.Name)
		}
	}
}

// TestFixedGenderSpeciesNeedNoRoll: Nidoqueen is always female and Nidoking
// always male, straight out of the team build, with no roll involved.
func TestFixedGenderSpeciesNeedNoRoll(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		dexNo int
		want  string
	}{
		{nidoqueenDex, domain.GenderFemale},
		{nidokingDex, domain.GenderMale},
	} {
		sp, ok := d.Species[c.dexNo]
		if !ok {
			continue
		}
		// Across many seeds it never moves — a fixed gender is not rollable.
		for seed := uint64(1); seed < 40; seed++ {
			s, _ := NewBattle(d, "b", "P1", []int{c.dexNo}, "P2", []int{143}, seed)
			if got := s.Active(0).Gender; got != c.want {
				t.Fatalf("%s at seed %d: gender %q, want %q", sp.Name, seed, got, c.want)
			}
		}
	}
}

// TestGenderRollIsSeededAndMixed: an unpicked gender is rolled from the
// battle seed. Same seed, same answer; across seeds a two-gender species
// produces both, so a team built without thinking about gender comes out
// mixed instead of uniform.
func TestGenderRollIsSeededAndMixed(t *testing.T) {
	d := loadDex(t)
	team := []int{143, 3, 6, 9, 91, 112}
	first, err := NewBattle(d, "b", "P1", team, "P2", []int{143}, 7)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	again, err := NewBattle(d, "b", "P1", team, "P2", []int{143}, 7)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range first.Sides[0].Team {
		if a, b := first.Sides[0].Team[i].Gender, again.Sides[0].Team[i].Gender; a != b {
			t.Fatalf("slot %d: same seed gave %q then %q — the roll must be deterministic", i, a, b)
		}
		if first.Sides[0].Team[i].Gender == "" {
			t.Fatalf("slot %d came out with no gender at all", i)
		}
	}

	seen := map[string]bool{}
	for seed := uint64(1); seed < 200; seed++ {
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, seed)
		seen[s.Active(0).Gender] = true
	}
	if !seen[domain.GenderMale] || !seen[domain.GenderFemale] {
		t.Errorf("a two-gender species should roll both across seeds, saw %v", seen)
	}
}

// TestGenderRollDoesNotDisturbTheBattleRNG: the roll runs off its own stream.
// Drawing from RNGState would shift every subsequent roll in the battle and
// invalidate every recorded replay.
func TestGenderRollDoesNotDisturbTheBattleRNG(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 42)
	if s.RNGState != 42 {
		t.Errorf("RNGState = %d after construction, want the untouched seed 42", s.RNGState)
	}
}

// TestPickedGenderIsKept: a team that chose a gender keeps it, including
// when the choice happens to match the species' likelier one — the roll must
// not quietly re-decide it.
func TestPickedGenderIsKept(t *testing.T) {
	d := loadDex(t)
	sp := d.Species[143]
	picks := []TeamPick{{DexNo: 143, MoveIDs: []string{sp.Moves[0]}, Gender: domain.GenderFemale}}
	other := []TeamPick{{DexNo: 3, MoveIDs: []string{d.Species[3].Moves[0]}}}
	for seed := uint64(1); seed < 40; seed++ {
		s, err := NewBattleFromPicks(d, "b", "P1", picks, "P2", other, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		if got := s.Active(0).Gender; got != domain.GenderFemale {
			t.Fatalf("seed %d: picked female, got %q", seed, got)
		}
	}

	// And the likelier gender, which is also the pre-roll default — the case
	// a naive "did it change from the default?" check would get wrong.
	picks[0].Gender = domain.GenderMale
	for seed := uint64(1); seed < 40; seed++ {
		s, _ := NewBattleFromPicks(d, "b", "P1", picks, "P2", other, seed)
		if got := s.Active(0).Gender; got != domain.GenderMale {
			t.Fatalf("seed %d: picked male, got %q", seed, got)
		}
	}
}

// TestValidateTeamRefusesImpossibleGenders: a female Nidoking or a gendered
// Magneton is not a legal pick.
func TestValidateTeamRefusesImpossibleGenders(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		name   string
		dexNo  int
		gender string
		ok     bool
	}{
		{"male Nidoking", nidokingDex, domain.GenderMale, true},
		{"female Nidoking", nidokingDex, domain.GenderFemale, false},
		{"female Nidoqueen", nidoqueenDex, domain.GenderFemale, true},
		{"male Nidoqueen", nidoqueenDex, domain.GenderMale, false},
		{"female Mewtwo", mewtwoDex, domain.GenderFemale, false},
		{"genderless Mewtwo", mewtwoDex, domain.GenderGenderless, true},
		{"genderless Snorlax", 143, domain.GenderGenderless, false},
		{"female Snorlax", 143, domain.GenderFemale, true},
		{"nonsense", 143, "other", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			sp, ok := d.Species[c.dexNo]
			if !ok {
				t.Skip("species not in dataset")
			}
			err := validateGender(1, sp, c.gender)
			if (err == nil) != c.ok {
				t.Errorf("validateGender(%s, %q) err = %v, want ok=%v", sp.Name, c.gender, err, c.ok)
			}
		})
	}
	// An unset gender is always legal — it means "let the battle decide".
	if err := validateGender(1, d.Species[143], ""); err != nil {
		t.Errorf("an unset gender should be legal, got %v", err)
	}
}

// TestRivalryScalesByGender: the ability was registered but inert, blocked
// on gender. ×1.25 into the same gender, ×0.75 into the opposite, and
// untouched when either side is genderless.
func TestRivalryScalesByGender(t *testing.T) {
	d := loadDex(t)
	def := buildPokemon(d, d.Species[143])
	m := d.Moves["tackle"]

	base := buildPokemon(d, d.Species[143])
	base.Ability = ""
	base.Gender = domain.GenderMale
	def.Gender = domain.GenderMale
	plain := ExpectedDamage(d, &base, &def, m, nil, nil, nil)

	rival := base
	rival.Ability = "rivalry"

	// Rivalry is a base-power handler upstream (`onBasePower`, priority 24), so
	// the ×1.25 and ×0.75 land on base power rather than on the finished damage
	// figure. That makes the comparison against the plain figure exact rather
	// than approximate — and the ratio on the *damage* is not 1.25 or 0.75 at
	// all once the formula's truncations have had their say, which is why the
	// old "want ~0.75x" band went red on a correct engine. The honest statement
	// is that Rivalry is worth exactly a base power of modify(power, mod).
	bpWith := func(num int) int { return (m.Power*num + 2047) >> 12 }
	damageAtPower := func(power int) int {
		boosted := m
		boosted.Power = power
		return ExpectedDamage(d, &base, &def, boosted, nil, nil, nil)
	}

	same := ExpectedDamage(d, &rival, &def, m, nil, nil, nil)
	if want := damageAtPower(bpWith(5120)); same != want {
		t.Errorf("same gender: %d -> %d, want %d (base power %d → %d)",
			plain, same, want, m.Power, bpWith(5120))
	}

	def.Gender = domain.GenderFemale
	opposite := ExpectedDamage(d, &rival, &def, m, nil, nil, nil)
	if want := damageAtPower(bpWith(3072)); opposite != want {
		t.Errorf("opposite gender: %d -> %d, want %d (base power %d → %d)",
			plain, opposite, want, m.Power, bpWith(3072))
	}

	def.Gender = domain.GenderGenderless
	neutral := ExpectedDamage(d, &rival, &def, m, nil, nil, nil)
	if neutral != plain {
		t.Errorf("genderless target: %d -> %d, want unchanged", plain, neutral)
	}
}

// TestCuteCharmFollowsTheGenderRule: the contact rider infatuates, so it
// needs the same opposite-gender precondition Attract does.
func TestCuteCharmFollowsTheGenderRule(t *testing.T) {
	d := loadDex(t)
	tackle := d.Moves["tackle"]

	infatuated := func(holderGender, attackerGender string) bool {
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
		s.Active(1).Ability = "cute-charm"
		s.Active(1).Gender = holderGender
		s.Active(0).Gender = attackerGender
		// Walk seeds so the 30% roll is not what decides the result.
		for seed := uint64(1); seed < 60; seed++ {
			s.Active(0).Volatiles.Attract = false
			var log []LogLine
			applyOnHit(s, 1, tackle, false, NewRNG(seed), &log)
			if s.Active(0).Volatiles.Attract {
				return true
			}
		}
		return false
	}

	if !infatuated(domain.GenderFemale, domain.GenderMale) {
		t.Error("Cute Charm should infatuate an opposite-gender contact attacker")
	}
	if infatuated(domain.GenderFemale, domain.GenderFemale) {
		t.Error("Cute Charm should not infatuate a same-gender attacker")
	}
	if infatuated(domain.GenderGenderless, domain.GenderMale) {
		t.Error("a genderless Cute Charm holder should infatuate nobody")
	}
	if infatuated(domain.GenderFemale, domain.GenderGenderless) {
		t.Error("a genderless attacker should not be infatuated")
	}
}
