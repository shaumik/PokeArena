package engine

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// snorlax is a convenient stat stick: base Atk 110, base HP 160, base Spe 30.
const snorlaxDex = 143

// TestDefaultSpreadMatchesLegacyStats is the regression that matters most in
// this change: every Pokémon built the old way must come out with exactly the
// numbers it had before spreads existed. The old formula was
// (2*base+31)*Level/100 (+5 or +Level+10), so we re-derive it inline here
// rather than calling the new code — a test that used calcStat to check
// calcStat would pass no matter what we broke.
func TestDefaultSpreadMatchesLegacyStats(t *testing.T) {
	d := loadDex(t)
	legacyStat := func(base int) int { return (2*base+31)*Level/100 + 5 }
	legacyHP := func(base int) int { return (2*base+31)*Level/100 + Level + 10 }

	for _, sp := range d.AllSpecies() {
		p := pokemonShell(sp, DefaultSpread())
		if got, want := p.MaxHP, legacyHP(sp.Base.HP); got != want {
			t.Errorf("%s MaxHP = %d, want %d", sp.Name, got, want)
		}
		for _, c := range []struct {
			label string
			got   int
			base  int
		}{
			{"Atk", p.Stats.Atk, sp.Base.Atk},
			{"Def", p.Stats.Def, sp.Base.Def},
			{"SpA", p.Stats.SpA, sp.Base.SpA},
			{"SpD", p.Stats.SpD, sp.Base.SpD},
			{"Spe", p.Stats.Spe, sp.Base.Spe},
		} {
			if want := legacyStat(c.base); c.got != want {
				t.Errorf("%s %s = %d, want %d", sp.Name, c.label, c.got, want)
			}
		}
	}
}

// TestCalcStatKnownSpreads pins the formula against hand-computed canonical
// values at L50. Each case shows its arithmetic so a failure says which step
// drifted.
func TestCalcStatKnownSpreads(t *testing.T) {
	cases := []struct {
		name         string
		base, iv, ev int
		num, den     int
		want         int
		why          string
	}{
		{
			name: "neutral 0 EV", base: 110, iv: 31, ev: 0, num: 1, den: 1, want: 130,
			why: "(2*110+31+0)*50/100 = 125; +5 = 130",
		},
		{
			name: "neutral 252 EV", base: 110, iv: 31, ev: 252, num: 1, den: 1, want: 162,
			why: "(220+31+63)*50/100 = 157; +5 = 162",
		},
		{
			name: "boosting nature 252 EV", base: 110, iv: 31, ev: 252, num: 11, den: 10, want: 178,
			why: "162 * 1.1 = 178.2 -> 178",
		},
		{
			name: "hindering nature 252 EV", base: 110, iv: 31, ev: 252, num: 9, den: 10, want: 145,
			why: "162 * 0.9 = 145.8 -> 145",
		},
		{
			name: "zero IV zero EV neutral", base: 110, iv: 0, ev: 0, num: 1, den: 1, want: 115,
			why: "(220+0+0)*50/100 = 110; +5 = 115",
		},
		{
			// EVs are consumed in blocks of four; 4 EVs buys one point at
			// L100 but nothing at all here until the floor rolls over.
			name: "EV floor divides by four", base: 110, iv: 31, ev: 3, num: 1, den: 1, want: 130,
			why: "floor(3/4) = 0, identical to 0 EV",
		},
		{
			name: "EV 8 at L50", base: 110, iv: 31, ev: 8, num: 1, den: 1, want: 131,
			why: "(220+31+2)*50/100 = 126; +5 = 131",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := calcStat(c.base, c.iv, c.ev, c.num, c.den); got != c.want {
				t.Errorf("calcStat(%d, %d, %d, %d/%d) = %d, want %d (%s)",
					c.base, c.iv, c.ev, c.num, c.den, got, c.want, c.why)
			}
		})
	}
}

