package engine

import (
	"fmt"
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// callbackBattle sets up Snorlax vs Snorlax with a three-deep bench on side 1,
// so the trapping and team-cure tests have something to work with.
func callbackBattle(t *testing.T, d *domain.Dex) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "b", "P1", []int{143, 3}, "P2", []int{143, 6, 9}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	return s
}

// useStatus resolves a status move from side through the real dispatch, so
// each test exercises the path a battle actually takes rather than calling the
// handler directly.
func useStatus(t *testing.T, d *domain.Dex, s *BattleState, side int, moveID string) []LogLine {
	t.Helper()
	m, ok := d.Moves[moveID]
	if !ok {
		t.Skipf("%s not in dataset", moveID)
	}
	var log []LogLine
	applyStatusMove(loadDex(t), s, side, m, NewRNG(1), &log)
	return log
}

// --- stat reset ---

// TestHazeResetsBothSides is the balance-relevant one: before it, no move in
// the engine could remove a stat boost, so phazing was the only counterplay to
// setup in the whole game.
func TestHazeResetsBothSides(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	s.Active(0).Stages.Atk = 3
	s.Active(0).Stages.Spe = 2
	s.Active(1).Stages.SpA = 4
	s.Active(1).Stages.Def = -2

	log := useStatus(t, d, s, 0, "haze")
	if s.Active(0).Stages != (Stages{}) {
		t.Errorf("Haze should clear the user's own boosts too, got %+v", s.Active(0).Stages)
	}
	if s.Active(1).Stages != (Stages{}) {
		t.Errorf("Haze should clear the foe's boosts, got %+v", s.Active(1).Stages)
	}
	if !logHas(log, "eliminated") {
		t.Errorf("Haze should announce itself, got %v", logTexts(log))
	}
}

// TestClearSmogResetsOnlyTheTarget: unlike Haze it is one-sided, and it only
// fires when the hit lands.
func TestClearSmogResetsOnlyTheTarget(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	s.Active(0).Stages.Atk = 2
	s.Active(1).Stages.Atk = 3

	var log []LogLine
	applyClearSmog(s, 0, &log)
	if s.Active(1).Stages.Atk != 0 {
		t.Errorf("the target's boosts should be gone, got %+v", s.Active(1).Stages)
	}
	if s.Active(0).Stages.Atk != 2 {
		t.Errorf("the user keeps its own boosts, got %+v", s.Active(0).Stages)
	}
}

// TestPsychUpCopiesTheTargetsStages, negatives included — that risk is what
// makes it a read rather than a free steal.
func TestPsychUpCopiesTheTargetsStages(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	s.Active(0).Stages.Atk = 5
	s.Active(1).Stages.Atk = 2
	s.Active(1).Stages.Spe = -3

	useStatus(t, d, s, 0, "psych-up")
	if got := s.Active(0).Stages; got.Atk != 2 || got.Spe != -3 {
		t.Errorf("user stages = %+v, want the target's (+2 Atk, -3 Spe)", got)
	}
	if got := s.Active(1).Stages; got.Atk != 2 || got.Spe != -3 {
		t.Errorf("the target should keep its own boosts, got %+v", got)
	}
}

// --- trapping ---

// TestMeanLookBlocksSwitching: move-based trapping didn't exist at all.
func TestMeanLookBlocksSwitching(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)

	switchable := func(side int) bool {
		for _, a := range LegalActions(s, side) {
			if a.Kind == ActionSwitch {
				return true
			}
		}
		return false
	}
	if !switchable(1) {
		t.Fatal("setup: the target should be able to switch before it is trapped")
	}

	useStatus(t, d, s, 0, "mean-look")
	if !s.Active(1).Volatiles.Trapped {
		t.Fatal("Mean Look should set the trapped volatile")
	}
	if switchable(1) {
		t.Error("a trapped Pokémon should have no switch options")
	}
	// The trapper is free to leave.
	if !switchable(0) {
		t.Error("Mean Look traps the target, not the user")
	}
}

// TestShedShellBeatsMeanLook: the item is an unconditional escape hatch and
// already beats partial traps and the trapping abilities.
func TestShedShellBeatsMeanLook(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	useStatus(t, d, s, 0, "mean-look")
	s.Active(1).Item = ItemShedShell

	for _, a := range LegalActions(s, 1) {
		if a.Kind == ActionSwitch {
			return
		}
	}
	t.Error("a Shed Shell holder should still be able to switch out of Mean Look")
}

