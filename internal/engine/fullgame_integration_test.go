package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"pokearena/internal/domain"
)

// fullgame_integration_test.go plays complete battles end to end and audits
// every resolution of every one of them.
//
// The unit tests in this package each pin one mechanic in a hand-built
// position. That is necessary and not sufficient: the two things that actually
// went wrong in this engine's history were an interaction (a secondary effect
// landing inside the faint window) and an absence (Facade never wired up at
// all), and neither shows up in a fixture written by someone who already
// believes the mechanic works. Both were found by playing real games and
// looking hard at the logs.
//
// So this suite plays real games. The corpus is six competitive archetypes —
// stall, hazard stack, glass cannon, sun, trick room, status — chosen because
// between them they reach the parts of the engine a single roster never does:
// weather and the abilities keyed to it, entry hazards and phazing, Trick
// Room's speed inversion, every non-volatile status, priority, recovery, and
// item and ability procs on both sides. Every pairing is played from a fixed
// seed, so the whole thing is reproducible from the command line alone.
//
// Four properties are checked, and they are deliberately different in kind:
//
//   - INVARIANTS — after every single resolution, the state must be
//     well-formed. Cheap, and it fails at the turn that broke rather than
//     several turns later.
//   - LOG AGREEMENT — the battle log must describe the state transition that
//     actually happened. A faint line implies a fainted Pokémon, and every
//     Pokémon that ends the game fainted announced it exactly once.
//   - DETERMINISM — the same seed replays bit for bit, and different seeds
//     genuinely diverge. The second half matters: a fingerprint that never
//     changes is not evidence of determinism, it is evidence the seed is
//     ignored.
//   - COVERAGE — the corpus is asserted to have actually exercised the
//     mechanics it claims to. Without this the suite could pass having never
//     set weather once, which is exactly how a referee described one of the
//     tournament matches: "no hazards, weather, terrain or screens appeared
//     all match, so those paths went untested here."

var updateGolden = flag.Bool("update-golden", false,
	"rewrite testdata/fullgame-golden.json from the current engine")

// maxDecisions stops a non-terminating battle from hanging the suite. A real
// game in this corpus resolves in well under a hundred decisions.
const maxDecisions = 2000

// archetypeTeam is one roster from the corpus.
type archetypeTeam struct {
	Slug  string     `json:"slug"`
	Name  string     `json:"name"`
	Picks []TeamPick `json:"picks"`
}

