package engine

import (
	"fmt"
	"testing"

	"pokearena/internal/domain"
)

// items_integration_test.go plays real battles to completion with items held,
// rather than probing one mechanic at a time. The unit tests in
// items_berries_test.go prove each item does the right thing in a hand-built
// state; these prove that holding one never corrupts a battle — no negative HP,
// no fainted-but-alive Pokémon, no hang, no divergence between two runs of the
// same seed.
//
// The sweep is driven off itemRegistry, so an item family added later is
// covered the day it is registered without touching this file.

// integrationTeam is a six-Pokémon roster spanning the type chart, so a battle
// exercises super-effective and resisted matchups (which several items key on)
// rather than a single neutral mirror.
var integrationTeam = []int{143, 6, 9, 65, 94, 112} // Snorlax, Charizard, Blastoise, Alakazam, Gengar, Rhydon

// buildItemBattle makes a battle where every Pokémon on side 0 holds item and
// side 1 holds nothing, so any difference in outcome is attributable.
//
// Moves are chosen by hand rather than taken off the learnset head: the sweep
// needs damage in both categories, a status move, and a healing move so the
// HP-threshold, category-reactive, and status-cure triggers all get a chance to
// fire during a normal battle.
func buildItemBattle(t *testing.T, d *domain.Dex, item ItemKind, seed uint64) *BattleState {
	t.Helper()
	picks := make([]TeamPick, 0, len(integrationTeam))
	for _, dexNo := range integrationTeam {
		sp, ok := d.Species[dexNo]
		if !ok {
			t.Fatalf("species %d missing from the dex", dexNo)
		}
		moves := pickIntegrationMoves(d, sp)
		if len(moves) == 0 {
			t.Fatalf("%s: no usable moves found", sp.Name)
		}
		picks = append(picks, TeamPick{DexNo: dexNo, MoveIDs: moves})
	}
	held := make([]TeamPick, len(picks))
	copy(held, picks)
	for i := range held {
		held[i].Item = string(item)
	}
	// Item Clause is relaxed on purpose: the point of this sweep is to give
	// one item as many chances to fire as a battle allows, and that means six
	// holders. Every other rule still applies, so a fixture that breaks
	// ordinary legality still fails here.
	clauses := StandardClauses()
	clauses.Item = false
	if err := ValidateTeamWithClauses(held, d, clauses); err != nil {
		t.Fatalf("item %q: team rejected by ValidateTeam: %v", item, err)
	}
	s, err := NewBattleFromPicks(d, fmt.Sprintf("itemsweep-%s-%d", item, seed),
		"Holders", held, "Bare", picks, seed)
	if err != nil {
		t.Fatalf("item %q: new battle: %v", item, err)
	}
	return s
}

// pickIntegrationMoves takes up to four moves off a species' learnset with a
// deliberate spread: one physical, one special, one status, then whatever fills
// the last slot. Deterministic (learnset order is stable) so the sweep replays.
func pickIntegrationMoves(d *domain.Dex, sp domain.Species) []string {
	var phys, spec, stat string
	for _, id := range sp.Moves {
		m, ok := d.Moves[id]
		if !ok {
			continue
		}
		switch {
		case m.Category == domain.CatPhysical && m.Power > 0 && phys == "":
			phys = id
		case m.Category == domain.CatSpecial && m.Power > 0 && spec == "":
			spec = id
		case m.Category == domain.CatStatus && stat == "":
			stat = id
		}
	}
	out := make([]string, 0, MovesMax)
	for _, id := range []string{phys, spec, stat} {
		if id != "" {
			out = append(out, id)
		}
	}
	// Fill the remaining slot(s) with the first learnset entries not already in.
	seen := map[string]bool{}
	for _, id := range out {
		seen[id] = true
	}
	for _, id := range sp.Moves {
		if len(out) >= MovesMax {
			break
		}
		if _, ok := d.Moves[id]; !ok || seen[id] {
			continue
		}
		out = append(out, id)
		seen[id] = true
	}
	return out
}

