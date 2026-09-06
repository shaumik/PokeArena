// The live-dataset tests here need the engine's init() functions to have run:
// internal/specs' dynamic vocabularies (volatiles, side conditions, weather)
// are registered by internal/engine, and domain.LoadDex rejects the shipped
// moves.json without them. domain cannot import engine (engine imports
// domain), so this is an external test package with the same blank import
// cmd/data-validate and cmd/data-sync use.
package domain_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/shaumik/PokeArena/internal/domain"
	_ "github.com/shaumik/PokeArena/internal/engine"
)

// TestNatureMultiplier covers the ratio table, including the two shapes that
// are easy to get wrong: a neutral nature (no keys at all) and a stat the
// nature simply doesn't name.
func TestNatureMultiplier(t *testing.T) {
	adamant := domain.Nature{ID: "adamant", Name: "Adamant", Plus: "atk", Minus: "spatk"}
	hardy := domain.Nature{ID: "hardy", Name: "Hardy"}

	cases := []struct {
		nature   domain.Nature
		stat     string
		num, den int
	}{
		{adamant, "atk", 11, 10},
		{adamant, "spatk", 9, 10},
		{adamant, "def", 1, 1},
		{adamant, "speed", 1, 1},
		{adamant, "hp", 1, 1},
		{hardy, "atk", 1, 1},
		{hardy, "spatk", 1, 1},
		// The empty string is not a stat, and must not match a neutral
		// nature's empty Plus/Minus into a bogus 11/10.
		{hardy, "", 1, 1},
		{adamant, "", 1, 1},
	}
	for _, c := range cases {
		num, den := c.nature.Multiplier(c.stat)
		if num != c.num || den != c.den {
			t.Errorf("%s.Multiplier(%q) = %d/%d, want %d/%d",
				c.nature.ID, c.stat, num, den, c.num, c.den)
		}
	}
}

func TestNatureIsNeutral(t *testing.T) {
	if !(domain.Nature{ID: "hardy"}).IsNeutral() {
		t.Error("a nature with no plus/minus must be neutral")
	}
	if (domain.Nature{ID: "adamant", Plus: "atk", Minus: "spatk"}).IsNeutral() {
		t.Error("Adamant is not neutral")
	}
}

func TestStatsHelpers(t *testing.T) {
	s := domain.Stats{HP: 1, Atk: 2, Def: 3, SpA: 4, SpD: 5, Spe: 6}
	for i, key := range domain.StatKeys {
		got, ok := s.Get(key)
		if !ok {
			t.Fatalf("Get(%q) reported unknown key", key)
		}
		if want := i + 1; got != want {
			t.Errorf("Get(%q) = %d, want %d", key, got, want)
		}
	}
	if _, ok := s.Get("attack"); ok {
		t.Error(`Get("attack") must not resolve — that is the boost vocabulary, not the Stats one`)
	}
	if got := s.Total(); got != 21 {
		t.Errorf("Total() = %d, want 21", got)
	}
	if got, want := domain.Uniform(31), (domain.Stats{HP: 31, Atk: 31, Def: 31, SpA: 31, SpD: 31, Spe: 31}); got != want {
		t.Errorf("Uniform(31) = %+v, want %+v", got, want)
	}
}

// TestLiveNatureTable checks the shipped dataset, not a fixture: all 25
// natures present, exactly 5 neutral, and the 20 non-neutral ones forming
// every ordered (plus, minus) pair of distinct stats — 5×4 = 20, which is
// what makes 25 the right count in the first place rather than a number
// someone chose.
func TestLiveNatureTable(t *testing.T) {
	d, err := domain.LoadDex("../../data", "test")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	if len(d.Natures) != domain.NatureCount {
		t.Fatalf("nature count = %d, want %d", len(d.Natures), domain.NatureCount)
	}

	neutral := 0
	pairs := map[[2]string]string{}
	for id, n := range d.Natures {
		if n.IsNeutral() {
			neutral++
			continue
		}
		key := [2]string{n.Plus, n.Minus}
		if prev, dup := pairs[key]; dup {
			t.Errorf("natures %q and %q share the spread +%s/-%s", prev, id, n.Plus, n.Minus)
		}
		pairs[key] = id
	}
	if neutral != 5 {
		t.Errorf("neutral natures = %d, want 5 (Hardy, Docile, Serious, Bashful, Quirky)", neutral)
	}
	if len(pairs) != 20 {
		t.Errorf("distinct +/- pairs = %d, want 20 (5 stats × 4 others)", len(pairs))
	}

	// Spot-check two the rest of the suite depends on by name.
	if n := d.Natures["adamant"]; n.Plus != "atk" || n.Minus != "spatk" {
		t.Errorf("adamant = +%s/-%s, want +atk/-spatk", n.Plus, n.Minus)
	}
	if n := d.Natures["timid"]; n.Plus != "speed" || n.Minus != "atk" {
		t.Errorf("timid = +%s/-%s, want +speed/-atk", n.Plus, n.Minus)
	}
}

