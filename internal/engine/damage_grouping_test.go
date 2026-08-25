package engine

import (
	"sort"
	"testing"

	"pokearena/internal/domain"
)

// damage_grouping_test.go pins *which group* each damage modifier belongs to.
//
// Showdown applies modifiers in three groups, and the group decides where the
// number gets truncated:
//
//  1. Base-power group — `runEvent('BasePower')` chains every `onBasePower`
//     handler into one modifier and applies it to base power *before* the
//     damage formula runs.
//  2. Stat group — `onModify{Atk,SpA,Def,SpD}`, applied to the stat the
//     formula reads, after the stat stage has already been floored.
//  3. Final group — `runEvent('ModifyDamage')`, after weather, crit, the
//     random roll, STAB, type effectiveness and burn.
//
// Two things follow from putting a base-power modifier in the final group, and
// they are why this is a wrong number rather than merely a wrong shape:
//
//   - **The `+2` gets boosted.** The final group multiplies the finished
//     figure, which includes the `+2` from `base + 2`. Canon adds that after
//     the base-power boost, so it is never scaled. Everything misplaced this
//     way is systematically a little high.
//   - **Base power never truncates to a whole number.** Canon rounds base
//     power to an integer before the formula sees it. Misplaced, the boost
//     rides all the way through STAB and type effectiveness before a single
//     rounding, so the error compounds with effectiveness instead of staying
//     flat.
//
// Forty of the forty-eight modifiers this engine models sat in the wrong group
// with no test that would have noticed, which is the reason this file states
// the expected damage as an independently derived number rather than a
// relative comparison ("with the item it should be bigger"). A relative
// assertion passes in every group.
//
// The reference below is transcribed from Showdown's source, not factored out
// of computeDamage. A test that reuses the code under test proves only that
// the code equals itself.

// --- an independent transcription of Showdown's fixed-point arithmetic ---

// canonModify is Battle#modify: `tr((tr(value * modifier) + 2048 - 1) / 4096)`.
// The bias is 2047, which rounds a half *down*.
func canonModify(value, mod int) int { return (value*mod + 2047) / 4096 }

// canonChain is Battle#chainModify composed from a starting modifier of 1:
// `M” = ((M * M') + 0x800) >> 12`, rounding half *up* at each pairing.
//
// Chaining and then applying once is not the same as applying each in turn,
// which is exactly why a group is a group. The modifiers are passed in canon's
// handler order (highest `on*Priority` first) because the rounding at each
// pairing makes the order observable.
func canonChain(mods ...int) int {
	m := 4096
	for _, n := range mods {
		m = (m*n + 2048) >> 12
	}
	return m
}

// canonStat is the stat group: Pokemon#calculateStat floors the stat stage
// against the raw stat, and only then does `runEvent('Modify<Stat>')` apply the
// chained stat modifiers.
//
// The stage arithmetic is canon's exactly — `Math.floor(stat * (2+s)/2)` going
// up and `Math.floor(stat / ((2+|s|)/2))` coming down — written as integer
// division so no float ever enters it.
func canonStat(raw, stage int, mods ...int) int {
	s := raw
	switch {
	case stage > 0:
		s = raw * (2 + stage) / 2
	case stage < 0:
		s = raw * 2 / (2 - stage)
	}
	return canonModify(s, canonChain(mods...))
}

// canonHit is one damaging hit, described the way Showdown describes it: a base
// power with its own modifier chain, two stats each with theirs, and a final
// chain applied at the end. Every field is filled in from the reference, never
// from what this engine currently produces.
type canonHit struct {
	basePower int
	bpMods    []int // onBasePower chain, canon priority order

	atkRaw, atkStage int
	atkMods          []int // onModifyAtk / onModifySpA chain
	defRaw, defStage int
	defMods          []int // onModifyDef / onModifySpD chain

	// weatherMod is the WeatherModifyDamage group — its own step between the
	// `+2` and the roll (sun's ×1.5 on Fire, rain's ×0.5 on it). Zero means the
	// identity; it is not part of any of the three groups this file is about,
	// but a case run in weather has to account for it.
	weatherMod int

	stab bool
	eff  float64
	// burn is the physical-attacker burn halve. Canon applies it as a modifier
	// on the damage figure after type effectiveness — not to the Attack stat —
	// and skips it outright for a Guts holder.
	burn bool

	finalMods []int // ModifyDamage chain
}

