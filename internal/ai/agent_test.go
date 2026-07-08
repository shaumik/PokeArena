package ai

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

func loadDex(t *testing.T) *domain.Dex {
	t.Helper()
	d, err := domain.LoadDex("../../data", "test")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	return d
}

func TestRandomAgentReturnsLegal(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9}, "B", []int{3, 65}, 1)
	a := NewRandomAgent(42)
	for i := 0; i < 50; i++ {
		v := MakeView(s, 0)
		act, _ := a.Decide(context.Background(), v)
		if !isLegal(v, act) {
			t.Fatalf("random agent produced illegal action %+v", act)
		}
	}
}

func TestHeuristicTakesKnockout(t *testing.T) {
	d := loadDex(t)
	// Charizard vs Vileplume — drop the foe to a sliver of HP.
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{45}, 1)
	s.Sides[1].Team[0].HP = 8
	v := MakeView(s, 0)

	act, _ := NewHeuristicAgent(d).Decide(context.Background(), v)
	if act.Kind != engine.ActionMove || act.Index < 0 {
		t.Fatalf("expected a real move, got %+v", act)
	}
	m := d.Moves[v.Self.Team[0].Moves[act.Index].MoveID]
	if dmg := engine.ExpectedDamage(d, &v.Self.Team[0], &v.Foe, m, v.Weather, v.Terrain, &v.FoeConditions); dmg < v.Foe.HP {
		t.Fatalf("heuristic skipped the KO: chose %s (%d dmg vs %d HP)", m.Name, dmg, v.Foe.HP)
	}
}

func TestExpectimaxReturnsLegalWithinBudget(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9, 26}, "B", []int{3, 65, 143}, 5)
	v := MakeView(s, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	act, err := NewExpectimaxAgent(d).Decide(ctx, v)
	if err != nil {
		t.Fatalf("expectimax error: %v", err)
	}
	if !isLegal(v, act) {
		t.Fatalf("expectimax produced illegal action %+v", act)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("expectimax overran its budget: %v", elapsed)
	}
}

// panicAgent and slowAgent are fault injectors for the harness.
type panicAgent struct{}

func (panicAgent) Name() string { return "panic" }
func (panicAgent) Decide(context.Context, View) (engine.Action, error) {
	panic("boom")
}

type slowAgent struct{}

func (slowAgent) Name() string { return "slow" }
func (slowAgent) Decide(ctx context.Context, v View) (engine.Action, error) {
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
	}
	return engine.Action{Kind: engine.ActionMove, Index: 0}, nil
}

func TestHarnessFallsBackOnPanic(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9}, "B", []int{3, 65}, 1)
	h := &Harness{primary: panicAgent{}, fallback: NewHeuristicAgent(d), budget: 200 * time.Millisecond}
	act := h.Decide(s, 0)
	if !isLegal(MakeView(s, 0), act) {
		t.Fatalf("harness did not recover from a panicking agent: %+v", act)
	}
}

func TestHarnessFallsBackOnTimeout(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9}, "B", []int{3, 65}, 1)
	h := &Harness{primary: slowAgent{}, fallback: NewHeuristicAgent(d), budget: 50 * time.Millisecond}
	start := time.Now()
	act := h.Decide(s, 0)
	if time.Since(start) > 2*time.Second {
		t.Fatal("harness did not enforce the time budget")
	}
	if !isLegal(MakeView(s, 0), act) {
		t.Fatalf("harness fallback produced an illegal action: %+v", act)
	}
}

