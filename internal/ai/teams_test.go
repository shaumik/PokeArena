package ai

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaumik/PokeArena/internal/engine"
)

// TestLoadTeamPool_ExplicitPicksUseTunedMoves proves the pool honors explicit
// "picks" verbatim instead of auto-deriving the learnset's first four. This is
// what lets the live AI field competitively-tuned rosters (e.g. Mewtwo with
// Psystrike/Recover) rather than whatever four moves happen to sort first — the
// difference between a real opponent and a lobotomized one.
func TestLoadTeamPool_ExplicitPicksUseTunedMoves(t *testing.T) {
	d := loadDex(t)
	const poolJSON = `{"teams":[{"name":"Tuned","picks":[
		{"dex_no":150,"moves":["psystrike","aura-sphere","ice-beam","recover"]},
		{"dex_no":149,"moves":["dragon-dance","outrage","earthquake","fire-punch"]},
		{"dex_no":143,"moves":["body-slam","earthquake","crunch","rest"]},
		{"dex_no":145,"moves":["thunderbolt","hurricane","roost","thunder-wave"]},
		{"dex_no":121,"moves":["surf","thunderbolt","ice-beam","recover"]},
		{"dex_no":112,"moves":["earthquake","stone-edge","megahorn","swords-dance"]}
	]}]}`
	path := filepath.Join(t.TempDir(), "tuned.json")
	if err := os.WriteFile(path, []byte(poolJSON), 0o644); err != nil {
		t.Fatalf("write pool: %v", err)
	}

	p, err := LoadTeamPool(d, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	picks, err := p.Pick(rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if picks[0].DexNo != 150 {
		t.Fatalf("first pick dex_no = %d, want 150", picks[0].DexNo)
	}
	got := picks[0].MoveIDs
	want := []string{"psystrike", "aura-sphere", "ice-beam", "recover"}
	if len(got) != len(want) {
		t.Fatalf("Mewtwo moves = %v, want the tuned set %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("move %d = %q, want %q — explicit picks must be used verbatim, not auto-derived", i, got[i], want[i])
		}
	}
}

// TestLoadTeamPool_RealDataLoads is the canary: the curated
// data/ai-teams.json must load against the real dataset and every
// team must pass engine.ValidateTeam. A future bad edit fails the
// test rather than crashing the gateway on startup.
func TestLoadTeamPool_RealDataLoads(t *testing.T) {
	d := loadDex(t)
	p, err := LoadTeamPool(d, "../../data/ai-teams.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rng := rand.New(rand.NewSource(1))
	picks, err := p.Pick(rng)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(picks) != engine.TeamSize {
		t.Fatalf("picks: got %d, want %d", len(picks), engine.TeamSize)
	}
	if err := engine.ValidateTeam(picks, d); err != nil {
		t.Fatalf("picks failed validator: %v", err)
	}
}

func TestPick_IsADeepCopy(t *testing.T) {
	d := loadDex(t)
	p, err := LoadTeamPool(d, "../../data/ai-teams.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	a, _ := p.Pick(rand.New(rand.NewSource(7)))
	b, _ := p.Pick(rand.New(rand.NewSource(7)))
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("picks empty")
	}
	// Mutating one must not affect the other.
	a[0].MoveIDs[0] = "GARBAGE-ID"
	if b[0].MoveIDs[0] == "GARBAGE-ID" {
		t.Fatal("Pick returned a shared move slice — mutations leak across picks")
	}
}

// TestPick_CarriesAbilityAndItem: Pick rebuilds each TeamPick field by field,
// so any field it forgets is silently dropped — a curated team that declares
// Choice Band on its sweeper would field a bare sweeper and nobody would see
// an error. Ability had this bug before items existed; this test covers both.
// TestPick_CarriesSpread is the regression for a real drop: Pick used to
// rebuild each TeamPick from an enumerated list of fields, so the EV/IV/nature
// fields were silently discarded on their way out of the pool — a curated
// Adamant 252-Atk set would have arrived in battle as a neutral 0-EV one, with
// nothing failing anywhere.
//
// It asserts the whole path (JSON → pool → Pick → battle stats), because the
// only observable symptom was the final stat number.
func TestPick_CarriesSpread(t *testing.T) {
	d := loadDex(t)
	const poolJSON = `{"teams":[{"name":"Trained","picks":[
		{"dex_no":150,"moves":["psystrike","recover"],"nature":"timid","evs":{"spatk":252,"speed":252,"hp":4}},
		{"dex_no":149,"moves":["outrage","earthquake"],"nature":"adamant","evs":{"atk":252,"speed":252}},
		{"dex_no":143,"moves":["body-slam","rest"],"ivs":{"hp":31,"atk":0,"def":31,"spatk":31,"spdef":31,"speed":31}},
		{"dex_no":145,"moves":["thunderbolt","roost"]},
		{"dex_no":121,"moves":["surf","recover"]},
		{"dex_no":112,"moves":["earthquake","stone-edge"]}
	]}]}`
	path := filepath.Join(t.TempDir(), "trained.json")
	if err := os.WriteFile(path, []byte(poolJSON), 0o644); err != nil {
		t.Fatalf("write pool: %v", err)
	}
	p, err := LoadTeamPool(d, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	picks, err := p.Pick(rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("pick: %v", err)
	}

	if picks[0].Nature != "timid" {
		t.Errorf("pick 0 nature = %q, want timid", picks[0].Nature)
	}
	if picks[0].EVs == nil || picks[0].EVs.Spe != 252 {
		t.Errorf("pick 0 EVs = %+v, want 252 Speed", picks[0].EVs)
	}
	if picks[2].IVs == nil || picks[2].IVs.Atk != 0 {
		t.Errorf("pick 2 IVs = %+v, want 0 Atk", picks[2].IVs)
	}

	// The spread must actually change the battle numbers — the point of the
	// whole feature, and the only thing that would have caught the drop.
	s, err := engine.NewBattleFromPicks(d, "b", "P1", picks, "P2", picks, 1)
	if err != nil {
		t.Fatalf("new battle from picks: %v", err)
	}
	mewtwo := s.Sides[0].Team[0]
	if mewtwo.Nature != "timid" {
		t.Errorf("battle Pokémon nature = %q, want timid", mewtwo.Nature)
	}
	// Timid is +Spe / -Atk with 252 Speed EVs: Speed must beat, and Attack
	// must trail, the same species built with no spread at all.
	plain, err := engine.NewBattleFromPicks(d, "b2", "P1",
		[]engine.TeamPick{{DexNo: 150, MoveIDs: []string{"psystrike", "recover"}}}, "P2",
		[]engine.TeamPick{{DexNo: 150, MoveIDs: []string{"psystrike", "recover"}}}, 1)
	if err != nil {
		t.Fatalf("new baseline battle: %v", err)
	}
	base := plain.Sides[0].Team[0]
	if mewtwo.Stats.Spe <= base.Stats.Spe {
		t.Errorf("trained Speed %d, want more than untrained %d", mewtwo.Stats.Spe, base.Stats.Spe)
	}
	if mewtwo.Stats.Atk >= base.Stats.Atk {
		t.Errorf("Timid Attack %d, want less than neutral %d", mewtwo.Stats.Atk, base.Stats.Atk)
	}
}

func TestPick_CarriesAbilityAndItem(t *testing.T) {
	d := loadDex(t)
	const poolJSON = `{"teams":[{"name":"Held","picks":[
		{"dex_no":150,"moves":["psystrike","recover"],"ability":"unnerve","item":"life-orb"},
		{"dex_no":149,"moves":["outrage","earthquake"],"item":"choice-band"},
		{"dex_no":143,"moves":["body-slam","rest"],"item":"leftovers"},
		{"dex_no":145,"moves":["thunderbolt","roost"],"item":"choice-specs"},
		{"dex_no":121,"moves":["surf","recover"],"item":"choice-scarf"},
		{"dex_no":112,"moves":["earthquake","stone-edge"],"item":"focus-sash"}
	]}]}`
	path := filepath.Join(t.TempDir(), "held.json")
	if err := os.WriteFile(path, []byte(poolJSON), 0o644); err != nil {
		t.Fatalf("write pool: %v", err)
	}
	p, err := LoadTeamPool(d, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	picks, err := p.Pick(rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	want := []string{"life-orb", "choice-band", "leftovers", "choice-specs", "choice-scarf", "focus-sash"}
	for i, w := range want {
		if picks[i].Item != w {
			t.Errorf("pick %d item = %q, want %q", i, picks[i].Item, w)
		}
	}
	if picks[0].Ability != "unnerve" {
		t.Errorf("pick 0 ability = %q, want unnerve", picks[0].Ability)
	}

	// The picks must actually reach the battle: buildPokemonFromPick is the
	// only consumer, and a dropped field there is just as invisible.
	s, err := engine.NewBattleFromPicks(d, "b", "P1", picks, "P2", picks, 1)
	if err != nil {
		t.Fatalf("new battle from picks: %v", err)
	}
	if got := s.Sides[0].Team[0].Item; got != engine.ItemLifeOrb {
		t.Errorf("battle Pokémon item = %q, want %q", got, engine.ItemLifeOrb)
	}
}