// TestMeanLookDoesNotAffectGhosts: Gen 6+ lets Ghost-types walk out of every
// trapping effect.
func TestMeanLookDoesNotAffectGhosts(t *testing.T) {
	d := loadDex(t)
	// Gengar (#94) is Ghost/Poison.
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{94, 6}, 1)
	if err != nil {
		t.Skip("Gengar not in dataset")
	}
	useStatus(t, d, s, 0, "mean-look")
	if s.Active(1).Volatiles.Trapped {
		t.Error("a Ghost-type should not be trapped")
	}
}

// --- team status ---

// TestHealBellCuresTheWholeTeam, bench included — that reach is the entire
// reason to run a cleric.
func TestHealBellCuresTheWholeTeam(t *testing.T) {
	d := loadDex(t)
	for _, moveID := range []string{"heal-bell", "aromatherapy"} {
		s := callbackBattle(t, d)
		s.Sides[1].Team[0].Status = StatusToxic
		s.Sides[1].Team[0].ToxicCounter = 5
		s.Sides[1].Team[1].Status = StatusSleep
		s.Sides[1].Team[1].SleepTurns = 3
		s.Sides[1].Team[2].Status = StatusBurn
		// The foe's status is none of the cleric's business.
		s.Sides[0].Team[0].Status = StatusParalysis

		useStatus(t, d, s, 1, moveID)

		for i := range s.Sides[1].Team {
			if got := s.Sides[1].Team[i].Status; got != StatusNone {
				t.Errorf("%s: team slot %d still has %v", moveID, i, got)
			}
		}
		if s.Sides[1].Team[0].ToxicCounter != 0 || s.Sides[1].Team[1].SleepTurns != 0 {
			t.Errorf("%s: the sleep clock and toxic counter should clear with the status", moveID)
		}
		if s.Sides[0].Team[0].Status != StatusParalysis {
			t.Errorf("%s: it should not touch the other side", moveID)
		}
	}
}

// TestHealBellFailsWithNothingToCure: no free turn for a healthy team.
func TestHealBellFailsWithNothingToCure(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	log := useStatus(t, d, s, 1, "heal-bell")
	if !logHas(log, "But it failed!") {
		t.Errorf("expected a failure line, got %v", logTexts(log))
	}
}

// --- Perish Song ---

// TestPerishSongCountsDownAndKills, on both sides including the user's own.
func TestPerishSongCountsDownAndKills(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	useStatus(t, d, s, 0, "perish-song")

	for i := 0; i < 2; i++ {
		if s.Active(i).Volatiles.PerishSong == nil {
			t.Fatalf("side %d should be counting", i)
		}
		if got := s.Active(i).Volatiles.PerishSong.TurnsLeft; got != perishSongTurns {
			t.Fatalf("side %d starts at %d, want %d", i, got, perishSongTurns)
		}
	}

	// Four end-of-turns: the one on the turn it landed announces the starting
	// count without spending it, then 2, 1, and the 0 that kills.
	var log []LogLine
	for tick := 1; tick <= perishSongTurns+1; tick++ {
		log = nil
		tickPerishSong(s, 0, &log)
		tickPerishSong(s, 1, &log)
		if tick <= perishSongTurns && (s.Active(0).Fainted || s.Active(1).Fainted) {
			t.Fatalf("tick %d: nobody should faint before the count runs out", tick)
		}
	}
	if !s.Active(0).Fainted || !s.Active(1).Fainted {
		t.Errorf("both actives should faint when the count reaches zero (side0=%v side1=%v)",
			s.Active(0).Fainted, s.Active(1).Fainted)
	}
	if !logHas(log, "fainted") {
		t.Errorf("the faint should be logged, got %v", logTexts(log))
	}
}

// TestPerishSongClearsOnSwitchOut: the count is the reason to switch, so
// switching has to answer it.
func TestPerishSongClearsOnSwitchOut(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	useStatus(t, d, s, 0, "perish-song")

	var log []LogLine
	doSwitch(s, 1, 1, NewRNG(1), &log)
	if s.Active(1).Volatiles.PerishSong != nil {
		t.Error("the incoming Pokémon should not inherit the count")
	}
	if s.Sides[1].Team[0].Volatiles.PerishSong != nil {
		t.Error("the outgoing Pokémon's count should have cleared")
	}
}

// --- Spite ---

