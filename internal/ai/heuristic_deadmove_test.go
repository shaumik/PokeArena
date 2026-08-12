package ai

import (
	"context"
	"testing"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// The heuristic used to spend turns on moves the rules make impossible: a
// status the target already had, a boost with the stat pinned at +6, a heal at
// full HP. Found by internal/eval's verifiable-error metric, which counted 27
// boost-at-cap turns across six games and caught the agent re-applying Thunder
// Wave to an already-paralyzed target for six consecutive turns.
//
// The tests below pin each case at the scoring level, where the intent is
// legible, plus one end-to-end check that Decide actually picks something else.

// deadMoveView builds a one-move view so a single scoring branch is isolated.
func deadMoveView(self engine.Pokemon, moveID string, foe engine.Pokemon) View {
	self.Moves = []engine.MoveSlot{{MoveID: moveID, PP: 10, MaxPP: 10}}
	return View{
		Me:    0,
		Turn:  5,
		Self:  engine.Side{Trainer: "P0", Active: 0, Team: []engine.Pokemon{self}},
		Foe:   foe,
		Phase: engine.PhaseChoosing,
	}
}

func healthy(name string, t1 domain.Type) engine.Pokemon {
	return engine.Pokemon{Name: name, Type1: t1, HP: 100, MaxHP: 100}
}

func TestHeuristic_ScoresProvablyDeadMovesBelowEverything(t *testing.T) {
	d := loadDex(t)
	a := NewHeuristicAgent(d)

	cases := []struct {
		name string
		move string
		self engine.Pokemon
		foe  engine.Pokemon
		dead bool
	}{
		{
			name: "status on an already-statused foe is dead",
			move: "thunder-wave",
			self: healthy("Pikachu", "electric"),
			foe: func() engine.Pokemon {
				p := healthy("Starmie", "water")
				p.Status = engine.StatusParalysis
				return p
			}(),
			dead: true,
		},
		{
			name: "status on a clean foe is live",
			move: "thunder-wave",
			self: healthy("Pikachu", "electric"),
			foe:  healthy("Starmie", "water"),
			dead: false,
		},
		{
			name: "heal at full HP is dead",
			move: "recover",
			self: healthy("Starmie", "water"),
			foe:  healthy("Snorlax", "normal"),
			dead: true,
		},
		{
			name: "heal below full HP is live",
			move: "recover",
			self: func() engine.Pokemon {
				p := healthy("Starmie", "water")
				p.HP = 40
				return p
			}(),
			foe:  healthy("Snorlax", "normal"),
			dead: false,
		},
		{
			name: "boost with the stat at +6 is dead",
			move: "swords-dance",
			self: func() engine.Pokemon {
				p := healthy("Scyther", "bug")
				p.Stages.Atk = 6
				return p
			}(),
			foe:  healthy("Snorlax", "normal"),
			dead: true,
		},
		{
			name: "boost with room left is live",
			move: "swords-dance",
			self: func() engine.Pokemon {
				p := healthy("Scyther", "bug")
				p.Stages.Atk = 4
				return p
			}(),
			foe:  healthy("Snorlax", "normal"),
			dead: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := d.Moves[c.move]; !ok {
				t.Skipf("move %q not in the dataset", c.move)
			}
			v := deadMoveView(c.self, c.move, c.foe)
			got := a.score(v, engine.Action{Kind: engine.ActionMove, Index: 0})
			if c.dead && got != deadMoveScore {
				t.Errorf("score = %v, want deadMoveScore (%v)", got, deadMoveScore)
			}
			if !c.dead && got <= 0 {
				t.Errorf("score = %v, want a positive score for a move that can act", got)
			}
		})
	}
}