// playToEnd drives a battle to completion with a seeded random-legal policy,
// checking the structural invariants after every resolution. A random policy is
// deliberately chosen over the heuristic: it wanders into switch-heavy, stall-
// heavy, faint-heavy lines that a competent policy avoids, which is exactly
// where item bookkeeping breaks.
//
// It returns the concatenated log so callers can compare two runs byte for byte.
func playToEnd(t *testing.T, d *domain.Dex, s *BattleState, policySeed uint64) []LogLine {
	t.Helper()
	rng := NewRNG(policySeed)
	var full []LogLine
	for turns := 0; !s.Ended(); turns++ {
		if turns > maxTurns*3 {
			t.Fatalf("battle did not terminate after %d resolutions (phase %q, turn %d)",
				turns, s.Phase, s.Turn)
		}
		switch s.Phase {
		case PhaseChoosing:
			var acts [2]Action
			for side := 0; side < 2; side++ {
				legal := LegalActionsDex(d, s, side)
				if len(legal) == 0 {
					t.Fatalf("side %d has no legal action in PhaseChoosing (turn %d)", side, s.Turn)
				}
				acts[side] = legal[rng.IntN(len(legal))]
			}
			full = append(full, ResolveTurn(d, s, acts)...)
		case PhaseReplace:
			var sw [2]*Action
			for side := 0; side < 2; side++ {
				if !s.Replace[side] {
					continue
				}
				legal := LegalActionsDex(d, s, side)
				if len(legal) == 0 {
					t.Fatalf("side %d must replace but has no legal switch (turn %d)", side, s.Turn)
				}
				a := legal[rng.IntN(len(legal))]
				sw[side] = &a
			}
			full = append(full, ResolveReplace(s, sw)...)
		default:
			t.Fatalf("unexpected phase %q", s.Phase)
		}
		if err := ValidateStateInvariants(s); err != nil {
			t.Fatalf("state invariants broken after turn %d: %v", s.Turn, err)
		}
		if err := checkItemInvariants(s); err != nil {
			t.Fatalf("item invariants broken after turn %d: %v", s.Turn, err)
		}
	}
	return full
}

// checkItemInvariants asserts the item-specific properties no single mechanic
// owns but every mechanic can break: a held slug must stay a slug the catalog
// knows (a consumed item becomes ItemNone, never garbage), and a Pokémon must
// never end a turn holding an item while also carrying that item's spent
// marker.
func checkItemInvariants(s *BattleState) error {
	for side := 0; side < 2; side++ {
		for i := range s.Sides[side].Team {
			p := &s.Sides[side].Team[i]
			if p.Item == ItemNone {
				continue
			}
			if _, ok := itemRegistry[p.Item]; !ok {
				return fmt.Errorf("side %d team[%d] %s holds unregistered item %q",
					side, i, p.Name, p.Item)
			}
			// A choice lock can only exist on a holder of a Choice item.
			if p.Volatiles.ChoiceLockMoveID != "" && !isChoiceLockItem(p) {
				return fmt.Errorf("side %d team[%d] %s is choice-locked to %q while holding %q",
					side, i, p.Name, p.Volatiles.ChoiceLockMoveID, p.Item)
			}
		}
	}
	return nil
}

// TestFullBattleWithEveryModeledItem is the sweep: every item in the registry
// gets a full battle where one whole side holds it. Each item is a subtest so a
// failure names the culprit directly instead of reporting "some battle broke".
func TestFullBattleWithEveryModeledItem(t *testing.T) {
	d := loadDex(t)
	for item := range itemRegistry {
		t.Run(string(item), func(t *testing.T) {
			for seed := uint64(1); seed <= 4; seed++ {
				s := buildItemBattle(t, d, item, seed)
				playToEnd(t, d, s, seed*31+7)
				if s.Winner < 0 || s.Winner > 2 {
					t.Fatalf("seed %d: battle ended with winner=%d", seed, s.Winner)
				}
			}
		})
	}
}

// TestFullBattleWithItemsIsDeterministic is the property the replay system and
// the benchmark both stand on: item triggers must draw from the battle's own RNG
// stream, never a fresh one. Starf Berry's random stat pick is the case that
// would break this, and the failure would only ever show up as an unreproducible
// battle — so it is asserted over every item, not just the obviously random one.
func TestFullBattleWithItemsIsDeterministic(t *testing.T) {
	d := loadDex(t)
	for item := range itemRegistry {
		t.Run(string(item), func(t *testing.T) {
			run := func() ([]LogLine, int, int) {
				s := buildItemBattle(t, d, item, 21)
				log := playToEnd(t, d, s, 99)
				return log, s.Winner, s.Turn
			}
			logA, winA, turnA := run()
			logB, winB, turnB := run()
			if winA != winB || turnA != turnB {
				t.Fatalf("same seed diverged: winner %d/%d, turns %d/%d", winA, winB, turnA, turnB)
			}
			if len(logA) != len(logB) {
				t.Fatalf("log length diverged: %d vs %d lines", len(logA), len(logB))
			}
			for i := range logA {
				if logA[i] != logB[i] {
					t.Fatalf("log diverged at line %d:\n  %+v\n  %+v", i, logA[i], logB[i])
				}
			}
		})
	}
}