// TestSpiteDrainsTheLastMovesPP.
func TestSpiteDrainsTheLastMovesPP(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	def := s.Active(1)
	def.Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}, {MoveID: "rest", PP: 10, MaxPP: 10}}
	def.Volatiles.LastMoveID = "tackle"
	def.Volatiles.LastMoveName = "Tackle"

	useStatus(t, d, s, 0, "spite")
	if got := def.Moves[0].PP; got != 35-spitePPLoss {
		t.Errorf("Tackle PP = %d, want %d", got, 35-spitePPLoss)
	}
	if def.Moves[1].PP != 10 {
		t.Error("Spite should only touch the move that was last used")
	}
}

// TestSpiteFailsWithNoLastMove and clamps at zero rather than going negative.
func TestSpiteFailsWithNoLastMove(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	log := useStatus(t, d, s, 0, "spite")
	if !logHas(log, "But it failed!") {
		t.Errorf("Spite against a target that hasn't moved should fail, got %v", logTexts(log))
	}

	def := s.Active(1)
	def.Moves = []MoveSlot{{MoveID: "tackle", PP: 2, MaxPP: 35}}
	def.Volatiles.LastMoveID = "tackle"
	def.Volatiles.LastMoveName = "Tackle"
	useStatus(t, d, s, 0, "spite")
	if got := def.Moves[0].PP; got != 0 {
		t.Errorf("PP should clamp at 0, got %d", got)
	}
}

// --- dynamic power ---

// TestHexAndVenoshockDoubleOnStatus: both were flat 65 BP, which is no reason
// to run either over an ordinary STAB.
func TestHexAndVenoshockDoubleOnStatus(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	cases := []struct {
		move    string
		status  StatusCond
		doubles bool
	}{
		{"hex", StatusNone, false},
		{"hex", StatusBurn, true},
		{"hex", StatusPoison, true},
		{"hex", StatusParalysis, true},
		{"venoshock", StatusNone, false},
		{"venoshock", StatusPoison, true},
		{"venoshock", StatusToxic, true},
		{"venoshock", StatusBurn, false},
		{"venoshock", StatusParalysis, false},
	}
	for _, c := range cases {
		m, ok := d.Moves[c.move]
		if !ok {
			continue
		}
		s.Active(1).Status = c.status
		got := applyCallbackPower(s, s.Active(0), s.Active(1), m, "")
		want := m.Power
		if c.doubles {
			want *= 2
		}
		if got.Power != want {
			t.Errorf("%s vs %v: power %d, want %d", c.move, c.status, got.Power, want)
		}
	}
}

// TestWeatherBallChangesTypeAndDoubles.
func TestWeatherBallChangesTypeAndDoubles(t *testing.T) {
	d := loadDex(t)
	m, ok := d.Moves["weather-ball"]
	if !ok {
		t.Skip("weather-ball not in dataset")
	}
	s := callbackBattle(t, d)

	// No weather: unchanged Normal.
	if got := applyCallbackPower(s, s.Active(0), s.Active(1), m, ""); got.Type != "normal" || got.Power != m.Power {
		t.Errorf("clear skies: %s %d BP, want normal %d BP", got.Type, got.Power, m.Power)
	}

	for _, c := range []struct {
		weather WeatherKind
		want    domain.Type
	}{
		{WeatherSun, "fire"},
		{WeatherRain, "water"},
		{WeatherSandstorm, "rock"},
		{WeatherSnow, "ice"},
	} {
		s.Weather = &WeatherState{Kind: c.weather, TurnsLeft: 5}
		got := applyCallbackPower(s, s.Active(0), s.Active(1), m, "")
		if got.Type != c.want {
			t.Errorf("%s: type %s, want %s", c.weather, got.Type, c.want)
		}
		if got.Power != m.Power*2 {
			t.Errorf("%s: power %d, want %d", c.weather, got.Power, m.Power*2)
		}
	}

	// A Utility Umbrella holder is out of the rain and out of the sun.
	s.Weather = &WeatherState{Kind: WeatherRain, TurnsLeft: 5}
	s.Active(0).Item = ItemUtilityUmbrella
	if got := applyCallbackPower(s, s.Active(0), s.Active(1), m, ""); got.Type != "normal" {
		t.Errorf("under an umbrella the ball stays Normal, got %s", got.Type)
	}
}

// --- Growth ---

