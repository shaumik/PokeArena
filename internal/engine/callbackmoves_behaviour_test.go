package engine

import (
	"fmt"
	"testing"

	"pokearena/internal/domain"
)

// callbackmoves_behaviour_test.go is the whole-battle half of callbackmoves.go.
//
// The unit tests next door reach straight into the handlers — applyClearSmog,
// tickPerishSong, applyCallbackPower — and prove the handlers do the right
// thing when called. What they cannot prove is that a battle ever calls them,
// or calls them at the right moment: every one of these moves arrives from
// data-sync as a shell (a status move with no effect block, or a damaging move
// whose power is a lie), and the *only* thing standing between the shell and a
// silent no-op is a move-ID gate at a dispatch site in turn.go / effects.go.
// A test that skips the dispatch cannot see that gate disappear.
//
// So everything here goes through the front door: NewBattle, ResolveTurn,
// ResolveReplace, and the exported state. Positions are arranged by setting
// exported fields (that is setup), but every mechanic under test is reached by
// choosing the move in a real turn and reading the real turn log. Nothing here
// picks a seed for the roll it produces — the probabilistic moves are measured
// over a sweep of seeds instead, so the assertion is about the rule and not
// about splitmix64.

// cbmBattle builds a battle where the only live mechanic is the one under
// test: every Pokémon on both teams loses its ability and item and is left
// holding Splash alone. Tests then hand the movers they care about a real
// move list. Same idea as berryBattle, extended to the bench, because these
// tests switch and the incoming Pokémon must be as inert as the outgoing one.
func cbmBattle(t *testing.T, d *domain.Dex, team0, team1 []int, seed uint64) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "cbm", "P1", team0, "P2", team1, seed)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			p.Ability, p.Item = AbilityNone, ItemNone
			p.Moves = cbmMoves("splash")
		}
	}
	return s
}

// cbmMoves is a move list with PP to spare; tests that assert on PP build
// their slots by hand instead.
func cbmMoves(ids ...string) []MoveSlot {
	out := make([]MoveSlot, 0, len(ids))
	for _, id := range ids {
		out = append(out, MoveSlot{MoveID: id, PP: 40, MaxPP: 40})
	}
	return out
}

// cbmTurn resolves one turn with each side using the named move slot.
func cbmTurn(d *domain.Dex, s *BattleState, slot0, slot1 int) []LogLine {
	return playTurn(d, s, slot0, slot1)
}

// cbmDamageSweep plays "Snorlax uses moveID on a Charizard that idles" across
// a sweep of seeds and returns the total damage dealt. Summing a sweep rather
// than reading one seed is what keeps a power assertion off the damage roll
// and off crits: no seed is chosen for its outcome, and the 85–100 spread plus
// the odd critical average out of the ratio between two sweeps.
//
// prep arranges the defender (status, ability) before the turn.
func cbmDamageSweep(t *testing.T, d *domain.Dex, moveID string, prep func(def *Pokemon)) int {
	t.Helper()
	total := 0
	for seed := uint64(1); seed <= 80; seed++ {
		s := cbmBattle(t, d, []int{143}, []int{6}, seed)
		s.Active(0).Moves = cbmMoves(moveID)
		def := s.Active(1)
		if prep != nil {
			prep(def)
		}
		before := def.HP
		cbmTurn(d, s, 0, 0)
		total += before - def.HP
	}
	if total == 0 {
		t.Fatalf("%s dealt no damage at all across the sweep — the fixture is wrong", moveID)
	}
	return total
}

// --- dynamic power (applyCallbackPower) ---

// TestHexBattleDoublesAgainstAStatusedFoe pins the reason to run Hex at all.
// It ships as a flat 65 BP move because Showdown computes the doubling in a
// basePowerCallback the data dump cannot carry — leaving a 65 BP Ghost move
// nobody would ever click over an ordinary STAB. The doubling is the move.
//
// The foe is paralyzed rather than burned or poisoned on purpose: a status
// that chips at end of turn would land in the HP delta this measures and
// flatter the doubling for the wrong reason.
func TestHexBattleDoublesAgainstAStatusedFoe(t *testing.T) {
	d := loadDex(t)
	clean := cbmDamageSweep(t, d, "hex", nil)
	statused := cbmDamageSweep(t, d, "hex", func(def *Pokemon) {
		def.Status = StatusParalysis
	})
	if statused*10 < clean*18 || statused*10 > clean*23 {
		t.Errorf("Hex dealt %d over the sweep against a paralyzed foe and %d against a healthy one; want ~2x",
			statused, clean)
	}
}