func TestMakeView_RedactsFoeFog(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	foe := &s.Sides[1].Team[0]

	// Burn a single PP off slot 1: it has been "used" and should be revealed.
	if len(foe.Moves) < 2 {
		t.Fatalf("test fixture needs at least 2 moves on the foe; got %d", len(foe.Moves))
	}
	foe.Moves[1].PP--
	usedMoveID := foe.Moves[1].MoveID

	// Pile on hidden status state so we can confirm it's all zeroed.
	foe.Status = engine.StatusToxic
	foe.ToxicCounter = 7
	foe.SleepTurns = 3
	foe.Volatiles.Confusion = &engine.ConfusionState{Turns: 4}
	foe.HP = 137 // odd value, off a 1%-bucket grid

	v := MakeView(s, 0)

	// Used move stays visible; the others are blanked but the slot count remains.
	if got := len(v.Foe.Moves); got != len(foe.Moves) {
		t.Fatalf("redaction must preserve move-slot count: got %d, want %d", got, len(foe.Moves))
	}
	revealed := 0
	for _, m := range v.Foe.Moves {
		if m.MoveID != "" {
			revealed++
		}
	}
	if revealed != 1 {
		t.Fatalf("expected exactly one revealed move, got %d", revealed)
	}
	if v.Foe.Moves[1].MoveID != usedMoveID {
		t.Fatalf("revealed slot 1 should carry the used move %q, got %q", usedMoveID, v.Foe.Moves[1].MoveID)
	}

	// HP redacted to the nearest 1% bucket; original 137 must not leak.
	if v.Foe.HP == 137 {
		t.Errorf("exact HP %d leaked through redaction", v.Foe.HP)
	}

	// Status visible, clocks hidden.
	if v.Foe.Status != engine.StatusToxic {
		t.Errorf("status condition was redacted: got %q, want %q", v.Foe.Status, engine.StatusToxic)
	}
	if v.Foe.ToxicCounter != 0 {
		t.Errorf("ToxicCounter must be zeroed; got %d", v.Foe.ToxicCounter)
	}
	if v.Foe.SleepTurns != 0 {
		t.Errorf("SleepTurns must be zeroed; got %d", v.Foe.SleepTurns)
	}
	if v.Foe.Volatiles.Confusion == nil {
		t.Errorf("confusion presence must remain visible; got nil")
	} else if v.Foe.Volatiles.Confusion.Turns != 0 {
		t.Errorf("confusion turn count must be hidden; got %d", v.Foe.Volatiles.Confusion.Turns)
	}

	// Source state must not be mutated by view construction.
	if foe.HP != 137 || foe.ToxicCounter != 7 {
		t.Fatalf("MakeView mutated the source BattleState: HP=%d, ToxicCounter=%d", foe.HP, foe.ToxicCounter)
	}
}