// TestCalcHPIgnoresNature: no nature touches HP. The nature is not even an
// argument to calcHP, so this guards the plumbing — that pokemonShell doesn't
// route HP through the nature-aware path by mistake.
func TestCalcHPIgnoresNature(t *testing.T) {
	d := loadDex(t)
	sp := d.Species[snorlaxDex]

	var hps []int
	for _, natureID := range []string{"", "adamant", "bold", "timid", "sassy"} {
		spread := DefaultSpread()
		if natureID != "" {
			n, ok := d.Natures[natureID]
			if !ok {
				t.Fatalf("nature %q missing from dex", natureID)
			}
			spread.Nature = n
		}
		hps = append(hps, pokemonShell(sp, spread).MaxHP)
	}
	for i, hp := range hps {
		if hp != hps[0] {
			t.Errorf("nature changed MaxHP: case %d = %d, want %d", i, hp, hps[0])
		}
	}
}

// TestNatureAppliesToRightStat walks a real nature end-to-end through
// pokemonShell: Adamant is +Atk / -SpA, and must leave the other four alone.
func TestNatureAppliesToRightStat(t *testing.T) {
	d := loadDex(t)
	sp := d.Species[snorlaxDex]
	neutral := pokemonShell(sp, DefaultSpread())

	spread := DefaultSpread()
	spread.Nature = d.Natures["adamant"]
	adamant := pokemonShell(sp, spread)

	if want := neutral.Stats.Atk * 11 / 10; adamant.Stats.Atk != want {
		t.Errorf("Adamant Atk = %d, want %d", adamant.Stats.Atk, want)
	}
	if want := neutral.Stats.SpA * 9 / 10; adamant.Stats.SpA != want {
		t.Errorf("Adamant SpA = %d, want %d", adamant.Stats.SpA, want)
	}
	if adamant.Stats.Def != neutral.Stats.Def ||
		adamant.Stats.SpD != neutral.Stats.SpD ||
		adamant.Stats.Spe != neutral.Stats.Spe ||
		adamant.MaxHP != neutral.MaxHP {
		t.Errorf("Adamant disturbed an untargeted stat: %+v vs neutral %+v", adamant.Stats, neutral.Stats)
	}
}

// TestNeutralNaturesChangeNothing: all five neutral natures must be exactly
// equivalent to no nature at all. Their data rows carry no plus/minus, so
// this also proves Multiplier's empty-key handling.
func TestNeutralNaturesChangeNothing(t *testing.T) {
	d := loadDex(t)
	sp := d.Species[snorlaxDex]
	base := pokemonShell(sp, DefaultSpread())

	neutrals := 0
	for id, n := range d.Natures {
		if !n.IsNeutral() {
			continue
		}
		neutrals++
		spread := DefaultSpread()
		spread.Nature = n
		if got := pokemonShell(sp, spread); got.Stats != base.Stats {
			t.Errorf("neutral nature %q changed stats: %+v, want %+v", id, got.Stats, base.Stats)
		}
	}
	if neutrals != 5 {
		t.Errorf("found %d neutral natures, want 5 (Hardy, Docile, Serious, Bashful, Quirky)", neutrals)
	}
}

// TestNatureRatioMatchesFloat is the check the Multiplier doc comment cites.
// Integer ratios are used because they are exact by construction, not because
// float64 was measured to be wrong — this documents that the two agree across
// every stat value the engine can produce, so nobody "fixes" the integer math
// back to floats believing they found a bug.
//
// The reachable range is roughly 21..310 (base 1..255, IV 31, EV 252, L50);
// 0..2000 covers it with room to spare.
func TestNatureRatioMatchesFloat(t *testing.T) {
	for s := 0; s <= 2000; s++ {
		if got, want := s*11/10, int(math.Floor(float64(s)*1.1)); got != want {
			t.Fatalf("+nature at stat %d: integer %d, float %d", s, got, want)
		}
		if got, want := s*9/10, int(math.Floor(float64(s)*0.9)); got != want {
			t.Fatalf("-nature at stat %d: integer %d, float %d", s, got, want)
		}
	}
}

