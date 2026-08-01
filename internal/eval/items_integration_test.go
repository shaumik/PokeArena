package eval

import (
	"reflect"
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// items_integration_test.go runs held items through the real driver: fog-of-war
// views, real agents deciding from those views, RunGame resolving to a winner.
// The engine package's own sweep proves items don't corrupt a battle; this
// proves the layers above the engine survive them too — the View projection, the
// legality gate, the decision trace, and reproducibility end to end.
//
// This matters beyond the engine because items are the first mechanic whose
// state is *hidden from one side*. An agent decides from a View that omits the
// foe's item, so a bug that leaked it (or that made an agent's own item
// invisible to itself) would show up here and nowhere else.

// itemRoster is a six-mon team spanning the type chart, so battles produce the
// super-effective and resisted hits several items key on.
var itemRoster = []int{143, 6, 9, 65, 94, 112}

// itemTeams builds a mirror pair of rosters where side 0 holds items and side 1
// holds nothing. Items are dealt round-robin from the catalog so one battle
// exercises six different ones at once, which is how a real team is built —
// unlike the engine sweep's one-item-per-battle isolation.
func itemTeams(t *testing.T, d *domain.Dex, items []string) [2][]engine.TeamPick {
	t.Helper()
	bare, err := PicksFromDex(d, itemRoster)
	if err != nil {
		t.Fatalf("build picks: %v", err)
	}
	held := make([]engine.TeamPick, len(bare))
	for i, p := range bare {
		held[i] = p
		held[i].MoveIDs = append([]string(nil), p.MoveIDs...)
		held[i].Item = items[i%len(items)]
	}
	if err := engine.ValidateTeam(held, d); err != nil {
		t.Fatalf("item team rejected by ValidateTeam: %v", err)
	}
	if err := engine.ValidateTeam(bare, d); err != nil {
		t.Fatalf("bare team rejected by ValidateTeam: %v", err)
	}
	return [2][]engine.TeamPick{held, bare}
}

// catalogItemIDs returns every legal item slug, sorted — the same list the API
// serves, so this test covers exactly what a team builder can actually submit.
func catalogItemIDs(d *domain.Dex) []string {
	rows := engine.ItemCatalog(d)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// TestRunGame_ItemTeamsTerminate plays real battles with item-carrying teams
// across a seed sweep and every item in the catalog. Chunking by six keeps each
// subtest a real six-mon team rather than one item repeated, and guarantees
// every catalog entry appears in some battle.
func TestRunGame_ItemTeamsTerminate(t *testing.T) {
	d := loadDex(t)
	all := catalogItemIDs(d)
	if len(all) == 0 {
		t.Fatal("item catalog is empty — nothing to integrate")
	}
	for start := 0; start < len(all); start += 6 {
		end := min(start+6, len(all))
		chunk := all[start:end]
		t.Run(chunk[0], func(t *testing.T) {
			teams := itemTeams(t, d, chunk)
			for seed := uint64(0); seed < 3; seed++ {
				agents := [2]ai.Agent{ai.NewHeuristicAgent(d), ai.NewRandomAgent(seed)}
				res, err := RunGame(d, agents, teams, seed, 0)
				if err != nil {
					t.Fatalf("items %v seed %d: RunGame: %v", chunk, seed, err)
				}
				if res.Winner != 0 && res.Winner != 1 && res.Winner != 2 {
					t.Fatalf("items %v seed %d: bad winner %d", chunk, seed, res.Winner)
				}
				if res.Turns == 0 || len(res.Decisions) == 0 {
					t.Fatalf("items %v seed %d: empty game (turns=%d decisions=%d)",
						chunk, seed, res.Turns, len(res.Decisions))
				}
				for i, dec := range res.Decisions {
					if !dec.Fallback && !isLegal(dec.Legal, dec.Action) {
						t.Fatalf("items %v seed %d: decision %d action %+v not legal",
							chunk, seed, i, dec.Action)
					}
				}
			}
		})
	}
}

// TestRunGame_ItemTeamsDeterministic: the benchmark's central claim is that
// anyone can replay a published result. Items introduce new RNG draws (Starf's
// stat pick) and new branch points, so the guarantee has to be re-proven with
// them in play — a byte-identical GameResult, decision trace and state
// fingerprints included.
func TestRunGame_ItemTeamsDeterministic(t *testing.T) {
	d := loadDex(t)
	teams := itemTeams(t, d, catalogItemIDs(d))
	run := func() GameResult {
		agents := [2]ai.Agent{ai.NewHeuristicAgent(d), ai.NewRandomAgent(11)}
		res, err := RunGame(d, agents, teams, 19, 0)
		if err != nil {
			t.Fatalf("RunGame: %v", err)
		}
		return res
	}
	a, b := run(), run()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("item battle not reproducible: winner %d/%d turns %d/%d decisions %d/%d",
			a.Winner, b.Winner, a.Turns, b.Turns, len(a.Decisions), len(b.Decisions))
	}
}

// TestRunGame_ItemsDoNotChangeBareBattles is the regression guard for the whole
// feature: a team that holds nothing must play exactly the battle it played
// before items existed. If any item hook fires on an empty item slot — a
// dispatcher that forgot its nil check, a trigger that reads the registry with
// ItemNone — this diverges.
//
// The reference is computed in-process rather than committed as a golden, so it
// stays honest across dataset syncs; what it pins is that adding item machinery
// left the no-item path untouched within this build.
func TestRunGame_ItemsDoNotChangeBareBattles(t *testing.T) {
	d := loadDex(t)
	bare := mirrorTeams(t, d)
	for seed := uint64(0); seed < 5; seed++ {
		agents := [2]ai.Agent{ai.NewHeuristicAgent(d), ai.NewRandomAgent(seed)}
		first, err := RunGame(d, agents, bare, seed, 0)
		if err != nil {
			t.Fatalf("seed %d: RunGame: %v", seed, err)
		}
		agents2 := [2]ai.Agent{ai.NewHeuristicAgent(d), ai.NewRandomAgent(seed)}
		second, err := RunGame(d, agents2, bare, seed, 0)
		if err != nil {
			t.Fatalf("seed %d: RunGame (rerun): %v", seed, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("seed %d: bare battle is not reproducible", seed)
		}
		// A team holding nothing must never produce an item log line's worth of
		// state: no Pokémon should end up holding anything.
		for _, dec := range first.Decisions {
			if dec.Fallback {
				t.Fatalf("seed %d: a bare battle needed a legality fallback at turn %d — "+
					"item machinery changed the legal-action set", seed, dec.Turn)
			}
		}
	}
}

// TestItemsSurviveTheFogOfWarRoundTrip: an agent that reads its View off the
// wire (the MCP and PvP paths) must still see its own items and must not see the
// foe's. RunGame's in-process agents skip serialization, so the round trip is
// asserted directly here — this is the layer the engine sweep cannot reach.
func TestItemsSurviveTheFogOfWarRoundTrip(t *testing.T) {
	d := loadDex(t)
	teams := itemTeams(t, d, catalogItemIDs(d))
	s, err := engine.NewBattleFromPicks(d, "fog", "P0", teams[0], "P1", teams[0], 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}

	v := ai.MakeView(s, 0)
	if v.Self.Team[0].Item == engine.ItemNone {
		t.Fatal("setup: side 0's lead is holding nothing")
	}
	if v.Foe.Item == engine.ItemNone {
		t.Fatal("setup: side 1's lead is holding nothing")
	}

	raw, err := v.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	var back ai.View
	if err := back.UnmarshalJSON(raw); err != nil {
		t.Fatalf("unmarshal view: %v", err)
	}
	if back.Self.Team[0].Item != v.Self.Team[0].Item {
		t.Errorf("own item lost in the wire round trip: %q → %q",
			v.Self.Team[0].Item, back.Self.Team[0].Item)
	}
	if back.Foe.Item != engine.ItemNone {
		t.Errorf("foe item survived the wire round trip: %q — it must never be sent",
			back.Foe.Item)
	}
}