// TestLegalActionsMatchesEngine guards the consolidation: ai.LegalActions is
// a shim over engine.LegalActions on a reconstructed BattleState, so the two
// must agree on every state for the deciding side. If anyone reintroduces a
// parallel implementation in this package, this test fires immediately.
//
// One row per gate engine.LegalActions checks. The PartialTrap row is the
// historical regression case (a Whirlpool-trapped AI proposed a switch the
// engine refused, hanging collectActions). The rest existed before the
// catch — added here so we never re-learn them the same way.
func TestLegalActionsMatchesEngine(t *testing.T) {
	d := loadDex(t)
	cases := []struct {
		name string
		mut  func(*engine.BattleState)
	}{
		{"baseline", func(s *engine.BattleState) {}},

		// Switch-blocking volatiles.
		{"partial_trap_blocks_switch", func(s *engine.BattleState) {
			s.Sides[0].Team[s.Sides[0].Active].Volatiles.PartialTrap = &engine.PartialTrapState{Turns: 3, MoveName: "Whirlpool"}
		}},
		{"ingrain_blocks_switch", func(s *engine.BattleState) {
			s.Sides[0].Team[s.Sides[0].Active].Volatiles.Ingrain = true
		}},

		// Lock-into-move volatiles.
		{"charging_locks_into_move", func(s *engine.BattleState) {
			s.Sides[0].Team[s.Sides[0].Active].Volatiles.Charging = &engine.ChargingState{MoveIdx: 1}
		}},
		{"must_recharge_returns_sentinel", func(s *engine.BattleState) {
			s.Sides[0].Team[s.Sides[0].Active].Volatiles.MustRecharge = true
		}},

		// Per-slot restrictions (lockRestrict).
		{"disable_drops_one_slot", func(s *engine.BattleState) {
			act := &s.Sides[0].Team[s.Sides[0].Active]
			s.Sides[0].Team[s.Sides[0].Active].Volatiles.Disable = &engine.DisableState{MoveID: act.Moves[0].MoveID, Turns: 4}
		}},
		{"encore_forces_one_slot", func(s *engine.BattleState) {
			act := &s.Sides[0].Team[s.Sides[0].Active]
			s.Sides[0].Team[s.Sides[0].Active].Volatiles.Encore = &engine.EncoreState{MoveID: act.Moves[0].MoveID, Turns: 3}
			s.Sides[0].Team[s.Sides[0].Active].Volatiles.LastMoveID = act.Moves[0].MoveID
		}},
		{"torment_blocks_last_move", func(s *engine.BattleState) {
			act := &s.Sides[0].Team[s.Sides[0].Active]
			s.Sides[0].Team[s.Sides[0].Active].Volatiles.Torment = true
			s.Sides[0].Team[s.Sides[0].Active].Volatiles.LastMoveID = act.Moves[0].MoveID
		}},
		{"imprison_blocks_shared_slots", func(s *engine.BattleState) {
			selfAct := &s.Sides[1].Team[s.Sides[1].Active]
			foeAct := &s.Sides[0].Team[s.Sides[0].Active]
			s.Sides[1].Team[s.Sides[1].Active].Volatiles.Imprison = &engine.ImprisonState{MoveIDs: []string{foeAct.Moves[0].MoveID}}
			_ = selfAct
		}},

		// Resource / replacement edges.
		{"all_pp_drained_forces_struggle", func(s *engine.BattleState) {
			act := &s.Sides[0].Team[s.Sides[0].Active]
			for i := range act.Moves {
				act.Moves[i].PP = 0
			}
		}},
		{"replace_phase_switches_only", func(s *engine.BattleState) {
			s.Phase = engine.PhaseReplace
			s.Replace[0] = true
			s.Sides[0].Team[s.Sides[0].Active].Fainted = true
			s.Sides[0].Team[s.Sides[0].Active].HP = 0
		}},
		{"trapped_with_dead_bench_only_moves", func(s *engine.BattleState) {
			s.Sides[0].Team[s.Sides[0].Active].Volatiles.PartialTrap = &engine.PartialTrapState{Turns: 2, MoveName: "Whirlpool"}
			for i := range s.Sides[0].Team {
				if i != s.Sides[0].Active {
					s.Sides[0].Team[i].Fainted = true
					s.Sides[0].Team[i].HP = 0
				}
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := engine.NewBattle(d, "b", "R", []int{6, 9, 26}, "B", []int{3, 65, 143}, 1)
			tc.mut(s)

			want := engine.LegalActions(s, 0)
			got := LegalActions(MakeView(s, 0))

			if len(got) != len(want) {
				t.Fatalf("count mismatch: ai=%d engine=%d\nai=%+v\nengine=%+v",
					len(got), len(want), got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("index %d: ai=%+v engine=%+v", i, got[i], want[i])
				}
			}
		})
	}
}

// TestReconstructFromView_PreservesField: the AI's simulator is fed a
// View, and the View has to carry every field condition that affects
// move resolution — weather, terrain, side conditions. Earlier the
// expectimax reconstruct hardcoded Phase=Choosing and dropped Weather
// and Terrain, so the agent simulated turns where sandstorm chip,
// rain-boosted Water moves, and grassy-heal were silently absent —
// degrading hard-mode play without crashing.
func TestReconstructFromView_PreservesField(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "A", []int{6}, "B", []int{3}, 1)
	s.Weather = &engine.WeatherState{Kind: engine.WeatherSandstorm, TurnsLeft: 4}
	s.Terrain = &engine.TerrainState{Kind: engine.TerrainElectric, TurnsLeft: 5}

	sim := reconstructFromView(MakeView(s, 0))
	if sim.Weather == nil || sim.Weather.Kind != engine.WeatherSandstorm {
		t.Errorf("Weather lost in reconstruction: %+v", sim.Weather)
	}
	if sim.Terrain == nil || sim.Terrain.Kind != engine.TerrainElectric {
		t.Errorf("Terrain lost in reconstruction: %+v", sim.Terrain)
	}
}