// TestVenoshockBattleDoublesOnlyAgainstPoison: same shell problem as Hex, but
// Venoshock reads poison *specifically*. A predicate that fired on any status
// would look right in every Toxic-stall test and quietly turn a niche move
// into a second Hex.
//
// The defender holds Magic Guard so the poison it is carrying costs it no
// end-of-turn HP: the number this compares must be the move's damage and
// nothing else.
func TestVenoshockBattleDoublesOnlyAgainstPoison(t *testing.T) {
	d := loadDex(t)
	guarded := func(st StatusCond) func(*Pokemon) {
		return func(def *Pokemon) {
			def.Ability = "magic-guard"
			def.Status = st
		}
	}
	clean := cbmDamageSweep(t, d, "venoshock", guarded(StatusNone))
	for _, c := range []struct {
		status  StatusCond
		doubles bool
	}{
		{StatusPoison, true},
		{StatusToxic, true},
		{StatusBurn, false},
		{StatusParalysis, false},
	} {
		got := cbmDamageSweep(t, d, "venoshock", guarded(c.status))
		lo, hi := clean*9/10, clean*11/10
		if c.doubles {
			lo, hi = clean*18/10, clean*23/10
		}
		if got < lo || got > hi {
			t.Errorf("Venoshock vs %v dealt %d over the sweep, want %d..%d (healthy baseline %d)",
				c.status, got, lo, hi, clean)
		}
	}
}

// TestWeatherBallBattleTakesTheSkysType is the one callback-power move whose
// payload is a *type* rather than a number, so the type chart is the proof:
// thrown at a Charizard, the same move must read super effective out of the
// rain, not very effective out of the sun, and neutral under clear skies.
// A power-only implementation passes a damage test and fails this one.
//
// The umbrella case is here for the same reason it is in every other
// weather-keyed effect: weatherFor is the single door to the sky, and an
// effect that reads s.Weather directly gives a Utility Umbrella holder a
// Water-type ball while standing under an umbrella.
func TestWeatherBallBattleTakesTheSkysType(t *testing.T) {
	d := loadDex(t)
	// setter is used on turn 1, Weather Ball on turn 2, against a Charizard
	// (Fire/Flying) that idles with Splash.
	//
	// Hypno throws it, not a Snorlax: a Normal-type thrower gets STAB on the
	// clear-sky ball and loses it the moment the weather retypes the move, so
	// every comparison below would be measuring STAB as much as base power.
	throw := func(setter string, item ItemKind, defender int, seed uint64) (int, []LogLine) {
		s := cbmBattle(t, d, []int{97, 3}, []int{defender, 143}, seed)
		s.Active(0).Moves = cbmMoves(setter, "weather-ball")
		s.Active(0).Item = item
		cbmTurn(d, s, 0, 0)
		def := s.Active(1)
		before := def.HP
		log := cbmTurn(d, s, 1, 0)
		return before - def.HP, log
	}
	const charizard, blastoise = 6, 9
	sweep := func(setter string, item ItemKind) int {
		total := 0
		for seed := uint64(1); seed <= 40; seed++ {
			dmg, _ := throw(setter, item, charizard, seed)
			total += dmg
		}
		return total
	}

	// Effectiveness is read off the log, which is the type chart's own voice.
	// Charizard alone does not separate Water from Rock (both read super
	// effective on Fire/Flying), so each weather is also thrown at a Blastoise,
	// where the four candidate types disagree: Water and Ice are resisted,
	// Fire is resisted, and Rock alone is neutral.
	for _, c := range []struct {
		setter                string
		zardLine, blastoiseLn string
	}{
		{"splash", "", ""}, // Normal: neutral on both
		{"rain-dance", "It's super effective!", "It's not very effective..."},     // Water
		{"sunny-day", "It's not very effective...", "It's not very effective..."}, // Fire
		{"sandstorm", "It's super effective!", ""},                                // Rock
		{"snowscape", "", "It's not very effective..."},                           // Ice
	} {
		// Swept over seeds rather than read off one: the type chart does not
		// roll, so every seed must agree, and asserting that says so.
		for seed := uint64(1); seed <= 5; seed++ {
			for _, target := range []struct {
				dexNo int
				want  string
			}{{charizard, c.zardLine}, {blastoise, c.blastoiseLn}} {
				_, log := throw(c.setter, ItemNone, target.dexNo, seed)
				gotSuper := logHas(log, "It's super effective!")
				gotWeak := logHas(log, "It's not very effective...")
				got := ""
				switch {
				case gotSuper:
					got = "It's super effective!"
				case gotWeak:
					got = "It's not very effective..."
				}
				if got != target.want {
					t.Errorf("after %s (seed %d) vs #%d: effectiveness line %q, want %q — full log %v",
						c.setter, seed, target.dexNo, got, target.want, logTexts(log))
				}
			}
		}
	}

	// Power doubles too, and snow is where that can be measured on its own:
	// Ice is neutral on Charizard and snow neither chips nor boosts, so the
	// whole difference from clear skies is the doubled base power.
	clear, snow := sweep("splash", ItemNone), sweep("snowscape", ItemNone)
	if snow*10 < clear*18 || snow*10 > clear*23 {
		t.Errorf("Weather Ball in snow dealt %d over the sweep against %d under clear skies; want ~2x",
			snow, clear)
	}
	// Rain stacks three multipliers and no others: 2x base power, 2x for Water
	// on a Fire/Flying target, and the rain's own 1.5x on Water moves — 6x the
	// clear-sky ball. The band is tight enough to exclude the other candidates
	// (a Rock ball would land 8x here, with no rain boost to earn).
	if rain := sweep("rain-dance", ItemNone); rain < clear*5 || rain > clear*7 {
		t.Errorf("Weather Ball in rain dealt %d over the sweep against %d under clear skies; want ~6x",
			rain, clear)
	}
	// Under an umbrella the thrower is out of the rain: plain Normal ball at
	// plain power, indistinguishable from clear skies.
	umbrella := sweep("rain-dance", ItemUtilityUmbrella)
	if umbrella < clear*9/10 || umbrella > clear*11/10 {
		t.Errorf("a Utility Umbrella holder's Weather Ball dealt %d in rain, want ~%d (the clear-sky number)",
			umbrella, clear)
	}
	for seed := uint64(1); seed <= 5; seed++ {
		if _, log := throw("rain-dance", ItemUtilityUmbrella, charizard, seed); logHas(log, "It's super effective!") {
			t.Errorf("under an umbrella the ball must stay Normal, got %v", logTexts(log))
		}
	}
}

