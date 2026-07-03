package eval

import (
	"math"
	"testing"
)

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// TestWilsonInterval_KnownValues pins the interval against textbook results.
func TestWilsonInterval_KnownValues(t *testing.T) {
	// 50/100: symmetric interval centered on 0.5, ~[0.404, 0.596].
	lo, hi := WilsonInterval(50, 100, z95)
	if !approx(lo, 0.4038, 1e-3) || !approx(hi, 0.5962, 1e-3) {
		t.Fatalf("50/100: got [%.4f, %.4f], want ~[0.4038, 0.5962]", lo, hi)
	}
	// Degenerate extremes stay inside [0,1] (Wilson's whole point).
	lo, hi = WilsonInterval(0, 20, z95)
	if lo != 0 || hi <= 0 || hi >= 1 {
		t.Fatalf("0/20: got [%.4f, %.4f], want lo=0 and 0<hi<1", lo, hi)
	}
	lo, hi = WilsonInterval(20, 20, z95)
	if hi != 1 || lo <= 0 || lo >= 1 {
		t.Fatalf("20/20: got [%.4f, %.4f], want hi=1 and 0<lo<1", lo, hi)
	}
	// n=0 must not divide by zero.
	if lo, hi = WilsonInterval(0, 0, z95); lo != 0 || hi != 0 {
		t.Fatalf("0/0: got [%.4f, %.4f], want [0,0]", lo, hi)
	}
}

// TestStandings_Symmetric: two evenly-matched agents get equal Elo (~1500) and
// win rate 0.5.
func TestStandings_Symmetric(t *testing.T) {
	m := MatchResult{A: "x", B: "y", AWins: 50, BWins: 50}
	s := Standings([]MatchResult{m})
	if len(s) != 2 {
		t.Fatalf("got %d rows, want 2", len(s))
	}
	for _, r := range s {
		if !approx(r.WinRate, 0.5, 1e-9) {
			t.Fatalf("%s win rate %.4f, want 0.5", r.Name, r.WinRate)
		}
		if !approx(r.Elo, 1500, 1.0) {
			t.Fatalf("%s Elo %.2f, want ~1500", r.Name, r.Elo)
		}
	}
}

// TestStandings_TransitiveOrder: a clean skill ladder A>B>C must produce
// Elo(A) > Elo(B) > Elo(C), sorted, with everyone finite (including the agent
// that wins all its games and the one that loses all of them).
func TestStandings_TransitiveOrder(t *testing.T) {
	matches := []MatchResult{
		{A: "A", B: "B", AWins: 8, BWins: 2},
		{A: "A", B: "C", AWins: 10, BWins: 0}, // A never loses
		{A: "B", B: "C", AWins: 7, BWins: 3},  // C never wins
	}
	s := Standings(matches)
	if s[0].Name != "A" || s[1].Name != "B" || s[2].Name != "C" {
		t.Fatalf("order = %s > %s > %s, want A > B > C", s[0].Name, s[1].Name, s[2].Name)
	}
	if !(s[0].Elo > s[1].Elo && s[1].Elo > s[2].Elo) {
		t.Fatalf("Elo not strictly ordered: %.1f, %.1f, %.1f", s[0].Elo, s[1].Elo, s[2].Elo)
	}
	for _, r := range s {
		if math.IsInf(r.Elo, 0) || math.IsNaN(r.Elo) {
			t.Fatalf("%s Elo not finite: %v (all-win/all-lose must stay bounded)", r.Name, r.Elo)
		}
	}
}

// TestStandings_RecordBookkeeping: aggregate W/L/D and games are counted
// correctly across multiple pairings for a shared agent.
func TestStandings_RecordBookkeeping(t *testing.T) {
	matches := []MatchResult{
		{A: "A", B: "B", AWins: 6, BWins: 4},
		{A: "A", B: "C", AWins: 7, BWins: 2, Draws: 1},
	}
	s := Standings(matches)
	var a AgentStanding
	for _, r := range s {
		if r.Name == "A" {
			a = r
		}
	}
	if a.Wins != 13 || a.Losses != 6 || a.Draws != 1 || a.Games != 20 {
		t.Fatalf("A record = %dW %dL %dD /%d, want 13/6/1/20", a.Wins, a.Losses, a.Draws, a.Games)
	}
	wantWR := (13.0 + 0.5) / 20.0
	if !approx(a.WinRate, wantWR, 1e-9) {
		t.Fatalf("A win rate %.4f, want %.4f", a.WinRate, wantWR)
	}
	if !(a.CILow < a.WinRate && a.WinRate < a.CIHigh) {
		t.Fatalf("A win rate %.4f not inside CI [%.4f, %.4f]", a.WinRate, a.CILow, a.CIHigh)
	}
}