// rolls returns the sixteen damage figures this hit can produce, one per roll
// in 85..100, with no critical hit.
func (h canonHit) rolls() []int {
	bp := h.basePower
	if bp < 1 {
		bp = 1
	}
	bp = canonModify(bp, canonChain(h.bpMods...))
	if bp < 1 {
		bp = 1
	}

	a := canonStat(h.atkRaw, h.atkStage, h.atkMods...)
	d := canonStat(h.defRaw, h.defStage, h.defMods...)
	if a < 1 {
		a = 1
	}
	if d < 1 {
		d = 1
	}

	base := (2*Level/5 + 2) * bp * a / d / 50

	out := make([]int, 0, 16)
	for roll := 85; roll <= 100; roll++ {
		dmg := base + 2
		if h.weatherMod != 0 {
			dmg = canonModify(dmg, h.weatherMod)
		}
		dmg = dmg * roll / 100
		if h.stab {
			dmg = canonModify(dmg, 6144)
		}
		e := h.eff
		for e >= 2 {
			dmg *= 2
			e /= 2
		}
		for e > 0 && e <= 0.5 {
			dmg /= 2
			e *= 2
		}
		if h.burn {
			dmg = canonModify(dmg, 2048)
		}
		dmg = canonModify(dmg, canonChain(h.finalMods...))
		if dmg < 1 {
			dmg = 1
		}
		out = append(out, dmg)
	}
	return out
}