// TestAIDecideAlwaysLegal: drive a long AI-vs-AI battle and assert that
// every action the harness returns is legal per engine.LegalActions. This
// is the integration form of the parity test — covers any drift the table
// rows miss, and catches the original "Whirlpool stall" by construction
// (illegal action would fail the assertion before the gateway ever saw it).
func TestAIDecideAlwaysLegal(t *testing.T) {
	d := loadDex(t)
	h := [2]*Harness{NewHarness(d, 100*time.Millisecond), NewHarness(d, 100*time.Millisecond)}
	s, _ := engine.NewBattle(d, "b", "Red", []int{6, 9, 26}, "Blue", []int{3, 65, 143}, 7)

	for guard := 0; !s.Ended(); guard++ {
		if guard > 2000 {
			t.Fatal("battle failed to terminate")
		}
		for side := 0; side < 2; side++ {
			if s.Phase == engine.PhaseReplace && !s.Replace[side] {
				continue
			}
			act := h[side].Decide(s, side)
			legal := engine.LegalActions(s, side)
			found := false
			for _, a := range legal {
				if a == act {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("turn %d side %d: harness returned illegal action %+v; legal=%+v",
					s.Turn, side, act, legal)
			}
		}
		switch s.Phase {
		case engine.PhaseChoosing:
			engine.ResolveTurn(d, s, [2]engine.Action{h[0].Decide(s, 0), h[1].Decide(s, 1)})
		case engine.PhaseReplace:
			var sw [2]*engine.Action
			for i := 0; i < 2; i++ {
				if s.Replace[i] {
					a := h[i].Decide(s, i)
					sw[i] = &a
				}
			}
			engine.ResolveReplace(s, sw)
		}
	}
}

func TestMakeView_LiveFoeNeverFakeFaints(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	foe := &s.Sides[1].Team[0]
	// 1 HP out of a few hundred — strictly less than the 1%-bucket size.
	foe.HP = 1
	v := MakeView(s, 0)
	if v.Foe.HP <= 0 {
		t.Fatalf("a live foe (HP=1) must never round to 0 in the view; got %d", v.Foe.HP)
	}
}

// TestView_RoundTripsFoeHPPct locks the inverse of the wire contract: a View
// marshaled and decoded back must RECOVER the foe's public HP percentage. The
// wire drops absolute hp/max_hp (fog of war), so without View.UnmarshalJSON the
// decoded foe reads as fainted — the relay bug that made a live opponent look
// dead. After the round-trip, pctHP(Foe.HP, Foe.MaxHP) equals the sent hp_pct.
func TestView_RoundTripsFoeHPPct(t *testing.T) {
	v := View{Turn: 4, Foe: engine.Pokemon{Name: "Gengar", Type1: "ghost", HP: 120, MaxHP: 240}}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got View
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// hp/max_hp are gone from the wire; the percentage survives as HP out of 100.
	if got.Foe.HP != 50 || got.Foe.MaxHP != 100 {
		t.Fatalf("decoded foe HP = %d/%d, want 50/100 (recovered from hp_pct)", got.Foe.HP, got.Foe.MaxHP)
	}
	// A live foe must never decode as fainted.
	if got.Foe.HP <= 0 {
		t.Error("decoded foe reads as fainted — the relay bug is back")
	}
	if got.Foe.Name != "Gengar" || got.Foe.Type1 != "ghost" {
		t.Errorf("foe identity lost in round-trip: %+v", got.Foe)
	}
}

// TestView_FoeSerializesAsPercent locks the wire contract: a client must
// see the foe's HP only as a percentage (hp_pct), never an absolute count.
// This is the fix for the bug where a Golem at 1 HP serialized as
// "hp":7,"max_hp":155 — a fog-redacted value masquerading as exact.
func TestView_FoeSerializesAsPercent(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	foe := &s.Sides[1].Team[0]
	fullMax := foe.MaxHP
	foe.HP = 1 // the bug case: a sliver must read as a sliver, not round up
	v := MakeView(s, 0)

	var wire struct {
		Foe map[string]json.RawMessage `json:"foe"`
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}

	// Absolute HP and max HP must not leak to clients.
	if _, ok := wire.Foe["hp"]; ok {
		t.Errorf("foe leaked absolute hp on the wire: %s", raw)
	}
	if _, ok := wire.Foe["max_hp"]; ok {
		t.Errorf("foe leaked absolute max_hp on the wire: %s", raw)
	}

	pct := unmarshalInt(t, wire.Foe, "hp_pct")
	if pct < 1 || pct > 100 {
		t.Errorf("hp_pct out of range: got %d", pct)
	}
	if pct > 5 {
		t.Errorf("a 1-HP foe must read as a sliver, not %d%%", pct)
	}

	// A full-HP foe reads as exactly 100% — never one bucket short.
	foe.HP = fullMax
	v = MakeView(s, 0)
	raw, _ = json.Marshal(v)
	_ = json.Unmarshal(raw, &wire)
	if got := unmarshalInt(t, wire.Foe, "hp_pct"); got != 100 {
		t.Errorf("full-HP foe must read 100%%, got %d", got)
	}
}

// TestMakeView_CarriesPseudoWeather: rooms and Gravity are field-wide,
// loudly announced, public info — they must reach agents (Trick Room
// inverts move order; deciding without it is deciding blind), and the
// reconstruction path must carry them back so sims honor them.
func TestMakeView_CarriesPseudoWeather(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	s.PseudoWeather.TrickRoom = &engine.PWTimer{TurnsLeft: 3}

	v := MakeView(s, 0)
	if v.PseudoWeather.TrickRoom == nil || v.PseudoWeather.TrickRoom.TurnsLeft != 3 {
		t.Fatalf("view must carry Trick Room with its timer, got %+v", v.PseudoWeather.TrickRoom)
	}
	// The view owns a clone — mutating it must not reach back into the battle.
	v.PseudoWeather.TrickRoom.TurnsLeft = 99
	if s.PseudoWeather.TrickRoom.TurnsLeft != 3 {
		t.Errorf("view aliases the battle's pseudo-weather timer")
	}

	// And it must survive reconstruction, so expectimax rollouts see it.
	r := reconstructFromView(v)
	if r.PseudoWeather.TrickRoom == nil {
		t.Errorf("reconstructFromView dropped pseudo-weather")
	}

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var wire struct {
		PseudoWeather map[string]json.RawMessage `json:"pseudo_weather"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	if _, ok := wire.PseudoWeather["trick_room"]; !ok {
		t.Errorf("pseudo_weather.trick_room missing from the wire: %s", raw)
	}
}

// TestMakeView_FoeWishRedacted: a foe's pending Wish is public as an
// event (the move is used in plain sight) but its Amount is the caster's
// MaxHP/2 — hidden HP investment. The View carries who cast it and when
// it lands; the figure never appears, in the struct or on the wire.
func TestMakeView_FoeWishRedacted(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	s.Sides[1].SlotConditions.Wish = &engine.WishState{Healer: "Blissey", Amount: 357, TurnsLeft: 1}
	s.Sides[1].SlotConditions.HealingWish = true

	v := MakeView(s, 0)
	w := v.FoeSlotConditions.Wish
	if w == nil || w.Healer != "Blissey" || w.TurnsLeft != 1 {
		t.Fatalf("foe wish event must be visible, got %+v", w)
	}
	if !v.FoeSlotConditions.HealingWish {
		t.Errorf("foe healing wish flag must be visible")
	}

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var wire struct {
		FoeSlot struct {
			Wish map[string]json.RawMessage `json:"wish"`
		} `json:"foe_slot_conditions"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	if wire.FoeSlot.Wish == nil {
		t.Fatalf("foe_slot_conditions.wish missing from the wire: %s", raw)
	}
	if _, ok := wire.FoeSlot.Wish["amount"]; ok {
		t.Errorf("foe wish leaked its heal amount on the wire: %s", raw)
	}

	// Reconstruction rebuilds a Wish for sims (estimated amount) — the
	// pending heal must not vanish from rollouts.
	r := reconstructFromView(v)
	rw := r.Sides[1].SlotConditions.Wish
	if rw == nil || rw.TurnsLeft != 1 {
		t.Fatalf("reconstructFromView dropped the foe's pending wish, got %+v", rw)
	}
	if rw.Amount == 357 {
		t.Errorf("reconstructed wish must use an estimate, not the hidden amount")
	}

	// Our own pending Wish must not alias the battle's pointer: sims tick
	// timers on reconstructed state and would mutate the real battle.
	s.Sides[0].SlotConditions.Wish = &engine.WishState{Healer: "Me", Amount: 100, TurnsLeft: 2}
	v = MakeView(s, 0)
	v.Self.SlotConditions.Wish.TurnsLeft = 99
	if s.Sides[0].SlotConditions.Wish.TurnsLeft != 2 {
		t.Errorf("view aliases the battle's own-side wish state")
	}
}

// TestView_FoeWireMatchesShowdownFog locks the rest of the foe wire
// contract to what Pokémon Showdown sends a player about the opponent:
// no ability (Showdown reveals it only when it acts — we never send it),
// no exact stats, and revealed moves carry identity but no PP. Boosts
// and status are public in Showdown and must stay on the wire.
func TestView_FoeWireMatchesShowdownFog(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	foe := &s.Sides[1].Team[0]
	foe.Moves[0].PP-- // one revealed move: identity public, PP not
	v := MakeView(s, 0)

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var wire struct {
		Foe map[string]json.RawMessage `json:"foe"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}

	for _, key := range []string{"ability", "stats"} {
		if _, ok := wire.Foe[key]; ok {
			t.Errorf("foe leaked %q on the wire: %s", key, wire.Foe[key])
		}
	}

	var moves []map[string]json.RawMessage
	if err := json.Unmarshal(wire.Foe["moves"], &moves); err != nil {
		t.Fatalf("foe moves not a slot list: %v", err)
	}
	if len(moves) != len(foe.Moves) {
		t.Errorf("slot count must survive redaction: got %d, want %d", len(moves), len(foe.Moves))
	}
	for i, m := range moves {
		for _, key := range []string{"pp", "max_pp"} {
			if _, ok := m[key]; ok {
				t.Errorf("foe move %d leaked %q on the wire", i, key)
			}
		}
	}
	var revealed string
	if err := json.Unmarshal(moves[0]["move_id"], &revealed); err != nil || revealed != foe.Moves[0].MoveID {
		t.Errorf("revealed move identity must survive: got %q, want %q", revealed, foe.Moves[0].MoveID)
	}

	// Boosts and status are public info in Showdown — they must stay.
	for _, key := range []string{"stages", "status"} {
		if _, ok := wire.Foe[key]; !ok {
			t.Errorf("foe missing public field %q on the wire", key)
		}
	}
}

func unmarshalInt(t *testing.T, m map[string]json.RawMessage, key string) int {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("foe missing %q on the wire", key)
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("%q not an int: %v", key, err)
	}
	return n
}

func TestAIBattleTerminates(t *testing.T) {
	d := loadDex(t)
	h := [2]*Harness{NewHarness(d, 150*time.Millisecond), NewHarness(d, 150*time.Millisecond)}
	s, _ := engine.NewBattle(d, "b", "Red", []int{6, 9, 26}, "Blue", []int{3, 65, 143}, 7)

	for guard := 0; !s.Ended(); guard++ {
		if guard > 2000 {
			t.Fatal("AI-vs-AI battle failed to terminate")
		}
		switch s.Phase {
		case engine.PhaseChoosing:
			engine.ResolveTurn(d, s, [2]engine.Action{h[0].Decide(s, 0), h[1].Decide(s, 1)})
		case engine.PhaseReplace:
			var sw [2]*engine.Action
			for i := 0; i < 2; i++ {
				if s.Replace[i] {
					a := h[i].Decide(s, i)
					sw[i] = &a
				}
			}
			engine.ResolveReplace(s, sw)
		}
	}
	if s.Winner < 0 || s.Winner > 2 {
		t.Fatalf("invalid winner %d", s.Winner)
	}
}
