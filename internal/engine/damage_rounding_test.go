package engine

import (
	"sort"
	"testing"
)

// showdownRolls is an independent transcription of Showdown's damage chain for
// the plain case: no weather, terrain, screens, crit, abilities or items. It is
// deliberately written from the reference rather than factored out of
// computeDamage — a test that reuses the code under test proves only that the
// code equals itself, which is exactly how the old single-floor formula stayed
// green while rolling one point over the cartridge maximum.
//
// The chain, step for step:
//
//	base = tr(tr(tr(tr(2L/5 + 2) * bp * A) / D) / 50)
//	dmg  = base + 2
//	dmg  = tr(tr(dmg * roll) / 100)      // roll 85..100
//	dmg  = modify(dmg, stab)             // tr((dmg*mod + 2047) / 4096)
//	dmg  = type doublings / halvings, each truncated
func showdownRolls(bp, atk, def int, stab bool, eff float64) []int {
	base := (2*Level/5 + 2) * bp * atk / def / 50
	out := make([]int, 0, 16)
	for roll := 85; roll <= 100; roll++ {
		d := base + 2
		d = d * roll / 100
		if stab {
			d = (d*6144 + 2047) >> 12 // modify(d, 1.5)
		}
		e := eff
		for e >= 2 {
			d *= 2
			e /= 2
		}
		for e > 0 && e <= 0.5 {
			d /= 2
			e *= 2
		}
		if d < 1 {
			d = 1
		}
		out = append(out, d)
	}
	return out
}

// TestDamageMatchesShowdownRoundingChain pins OPEN-3: damage is carried as an
// integer and truncated at every modifier boundary, the way Showdown does it,
// rather than kept in full precision and floored once at the end.
//
// The old formula multiplied twelve floats together and applied a single
// math.Floor. Per hit the error is small; systematically it is not, and it is
// signed — it can only ever produce *more* damage than the cartridge would,
// which is how an Air Slash rolled 86 on a Gengar whose maximum is 85. Crossing
// a KO threshold in the wrong direction is a real difference in outcome, not a
// cosmetic one.
//
// The assertion is set equality across the whole 85..100 roll spread, not a
// single sample: an off-by-one in the rounding rule shows up on some rolls and
// not others, so checking one roll would pass a broken chain most of the time.
func TestDamageMatchesShowdownRoundingChain(t *testing.T) {
	d := loadDex(t)

	cases := []struct {
		name       string
		atkSpecies int
		defSpecies int
		move       string
	}{
		{"neutral, no STAB", 143, 143, "tackle"},
		{"STAB", 6, 143, "flamethrower"},
		{"super effective", 6, 3, "flamethrower"},
		{"resisted", 6, 6, "flamethrower"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, ok := d.Moves[tc.move]
			if !ok {
				t.Fatalf("move %q missing from dex", tc.move)
			}
			atk := buildPokemon(d, d.Species[tc.atkSpecies])
			def := buildPokemon(d, d.Species[tc.defSpecies])
			// Strip everything the plain reference does not model. Shell Armor
			// blocks crits outright, so the observed spread is the roll spread
			// and nothing else.
			atk.Ability, atk.Item = AbilityNone, ItemNone
			def.Ability, def.Item = "shell-armor", ItemNone

			a, dd := offensiveDefensiveStats(&atk, &def, m, nil)
			dd *= defenseMult(nil, &def, m.Category)
			ai, di := int(a), int(dd)
			stab := m.Type == atk.Type1 || m.Type == atk.Type2
			eff := d.Effectiveness(m.Type, def.Type1, def.Type2)
			if eff == 0 {
				t.Fatalf("test case %q is an immunity; pick another pair", tc.name)
			}
			want := showdownRolls(m.Power, ai, di, stab, eff)

			// Sweep seeds until every roll has been observed. 4000 is far more
			// than the coupon-collector expectation for 16 buckets and keeps
			// the test from being flaky if the stream is unlucky.
			seen := map[int]bool{}
			for seed := uint64(1); seed <= 4000; seed++ {
				rng := NewRNG(seed)
				res := computeDamage(d, &atk, &def, m, nil, nil, nil, nil, rng)
				if res.Crit {
					t.Fatalf("Shell Armor should have blocked every crit; got one at seed %d", seed)
				}
				seen[res.Damage] = true
			}

			got := make([]int, 0, len(seen))
			for v := range seen {
				got = append(got, v)
			}
			sort.Ints(got)
			wantSet := map[int]bool{}
			for _, v := range want {
				wantSet[v] = true
			}
			wantUniq := make([]int, 0, len(wantSet))
			for v := range wantSet {
				wantUniq = append(wantUniq, v)
			}
			sort.Ints(wantUniq)

			if len(got) != len(wantUniq) {
				t.Fatalf("damage spread = %v, want %v (A=%d D=%d bp=%d stab=%v eff=%v)",
					got, wantUniq, ai, di, m.Power, stab, eff)
			}
			for i := range got {
				if got[i] != wantUniq[i] {
					t.Fatalf("damage spread = %v, want %v (A=%d D=%d bp=%d stab=%v eff=%v)",
						got, wantUniq, ai, di, m.Power, stab, eff)
				}
			}
		})
	}
}

// TestDamageNeverExceedsTheMaximumRoll is the property the old formula broke,
// stated directly: nothing the engine rolls may come out above the damage of a
// 100% roll. Under float math the twelve-way product could land a hair over the
// value the same expression produced at roll 100 once floored, which is what
// "one or two points above the canonical maximum" meant in practice.
func TestDamageNeverExceedsTheMaximumRoll(t *testing.T) {
	d := loadDex(t)
	m, ok := d.Moves["air-slash"]
	if !ok {
		t.Fatal("air-slash missing from dex")
	}
	atk := buildPokemon(d, d.Species[6])
	def := buildPokemon(d, d.Species[94]) // Gengar, the roll that filed OPEN-3
	atk.Ability, atk.Item = AbilityNone, ItemNone
	def.Ability, def.Item = "shell-armor", ItemNone

	a, dd := offensiveDefensiveStats(&atk, &def, m, nil)
	dd *= defenseMult(nil, &def, m.Category)
	stab := m.Type == atk.Type1 || m.Type == atk.Type2
	eff := d.Effectiveness(m.Type, def.Type1, def.Type2)
	rolls := showdownRolls(m.Power, int(a), int(dd), stab, eff)
	max := rolls[len(rolls)-1]

	for seed := uint64(1); seed <= 4000; seed++ {
		res := computeDamage(d, &atk, &def, m, nil, nil, nil, nil, NewRNG(seed))
		if res.Damage > max {
			t.Fatalf("seed %d rolled %d, above the 100%% roll of %d", seed, res.Damage, max)
		}
	}
}