// TestGrowthDoublesInSun: +1/+1 normally, +2/+2 under the sun.
func TestGrowthDoublesInSun(t *testing.T) {
	d := loadDex(t)

	s := callbackBattle(t, d)
	useStatus(t, d, s, 0, "growth")
	if got := s.Active(0).Stages; got.Atk != 1 || got.SpA != 1 {
		t.Errorf("no sun: Atk=%d SpA=%d, want +1/+1", got.Atk, got.SpA)
	}

	sunny := callbackBattle(t, d)
	sunny.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 5}
	useStatus(t, d, sunny, 0, "growth")
	if got := sunny.Active(0).Stages; got.Atk != 2 || got.SpA != 2 {
		t.Errorf("in sun: Atk=%d SpA=%d, want +2/+2", got.Atk, got.SpA)
	}

	// An umbrella holder grows at the ordinary rate.
	shaded := callbackBattle(t, d)
	shaded.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 5}
	shaded.Active(0).Item = ItemUtilityUmbrella
	useStatus(t, d, shaded, 0, "growth")
	if got := shaded.Active(0).Stages; got.Atk != 1 {
		t.Errorf("under an umbrella: Atk=%d, want +1", got.Atk)
	}
}

// --- Tri Attack ---

// TestTriAttackInflictsOneOfThree: the dataset ships a 20% chance with no
// payload, so the move was rolling a 20% chance of nothing.
func TestTriAttackInflictsOneOfThree(t *testing.T) {
	d := loadDex(t)
	seen := map[StatusCond]bool{}
	for seed := uint64(1); seed < 400; seed++ {
		s := callbackBattle(t, d)
		var log []LogLine
		applyTriAttack(s, 0, NewRNG(seed), &log)
		if st := s.Active(1).Status; st != StatusNone {
			seen[st] = true
		}
	}
	for _, want := range triAttackStatuses {
		if !seen[want] {
			t.Errorf("Tri Attack should be able to inflict %v across 400 rolls, saw %v", want, seen)
		}
	}
}

// TestTriAttackRespectsTheSecondaryBlockers: it is an added effect, so Shield
// Dust refuses it and Sheer Force trades it away — the same gates the
// declarative secondaries loop applies, which this rider bypasses.
func TestTriAttackRespectsTheSecondaryBlockers(t *testing.T) {
	d := loadDex(t)
	landed := func(prep func(*BattleState)) bool {
		for seed := uint64(1); seed < 200; seed++ {
			s := callbackBattle(t, d)
			prep(s)
			var log []LogLine
			applyTriAttack(s, 0, NewRNG(seed), &log)
			if s.Active(1).Status != StatusNone {
				return true
			}
		}
		return false
	}
	if !landed(func(*BattleState) {}) {
		t.Fatal("baseline: Tri Attack should land a status across 200 rolls")
	}
	if landed(func(s *BattleState) { s.Active(1).Ability = "shield-dust" }) {
		t.Error("Shield Dust should refuse Tri Attack's status")
	}
	if landed(func(s *BattleState) { s.Active(0).Ability = "sheer-force" }) {
		t.Error("Sheer Force should suppress Tri Attack's status")
	}
	if landed(func(s *BattleState) { s.Active(1).Item = ItemCovertCloak }) {
		t.Error("Covert Cloak should refuse Tri Attack's status")
	}
}

// TestNoCallbackMoveStillResolvesToNothing is the regression guard for the
// whole class: a status move with no Primary block that isn't handled
// anywhere pays its PP and reads as a success. Every ID here must be claimed
// by the dispatch, so a future data-sync that reshapes one of them fails
// loudly instead of quietly reverting it to a no-op.
func TestNoCallbackMoveStillResolvesToNothing(t *testing.T) {
	d := loadDex(t)
	for _, id := range []string{
		"haze", "psych-up", "mean-look", "block",
		"heal-bell", "aromatherapy", "perish-song", "spite",
	} {
		m, ok := d.Moves[id]
		if !ok {
			continue
		}
		if m.Primary != nil {
			continue // it has a declarative effect; the shell problem doesn't apply
		}
		s := callbackBattle(t, d)
		// Give the dispatch something to act on so a "nothing to do" failure
		// isn't mistaken for an unhandled move.
		s.Active(0).Stages.Atk = 2
		s.Active(1).Stages.Atk = 2
		s.Sides[0].Team[1].Status = StatusBurn
		s.Sides[1].Team[1].Status = StatusBurn
		s.Active(1).Volatiles.LastMoveID = s.Active(1).Moves[0].MoveID
		s.Active(1).Volatiles.LastMoveName = "something"

		var log []LogLine
		applyStatusMove(loadDex(t), s, 0, m, NewRNG(1), &log)
		if len(log) == 0 {
			t.Errorf("%s resolved silently — it is still a no-op", id)
		}
	}
}

