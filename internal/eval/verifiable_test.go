package eval

import (
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// viewWith builds a fog-of-war view with a chosen move on the active Pokémon
// and a chosen foe, so a single rule can be exercised in isolation.
func viewWith(t *testing.T, selfMove string, self engine.Pokemon, foe engine.Pokemon) ai.View {
	t.Helper()
	self.Moves = []engine.MoveSlot{{MoveID: selfMove, PP: 10, MaxPP: 10}}
	return ai.View{
		Me:    0,
		Turn:  3,
		Self:  engine.Side{Trainer: "P0", Active: 0, Team: []engine.Pokemon{self}},
		Foe:   foe,
		Phase: engine.PhaseChoosing,
	}
}

func mon(name string, t1, t2 string, hp, maxHP int) engine.Pokemon {
	p := engine.Pokemon{Name: name, HP: hp, MaxHP: maxHP}
	p.Type1 = engineType(t1)
	if t2 != "" {
		p.Type2 = engineType(t2)
	}
	return p
}

func kindsOf(errs []VerifiableError) map[ErrKind]int {
	out := map[ErrKind]int{}
	for _, e := range errs {
		out[e.Kind]++
	}
	return out
}

// The metric's entire value is that every count survives scrutiny, so the
// false-positive cases matter more than the true positives. Each of these is a
// move that looks wasteful by a shallow rule but genuinely does something.
func TestCheckAction_DoesNotFlagLegitimateMoves(t *testing.T) {
	d := loadDex(t)

	cases := []struct {
		name string
		why  string
		move string
		self engine.Pokemon
		foe  engine.Pokemon
	}{
		{
			name: "super effective attack",
			why:  "the ordinary case must never be charged",
			move: "thunderbolt",
			self: mon("Pikachu", "electric", "", 100, 100),
			foe:  mon("Gyarados", "water", "flying", 100, 100),
		},
		{
			// Note this is Grass, not Ground: Electric vs Ground is a true 0x
			// immunity, so Thunderbolt into a Rhydon *is* a provable error and
			// belongs in the positive cases, not here.
			name: "resisted but not immune",
			why:  "0.5x is a weak move, not a provable no-op — that is a judgement call",
			move: "thunderbolt",
			self: mon("Pikachu", "electric", "", 100, 100),
			foe:  mon("Venusaur", "grass", "poison", 100, 100),
		},
		{
			name: "heal below full HP",
			why:  "healing at 99/100 restores something",
			move: "recover",
			self: mon("Chansey", "normal", "", 99, 100),
			foe:  mon("Snorlax", "normal", "", 100, 100),
		},
		{
			name: "status on a clean target",
			why:  "the target has no status, so it can land",
			move: "thunder-wave",
			self: mon("Pikachu", "electric", "", 100, 100),
			foe:  mon("Snorlax", "normal", "", 100, 100),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := viewWith(t, c.move, c.self, c.foe)
			got := CheckAction(d, v, engine.Action{Kind: engine.ActionMove, Index: 0})
			if len(got) != 0 {
				t.Errorf("charged %d error(s) on a legitimate move (%s): %+v", len(got), c.why, got)
			}
		})
	}
}

// Attacking into a type immunity is the flagship category: guaranteed zero
// damage, and both defender types are public information.
func TestCheckAction_FlagsTypeImmuneAttack(t *testing.T) {
	d := loadDex(t)

	// Normal does nothing to Ghost. Both types are visible in the view.
	v := viewWith(t, "body-slam",
		mon("Snorlax", "normal", "", 100, 100),
		mon("Gengar", "ghost", "poison", 100, 100))

	got := CheckAction(d, v, engine.Action{Kind: engine.ActionMove, Index: 0})
	if k := kindsOf(got); k[ErrImmuneAttack] != 1 {
		t.Fatalf("want 1 immune-attack, got %+v", got)
	}
	if got[0].Turn != 3 || got[0].Side != 0 {
		t.Errorf("error not attributed to the right turn/side: %+v", got[0])
	}
}

// A switch cannot be proven wasteful from the view — its value depends on what
// the opponent does next, which is exactly the kind of judgement this metric
// refuses to make.
func TestCheckAction_NeverFlagsASwitch(t *testing.T) {
	d := loadDex(t)
	v := viewWith(t, "body-slam",
		mon("Snorlax", "normal", "", 100, 100),
		mon("Gengar", "ghost", "poison", 100, 100))

	got := CheckAction(d, v, engine.Action{Kind: engine.ActionSwitch, Index: 1})
	if len(got) != 0 {
		t.Errorf("charged a switch: %+v", got)
	}
}