// --- Clear Smog (applyClearSmog) ---

// TestClearSmogBattleWipesOnlyTheTargetsBoosts. Clear Smog is the one-sided
// answer to setup: it takes the sweeper's boosts and leaves the user's own
// alone, which is why it can be run on a Pokémon that also wants to set up.
// Implemented as Haze it would still "work" against a sweeper and quietly cost
// its own side every boost it had.
//
// The third turn pins the quiet branch: with nothing to clear the move says
// nothing. A no-op announcement is not cosmetic here — the removal line is how
// the opposing player learns their boosts are gone.
func TestClearSmogBattleWipesOnlyTheTargetsBoosts(t *testing.T) {
	d := loadDex(t)
	// Alakazam leads so the smoker is unambiguously faster than the setup
	// Snorlax; a speed tie would leave the order to the RNG.
	s := cbmBattle(t, d, []int{65}, []int{143}, 3)
	s.Active(0).Moves = cbmMoves("swords-dance", "clear-smog")
	s.Active(1).Moves = cbmMoves("swords-dance", "splash")

	cbmTurn(d, s, 0, 0) // both set up: +2 Atk each
	if s.Active(0).Stages.Atk != 2 || s.Active(1).Stages.Atk != 2 {
		t.Fatalf("setup: wanted +2 Atk on both sides, got %+v / %+v",
			s.Active(0).Stages, s.Active(1).Stages)
	}

	log := cbmTurn(d, s, 1, 1) // Clear Smog into an idling Snorlax
	if got := s.Active(1).Stages; got != (Stages{}) {
		t.Errorf("the target's stat changes should be gone, got %+v", got)
	}
	if got := s.Active(0).Stages.Atk; got != 2 {
		t.Errorf("the user keeps its own +2 Atk, got %d", got)
	}
	if !logHas(log, "stat changes were removed") {
		t.Errorf("the removal should be announced, got %v", logTexts(log))
	}

	log = cbmTurn(d, s, 1, 1) // again, with nothing left to clear
	if logHas(log, "stat changes were removed") {
		t.Errorf("Clear Smog against an unboosted target should stay quiet, got %v", logTexts(log))
	}
}