// Sweep parameters for TestStatPreviewChecksum. Duplicated verbatim in
// tools/check-stat-preview.js — the point is that two independent
// implementations reduce the same space to the same number.
const (
	statSweepCount = 216000
	statSweepSum   = 23809332
)

// TestStatPreviewChecksum pins the formula's output over every species ×
// nature × EV × IV combination to a single checksum, which
// tools/check-stat-preview.js recomputes from web/app.js's mirror of the same
// arithmetic.
//
// The builder previews a spread's effect before submitting it, which means the
// formula exists twice: once here in Go as the source of truth, once in
// JavaScript so the number moves as you drag a slider. Two implementations of
// one formula drift silently — the browser would just quietly show wrong
// numbers, and nothing would fail. This is the tripwire: change the engine and
// this fails, telling you the mirror needs the same change.
//
// A checksum rather than a table because the point is coverage of the whole
// space, not the specific values — TestCalcStatKnownSpreads pins those.
func TestStatPreviewChecksum(t *testing.T) {
	d := loadDex(t)
	evSet := []int{0, 3, 4, 8, 100, 252} // 3 and 4 straddle the floor(EV/4) step
	ivSet := []int{0, 15, 31}

	count, sum := 0, 0
	for _, sp := range d.AllSpecies() {
		for _, n := range d.Natures {
			for _, ev := range evSet {
				for _, iv := range ivSet {
					for _, key := range domain.StatKeys {
						base, ok := sp.Base.Get(key)
						if !ok {
							t.Fatalf("species %s has no base stat %q", sp.Name, key)
						}
						if key == "hp" {
							sum += calcHP(base, iv, ev)
						} else {
							num, den := n.Multiplier(key)
							sum += calcStat(base, iv, ev, num, den)
						}
						count++
					}
				}
			}
		}
	}

	if count != statSweepCount || sum != statSweepSum {
		t.Errorf("stat sweep = count %d sum %d, want count %d sum %d.\n"+
			"If the formula or the dataset changed on purpose, update these constants, "+
			"the mirror in web/app.js:derivedStat, and the copy in "+
			"tools/check-stat-preview.js together.",
			count, sum, statSweepCount, statSweepSum)
	}
}

// TestResolveSpreadDefaults: each spread field independently falls back when
// absent, and an explicitly-zero IV spread is honored rather than treated as
// "unspecified" — the reason IVs are a pointer.
func TestResolveSpreadDefaults(t *testing.T) {
	d := loadDex(t)

	got := resolveSpread(d, TeamPick{DexNo: snorlaxDex})
	if want := domain.Uniform(MaxIV); got.IVs != want {
		t.Errorf("absent IVs = %+v, want %+v", got.IVs, want)
	}
	if (got.EVs != domain.Stats{}) {
		t.Errorf("absent EVs = %+v, want all zero", got.EVs)
	}
	if !got.Nature.IsNeutral() {
		t.Errorf("absent nature = %+v, want neutral", got.Nature)
	}

	zero := domain.Stats{}
	got = resolveSpread(d, TeamPick{DexNo: snorlaxDex, IVs: &zero})
	if (got.IVs != domain.Stats{}) {
		t.Errorf("explicit zero IVs = %+v, want all zero (not the 31 default)", got.IVs)
	}

	got = resolveSpread(d, TeamPick{DexNo: snorlaxDex, Nature: "jolly"})
	if got.Nature.ID != "jolly" {
		t.Errorf("nature = %q, want jolly", got.Nature.ID)
	}
}