// TestItemBattlesRespectRNGStateHandoff: RNGState is what a resumed battle
// replays from, so an item trigger that draws from the stream must leave the
// state advanced and an item that draws nothing must leave it alone. A trigger
// that used its own RNG would pass every behavior test and silently break
// mid-battle resume (the message-redelivery path the engine doc calls out).
func TestItemBattlesRespectRNGStateHandoff(t *testing.T) {
	d := loadDex(t)
	// Starf is the item that actually draws. The sweep fixture's rosters trade
	// real damage, which can KO a quarter-HP holder before its berry fires, so
	// this builds a controlled 1v1 where nothing but the berry happens.
	s, err := NewBattle(d, "rng", "P1", []int{143}, "P2", []int{143}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability = AbilityNone
	holder.Item = ItemStarfBerry
	holder.HP = holder.MaxHP / 4
	holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Ability = AbilityNone
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	before := s.RNGState
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.RNGState == before {
		t.Errorf("RNGState unchanged across a turn that drew from the stream")
	}
	if s.Active(0).Item != ItemNone {
		t.Fatalf("Starf did not fire; the draw under test never happened")
	}

	// Resuming a clone from the carried state must reproduce the next turn
	// exactly — the property message redelivery depends on.
	c1, c2 := s.Clone(), s.Clone()
	acts := [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}}
	l1 := ResolveTurn(d, c1, acts)
	l2 := ResolveTurn(d, c2, acts)
	if len(l1) != len(l2) {
		t.Fatalf("resumed turn diverged in length: %d vs %d", len(l1), len(l2))
	}
	for i := range l1 {
		if l1[i] != l2[i] {
			t.Fatalf("resumed turn diverged at line %d: %+v vs %+v", i, l1[i], l2[i])
		}
	}
}

// TestCloneCarriesItemState: the AI search clones the state thousands of times
// per turn. If Clone shared or dropped item state, a rollout would either
// corrupt the live battle or misjudge it. Both directions are checked: the clone
// must start equal, and mutating it must not touch the original.
func TestCloneCarriesItemState(t *testing.T) {
	d := loadDex(t)
	s := buildItemBattle(t, d, ItemSitrusBerry, 3)
	orig := s.Active(0)
	orig.Volatiles.MicleTurns = 2
	orig.Volatiles.CustapBoost = true
	orig.Volatiles.ChoiceLockMoveID = orig.Moves[0].MoveID

	c := s.Clone()
	cl := c.Active(0)
	if cl.Item != orig.Item {
		t.Errorf("clone lost the held item: %q vs %q", cl.Item, orig.Item)
	}
	if cl.Volatiles.MicleTurns != orig.Volatiles.MicleTurns || !cl.Volatiles.CustapBoost {
		t.Errorf("clone lost item volatiles: %+v", cl.Volatiles)
	}
	if cl.Volatiles.ChoiceLockMoveID != orig.Volatiles.ChoiceLockMoveID {
		t.Errorf("clone lost the choice lock: %q", cl.Volatiles.ChoiceLockMoveID)
	}

	consumeItem(cl)
	cl.Volatiles.MicleTurns = 0
	if orig.Item == ItemNone {
		t.Errorf("consuming the clone's item cleared the original's")
	}
	if orig.Volatiles.MicleTurns == 0 {
		t.Errorf("clearing the clone's volatile cleared the original's")
	}
}

// TestItemsNeverStrandABattle: every item held by both sides at once, played
// out. The mutual case is where ordering bugs live — two Custap holders racing
// for the same bracket, two Jaboca holders chipping each other, two resist
// berries firing on the same exchange.
func TestItemsNeverStrandABattle(t *testing.T) {
	d := loadDex(t)
	for item := range itemRegistry {
		t.Run(string(item), func(t *testing.T) {
			s := buildItemBattle(t, d, item, 13)
			// Give side 1 the same item so both sides trigger.
			for i := range s.Sides[1].Team {
				s.Sides[1].Team[i].Item = item
			}
			playToEnd(t, d, s, 41)
			if s.Winner < 0 || s.Winner > 2 {
				t.Fatalf("battle ended with winner=%d", s.Winner)
			}
		})
	}
}
