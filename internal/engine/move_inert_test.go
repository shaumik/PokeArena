package engine

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"pokearena/internal/domain"
)

// move_inert_test.go is the audit for one defect class: a curated move that
// resolves to nothing and says nothing.
//
// The class exists because of how moves reach this engine. Showdown encodes a
// great deal of move behavior as JS callbacks, and data-sync can only carry
// statics, so those moves arrive in data/moves.json as shells — a status move
// with no `primary` block. applyStatusMove's declarative tail reads a shell as
// a clean success, so the move pays its PP, logs "X used Y!", and changes
// nothing. The player is told the plan worked.
//
// It kept happening. Twelve such moves were enumerated by hand in issue #130.
// The Showdown port later found four more (the ability-setting moves) — but
// only because upstream happened to ship test cases for them, and it found
// them one at a time, filed as four unrelated rows. A hand-written list is
// only ever as complete as whoever wrote it.
//
// So this is the audit rather than another list. It plays every curated move
// in a fixture built to give that move something to act on, and requires each
// one to leave *some* mark: a change to the battle state, or a line in the log
// beyond its own name. "But it failed!" satisfies it — failing visibly is a
// legitimate outcome, and for a couple of moves it is the only honest one (see
// applyMagneticFlux and applyNaturePower). Silence does not.
//
// It found twelve moves on its first run, including four the port had no cases
// for at all, and then immediately caught a bug in their implementations: the
// first draft passed Showdown's short stat slugs ("atk", "spa") to stagePtr,
// which speaks this engine's longer ones ("attack", "spatk"), and every one of
// those calls silently did nothing. Which is the same defect, one layer down.

// inertFixture builds the battle each move is tried in. The point is fairness:
// a move judged inert must have had a real opportunity to act, or the audit
// reports its own fixture rather than the engine.
//
// Both actives are below full HP (heals have something to restore), poisoned
// (status cures and the poison-gated moves have something to work on), and
// carry differing stat stages (the swap and reset moves have something to
// move). Abilities and items are stripped so nothing on the board can absorb a
// move's effect and make it look inert.
func inertFixture(t *testing.T, d *domain.Dex, moveID string) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "inert-audit", "P1", []int{143, 65}, "P2", []int{143, 65}, 7)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			p.Item = ItemNone
			p.Ability = AbilityNone
			p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		}
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 40, MaxPP: 40}}
	for i := 0; i < 2; i++ {
		p := s.Active(i)
		p.HP = p.MaxHP * 3 / 5
		p.Status = StatusPoison
	}
	s.Active(0).Stages = Stages{Atk: 2, Def: -1, Spe: 1}
	s.Active(1).Stages = Stages{Atk: -2, Def: 3, SpA: 1}
	return s
}

// inertSnapshot serializes everything about a battle that a move could
// meaningfully change, normalizing the three things every move changes just by
// being used: the PP it spent, which move it was, and the RNG and turn
// counters. Without that normalization every move looks like it did something
// and the audit reports nothing, ever.
func inertSnapshot(s *BattleState) string {
	c := s.Clone()
	c.RNGState, c.Seed, c.Turn = 0, 0, 0
	for i := range c.Sides {
		for j := range c.Sides[i].Team {
			p := &c.Sides[i].Team[j]
			for k := range p.Moves {
				p.Moves[k].PP = p.Moves[k].MaxPP
				p.Moves[k].MoveID = ""
			}
			p.Volatiles.LastMoveID = ""
			p.Volatiles.LastMoveName = ""
		}
	}
	b, err := json.Marshal(c)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestNoCuratedMoveIsInert(t *testing.T) {
	d := loadDex(t)

	// Splash is the control, and it is the right one: it is the move whose
	// entire canonical behavior is to accomplish nothing. Anything the turn
	// produces with Splash on the board — the poison residuals, the turn
	// bookkeeping — is turn noise rather than evidence, and diffing against it
	// is what keeps the fixture's own side effects out of the result.
	ctrl := inertFixture(t, d, "splash")
	ctrlLog := ResolveTurn(d, ctrl, [2]Action{{Kind: ActionMove}, {Kind: ActionMove}})
	ctrlSnapshot := inertSnapshot(ctrl)
	turnNoise := map[string]bool{}
	for _, l := range ctrlLog {
		turnNoise[l.Text] = true
	}

	ids := make([]string, 0, len(d.Moves))
	for id := range d.Moves {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var inert []string
	for _, id := range ids {
		if id == "splash" {
			continue
		}
		s := inertFixture(t, d, id)
		log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove}, {Kind: ActionMove}})
		spoke := false
		for _, l := range log {
			// A move announcing itself is not evidence that it did anything —
			// that line is exactly what an inert move produces.
			if strings.Contains(l.Text, " used ") || turnNoise[l.Text] {
				continue
			}
			spoke = true
			break
		}
		if !spoke && inertSnapshot(s) == ctrlSnapshot {
			inert = append(inert, id)
		}
	}

	if len(inert) > 0 {
		t.Errorf("%d curated move(s) resolved to nothing and said nothing:\n\t%s\n\n"+
			"A move in this list narrates a hit and changes no state, which is worse than\n"+
			"an unimplemented move because the log claims it worked. Implement it, or make\n"+
			"it refuse visibly and record why. See callbackmoves.go for both shapes.",
			len(inert), strings.Join(inert, "\n\t"))
	}
}

// TestInertAuditDetectsAnInertMove is the audit's own self-test.
//
// TestNoCuratedMoveIsInert passing is only meaningful if it is still capable of
// failing, and it has one obvious way to rot: the snapshot normalization above
// exists to hide the marks every move leaves just by being used, and widening
// it slightly too far would hide real effects along with them. At that point
// the audit reports a clean bill of health for an engine full of inert moves
// and nobody finds out.
//
// So: plant a move that is inert by construction — a status move with no effect
// payload and no handler anywhere in the engine, which is exactly the shape the
// twelve real ones arrived in — and require the detector to catch it. loadDex
// reads from disk on every call, so the planted move is confined to this test.
func TestInertAuditDetectsAnInertMove(t *testing.T) {
	d := loadDex(t)
	const planted = "zzz-inert-canary"
	d.Moves[planted] = domain.Move{
		ID: planted, Name: "Inert Canary",
		Category: domain.CatStatus, Target: domain.TargetFoe,
		Accuracy: 100, PP: 10,
	}

	ctrl := inertFixture(t, d, "splash")
	ctrlLog := ResolveTurn(d, ctrl, [2]Action{{Kind: ActionMove}, {Kind: ActionMove}})
	ctrlSnapshot := inertSnapshot(ctrl)
	turnNoise := map[string]bool{}
	for _, l := range ctrlLog {
		turnNoise[l.Text] = true
	}

	s := inertFixture(t, d, planted)
	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove}, {Kind: ActionMove}})
	for _, l := range log {
		if strings.Contains(l.Text, " used ") || turnNoise[l.Text] {
			continue
		}
		t.Fatalf("the canary should have said nothing, but logged %q", l.Text)
	}
	if inertSnapshot(s) != ctrlSnapshot {
		t.Error("the canary changed the battle state, so the audit's fixture or its " +
			"snapshot no longer isolates what a move actually does")
	}
}