// TestSpreadReachesBattleState proves the spread survives the whole
// pick → validate → build path, and that the resolved values are recorded on
// the Pokémon (not just folded into Stats and forgotten) so replays and team
// previews can show them.
func TestSpreadReachesBattleState(t *testing.T) {
	d := loadDex(t)
	picks := neutralTeam(t, d)
	picks[0].Nature = "adamant"
	picks[0].EVs = &domain.Stats{Atk: 252, HP: 252, Spe: 4}

	if err := ValidateTeam(picks, d); err != nil {
		t.Fatalf("ValidateTeam: %v", err)
	}
	s, err := NewBattleFromPicks(d, "b", "P0", picks, "P1", neutralTeam(t, d), 1)
	if err != nil {
		t.Fatalf("NewBattleFromPicks: %v", err)
	}

	built := s.Sides[0].Team[0]
	sp := d.Species[picks[0].DexNo]
	spread := Spread{EVs: *picks[0].EVs, IVs: domain.Uniform(MaxIV), Nature: d.Natures["adamant"]}
	want := pokemonShell(sp, spread)

	if built.Stats != want.Stats {
		t.Errorf("built stats = %+v, want %+v", built.Stats, want.Stats)
	}
	if built.Nature != "adamant" {
		t.Errorf("built nature = %q, want adamant", built.Nature)
	}
	if built.EVs != *picks[0].EVs {
		t.Errorf("built EVs = %+v, want %+v", built.EVs, *picks[0].EVs)
	}
	if built.IVs != domain.Uniform(MaxIV) {
		t.Errorf("built IVs = %+v, want all %d", built.IVs, MaxIV)
	}

	// The untouched side must still be on the legacy numbers.
	if plain := s.Sides[1].Team[0]; plain.Nature != "" || (plain.EVs != domain.Stats{}) {
		t.Errorf("spreadless pick got a spread: nature %q EVs %+v", plain.Nature, plain.EVs)
	}
}

// TestValidateSpread covers the legality gate. Each case mutates one slot of
// an otherwise-valid team so the error can only come from the spread rules.
func TestValidateSpread(t *testing.T) {
	d := loadDex(t)
	cases := []struct {
		name    string
		mutate  func(*TeamPick)
		wantErr bool
	}{
		{"no spread at all", func(p *TeamPick) {}, false},
		{"max legal EVs", func(p *TeamPick) {
			p.EVs = &domain.Stats{HP: 252, Atk: 252, Spe: 6}
		}, false},
		{"exactly the budget", func(p *TeamPick) {
			p.EVs = &domain.Stats{HP: 252, Atk: 252, Spe: 6}
		}, false},
		{"one EV over the budget", func(p *TeamPick) {
			p.EVs = &domain.Stats{HP: 252, Atk: 252, Spe: 7}
		}, true},
		{"one EV over the per-stat cap", func(p *TeamPick) {
			p.EVs = &domain.Stats{Atk: 253}
		}, true},
		{"negative EVs", func(p *TeamPick) {
			p.EVs = &domain.Stats{Atk: -4}
		}, true},
		{"IV 31 is legal", func(p *TeamPick) {
			ivs := domain.Uniform(31)
			p.IVs = &ivs
		}, false},
		{"IV 32 is not", func(p *TeamPick) {
			p.IVs = &domain.Stats{Spe: 32}
		}, true},
		{"negative IV", func(p *TeamPick) {
			p.IVs = &domain.Stats{Spe: -1}
		}, true},
		{"known nature", func(p *TeamPick) { p.Nature = "modest" }, false},
		{"unknown nature", func(p *TeamPick) { p.Nature = "spicy" }, true},
		{"empty nature", func(p *TeamPick) { p.Nature = "" }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			picks := neutralTeam(t, d)
			c.mutate(&picks[0])
			err := ValidateTeam(picks, d)
			if c.wantErr && err == nil {
				t.Errorf("ValidateTeam accepted an illegal spread")
			}
			if !c.wantErr && err != nil {
				t.Errorf("ValidateTeam rejected a legal spread: %v", err)
			}
		})
	}
}