func loadArchetypes(t *testing.T) []archetypeTeam {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "archetype-teams.json"))
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var file struct {
		Teams []archetypeTeam `json:"teams"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	if len(file.Teams) != 6 {
		t.Fatalf("corpus has %d teams, want 6", len(file.Teams))
	}
	return file.Teams
}

// resolution is one decision point and everything the engine said about it.
type resolution struct {
	n     int
	turn  int
	phase Phase
	lines []LogLine
}

// gameRun is a completed battle: the log, the outcome, and a histogram of the
// log-line types it produced (which is how coverage is measured).
type gameRun struct {
	label       string
	turns       int
	decisions   int
	winner      int
	alive       [2]int
	resolutions []resolution
	types       map[string]int
	fingerprint string
}

// choose is the test's policy: deterministic given the seed, and weighted
// three-to-one toward attacking so games actually progress. A uniform pick
// over the legal set is mostly switches — six-slot rosters offer five of them
// against four moves — which produces long games where very little happens.
func choose(dex *domain.Dex, s *BattleState, side int, rng *RNG) (Action, bool) {
	legal := LegalActionsDex(dex, s, side)
	if len(legal) == 0 {
		return Action{}, false
	}
	var moves, switches []Action
	for _, a := range legal {
		if a.Kind == ActionMove {
			moves = append(moves, a)
		} else {
			switches = append(switches, a)
		}
	}
	if len(moves) > 0 && (len(switches) == 0 || rng.IntN(4) != 0) {
		return moves[rng.IntN(len(moves))], true
	}
	if len(switches) == 0 {
		return moves[rng.IntN(len(moves))], true
	}
	return switches[rng.IntN(len(switches))], true
}

// playGame runs one battle to completion, auditing after every resolution.
// audit is threaded through rather than always-on so the determinism replay
// can skip the (identical) second round of assertions.
func playGame(t *testing.T, dex *domain.Dex, a, b archetypeTeam, seed uint64, audit bool) *gameRun {
	t.Helper()
	label := fmt.Sprintf("%s vs %s @%d", a.Slug, b.Slug, seed)
	s, err := NewBattleFromPicks(dex, label, a.Name, a.Picks, b.Name, b.Picks, seed)
	if err != nil {
		t.Fatalf("%s: new battle: %v", label, err)
	}
	// Policy RNG is seeded off the battle seed but kept separate from it, so
	// choosing actions cannot perturb the engine's own stream.
	pol := [2]*RNG{NewRNG(seed*2654435761 + 1), NewRNG(seed*40503 + 2)}

	run := &gameRun{label: label, types: map[string]int{}}
	for !s.Ended() {
		if run.decisions > maxDecisions {
			t.Fatalf("%s: did not terminate within %d decisions (turn %d)", label, maxDecisions, s.Turn)
		}
		before := s.Clone()
		var lines []LogLine
		phase := s.Phase

		switch phase {
		case PhaseChoosing:
			var acts [2]Action
			for side := range 2 {
				act, ok := choose(dex, s, side, pol[side])
				if !ok {
					t.Fatalf("%s: side %d has no legal action in phase %s (turn %d)",
						label, side, s.Phase, s.Turn)
				}
				if !ActionAllowed(dex, s, side, act) {
					t.Fatalf("%s: LegalActions offered side %d an action ActionAllowed rejects: %+v",
						label, side, act)
				}
				acts[side] = act
			}
			lines = ResolveTurn(dex, s, acts)
		case PhaseReplace:
			var sw [2]*Action
			for side := range 2 {
				if !s.Replace[side] {
					continue
				}
				act, ok := choose(dex, s, side, pol[side])
				if !ok {
					t.Fatalf("%s: side %d owes a replacement but has no legal switch", label, side)
				}
				a := act
				sw[side] = &a
			}
			lines = ResolveReplace(s, sw)
		default:
			t.Fatalf("%s: unexpected phase %s", label, phase)
		}

		run.decisions++
		run.resolutions = append(run.resolutions, resolution{
			n: run.decisions, turn: s.Turn, phase: phase, lines: lines,
		})
		for _, l := range lines {
			run.types[l.Type]++
		}
		if audit {
			auditResolution(t, label, run.decisions, before, s, lines)
		}
	}

	run.turns = s.Turn
	run.winner = s.Winner
	run.alive = [2]int{s.LiveCount(0), s.LiveCount(1)}
	run.fingerprint = fingerprint(run, s)
	if audit {
		auditOutcome(t, run, s)
	}
	return run
}

// fingerprint hashes everything the engine said and where it ended up. Two
// runs agreeing on this agree on the whole battle, line for line.
func fingerprint(run *gameRun, s *BattleState) string {
	h := sha256.New()
	for _, r := range run.resolutions {
		fmt.Fprintf(h, "#%d t%d %s\n", r.n, r.turn, r.phase)
		for _, l := range r.lines {
			fmt.Fprintf(h, "  %s|%d|%s\n", l.Type, l.Side, l.Text)
		}
	}
	fmt.Fprintf(h, "end turn=%d winner=%d\n", s.Turn, s.Winner)
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			fmt.Fprintf(h, "  %d/%d %s hp=%d/%d st=%s fainted=%v\n",
				i, j, p.Name, p.HP, p.MaxHP, p.Status, p.Fainted)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// auditResolution is the per-turn assertion set: structural invariants first,
// then the properties that the bugs found in this engine actually violated.
func auditResolution(t *testing.T, label string, n int, before, after *BattleState, lines []LogLine) {
	t.Helper()
	where := fmt.Sprintf("%s: resolution #%d (turn %d, phase %s)", label, n, after.Turn, after.Phase)

	if err := ValidateStateInvariants(after); err != nil {
		t.Fatalf("%s: state invariants broken: %v", where, err)
	}

	for i := range after.Sides {
		for j := range after.Sides[i].Team {
			p := &after.Sides[i].Team[j]
			who := fmt.Sprintf("%s side %d slot %d (%s)", where, i, j, p.Name)

			if p.HP < 0 || p.HP > p.MaxHP {
				t.Fatalf("%s: HP %d out of range 0..%d", who, p.HP, p.MaxHP)
			}
			if p.Fainted != (p.HP == 0) {
				t.Fatalf("%s: Fainted=%v with HP %d — the flag and the HP must agree",
					who, p.Fainted, p.HP)
			}
			for k, ms := range p.Moves {
				if ms.PP < 0 || ms.PP > ms.MaxPP {
					t.Fatalf("%s: move %d (%s) PP %d out of range 0..%d",
						who, k, ms.MoveID, ms.PP, ms.MaxPP)
				}
			}
			// A sleeping Pokémon must have turns left to sleep. This is the
			// invariant the switch-out reset violated: it left Status==Sleep
			// with the counter at zero, which canAct reads as the wake
			// sentinel, so pivoting a sleeper out and back cured it outright.
			if p.Status == StatusSleep && p.SleepTurns < 1 {
				t.Fatalf("%s: asleep with SleepTurns=%d — a sleeper with no turns "+
					"left wakes free on its next action", who, p.SleepTurns)
			}
			if p.Status != StatusSleep && p.SleepTurns != 0 {
				t.Fatalf("%s: SleepTurns=%d while status is %q", who, p.SleepTurns, p.Status)
			}
			if p.Status == StatusToxic && p.ToxicCounter < 1 {
				t.Fatalf("%s: badly poisoned with ToxicCounter=%d", who, p.ToxicCounter)
			}
			if p.Status != StatusToxic && p.ToxicCounter != 0 {
				t.Fatalf("%s: ToxicCounter=%d while status is %q", who, p.ToxicCounter, p.Status)
			}
			// faint() clears status and volatiles; anything left on a corpse
			// means something wrote to it after it died.
			if p.Fainted && p.Status != StatusNone {
				t.Fatalf("%s: fainted but still carries status %q", who, p.Status)
			}
		}
		if a := after.Sides[i].Active; a < 0 || a >= len(after.Sides[i].Team) {
			t.Fatalf("%s: side %d active index %d out of range", where, i, a)
		}
	}

	// Harsh sunlight has forbidden freeze since Gen 2. Only flag when the sun
	// was up on both sides of the resolution, so a freeze landing on the turn
	// the weather lapsed is not miscounted as a violation.
	if isSun(before) && isSun(after) {
		for _, l := range lines {
			if l.Type == "status" && strings.Contains(l.Text, "frozen") {
				t.Fatalf("%s: %q — freeze cannot be inflicted in harsh sunlight", where, l.Text)
			}
		}
	}

	// Every announced faint must have actually happened.
	for _, l := range lines {
		if l.Type != "faint" {
			continue
		}
		name := strings.TrimSuffix(l.Text, " fainted!")
		if !nameIsFainted(after, l.Side, name) {
			t.Fatalf("%s: log announced %q but no Pokémon of that name on side %d is fainted",
				where, l.Text, l.Side)
		}
	}

	// Turn numbers never go backwards.
	if after.Turn < before.Turn {
		t.Fatalf("%s: turn went backwards, %d -> %d", where, before.Turn, after.Turn)
	}
}

func isSun(s *BattleState) bool {
	w := effectiveWeather(s)
	return w != nil && w.Kind == WeatherSun
}

func nameIsFainted(s *BattleState, side int, name string) bool {
	if side < 0 || side > 1 {
		return false
	}
	for i := range s.Sides[side].Team {
		p := &s.Sides[side].Team[i]
		if p.Name == name && p.Fainted {
			return true
		}
	}
	return false
}

// auditOutcome checks the things that only make sense once the battle is over.
func auditOutcome(t *testing.T, run *gameRun, s *BattleState) {
	t.Helper()
	if s.Phase != PhaseEnded {
		t.Fatalf("%s: loop exited with phase %s", run.label, s.Phase)
	}
	switch s.Winner {
	case 0, 1:
		loser := 1 - s.Winner
		if run.alive[loser] != 0 {
			t.Fatalf("%s: side %d declared the winner while the loser still has %d Pokémon",
				run.label, s.Winner, run.alive[loser])
		}
		if run.alive[s.Winner] == 0 {
			t.Fatalf("%s: side %d declared the winner with nothing left standing",
				run.label, s.Winner)
		}
	case 2:
		if run.alive[0] != 0 || run.alive[1] != 0 {
			t.Fatalf("%s: draw declared with %d-%d still standing",
				run.label, run.alive[0], run.alive[1])
		}
	default:
		t.Fatalf("%s: battle ended with winner=%d", run.label, s.Winner)
	}

	// Every Pokémon that ended the game fainted announced it exactly once —
	// no phantom faints, and nothing dying twice.
	announced := map[string]int{}
	for _, r := range run.resolutions {
		for _, l := range r.lines {
			if l.Type == "faint" {
				announced[fmt.Sprintf("%d/%s", l.Side, strings.TrimSuffix(l.Text, " fainted!"))]++
			}
		}
	}
	dead := 0
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			if !p.Fainted {
				continue
			}
			dead++
			key := fmt.Sprintf("%d/%s", i, p.Name)
			if announced[key] != 1 {
				t.Errorf("%s: %s is fainted but the log announced it %d times",
					run.label, key, announced[key])
			}
		}
	}
	total := 0
	for _, n := range announced {
		total += n
	}
	if total != dead {
		t.Errorf("%s: %d faint lines for %d fainted Pokémon", run.label, total, dead)
	}
}

// pairings is every unordered pair of archetypes including the mirrors, so
// twenty-one matchups across six rosters. The mirrors earn their place: two
// Drought setters fight over one sky, two Trick Rooms overwrite each other,
// and identical rosters produce the speed ties that a cross-archetype game
// almost never reaches.
func pairings(teams []archetypeTeam) [][2]archetypeTeam {
	var out [][2]archetypeTeam
	for i := range teams {
		for j := i; j < len(teams); j++ {
			out = append(out, [2]archetypeTeam{teams[i], teams[j]})
		}
	}
	return out
}

// corpusSeeds are arbitrary but fixed. More of them is strictly more coverage
// and the whole corpus resolves in well under a second, so this is cheap.
var corpusSeeds = []uint64{1337, 2718, 3141, 4669, 8888, 90210, 271828}

// TestFullGame_CorpusPlaysClean is the headline: every archetype pairing, at
// every seed, played to completion with every resolution audited.
func TestFullGame_CorpusPlaysClean(t *testing.T) {
	dex := loadDex(t)
	teams := loadArchetypes(t)

	// The corpus is only meaningful if it is legal under the format it claims
	// to be played in, so gate on that before playing anything.
	for _, tm := range teams {
		if err := ValidateTeam(tm.Picks, dex); err != nil {
			t.Fatalf("corpus team %s is not legal under standard clauses: %v", tm.Name, err)
		}
	}

	games := 0
	for _, p := range pairings(teams) {
		for _, seed := range corpusSeeds {
			run := playGame(t, dex, p[0], p[1], seed, true)
			games++
			if run.turns < 3 {
				t.Errorf("%s: ended after %d turns — too short to have exercised anything",
					run.label, run.turns)
			}
		}
	}
	if want := len(pairings(teams)) * len(corpusSeeds); games != want {
		t.Fatalf("played %d games, expected %d", games, want)
	}
	t.Logf("played %d full games across %d pairings", games, len(pairings(teams)))
}

// TestFullGame_Deterministic pins both halves of reproducibility: the same
// seed must replay identically, and different seeds must not.
func TestFullGame_Deterministic(t *testing.T) {
	dex := loadDex(t)
	teams := loadArchetypes(t)

	seen := map[string]string{}
	for _, p := range pairings(teams)[:6] {
		first := playGame(t, dex, p[0], p[1], corpusSeeds[0], false)
		again := playGame(t, dex, p[0], p[1], corpusSeeds[0], false)
		if first.fingerprint != again.fingerprint {
			t.Errorf("%s: replaying the same seed produced a different battle\n  %s\n  %s",
				first.label, first.fingerprint, again.fingerprint)
		}
		if first.turns != again.turns || first.winner != again.winner {
			t.Errorf("%s: replay diverged — turns %d/%d winner %d/%d",
				first.label, first.turns, again.turns, first.winner, again.winner)
		}

		other := playGame(t, dex, p[0], p[1], corpusSeeds[1], false)
		if other.fingerprint == first.fingerprint {
			t.Errorf("%s: seeds %d and %d produced identical battles — the seed is not "+
				"reaching the engine", first.label, corpusSeeds[0], corpusSeeds[1])
		}
		if prev, ok := seen[first.fingerprint]; ok {
			t.Errorf("%s and %s fingerprint identically", first.label, prev)
		}
		seen[first.fingerprint] = first.label
	}
}

// requiredCoverage is the set of engine paths the corpus must actually reach.
// Each entry is a log-line type the engine emits, and the comment says which
// archetype is expected to produce it. A miss here does not mean the engine is
// broken — it means this suite has stopped testing what it claims to.
var requiredCoverage = map[string]string{
	"faint":         "any game that finishes",
	"switch":        "the policy switches roughly a quarter of the time",
	"damage":        "any attack that connects",
	"status":        "the status roster, plus Scald/Discharge secondaries",
	"weather":       "Solaris' Drought, and the sun lapsing",
	"pseudoweather": "Deep Room's Trick Room",
	"hazard":        "the Spike Cartel's rocks and spikes",
	"heal":          "stall recovery, drains, Leftovers",
	"item":          "Leftovers, Life Orb, Focus Sash, Choice locks, berries",
	"ability":       "Drought, Levitate, Intimidate, Guts, Regenerator",
	"stat":          "setup moves and stat-dropping secondaries",
	"crit":          "critical hits, over a corpus this size",
	"effective":     "super-effective hits",
	"resisted":      "resisted hits",
	"immune":        "Levitate vs Ground, or a type immunity",
	"fail":          "a move that could not work",
	"win":           "the end of every game",
}

// TestFullGame_CorpusExercisesTheEngine asserts the corpus reaches the paths it
// is supposed to reach. Without this the rest of the suite could pass green
// having never once set weather, laid a hazard, or landed a status.
func TestFullGame_CorpusExercisesTheEngine(t *testing.T) {
	dex := loadDex(t)
	teams := loadArchetypes(t)

	total := map[string]int{}
	for _, p := range pairings(teams) {
		for _, seed := range corpusSeeds {
			run := playGame(t, dex, p[0], p[1], seed, false)
			for k, v := range run.types {
				total[k] += v
			}
		}
	}

	var missing []string
	for kind, why := range requiredCoverage {
		if total[kind] == 0 {
			missing = append(missing, fmt.Sprintf("%s (expected from: %s)", kind, why))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("the corpus never exercised %d engine path(s):\n  %s\n\nobserved: %s",
			len(missing), strings.Join(missing, "\n  "), histogram(total))
	}
	t.Logf("log-line types produced across the corpus: %s", histogram(total))
}

func histogram(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

// goldenPath holds one fingerprint per pairing per seed.
const goldenPath = "testdata/fullgame-golden.json"

// TestFullGame_MatchesGolden is the regression net. Any change to the engine
// that alters how a real game plays out shows up here as a changed
// fingerprint, including changes nobody thought were behavioral.
//
// A diff is not automatically a bug — a deliberate fix will move these. The
// point is that it cannot move silently. Re-record with:
//
//	go test ./internal/engine/ -run TestFullGame_MatchesGolden -update-golden
//
// and say in the commit message which fix moved them and why.
func TestFullGame_MatchesGolden(t *testing.T) {
	dex := loadDex(t)
	teams := loadArchetypes(t)

	got := map[string]string{}
	summary := map[string]string{}
	for _, p := range pairings(teams) {
		for _, seed := range corpusSeeds {
			run := playGame(t, dex, p[0], p[1], seed, false)
			key := fmt.Sprintf("%s|%s|%d", p[0].Slug, p[1].Slug, seed)
			got[key] = run.fingerprint
			summary[key] = fmt.Sprintf("turns=%d winner=%d alive=%d-%d",
				run.turns, run.winner, run.alive[0], run.alive[1])
		}
	}

	if *updateGolden {
		out := struct {
			Note    string            `json:"note"`
			Summary map[string]string `json:"summary"`
			Hashes  map[string]string `json:"hashes"`
		}{
			Note: "Fingerprints of every archetype pairing at every corpus seed. Each hash " +
				"covers the full battle log plus the final board. Regenerate with: " +
				"go test ./internal/engine/ -run TestFullGame_MatchesGolden -update-golden",
			Summary: summary,
			Hashes:  got,
		}
		b, err := json.MarshalIndent(out, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s with %d fingerprints", goldenPath, len(got))
		return
	}

	b, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v\n\nGenerate it with:\n"+
			"  go test ./internal/engine/ -run TestFullGame_MatchesGolden -update-golden", err)
	}
	var want struct {
		Summary map[string]string `json:"summary"`
		Hashes  map[string]string `json:"hashes"`
	}
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatalf("parse golden: %v", err)
	}

	var drift []string
	for key, h := range got {
		w, ok := want.Hashes[key]
		if !ok {
			drift = append(drift, fmt.Sprintf("%s: new pairing, not in golden", key))
			continue
		}
		if w != h {
			drift = append(drift, fmt.Sprintf("%s: %s\n      golden %s\n      now    %s",
				key, summary[key], w[:16], h[:16]))
		}
	}
	for key := range want.Hashes {
		if _, ok := got[key]; !ok {
			drift = append(drift, fmt.Sprintf("%s: in golden but no longer played", key))
		}
	}
	sort.Strings(drift)
	if len(drift) > 0 {
		t.Errorf("%d of %d recorded games now play differently:\n  %s\n\n"+
			"If this is an intended engine change, re-record with:\n"+
			"  go test ./internal/engine/ -run TestFullGame_MatchesGolden -update-golden",
			len(drift), len(want.Hashes), strings.Join(drift, "\n  "))
	}
}