// TestPerishSongGivesThreeTurnsOfCounterplay pins the timing, which is the
// whole balance of the move: it is a switch-forcing threat, and how many
// turns the victim gets to answer it is the threat.
//
// Canon counts 3 → 2 → 1 → 0, announcing on the end of the turn it landed and
// fainting on the fourth. An implementation that decrements before announcing
// looks almost right — it still counts down, still kills — but costs the
// victim a turn and prints a first number one below the real deadline.
func TestPerishSongGivesThreeTurnsOfCounterplay(t *testing.T) {
	d := loadDex(t)
	s := callbackBattle(t, d)
	useStatus(t, d, s, 0, "perish-song")

	want := []int{perishSongTurns, 2, 1, 0}
	for i, count := range want {
		var log []LogLine
		tickPerishSong(s, 0, &log)

		wantText := fmt.Sprintf("perish count fell to %d!", count)
		if !logHas(log, wantText) {
			t.Fatalf("end-of-turn %d: want %q, got %v", i+1, wantText, logTexts(log))
		}
		lastTick := i == len(want)-1
		if got := s.Active(0).Fainted; got != lastTick {
			t.Fatalf("end-of-turn %d (count %d): fainted = %v, want %v",
				i+1, count, got, lastTick)
		}
	}
}

// TestPerishSongDoubleKOEndsAsADraw: the song faints both actives on the same
// tick, so when both sides are on their last Pokémon it wipes the field and
// the battle is a draw.
//
// Worth pinning because the draw was very nearly unreachable before this move
// shipped — it needed an Explosion into a Destiny Bond, or an exact HP tie at
// the turn cap — and several layers had quietly grown a "the winner is 0 or 1"
// assumption on the strength of that. The engine's own contract is that
// PhaseEnded carries 0, 1 or 2, and ValidateStateInvariants enforces it.
func TestPerishSongDoubleKOEndsAsADraw(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143}, "B", []int{3}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	var log []LogLine
	applyPerishSong(s, 0, &log)
	for i := 0; i <= perishSongTurns; i++ {
		tickPerishSong(s, 0, &log)
		tickPerishSong(s, 1, &log)
	}
	updatePhase(s, &log)

	if !s.Active(0).Fainted || !s.Active(1).Fainted {
		t.Fatalf("both actives should have fainted (0=%v 1=%v)",
			s.Active(0).Fainted, s.Active(1).Fainted)
	}
	if s.Phase != PhaseEnded {
		t.Errorf("phase = %s, want %s", s.Phase, PhaseEnded)
	}
	// Gen 5+ settles a simultaneous wipe by faint order rather than calling it a
	// draw: the side whose last Pokemon falls *first* loses. This test used to
	// assert `Winner == 2` and a "draw" line, which was the engine's own reading
	// and is what the upstream Perish Song cases disagree with. The two faints
	// here are driven by hand in a fixed side-0-then-side-1 order, so side 0
	// empties first and side 1 takes it; in a real turn the order comes from the
	// residual phase's Speed walk (see residualOrder), which is why the same
	// board in play kills the *faster* Pokemon first.
	if s.Winner != 1 {
		t.Errorf("winner = %d, want 1 — side 0 fainted first, so side 1 wins", s.Winner)
	}
	if !logHas(log, "won the battle") {
		t.Errorf("the win should be announced; got %v", logTexts(log))
	}
	if err := ValidateStateInvariants(s); err != nil {
		t.Errorf("a battle won from an empty bench should still satisfy the state "+
			"invariants: %v", err)
	}
}

// TestPerishSongUserSurvivesWithABench: the song hits the user too, so the
// side that used it only wins the exchange if it has somewhere to go. With a
// bench on one side only, that side takes the battle rather than drawing.
func TestPerishSongUserSurvivesWithABench(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143, 6}, "B", []int{3}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	var log []LogLine
	applyPerishSong(s, 0, &log)
	for i := 0; i <= perishSongTurns; i++ {
		tickPerishSong(s, 0, &log)
		tickPerishSong(s, 1, &log)
	}
	updatePhase(s, &log)

	if s.Phase != PhaseEnded || s.Winner != 0 {
		t.Errorf("phase = %s winner = %d, want ended/0 — side 0 still has a bench",
			s.Phase, s.Winner)
	}
}