// TestEVBudgetErrorNamesTheStat: a spread that breaks both the per-stat cap and
// the total budget must be told which stats are illegal, with the specific
// finding ahead of the general one.
//
// This used to hold because validation stopped at the first failure. It now
// holds because validation reports everything in check order — which is
// strictly better here: a spread with two stats over the cap and a blown budget
// is fixed in one pass instead of three.
func TestEVBudgetErrorNamesTheStat(t *testing.T) {
	d := loadDex(t)
	picks := neutralTeam(t, d)
	picks[0].EVs = &domain.Stats{Atk: 400, HP: 400}
	err := ValidateTeam(picks, d)
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !contains(msg, "out of range") {
		t.Errorf("error = %q, want the per-stat range message", msg)
	}
	// Both offending stats, not just the first one found.
	for _, stat := range []string{"hp EVs 400", "atk EVs 400"} {
		if !contains(msg, stat) {
			t.Errorf("error = %q, missing the finding for %s", msg, stat)
		}
	}
	// The specific finding must lead; the budget line is the consequence.
	perStat := indexOf(msg, "out of range")
	budget := indexOf(msg, "over the")
	if budget >= 0 && perStat > budget {
		t.Errorf("error = %q, want the per-stat findings before the budget one", msg)
	}
}

// indexOf returns the first index of sub in s, or -1. A local helper so this
// file keeps its no-imports-beyond-domain shape.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestSpreadReachesTurnOrder proves the spread lands where it matters most.
// Speed decides who moves first, and turn order reads effectiveSpeed, not
// Stats.Spe directly — so this closes the gap between "the number changed"
// and "the battle changed". Two identical Snorlax, one Timid with maximum
// Speed investment, must not be a speed tie.
func TestSpreadReachesTurnOrder(t *testing.T) {
	d := loadDex(t)
	sp := d.Species[snorlaxDex]

	slow := pokemonShell(sp, DefaultSpread())

	fastSpread := DefaultSpread()
	fastSpread.Nature = d.Natures["timid"] // +Spe / -Atk
	fastSpread.EVs = domain.Stats{Spe: 252}
	fast := pokemonShell(sp, fastSpread)

	if effectiveSpeed(&fast, nil) <= effectiveSpeed(&slow, nil) {
		t.Errorf("invested Speed %v did not beat uninvested %v",
			effectiveSpeed(&fast, nil), effectiveSpeed(&slow, nil))
	}
	// Timid's downside must land too, or the nature is only half-applied.
	if fast.Stats.Atk >= slow.Stats.Atk {
		t.Errorf("Timid Attack %d, want less than neutral %d", fast.Stats.Atk, slow.Stats.Atk)
	}
}

// TestBuilderPayloadRoundTrips decodes the exact JSON the web team builder
// emits (captured from the browser driving web/app.js:picksFromState) and
// runs it through the real validator and constructor.
//
// The point is the key names. EVs and IVs reuse domain.Stats, whose JSON keys
// are hp/atk/def/spatk/spdef/speed — one letter off in the client ("spa",
// "spd", "spdef" vs "sp_def") and encoding/json silently drops the field,
// producing a legal team with a quietly wrong spread. Nothing else in the
// stack would notice.
func TestBuilderPayloadRoundTrips(t *testing.T) {
	d := loadDex(t)

	const trained = `{"dex_no":143,"moves":["double-edge","swallow","outrage","superpower"],` +
		`"nature":"adamant","evs":{"hp":252,"atk":252,"def":6,"spatk":0,"spdef":0,"speed":0},` +
		`"ivs":{"hp":31,"atk":31,"def":31,"spatk":31,"spdef":31,"speed":0}}`
	const untouched = `{"dex_no":6,"moves":["overheat","hurricane","work-up","dragon-pulse"]}`

	var pick TeamPick
	if err := json.Unmarshal([]byte(trained), &pick); err != nil {
		t.Fatalf("decode builder payload: %v", err)
	}
	if pick.Nature != "adamant" {
		t.Errorf("nature = %q, want adamant", pick.Nature)
	}
	if pick.EVs == nil {
		t.Fatal("evs decoded as nil — the client's key names do not match domain.Stats")
	}
	if pick.EVs.HP != 252 || pick.EVs.Atk != 252 || pick.EVs.Def != 6 {
		t.Errorf("evs = %+v, want 252 HP / 252 Atk / 6 Def", *pick.EVs)
	}
	if pick.IVs == nil || pick.IVs.Spe != 0 || pick.IVs.HP != 31 {
		t.Errorf("ivs = %+v, want 0 Speed and 31 elsewhere", pick.IVs)
	}

	var bare TeamPick
	if err := json.Unmarshal([]byte(untouched), &bare); err != nil {
		t.Fatalf("decode untouched payload: %v", err)
	}
	if bare.EVs != nil || bare.IVs != nil || bare.Nature != "" {
		t.Errorf("an untouched pick decoded with a spread: %+v", bare)
	}

	// Fill the roster out and prove the whole thing is legal and buildable.
	picks := neutralTeam(t, d)
	picks[0] = pick
	if err := ValidateTeam(picks, d); err != nil {
		t.Fatalf("ValidateTeam rejected the builder's payload: %v", err)
	}
	s, err := NewBattleFromPicks(d, "b", "P0", picks, "P1", neutralTeam(t, d), 1)
	if err != nil {
		t.Fatalf("NewBattleFromPicks: %v", err)
	}

	// The numbers the builder previewed for this exact spread.
	got := s.Sides[0].Team[0].Stats
	want := domain.Stats{HP: 267, Atk: 178, Def: 86, SpA: 76, SpD: 130, Spe: 35}
	if got != want {
		t.Errorf("built stats = %+v, want %+v (the values web/app.js renders for this spread)", got, want)
	}
}