// TestClearSmogBattleLeavesAnImmuneTargetBoosted: the reset is an on-hit
// effect, so a target the move cannot touch keeps everything. Magneton is
// Electric/Steel and Clear Smog is Poison — the move does not connect, and a
// reset wired to the *attempt* rather than to the hit would strip a Steel-type
// sweeper that canon says is a hard counter to this move.
func TestClearSmogBattleLeavesAnImmuneTargetBoosted(t *testing.T) {
	d := loadDex(t)
	s := cbmBattle(t, d, []int{65}, []int{82}, 3)
	s.Active(0).Moves = cbmMoves("clear-smog")
	s.Active(1).Moves = cbmMoves("swords-dance")

	cbmTurn(d, s, 0, 0)
	if s.Active(1).Stages.Atk == 0 {
		t.Fatalf("setup: Magneton should have boosted, got %+v", s.Active(1).Stages)
	}
	log := cbmTurn(d, s, 0, 0)
	if !logHas(log, "doesn't affect") {
		t.Fatalf("setup: Poison should not affect a Steel-type, got %v", logTexts(log))
	}
	if got := s.Active(1).Stages.Atk; got == 0 {
		t.Error("a target Clear Smog cannot hit must keep its boosts")
	}
	if logHas(log, "stat changes were removed") {
		t.Errorf("no hit, no reset — got %v", logTexts(log))
	}
}

// --- Perish Song (applyPerishSong, tickPerishSong) ---

// TestPerishSongBattleRunsItsClockToTheDoubleFaint plays the whole song out
// across four real turns, which is the only way to see the thing that matters
// about it: the number announced each end-of-turn, and which turn the faint
// lands on.
//
// Canon counts 3 → 2 → 1 → 0, announcing on the end of the turn it was used
// and killing on the fourth. An implementation that spends the count before
// announcing it still counts down and still kills, so a "does it eventually
// faint" test passes — but the victim gets three turns of counterplay instead
// of four and every number on screen is one below the real deadline. The count
// is public information a player counts switches against.
//
// Both actives faint on the same tick, the user's own included: Perish Song is
// a switch-forcing threat, not a free kill, and a user with no bench has signed
// its own death warrant.
func TestPerishSongBattleRunsItsClockToTheDoubleFaint(t *testing.T) {
	d := loadDex(t)
	s := cbmBattle(t, d, []int{143, 3}, []int{9, 6}, 11)
	s.Active(0).Moves = cbmMoves("perish-song", "splash")
	singer, victim := s.Active(0).Name, s.Active(1).Name

	for i, count := range []int{perishSongTurns, 2, 1, 0} {
		slot := 1 // Splash on every turn after the song
		if i == 0 {
			slot = 0
		}
		log := cbmTurn(d, s, slot, 0)
		if i == 0 && !logHas(log, "will faint in three turns") {
			t.Fatalf("the song should announce itself, got %v", logTexts(log))
		}
		for _, name := range []string{singer, victim} {
			want := fmt.Sprintf("%s's perish count fell to %d!", name, count)
			if !logHas(log, want) {
				t.Fatalf("end of turn %d: want %q in the log, got %v", i+1, want, logTexts(log))
			}
		}
		lastTurn := count == 0
		if fainted := s.Active(0).Fainted || s.Active(1).Fainted; fainted != lastTurn {
			t.Fatalf("end of turn %d (count %d): somebody fainted = %v, want %v",
				i+1, count, fainted, lastTurn)
		}
	}

	if !s.Active(0).Fainted || !s.Active(1).Fainted {
		t.Fatalf("the song takes both actives, the singer included (0=%v 1=%v)",
			s.Active(0).Fainted, s.Active(1).Fainted)
	}
	if s.Phase != PhaseReplace || !s.Replace[0] || !s.Replace[1] {
		t.Fatalf("phase = %s replace = %v, want both sides replacing", s.Phase, s.Replace)
	}

	// Both sides send in their bench, and the countdown does not follow them:
	// the song is on the Pokémon, not on the field.
	ResolveReplace(s, [2]*Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionSwitch, Index: 1}})
	for i := 0; i < 2; i++ {
		if s.Active(i).Fainted {
			t.Fatalf("side %d's replacement should be alive", i)
		}
		if s.Active(i).Volatiles.PerishSong != nil {
			t.Errorf("side %d's replacement inherited the count", i)
		}
	}
	if s.Phase != PhaseChoosing {
		t.Errorf("phase = %s after both replacements, want %s", s.Phase, PhaseChoosing)
	}
}