// Rest carries no Primary/Self effect block in this dataset — just a "heal"
// flag the engine special-cases by move id — so it never reaches statusScore's
// heal branch and lands on the generic fallback instead. The Rest exception in
// that branch is therefore unreachable today; it is kept because it is correct
// if the move ever grows an effect block, and pinned here so the reason is not
// re-derived.
//
// The consequence is that the heuristic under-values Rest. That is a
// pre-existing strategy weakness, not a provable-waste bug, and it is
// deliberately NOT fixed here: teaching the baseline to use Rest well would
// change how it plays, and every published number is measured against this
// opponent. Correctness fixes and strength changes are separate decisions.
func TestHeuristic_RestIsNotRoutedThroughTheHealBranch(t *testing.T) {
	d := loadDex(t)
	rest, ok := d.Moves["rest"]
	if !ok {
		t.Skip("rest not in the dataset")
	}
	if rest.Primary != nil || rest.Self != nil {
		t.Skip("rest grew an effect block; the heal branch now applies and this test is obsolete")
	}
	a := NewHeuristicAgent(d)

	// Full HP, no status: nothing to gain, yet it is not scored dead, because
	// the guard cannot see it.
	v := deadMoveView(healthy("Snorlax", "normal"), "rest", healthy("Starmie", "water"))
	got := a.score(v, engine.Action{Kind: engine.ActionMove, Index: 0})
	if got == deadMoveScore {
		t.Fatal("rest reached the dead-move guard; the dataset or the branch changed — update this test")
	}
	// It scores low enough that any real attack outranks it, which is why the
	// gap costs turns only in positions where nothing else works.
	if got > 10 {
		t.Errorf("rest scored %v; expected the low generic fallback", got)
	}
}

// The regression that started this: a dead status move in an early slot used to
// tie at 0 with a zero-damage attack and win on slot order, so the agent
// replayed it every turn. Decide must now reach past it.
func TestHeuristic_DoesNotRepeatADeadStatusMove(t *testing.T) {
	d := loadDex(t)
	a := NewHeuristicAgent(d)

	self := healthy("Pikachu", "electric")
	// Thunder Wave first, a real attack second — the ordering that used to lose.
	self.Moves = []engine.MoveSlot{
		{MoveID: "thunder-wave", PP: 10, MaxPP: 10},
		{MoveID: "thunderbolt", PP: 10, MaxPP: 10},
	}
	foe := healthy("Starmie", "water")
	foe.Status = engine.StatusParalysis

	v := View{
		Me:    0,
		Turn:  5,
		Self:  engine.Side{Trainer: "P0", Active: 0, Team: []engine.Pokemon{self}},
		Foe:   foe,
		Phase: engine.PhaseChoosing,
	}
	act, err := a.Decide(context.Background(), v)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if act.Kind == engine.ActionMove && act.Index == 0 {
		t.Error("picked Thunder Wave against an already-paralyzed foe; the dead-move guard is not holding")
	}
}

func TestAllBoostsAtCap(t *testing.T) {
	at6 := engine.Stages{Atk: 6, Def: 6, Spe: 3}

	cases := []struct {
		name   string
		boosts map[string]int
		want   bool
	}{
		{"single stat at cap", map[string]int{"atk": 1}, true},
		{"single stat with room", map[string]int{"spe": 1}, false},
		{"all boosted stats at cap", map[string]int{"atk": 1, "def": 1}, true},
		// One stat with room means the move still does something.
		{"one of two has room", map[string]int{"atk": 1, "spe": 1}, false},
		// A self-drop is a cost still to be paid, so the move is not inert.
		{"negative entries ignored", map[string]int{"atk": 1, "def": -1}, true},
		{"only negatives is not capped", map[string]int{"def": -1}, false},
		// Never guess on a key we do not recognize.
		{"unknown stat key", map[string]int{"mystery": 1}, false},
		{"empty", map[string]int{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := allBoostsAtCap(c.boosts, at6); got != c.want {
				t.Errorf("allBoostsAtCap(%v) = %v, want %v", c.boosts, got, c.want)
			}
		})
	}
}