// TestTeamPickCloneIsDeep guards the copy path that dropped the spread the
// day it was added. Two failure modes, both silent:
//
//   - a field is missing from the copy entirely (DeepEqual catches it)
//   - a field is copied as a shared pointer, so mutating one pick mutates
//     every other holder of that roster (the mutation checks catch it)
//
// The all-fields-set assertion is what keeps this test honest: adding a
// field to TeamPick without extending the fixture fails here, which is the
// prompt to decide whether Clone needs to deep-copy it.
func TestTeamPickCloneIsDeep(t *testing.T) {
	evs := domain.Stats{HP: 252, Atk: 252, Spe: 4}
	ivs := domain.Uniform(30)
	orig := TeamPick{
		DexNo:   snorlaxDex,
		MoveIDs: []string{"body-slam", "rest"},
		Ability: "immunity",
		Item:    "leftovers",
		EVs:     &evs,
		IVs:     &ivs,
		Nature:  "adamant",
		Gender:  domain.GenderMale,
	}

	rv := reflect.ValueOf(orig)
	for i := 0; i < rv.NumField(); i++ {
		if rv.Field(i).IsZero() {
			t.Fatalf("fixture leaves TeamPick.%s at its zero value — set it so this "+
				"test actually exercises the field, then check Clone handles it",
				rv.Type().Field(i).Name)
		}
	}

	clone := orig.Clone()
	if !reflect.DeepEqual(orig, clone) {
		t.Fatalf("Clone() = %+v, want %+v", clone, orig)
	}

	clone.MoveIDs[0] = "tackle"
	clone.EVs.Atk = 0
	clone.IVs.Spe = 0
	if orig.MoveIDs[0] != "body-slam" {
		t.Error("Clone shares the move slice with the original")
	}
	if orig.EVs.Atk != 252 {
		t.Error("Clone shares the EV pointer with the original")
	}
	if orig.IVs.Spe != 30 {
		t.Error("Clone shares the IV pointer with the original")
	}
}

// neutralTeam builds a legal 6-slot team with no spread fields set — the
// baseline every spread test mutates one slot of.
func neutralTeam(t *testing.T, d *domain.Dex) []TeamPick {
	t.Helper()
	picks := make([]TeamPick, 0, TeamSize)
	for _, sp := range d.AllSpecies() {
		if len(picks) == TeamSize {
			break
		}
		moves := sp.Moves
		if len(moves) > MovesMax {
			moves = moves[:MovesMax]
		}
		picks = append(picks, TeamPick{DexNo: sp.DexNo, MoveIDs: moves})
	}
	if len(picks) != TeamSize {
		t.Fatalf("dex yielded %d picks, need %d", len(picks), TeamSize)
	}
	return picks
}