// TestPerishSongBattleSwitchingOutBeatsTheClock is the counterplay half, and
// the reason the move is balanced at all: the count rides the volatile bag, so
// leaving the field answers it. A count stored per-side or per-slot instead
// would still tick correctly in every single-Pokémon test and turn a
// switch-forcing move into an unanswerable kill.
func TestPerishSongBattleSwitchingOutBeatsTheClock(t *testing.T) {
	d := loadDex(t)
	s := cbmBattle(t, d, []int{143, 3}, []int{9, 6}, 11)
	s.Active(0).Moves = cbmMoves("perish-song", "splash")

	cbmTurn(d, s, 0, 0) // the song lands; both counts announce 3
	// Turn 2: the victim walks out while the singer idles.
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 1}, {Kind: ActionSwitch, Index: 1}})
	if s.Sides[1].Team[0].Volatiles.PerishSong != nil {
		t.Fatal("the count should have cleared when the victim left the field")
	}
	if s.Active(1).Volatiles.PerishSong != nil {
		t.Fatal("the replacement should not have picked the count up")
	}

	// Turns 3 and 4: the singer's own clock runs out on schedule.
	cbmTurn(d, s, 1, 0)
	log := cbmTurn(d, s, 1, 0)
	if !s.Active(0).Fainted {
		t.Errorf("the singer's count should still have reached zero, log %v", logTexts(log))
	}
	if s.Active(1).Fainted {
		t.Error("the Pokémon that switched out should be untouched by the song")
	}
	if !s.Replace[0] || s.Replace[1] {
		t.Errorf("only the singer's side should be replacing, got %v", s.Replace)
	}

	// The original victim comes back after the singer is replaced. A count
	// that had merely been suspended rather than cleared would resume here
	// and kill it two turns later.
	ResolveReplace(s, [2]*Action{{Kind: ActionSwitch, Index: 1}, nil})
	log = ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionSwitch, Index: 0}})
	if logHas(log, "perish count") {
		t.Errorf("nothing on the field is counting any more, got %v", logTexts(log))
	}
	if s.Active(1).Fainted || s.Active(1).Volatiles.PerishSong != nil {
		t.Error("the returning Pokémon must come back clean")
	}
}

// --- Psych Up (applyPsychUp) ---

// TestPsychUpBattleReplacesTheUsersOwnBoosts: Psych Up *copies*, it does not
// merge and it does not steal. The target keeps everything, and the user's own
// stages are overwritten wholesale — the +4 Speed this Alakazam spent two
// turns building is gone the moment it copies a foe that has none.
//
// Overwrite-versus-merge is the failure mode a positive-only test misses: a
// merge implementation reads correctly on the stat being copied and hands the
// user a free stat everywhere else.
func TestPsychUpBattleReplacesTheUsersOwnBoosts(t *testing.T) {
	d := loadDex(t)
	s := cbmBattle(t, d, []int{65}, []int{143}, 5)
	s.Active(0).Moves = cbmMoves("agility", "psych-up")
	s.Active(1).Moves = cbmMoves("swords-dance", "splash")

	cbmTurn(d, s, 0, 0)
	cbmTurn(d, s, 0, 0) // user +4 Spe, foe +4 Atk
	if s.Active(0).Stages.Spe != 4 || s.Active(1).Stages.Atk != 4 {
		t.Fatalf("setup: want +4 Spe / +4 Atk, got %+v / %+v", s.Active(0).Stages, s.Active(1).Stages)
	}

	log := cbmTurn(d, s, 1, 1)
	if got, want := s.Active(0).Stages, (Stages{Atk: 4}); got != want {
		t.Errorf("user stages = %+v, want exactly the foe's %+v (its own +4 Spe is replaced, not kept)",
			got, want)
	}
	if got, want := s.Active(1).Stages, (Stages{Atk: 4}); got != want {
		t.Errorf("the target keeps its own boosts: %+v, want %+v", got, want)
	}
	if !logHas(log, "copied") {
		t.Errorf("the copy should be announced, got %v", logTexts(log))
	}
}