// uniq is the sorted set of a roll spread. The sixteen rolls collide heavily at
// low damage, so comparing sets rather than lists is the only honest
// comparison against an observed sweep.
func uniq(vs []int) []int {
	seen := map[int]bool{}
	for _, v := range vs {
		seen[v] = true
	}
	out := make([]int, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

// observedSpread sweeps seeds through computeDamage and returns every
// non-critical damage figure it produced. Crits are discarded rather than
// blocked with Shell Armor because half of these cases put an ability on the
// defender that Shell Armor would have to displace.
//
// The sweep is over seeds rather than a chosen one deliberately: an off-by-one
// in a rounding rule shows up on some rolls and not others, so a single seed
// would pass a broken chain most of the time. 4000 is far above the
// coupon-collector expectation for sixteen buckets.
func observedSpread(t *testing.T, d *domain.Dex, atk, def *Pokemon, m domain.Move,
	w *WeatherState, terrain *TerrainState,
) []int {
	t.Helper()
	seen := map[int]bool{}
	for seed := uint64(1); seed <= 4000; seed++ {
		res := computeDamage(d, atk, def, m, w, terrain, nil, nil, NewRNG(seed))
		if res.Crit {
			continue
		}
		seen[res.Damage] = true
	}
	out := make([]int, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func sameSpread(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// groupCase is one modifier, one matchup, and the canonical answer.
type groupCase struct {
	name string
	// what says which canon hook the modifier registers and therefore which
	// group it belongs to. It is the whole content of the assertion.
	what string

	atkDex, defDex int
	move           string

	atkAbility, defAbility AbilityKind
	atkItem, defItem       ItemKind

	weather *WeatherState
	// arrange runs after the two Pokémon are built, for the cases whose
	// condition is a piece of battle state rather than a held item.
	arrange func(atk, def *Pokemon)

	// weatherMod is the WeatherModifyDamage step, for the cases run in weather.
	weatherMod int

	// mods are the modifier numerators canon would chain, in its own priority
	// order, for whichever group this case is about.
	bpMods    []int
	atkMods   []int
	defMods   []int
	finalMods []int
}

func (c groupCase) run(t *testing.T, d *domain.Dex) {
	t.Helper()
	m, ok := d.Moves[c.move]
	if !ok {
		t.Fatalf("move %q is not in the dataset", c.move)
	}
	atk := buildPokemon(d, d.Species[c.atkDex])
	def := buildPokemon(d, d.Species[c.defDex])
	atk.Ability, atk.Item = c.atkAbility, c.atkItem
	def.Ability, def.Item = c.defAbility, c.defItem
	if c.arrange != nil {
		c.arrange(&atk, &def)
	}

	offRaw, offStage := rawStatAndStage(&atk, statForCategory(m.Category, true))
	defRaw, defStage := rawStatAndStage(&def, statForCategory(m.Category, false))

	eff := d.Effectiveness(m.Type, def.Type1, def.Type2)
	if eff == 0 {
		t.Fatalf("%s: %s is an immunity against this defender; pick another pair", c.name, c.move)
	}

	want := uniq(canonHit{
		basePower: m.Power,
		bpMods:    c.bpMods,
		atkRaw:    offRaw, atkStage: offStage, atkMods: c.atkMods,
		defRaw: defRaw, defStage: defStage, defMods: c.defMods,
		weatherMod: c.weatherMod,
		stab:       m.Type == atk.Type1 || m.Type == atk.Type2,
		eff:        eff,
		finalMods:  c.finalMods,
	}.rolls())

	got := observedSpread(t, d, &atk, &def, m, c.weather, nil)
	if !sameSpread(got, want) {
		t.Errorf("%s\n  canon (%s): %v\n  engine:     %v",
			c.name, c.what, want, got)
	}
}

// statForCategory names the raw stat the formula reads for a category. Only
// used to look up the reference's inputs; the moves in this file carry no
// offensive or defensive stat override.
func statForCategory(cat domain.Category, offensive bool) string {
	if cat == domain.CatPhysical {
		if offensive {
			return "attack"
		}
		return "defense"
	}
	if offensive {
		return "spatk"
	}
	return "spdef"
}

// Canon's modifier numerators, spelled as the fractions over 4096 that
// Showdown's data files actually carry. Several of them are *not* the round
// decimal they look like: Muscle Band is 4505/4096, which is 1.09985, and
// rounding 1.1 into 4096ths gives 4506 instead. One point of numerator is one
// point of damage often enough to matter.
const (
	modx1_1MuscleBand    = 4505 // muscleband, wiseglasses
	modx1_1PunchingGlove = 4506 // punchingglove — one more than Muscle Band's
	modx1_2              = 4915 // type boosters, reckless, ironfist, expertbelt
	modx1_25             = 5120 // rivalry (same gender), dryskin
	modx1_3              = 5325 // analytic, sheerforce, sandforce
	modx1_5              = 6144
	modx0_75             = 3072 // rivalry (opposite gender), filter
	modx0_5              = 2048
	modx2                = 8192
)

// TestGroupingReferenceAgreesOnAPlainHit is the control for everything below.
//
// With no ability, no item and no weather there is nothing in any of the three
// groups, so the reference and the engine must already agree — and they do,
// which is what makes a failure in the tests that follow attributable to the
// grouping rather than to a mistranscribed formula. Without this, a reference
// that was simply wrong would fail every case and read exactly like a
// forty-instance bug.
//
// The stat-stage cases are here for the same reason and pin a second thing:
// canon floors the stage against the raw stat (`Math.floor(stat * 1.5)` going
// up, `Math.floor(stat / 1.5)` coming down) before any modifier sees it.
func TestGroupingReferenceAgreesOnAPlainHit(t *testing.T) {
	d := loadDex(t)

	for _, c := range []groupCase{
		{
			name:   "no modifiers at all",
			what:   "every group empty",
			atkDex: 143, defDex: 143, move: "body-slam",
			defAbility: "shell-armor",
		},
		{
			name:   "no modifiers, super effective",
			what:   "every group empty",
			atkDex: 59, defDex: 3, move: "flamethrower", // Arcanine → Venusaur
			defAbility: "shell-armor",
		},
		{
			name:   "a raised Attack stage",
			what:   "calculateStat floors the stage before any modifier",
			atkDex: 143, defDex: 143, move: "body-slam",
			defAbility: "shell-armor",
			arrange:    func(atk, def *Pokemon) { atk.Stages.Atk = 2 },
		},
		{
			name:   "a lowered Attack stage",
			what:   "calculateStat floors the stage before any modifier",
			atkDex: 143, defDex: 143, move: "body-slam",
			defAbility: "shell-armor",
			arrange:    func(atk, def *Pokemon) { atk.Stages.Atk = -3 },
		},
	} {
		t.Run(c.name, func(t *testing.T) { c.run(t, d) })
	}
}

// TestBasePowerGroupModifiersApplyToBasePower is the first half of the
// damage-grouping fix: twenty-nine modifiers whose canon hook is `onBasePower`
// (or `onSourceBasePower`) and which this engine lumped into the final group.
//
// Every case here asserts the exact sixteen-roll spread. That is the point —
// the wrong group produces damage that is *close*, so anything short of the
// exact figure passes either way.
func TestBasePowerGroupModifiersApplyToBasePower(t *testing.T) {
	d := loadDex(t)

	cases := []groupCase{
		{
			name:   "Charcoal on a Fire move",
			what:   "items.ts charcoal registers onBasePower, chainModify([4915,4096])",
			atkDex: 59, defDex: 143, move: "flamethrower", // Arcanine → Snorlax
			atkItem: ItemCharcoal, defAbility: "shell-armor",
			bpMods: []int{modx1_2},
		},
		{
			name:   "Silk Scarf on a Normal move",
			what:   "items.ts silkscarf registers onBasePower",
			atkDex: 143, defDex: 143, move: "body-slam", // Snorlax → Snorlax
			atkItem: ItemSilkScarf, defAbility: "shell-armor",
			bpMods: []int{modx1_2},
		},
		{
			name:   "Muscle Band on a physical move",
			what:   "items.ts muscleband registers onBasePower, chainModify([4505,4096])",
			atkDex: 143, defDex: 143, move: "body-slam",
			atkItem: ItemMuscleBand, defAbility: "shell-armor",
			bpMods: []int{modx1_1MuscleBand},
		},
		{
			name:   "Wise Glasses on a special move",
			what:   "items.ts wiseglasses registers onBasePower, chainModify([4505,4096])",
			atkDex: 65, defDex: 143, move: "psychic", // Alakazam → Snorlax
			atkItem: ItemWiseGlasses, defAbility: "shell-armor",
			bpMods: []int{modx1_1MuscleBand},
		},
		{
			name:   "Punching Glove on a punch",
			what:   "items.ts punchingglove registers onBasePower, chainModify([4506,4096])",
			atkDex: 68, defDex: 143, move: "ice-punch", // Machamp → Snorlax
			atkItem: ItemPunchingGlove, defAbility: "shell-armor",
			bpMods: []int{modx1_1PunchingGlove},
		},
		{
			name:   "Technician on a 60 BP move",
			what:   "abilities.ts technician registers onBasePower, chainModify(1.5)",
			atkDex: 53, defDex: 143, move: "bite", // Persian → Snorlax, 60 BP
			atkAbility: "technician", defAbility: "shell-armor",
			bpMods: []int{modx1_5},
		},
		{
			name:   "Iron Fist on a punch",
			what:   "abilities.ts ironfist registers onBasePower, chainModify([4915,4096])",
			atkDex: 68, defDex: 143, move: "ice-punch",
			atkAbility: "iron-fist", defAbility: "shell-armor",
			bpMods: []int{modx1_2},
		},
		{
			name:   "Reckless on a recoil move",
			what:   "abilities.ts reckless registers onBasePower, chainModify([4915,4096])",
			atkDex: 143, defDex: 143, move: "double-edge",
			atkAbility: "reckless", defAbility: "shell-armor",
			bpMods: []int{modx1_2},
		},
		{
			name:   "Rivalry against the same gender",
			what:   "abilities.ts rivalry registers onBasePower, chainModify(1.25)",
			atkDex: 143, defDex: 143, move: "body-slam",
			atkAbility: "rivalry", defAbility: "shell-armor",
			arrange: func(atk, def *Pokemon) {
				atk.Gender, def.Gender = domain.GenderMale, domain.GenderMale
			},
			bpMods: []int{modx1_25},
		},
		{
			name:   "Rivalry against the opposite gender",
			what:   "abilities.ts rivalry registers onBasePower, chainModify(0.75)",
			atkDex: 143, defDex: 143, move: "body-slam",
			atkAbility: "rivalry", defAbility: "shell-armor",
			arrange: func(atk, def *Pokemon) {
				atk.Gender, def.Gender = domain.GenderMale, domain.GenderFemale
			},
			bpMods: []int{modx0_75},
		},
		{
			name:   "Analytic when moving last",
			what:   "abilities.ts analytic registers onBasePower, chainModify([5325,4096])",
			atkDex: 143, defDex: 143, move: "body-slam",
			atkAbility: "analytic", defAbility: "shell-armor",
			arrange: func(atk, def *Pokemon) { atk.Volatiles.MovedLast = true },
			bpMods:  []int{modx1_3},
		},
		{
			name:   "Sheer Force on a move with a secondary",
			what:   "abilities.ts sheerforce registers onBasePower, chainModify([5325,4096])",
			atkDex: 143, defDex: 143, move: "body-slam", // 30% paralysis
			atkAbility: "sheer-force", defAbility: "shell-armor",
			bpMods: []int{modx1_3},
		},
		{
			name:   "Sand Force on a Ground move in a sandstorm",
			what:   "abilities.ts sandforce registers onBasePower, chainModify([5325,4096])",
			atkDex: 51, defDex: 143, move: "earthquake", // Dugtrio → Snorlax
			atkAbility: "sand-force", defAbility: "shell-armor",
			weather: &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5},
			bpMods:  []int{modx1_3},
		},
		{
			name:   "Dry Skin taking a Fire move",
			what:   "abilities.ts dryskin registers onSourceBasePower, chainModify(1.25)",
			atkDex: 59, defDex: 143, move: "flamethrower",
			defAbility: "dry-skin",
			bpMods:     []int{modx1_25},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t, d) })
	}
}

// TestBasePowerGroupReachesARealBattle plays the group through ResolveTurn.
//
// The table above proves the formula; this proves the wiring reaches it. The
// two are not the same claim — a hook can be moved into the right group and
// still be read from the wrong place, or read twice, and a unit-level call
// into computeDamage would never notice. Per Part II of
// docs/royale-followups.md, any mechanic that only unit tests reach is a
// mechanic that is one refactor from being untested.
//
// Tackle has no secondary effect and Snorlax carries nothing that chips at end
// of turn, so the HP the defender is missing after the turn is the damage the
// move dealt and nothing else. Shell Armor on the defender keeps every roll
// non-critical, so the observed set is the roll spread.
func TestBasePowerGroupReachesARealBattle(t *testing.T) {
	d := loadDex(t)

	for _, tc := range []struct {
		name    string
		ability AbilityKind
		item    ItemKind
		mods    []int
	}{
		{"Silk Scarf, an item handler", "", ItemSilkScarf, []int{modx1_2}},
		{"Technician, an ability handler", "technician", "", []int{modx1_5}},
		{
			"both at once, chained in priority order", "technician", ItemSilkScarf,
			// Technician is priority 30 and Silk Scarf 15, so they chain in
			// that order — and chaining once is not the same as applying twice.
			[]int{modx1_5, modx1_2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := d.Moves["tackle"]

			// The reference reads the raw stats off a freshly built pair, which
			// is the same spread the battle below will hand its actives.
			ref := buildPokemon(d, d.Species[143])
			atkRaw, _ := rawStatAndStage(&ref, "attack")
			defRaw, _ := rawStatAndStage(&ref, "defense")
			want := uniq(canonHit{
				basePower: m.Power,
				bpMods:    tc.mods,
				atkRaw:    atkRaw,
				defRaw:    defRaw,
				stab:      true, // Snorlax is Normal and so is Tackle
				eff:       1,
			}.rolls())

			seen := map[int]bool{}
			for seed := uint64(1); seed <= 3000; seed++ {
				s := neutralBattle(t, d, seed, []int{143, 143}, []int{143, 143})
				atk, def := s.Active(0), s.Active(1)
				atk.Ability, atk.Item = tc.ability, tc.item
				def.Ability = "shell-armor"
				teachMoves(t, d, atk, "tackle")
				teachMoves(t, d, def, "splash")
				playTurn(d, s, 0, 0)
				if dmg := def.MaxHP - def.HP; dmg > 0 {
					seen[dmg] = true
				}
			}
			got := make([]int, 0, len(seen))
			for v := range seen {
				got = append(got, v)
			}
			sort.Ints(got)

			if !sameSpread(got, want) {
				t.Errorf("%s over a played turn\n  canon:  %v\n  engine: %v", tc.name, want, got)
			}
		})
	}
}