// TestLoadDexRejectsBadNatures: the loader is the only gate on this file, so
// each way it can be wrong must fail at boot rather than produce a dex that
// quietly mis-scales stats for the rest of the run.
func TestLoadDexRejectsBadNatures(t *testing.T) {
	cases := []struct {
		name    string
		natures string
		wantErr string
	}{
		{
			name:    "missing file",
			natures: "",
			wantErr: "natures.json",
		},
		{
			name:    "short table",
			natures: `[{"id":"hardy","name":"Hardy"}]`,
			wantErr: "expected 25",
		},
		{
			name:    "targets hp",
			natures: natureTable(`{"id":"adamant","name":"Adamant","plus":"hp","minus":"spatk"}`),
			wantErr: "no nature modifies",
		},
		{
			name:    "boost-vocabulary stat key",
			natures: natureTable(`{"id":"adamant","name":"Adamant","plus":"attack","minus":"spatk"}`),
			wantErr: "unknown stat key",
		},
		{
			name:    "only one of plus/minus",
			natures: natureTable(`{"id":"adamant","name":"Adamant","plus":"atk"}`),
			wantErr: "only one of plus/minus",
		},
		{
			name:    "malformed id",
			natures: natureTable(`{"id":"ADAMANT","name":"Adamant","plus":"atk","minus":"spatk"}`),
			wantErr: "malformed id",
		},
		{
			name:    "empty name",
			natures: natureTable(`{"id":"adamant","name":"","plus":"atk","minus":"spatk"}`),
			wantErr: "empty name",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := domain.LoadDexFS(dexFSWithNatures(c.natures), "test")
			if err == nil {
				t.Fatalf("LoadDexFS accepted a bad nature table")
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, c.wantErr)
			}
		})
	}
}

// natureTable builds a full-length table whose first entry is the (usually
// malformed) one under test, padded to NatureCount with valid neutral
// fillers so the count check never fires first and masks the real case.
func natureTable(first string) string {
	var b strings.Builder
	b.WriteString("[" + first)
	for i := 1; i < domain.NatureCount; i++ {
		b.WriteString(`,{"id":"filler-` + string(rune('a'+i)) + `","name":"Filler"}`)
	}
	b.WriteString("]")
	return b.String()
}

// dexFSWithNatures returns a minimal-but-valid dataset whose natures.json is
// the given body. An empty body omits the file entirely.
func dexFSWithNatures(natures string) fstest.MapFS {
	fsys := fstest.MapFS{
		"pokedex.json": &fstest.MapFile{Data: []byte(
			`[{"dex_no":1,"name":"Test","type1":"normal","type2":"",` +
				`"base":{"hp":1,"atk":1,"def":1,"spatk":1,"spdef":1,"speed":1},"moves":["tackle"]}]`)},
		"moves.json": &fstest.MapFile{Data: []byte(
			`[{"id":"tackle","name":"Tackle","type":"normal","category":"physical",` +
				`"power":40,"accuracy":100,"pp":35,"priority":0}]`)},
		"typechart.json": &fstest.MapFile{Data: []byte(emptyTypeChart())},
	}
	if natures != "" {
		fsys["natures.json"] = &fstest.MapFile{Data: []byte(natures)}
	}
	return fsys
}

// emptyTypeChart emits an 18-type chart with every row empty (all matchups
// neutral), which is the minimum LoadDexFS accepts.
func emptyTypeChart() string {
	types := []string{
		"normal", "fire", "water", "electric", "grass", "ice", "fighting",
		"poison", "ground", "flying", "psychic", "bug", "rock", "ghost",
		"dragon", "dark", "steel", "fairy",
	}
	var b strings.Builder
	b.WriteString("{")
	for i, tp := range types {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"` + tp + `":{}`)
	}
	b.WriteString("}")
	return b.String()
}