// TestPsychUpBattleCopiesNegativeStagesToo: the risk is the move. Copying a
// foe that has been growled twice hands the user those drops and throws away
// its own Swords Dance, which is exactly why Psych Up is a read rather than a
// free steal. An implementation that copies only positive stages plays as a
// strictly better move than canon's.
func TestPsychUpBattleCopiesNegativeStagesToo(t *testing.T) {
	d := loadDex(t)
	s := cbmBattle(t, d, []int{65}, []int{143}, 5)
	s.Active(0).Moves = cbmMoves("swords-dance", "growl", "psych-up")

	cbmTurn(d, s, 0, 0) // user +2 Atk
	cbmTurn(d, s, 1, 0)
	cbmTurn(d, s, 1, 0) // foe -2 Atk
	if s.Active(0).Stages.Atk != 2 || s.Active(1).Stages.Atk != -2 {
		t.Fatalf("setup: want +2 / -2 Atk, got %+v / %+v", s.Active(0).Stages, s.Active(1).Stages)
	}

	cbmTurn(d, s, 2, 0)
	if got := s.Active(0).Stages.Atk; got != -2 {
		t.Errorf("user Atk stage = %d after copying a -2 foe, want -2", got)
	}
}

// --- Spite (applySpite) ---

// TestSpiteBattleDrainsFourPPFromTheLastMoveUsed. Spite is a PP-pressure move
// and the two halves that make it a read are which slot it takes from and how
// much: it must find the move the target actually used last turn, and leave
// every other slot alone.
//
// The second battle pins the clamp. PP is a legal-action gate — a negative
// count would make a move that is out of PP still selectable, or worse, make
// the Struggle check never fire.
func TestSpiteBattleDrainsFourPPFromTheLastMoveUsed(t *testing.T) {
	d := loadDex(t)
	// Alakazam outruns Snorlax, so Spite resolves while "the last move" is
	// still the Tackle from the turn before. Tackle deliberately sits in the
	// *second* slot: an implementation that drains the first slot it finds
	// with PP left reads as correct whenever the target has one move, and
	// this is the fixture that tells the two apart.
	s := cbmBattle(t, d, []int{65}, []int{143}, 4)
	s.Active(0).Moves = cbmMoves("splash", "spite")
	s.Active(1).Moves = []MoveSlot{
		{MoveID: "splash", PP: 40, MaxPP: 40},
		{MoveID: "tackle", PP: 35, MaxPP: 35},
	}

	cbmTurn(d, s, 0, 1) // the target tackles: 35 → 34
	log := cbmTurn(d, s, 1, 0)
	if got, want := s.Active(1).Moves[1].PP, 34-spitePPLoss; got != want {
		t.Errorf("Tackle PP = %d, want %d", got, want)
	}
	if got := s.Active(1).Moves[0].PP; got != 39 {
		t.Errorf("Splash PP = %d, want 39 — Spite may only touch the move that was used last", got)
	}
	if !logHas(log, fmt.Sprintf("lost %d PP", spitePPLoss)) {
		t.Errorf("the drain should be announced, got %v", logTexts(log))
	}

	// Clamp: a slot with less PP than Spite takes goes to zero, not negative.
	c := cbmBattle(t, d, []int{65}, []int{143}, 4)
	c.Active(0).Moves = cbmMoves("splash", "spite")
	c.Active(1).Moves = []MoveSlot{
		{MoveID: "splash", PP: 40, MaxPP: 40},
		{MoveID: "tackle", PP: 2, MaxPP: 35},
	}
	cbmTurn(d, c, 0, 1) // 2 → 1
	log = cbmTurn(d, c, 1, 0)
	if got := c.Active(1).Moves[1].PP; got != 0 {
		t.Errorf("Tackle PP = %d after a 4-PP drain on a 1-PP slot, want 0", got)
	}
	if !logHas(log, "lost 1 PP") {
		t.Errorf("the announcement should report what was actually taken, got %v", logTexts(log))
	}
}