// End to end on real battles, asserting per *category* rather than on a pooled
// rate. The categories measure different failures and different policies fail
// differently, so a single pooled number compares nothing meaningful — see
// TestScoreVerifiable_HeuristicBoostsAtCap for the case that makes this vivid.
//
// The claim here is the sharp one: the heuristic explicitly checks type
// effectiveness before attacking, so it must never attack into a type immunity,
// while a policy choosing uniformly at random does. If the metric cannot see
// that gap it is not measuring anything.
func TestScoreVerifiable_SeparatesRandomFromCompetentOnTypeImmunity(t *testing.T) {
	d := loadDex(t)
	lib, err := LoadTeamLibrary(libraryPath, d)
	if err != nil {
		t.Fatalf("load team library: %v", err)
	}

	countKind := func(build func() ai.Agent, kind ErrKind) (hits, decisions int) {
		for _, team := range lib.Teams {
			agents := [2]ai.Agent{build(), ai.NewHeuristicAgent(d)}
			picks := [2][]engine.TeamPick{team.Picks, team.Picks}
			_, turns, err := CaptureStored(d, agents, picks, 31337, 0)
			if err != nil {
				t.Fatalf("capture: %v", err)
			}
			errs, dec, skipped, err := ScoreVerifiable(d, 0, turns)
			if err != nil {
				t.Fatalf("score: %v", err)
			}
			if skipped > 0 {
				t.Errorf("%s: %d turns failed to recover", team.Name, skipped)
			}
			decisions += dec
			for _, e := range errs {
				if e.Kind == kind {
					hits++
				}
				if e.Side != 0 {
					t.Errorf("error attributed to side %d, scored side 0: %+v", e.Side, e)
				}
			}
		}
		return hits, decisions
	}

	randomHits, randomDec := countKind(func() ai.Agent { return ai.NewRandomAgent(20260811) }, ErrImmuneAttack)
	heurHits, heurDec := countKind(func() ai.Agent { return ai.NewHeuristicAgent(d) }, ErrImmuneAttack)
	t.Logf("immune attacks: random %d/%d, heuristic %d/%d", randomHits, randomDec, heurHits, heurDec)

	if heurHits != 0 {
		t.Errorf("heuristic attacked into a type immunity %d times; it checks effectiveness, so this "+
			"is a false positive in the checker", heurHits)
	}
	if randomHits == 0 {
		t.Error("random never attacked into a type immunity; the checker is not firing at all")
	}
}

func TestAggregateVerifiable_SortsCleanestFirstAndCountsCleanGames(t *testing.T) {
	results := []VerifiableBattle{
		{Contestant: "sloppy", Decisions: 50, Errors: []VerifiableError{
			{Kind: ErrImmuneAttack}, {Kind: ErrImmuneAttack}, {Kind: ErrHealAtFull},
		}},
		{Contestant: "sloppy", Decisions: 50, Errors: nil},
		{Contestant: "clean", Decisions: 100, Errors: []VerifiableError{{Kind: ErrBoostAtCap}}},
	}

	got := AggregateVerifiable(results)
	if len(got) != 2 {
		t.Fatalf("want 2 contestants, got %d", len(got))
	}
	if got[0].Contestant != "clean" {
		t.Errorf("want cleanest first, got %q", got[0].Contestant)
	}
	if got[0].Per100 != 1 {
		t.Errorf("clean: want 1.0 per 100, got %v", got[0].Per100)
	}
	sloppy := got[1]
	if sloppy.Errors != 3 || sloppy.Decisions != 100 || sloppy.Per100 != 3 {
		t.Errorf("sloppy: got %d errors / %d decisions / %v per100", sloppy.Errors, sloppy.Decisions, sloppy.Per100)
	}
	if sloppy.ByKind[ErrImmuneAttack] != 2 {
		t.Errorf("sloppy: want 2 immune-attacks, got %d", sloppy.ByKind[ErrImmuneAttack])
	}
	// A rate alone would hide that one of the two games was spotless.
	if sloppy.CleanGames != 1 {
		t.Errorf("sloppy: want 1 clean game, got %d", sloppy.CleanGames)
	}
}

// engineType converts a type name to the dex's Type. Kept here so the test
// tables read as plain strings.
func engineType(s string) domain.Type { return domain.Type(s) }