// TestSpiteBattleFailsAgainstAFreshSwitchIn: "the last move used" lives on the
// target's volatiles, and switching clears them. A Pokémon that has just come
// in has used nothing, so Spite fails outright — which is the counterplay to
// it, and the reason it is a read. Falling back to "some move in the slots"
// would make Spite a guaranteed hit on any switch.
func TestSpiteBattleFailsAgainstAFreshSwitchIn(t *testing.T) {
	d := loadDex(t)
	s := cbmBattle(t, d, []int{65}, []int{143, 6}, 4)
	s.Active(0).Moves = cbmMoves("spite")
	s.Active(1).Moves = cbmMoves("tackle")

	cbmTurn(d, s, 0, 0) // the target tackles, so it has a last move on record
	log := ResolveTurn(d, s, [2]Action{
		{Kind: ActionMove, Index: 0},
		{Kind: ActionSwitch, Index: 1},
	})
	if !logHas(log, "But it failed!") {
		t.Errorf("Spite into a Pokémon that has not moved should fail, got %v", logTexts(log))
	}
	if logHas(log, "lost") {
		t.Errorf("nothing should have been drained, got %v", logTexts(log))
	}
}

// --- Heal Bell / Aromatherapy (applyTeamStatusCure) ---

// TestHealBellBattleCuresTheBenchToo. The bench is the whole point of a
// cleric: a Heal Bell that only cured the active would be a worse Refresh, and
// the move that "works" in every one-on-one test is exactly the one that
// silently does nothing for the four Pokémon it was clicked for.
//
// The clocks go with the status — a toxic counter or sleep timer left behind
// re-poisons the arithmetic the moment the status comes back. And the cure
// lands in the move phase, before residuals, so the badly-poisoned active
// takes no chip on the turn it is cured.
func TestHealBellBattleCuresTheBenchToo(t *testing.T) {
	d := loadDex(t)
	for _, moveID := range []string{"heal-bell", "aromatherapy"} {
		s := cbmBattle(t, d, []int{143}, []int{143, 3, 6}, 8)
		s.Active(1).Moves = cbmMoves(moveID)

		cleric := s.Active(1)
		cleric.Status, cleric.ToxicCounter = StatusToxic, 5
		s.Sides[1].Team[1].Status, s.Sides[1].Team[1].SleepTurns = StatusSleep, 3
		s.Sides[1].Team[2].Status = StatusBurn
		// The foe's status is none of the cleric's business.
		s.Active(0).Status = StatusParalysis
		hpBefore := cleric.HP

		log := cbmTurn(d, s, 0, 0)

		for i := range s.Sides[1].Team {
			if got := s.Sides[1].Team[i].Status; got != StatusNone {
				t.Errorf("%s: team slot %d is still %v", moveID, i, got)
			}
		}
		if s.Sides[1].Team[0].ToxicCounter != 0 || s.Sides[1].Team[1].SleepTurns != 0 {
			t.Errorf("%s: the toxic counter and sleep clock should clear with the status", moveID)
		}
		if s.Active(0).Status != StatusParalysis {
			t.Errorf("%s: the other side's status is not the cleric's to cure", moveID)
		}
		if s.Active(1).HP != hpBefore {
			t.Errorf("%s: the cure lands before residuals, so no toxic chip this turn (%d → %d)",
				moveID, hpBefore, s.Active(1).HP)
		}
		if !logHas(log, "soothing aroma") {
			t.Errorf("%s: the cure should be announced, got %v", moveID, logTexts(log))
		}
	}
}

// TestHealBellBattleFailsOnAHealthyTeam: no free turn for a team with nothing
// to cure. It matters because "status move with no effect block" is the
// failure mode this whole file guards — a Heal Bell that is not wired up at
// all also does nothing to a healthy team, but does it silently. The failure
// line is the difference between a move that declined and a move that is not
// implemented.
func TestHealBellBattleFailsOnAHealthyTeam(t *testing.T) {
	d := loadDex(t)
	s := cbmBattle(t, d, []int{143}, []int{143, 3, 6}, 8)
	s.Active(1).Moves = cbmMoves("heal-bell")

	log := cbmTurn(d, s, 0, 0)
	if !logHas(log, "But it failed!") {
		t.Errorf("Heal Bell with nothing to cure should fail, got %v", logTexts(log))
	}
	if logHas(log, "soothing aroma") {
		t.Errorf("...and should not claim to have cured anything, got %v", logTexts(log))
	}
}

// --- Tri Attack (applyTriAttack) ---

// cbmTriAttackSweep throws Tri Attack across a sweep of seeds and reports how
// many turns left the target with each status. Seeds are swept rather than
// chosen: the point of a 20% roll is the rate, and any single seed's outcome
// is a fact about splitmix64 rather than about the move.
func cbmTriAttackSweep(t *testing.T, d *domain.Dex, trials int, prep func(s *BattleState)) map[StatusCond]int {
	t.Helper()
	out := map[StatusCond]int{}
	for seed := uint64(1); seed <= uint64(trials); seed++ {
		s := cbmBattle(t, d, []int{143}, []int{143}, seed)
		s.Active(0).Moves = cbmMoves("tri-attack")
		if prep != nil {
			prep(s)
		}
		cbmTurn(d, s, 0, 0)
		if st := s.Active(1).Status; st != StatusNone {
			out[st]++
		}
	}
	return out
}

// TestTriAttackBattleRollsThreeStatusesAroundTwentyPercent. Upstream ships the
// 20% chance with an empty payload — Showdown picks the condition in an onHit
// callback — so the move was rolling a 20% chance of nothing at all, and every
// deterministic test of it passed.
//
// Measured over a seed sweep rather than pinned to a seed: the rule is "one
// fifth of the time, one of these three, uniformly", and that is a rate.
func TestTriAttackBattleRollsThreeStatusesAroundTwentyPercent(t *testing.T) {
	d := loadDex(t)
	const trials = 600
	got := cbmTriAttackSweep(t, d, trials, nil)

	landed := 0
	for st, n := range got {
		landed += n
		switch st {
		case StatusBurn, StatusFreeze, StatusParalysis:
		default:
			t.Errorf("Tri Attack inflicted %v (%d times) — it rolls burn, freeze or paralysis only", st, n)
		}
	}
	// 20% of 600 is 120; the band is wide enough that the sampling noise of a
	// fair 20% roll cannot fail it, and narrow enough that a 10% or a 100%
	// implementation cannot pass it.
	if landed < trials*12/100 || landed > trials*29/100 {
		t.Errorf("Tri Attack landed a status %d times in %d turns (%.0f%%), want ~20%%",
			landed, trials, 100*float64(landed)/float64(trials))
	}
	// Uniform between the three: a third of ~120 is ~40 each. A payload that
	// always picked the same condition would land the rate and fail here.
	t.Logf("%d/%d turns statused: %v", landed, trials, got)
	for _, st := range []StatusCond{StatusBurn, StatusFreeze, StatusParalysis} {
		if got[st] < landed/6 {
			t.Errorf("Tri Attack rolled %v %d times out of %d successes; want a roughly even third",
				st, got[st], landed)
		}
	}
}

// TestTriAttackBattleAddedEffectBlockersRefuseIt: the status is an *added
// effect*, so everything that refuses added effects has to refuse this one
// too. The rider is hand-coded outside the declarative secondaries loop, which
// is precisely how it ends up bypassing gates the loop applies for free —
// Shield Dust and Covert Cloak on the target, Sheer Force on the user.
func TestTriAttackBattleAddedEffectBlockersRefuseIt(t *testing.T) {
	d := loadDex(t)
	const trials = 300
	total := func(m map[StatusCond]int) int {
		n := 0
		for _, v := range m {
			n += v
		}
		return n
	}
	if base := total(cbmTriAttackSweep(t, d, trials, nil)); base == 0 {
		t.Fatalf("baseline: Tri Attack should land a status somewhere in %d turns", trials)
	}
	for _, c := range []struct {
		name string
		prep func(*BattleState)
	}{
		{"Shield Dust", func(s *BattleState) { s.Active(1).Ability = "shield-dust" }},
		{"Covert Cloak", func(s *BattleState) { s.Active(1).Item = ItemCovertCloak }},
		{"Sheer Force", func(s *BattleState) { s.Active(0).Ability = "sheer-force" }},
	} {
		if got := cbmTriAttackSweep(t, d, trials, c.prep); total(got) != 0 {
			t.Errorf("%s should refuse Tri Attack's status, but it landed %v across %d turns",
				c.name, got, trials)
		}
	}
}
