package engine

import (
	"strings"
	"testing"

	"pokearena/internal/domain"
)

// abilities_behavior_test.go plays real battles for a set of mechanics that
// were previously only pinned by tests calling the engine's own internals
// (applyVolatile, applyOnSwitchIn, applyAbilityEndOfTurn, applyStagesFromFoe,
// applyTailwindSetter, doRest, executeMove). Those tests prove the helper does
// what the helper says; they do not prove that any sequence of legal player
// actions ever reaches the helper, and they cannot be carried to a port of this
// engine, because the port has no such helper to call.
//
// Everything here goes through the front door: build a battle, choose actions,
// call ResolveTurn, read exported state and the turn log. If a wire between a
// move and a mechanic is cut, a test in this file goes red.

// --- fixtures -------------------------------------------------------------

// firstMover returns the name of whoever announced a move first this turn,
// read off the log the way a spectator would. Turn order is not exposed as
// state, so the log is the only front-door view of it.
func firstMover(log []LogLine) string {
	for _, l := range log {
		if i := strings.Index(l.Text, " used "); i > 0 {
			return l.Text[:i]
		}
	}
	return ""
}

// --- Oblivious ------------------------------------------------------------

// Oblivious refuses infatuation: an Attract from an opposite-gendered foe
// simply does not stick, and the holder keeps acting normally.
//
// The rule is worth a battle-level test rather than only a handler-level one
// because Attract is a two-part mechanic — a volatile that lands, and a 50%
// per-turn immobilization that reads it — and the ability only touches the
// first part. A version of Oblivious that logged its refusal but still set the
// flag would pass a "did the handler log?" test and still cost the holder half
// its turns.
func TestObliviousHolderCannotBeInfatuatedInBattle(t *testing.T) {
	d := loadDex(t)
	s := speciesBattle(t, d, 1, []int{3}, []int{80}) // Venusaur vs Slowbro
	if got := s.Active(1).Ability; got != "oblivious" {
		t.Fatalf("Slowbro's slot-0 ability should be oblivious, got %q", got)
	}
	teachMoves(t, d, s.Active(0), "attract")
	teachMoves(t, d, s.Active(1), "splash")
	s.Active(0).Gender = domain.GenderMale
	s.Active(1).Gender = domain.GenderFemale

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if s.Active(1).Volatiles.Attract {
		t.Errorf("Oblivious Slowbro was infatuated; log: %v", logTexts(log))
	}
	if !logHas(log, "Oblivious keeps it from being infatuated") {
		t.Errorf("no Oblivious refusal announced; log: %v", logTexts(log))
	}

	// Control: the identical battle without the ability. If Attract cannot
	// land here either, the assertions above prove nothing about Oblivious.
	s = speciesBattle(t, d, 1, []int{3}, []int{80})
	s.Active(1).Ability = AbilityNone
	teachMoves(t, d, s.Active(0), "attract")
	teachMoves(t, d, s.Active(1), "splash")
	s.Active(0).Gender = domain.GenderMale
	s.Active(1).Gender = domain.GenderFemale

	log = ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !s.Active(1).Volatiles.Attract {
		t.Fatalf("without Oblivious, Attract should land; log: %v", logTexts(log))
	}
	if !logHas(log, "fell in love!") {
		t.Errorf("no infatuation announced in the control; log: %v", logTexts(log))
	}
}

// The consequence of the rule, measured rather than asserted on one lucky
// seed: infatuation's miss-turn is a 50% roll, so "Oblivious was not
// immobilized" is only meaningful across many seeds. An Oblivious holder must
// attack on every single turn after being Attracted — never once losing a turn
// to love — while a plain holder loses roughly half of them.
func TestObliviousNeverLosesATurnToLoveAcrossSeeds(t *testing.T) {
	// run plays "foe Attracts, then the holder attacks" and reports whether
	// the holder's attack connected on the second turn.
	run := func(ability AbilityKind, seed uint64) (attacked bool, log []LogLine) {
		d := loadDex(t)
		s := speciesBattle(t, d, seed, []int{3}, []int{80})
		s.Active(1).Ability = ability
		teachMoves(t, d, s.Active(0), "attract", "splash")
		teachMoves(t, d, s.Active(1), "splash", "tackle")
		s.Active(0).Gender = domain.GenderMale
		s.Active(1).Gender = domain.GenderFemale

		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		before := s.Active(0).HP
		log = ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(1)})
		return s.Active(0).HP < before, log
	}

	const seeds = 60
	for seed := uint64(1); seed <= seeds; seed++ {
		attacked, log := run("oblivious", seed)
		if !attacked {
			t.Fatalf("seed %d: an Oblivious holder lost a turn after Attract; log: %v", seed, logTexts(log))
		}
		if logHas(log, "is in love") {
			t.Fatalf("seed %d: an Oblivious holder was rolled for infatuation at all; log: %v", seed, logTexts(log))
		}
	}

	// The same walk without the ability must lose turns, otherwise the loop
	// above is measuring an Attract that never landed.
	lost := 0
	for seed := uint64(1); seed <= seeds; seed++ {
		if attacked, _ := run(AbilityNone, seed); !attacked {
			lost++
		}
	}
	if lost == 0 {
		t.Fatalf("without Oblivious no turn was ever lost to infatuation across %d seeds — "+
			"the Attract in this fixture is not landing", seeds)
	}
}

// --- Steadfast ------------------------------------------------------------

// Steadfast turns a flinch into +1 Speed. The flinch still happens — the
// ability is compensation, not immunity, which is what separates it from Inner
// Focus and is the half a "did it flinch?" test cannot see.
//
// Fake Out is the flinch source on purpose: it is the one 100%-chance flincher
// in the set, so this test measures the ability instead of a secondary-effect
// roll, and at +3 priority it always resolves before the target can move.
func TestSteadfastRaisesSpeedWhenAFlinchLands(t *testing.T) {
	// Persian leads Fake Out into Machamp; Machamp idles.
	d := loadDex(t)
	s := speciesBattle(t, d, 3, []int{53}, []int{68})
	s.Active(1).Ability = "steadfast"
	teachMoves(t, d, s.Active(0), "fake-out")
	teachMoves(t, d, s.Active(1), "splash")

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHas(log, "flinched and couldn't move!") {
		t.Fatalf("Fake Out did not flinch the target, so Steadfast never got its cue; log: %v", logTexts(log))
	}
	if got := s.Active(1).Stages.Spe; got != 1 {
		t.Errorf("Steadfast after a flinch: Spe stage = %d, want +1; log: %v", got, logTexts(log))
	}
	if !logHas(log, "Steadfast raised its Speed!") {
		t.Errorf("Steadfast did not announce itself; log: %v", logTexts(log))
	}
	if !s.Active(1).AbilityRevealed {
		t.Error("Steadfast fired visibly but its holder's ability is still hidden")
	}

	// Control: same flinch, no ability, no boost. Guards against a Speed
	// stage that some other part of the turn is handing out.
	s = speciesBattle(t, d, 3, []int{53}, []int{68})
	s.Active(1).Ability = AbilityNone
	teachMoves(t, d, s.Active(0), "fake-out")
	teachMoves(t, d, s.Active(1), "splash")

	log = ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHas(log, "flinched and couldn't move!") {
		t.Fatalf("control: Fake Out did not flinch; log: %v", logTexts(log))
	}
	if got := s.Active(1).Stages.Spe; got != 0 {
		t.Errorf("control: Spe stage = %d after a flinch with no ability, want 0", got)
	}
}

// --- Defiant / Competitive ------------------------------------------------

// Defiant and Competitive answer a stat drop the *foe* caused, and the answer
// is +2 to one specific stat per drop — Attack for Defiant, Sp. Atk for
// Competitive — regardless of which stat was lowered. The drop itself still
// applies: Growl into Defiant leaves the holder at Atk −1 +2 = +1, and Charm's
// −2 into Defiant nets exactly zero, which is arithmetic a "did Attack go up?"
// test would happily accept from a wrong +1 reaction.
func TestDefiantAndCompetitiveAnswerFoeInducedDrops(t *testing.T) {
	cases := []struct {
		name        string
		holderDex   int
		ability     AbilityKind
		foeMove     string
		wantAtk     int
		wantSpA     int
		wantLogLine string
	}{
		// Growl: −1 Atk from the foe. Defiant pays +2 Atk on top.
		{"Defiant vs Growl", 57, "defiant", "growl", 1, 0, "Defiant raised its Attack sharply!"},
		// Charm: −2 Atk from the foe, and still exactly one reaction.
		{"Defiant vs Charm", 57, "defiant", "charm", 0, 0, "Defiant raised its Attack sharply!"},
		// Competitive answers the same Attack drop on a different stat.
		{"Competitive vs Growl", 40, "competitive", "growl", -1, 2, "Competitive raised its Sp. Atk sharply!"},
		{"Competitive vs Charm", 40, "competitive", "charm", -2, 2, "Competitive raised its Sp. Atk sharply!"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := loadDex(t)
			s := speciesBattle(t, d, 1, []int{3}, []int{c.holderDex}) // Venusaur vs holder
			s.Active(1).Ability = c.ability
			teachMoves(t, d, s.Active(0), c.foeMove)
			teachMoves(t, d, s.Active(1), "splash")

			log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
			holder := s.Active(1)
			if holder.Stages.Atk != c.wantAtk {
				t.Errorf("Atk stage = %d, want %d; log: %v", holder.Stages.Atk, c.wantAtk, logTexts(log))
			}
			if holder.Stages.SpA != c.wantSpA {
				t.Errorf("SpA stage = %d, want %d; log: %v", holder.Stages.SpA, c.wantSpA, logTexts(log))
			}
			if !logHas(log, c.wantLogLine) {
				t.Errorf("reaction not announced; log: %v", logTexts(log))
			}
		})
	}
}

// The "ByFoe" half of the rule, which is the whole point of the hook: a drop
// the holder inflicted on itself is a cost it chose to pay, and must not be
// refunded. Superpower lowers its own user's Attack and Defense by one each;
// a Defiant user of Superpower ends the turn at Atk −1, not Atk +1.
//
// Without this, Defiant plus any self-lowering attack is a free +1 Attack every
// turn, which turns the drawback move into a setup move.
func TestDefiantIgnoresTheUsersOwnStatDrop(t *testing.T) {
	d := loadDex(t)
	s := speciesBattle(t, d, 1, []int{57}, []int{143}) // Primeape vs Snorlax
	s.Active(0).Ability = "defiant"
	s.Active(1).Ability = AbilityNone
	teachMoves(t, d, s.Active(0), "superpower")
	teachMoves(t, d, s.Active(1), "splash")

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	user := s.Sides[0].Team[0]
	if user.Stages.Atk != -1 {
		t.Errorf("Atk stage = %d after a self-inflicted Superpower drop, want -1 "+
			"(Defiant must not answer the user's own cost); log: %v", user.Stages.Atk, logTexts(log))
	}
	if user.Stages.Def != -1 {
		t.Errorf("Def stage = %d, want -1 — Superpower's own drop should still apply", user.Stages.Def)
	}
	if logHas(log, "Defiant") {
		t.Errorf("Defiant fired on a self-inflicted drop; log: %v", logTexts(log))
	}
}

// --- Damp -----------------------------------------------------------------

// Damp fizzles Explosion and Self-Destruct, and it does so if *either* active
// has it — the holder's own Explosion is refused too, not just the foe's. The
// user does not blow up: the move is canceled outright, so nothing on the
// field loses HP.
//
// Both halves matter because the guard scans both slots. A version that only
// looked at the defender would let a Damp Pokémon detonate itself, and the
// most common way a fizzle is written wrongly — spend the user's HP, then skip
// the damage — reads identically to canon from the target's side.
func TestDampFizzlesSelfDestructingMovesFromEitherSide(t *testing.T) {
	cases := []struct {
		name       string
		moveID     string
		dampOnUser bool
	}{
		{"foe has Damp, Explosion", "explosion", false},
		{"foe has Damp, Self-Destruct", "self-destruct", false},
		{"the user itself has Damp", "explosion", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Electrode explodes; Golduck carries Damp in slot 0. Two-member
			// teams so a faint in the control leaves the battle running.
			d := loadDex(t)
			s := speciesBattle(t, d, 1, []int{101, 143}, []int{55, 143})
			if got := s.Active(1).Ability; got != "damp" {
				t.Fatalf("Golduck's slot-0 ability should be damp, got %q", got)
			}
			if c.dampOnUser {
				s.Active(0).Ability = "damp"
				s.Active(1).Ability = AbilityNone
			}
			teachMoves(t, d, s.Active(0), c.moveID)
			teachMoves(t, d, s.Active(1), "splash")
			userHP, foeHP := s.Active(0).HP, s.Active(1).HP

			log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
			if s.Active(0).Fainted || s.Active(0).HP != userHP {
				t.Errorf("the exploder lost HP (%d → %d, fainted=%v) — a fizzled self-destruct costs "+
					"nothing but PP; log: %v", userHP, s.Active(0).HP, s.Active(0).Fainted, logTexts(log))
			}
			if s.Active(1).HP != foeHP {
				t.Errorf("the target took damage (%d → %d) from a fizzled move; log: %v",
					foeHP, s.Active(1).HP, logTexts(log))
			}
			if !logHas(log, "(Damp)") {
				t.Errorf("no Damp refusal announced; log: %v", logTexts(log))
			}
		})
	}

	// Control: neither side has Damp, so the same Explosion works — the user
	// drops to zero and the target takes a hit.
	d := loadDex(t)
	s := speciesBattle(t, d, 1, []int{101, 143}, []int{55, 143})
	s.Active(1).Ability = AbilityNone
	teachMoves(t, d, s.Active(0), "explosion")
	teachMoves(t, d, s.Active(1), "splash")
	foeHP := s.Sides[1].Team[0].HP

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !s.Sides[0].Team[0].Fainted {
		t.Errorf("without Damp, Explosion must faint its user; log: %v", logTexts(log))
	}
	if s.Sides[1].Team[0].HP >= foeHP {
		t.Errorf("without Damp, Explosion must damage the target (%d → %d); log: %v",
			foeHP, s.Sides[1].Team[0].HP, logTexts(log))
	}
}

// --- end-of-turn healing abilities ----------------------------------------

// Rain Dish, Ice Body and Dry Skin heal a fraction of max HP at the end of
// every turn their own weather is up, and do nothing under any other weather.
// Dry Skin is the pair to it: the same hook that heals it in rain burns it in
// sun, so the two rows below prove the branch is actually reading the weather
// rather than firing whenever weather exists.
//
// The weather is set by a real move used in the battle, not written onto the
// state, so this also pins that a weather set this turn is already in force
// when the end-of-turn abilities tick.
func TestEndOfTurnHealingAbilitiesTickUnderTheirOwnWeather(t *testing.T) {
	cases := []struct {
		name        string
		dex         int
		ability     AbilityKind
		weatherMove string
		frac        float64 // signed fraction of max HP; 0 means "nothing happens"
	}{
		{"Rain Dish in rain", 9, "rain-dish", "rain-dance", 1.0 / 16},
		{"Rain Dish in snow", 9, "rain-dish", "snowscape", 0},
		{"Rain Dish in clear skies", 9, "rain-dish", "splash", 0},
		{"Ice Body in snow", 87, "ice-body", "snowscape", 1.0 / 16},
		{"Ice Body in rain", 87, "ice-body", "rain-dance", 0},
		{"Dry Skin in rain", 47, "dry-skin", "rain-dance", 1.0 / 8},
		{"Dry Skin in sun", 47, "dry-skin", "sunny-day", -1.0 / 8},
		{"Dry Skin in snow", 47, "dry-skin", "snowscape", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The holder sets its own weather; the foe idles. None of these
			// three species is chipped by any weather in the set (sandstorm is
			// the only chipping weather), so end-of-turn HP moves by the
			// ability alone.
			d := loadDex(t)
			s := speciesBattle(t, d, 1, []int{c.dex}, []int{143})
			s.Active(0).Ability = c.ability
			s.Active(1).Ability = AbilityNone
			teachMoves(t, d, s.Active(0), c.weatherMove)
			teachMoves(t, d, s.Active(1), "splash")
			p := s.Active(0)
			p.HP = p.MaxHP / 2
			before := p.HP

			log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
			// Canon states these as a plain fraction of max HP, truncated.
			want := before + int(float64(p.MaxHP)*c.frac)
			if p.HP != want {
				t.Errorf("HP %d → %d, want %d (max %d); log: %v",
					before, p.HP, want, p.MaxHP, logTexts(log))
			}
		})
	}
}

// The heal is capped at full HP and, at full HP, does not happen at all — no
// log line, no reveal. A healer that "heals zero" every turn under its weather
// would still announce itself each turn and hand the opponent free information.
func TestWeatherHealerAtFullHPDoesNothing(t *testing.T) {
	d := loadDex(t)
	s := speciesBattle(t, d, 1, []int{9}, []int{143}) // Blastoise vs Snorlax
	s.Active(0).Ability = "rain-dish"
	s.Active(1).Ability = AbilityNone
	teachMoves(t, d, s.Active(0), "rain-dance")
	teachMoves(t, d, s.Active(1), "splash")
	p := s.Active(0)
	if p.HP != p.MaxHP {
		t.Fatalf("fixture should start at full HP, got %d/%d", p.HP, p.MaxHP)
	}

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if p.HP != p.MaxHP {
		t.Errorf("a full-HP Rain Dish holder ended the turn at %d/%d", p.HP, p.MaxHP)
	}
	if logHas(log, "restored a little HP") {
		t.Errorf("Rain Dish announced a heal it did not perform; log: %v", logTexts(log))
	}

	// One point of damage is enough to make it fire, which proves the turn
	// above was silent because of the HP cap and not because rain was absent.
	p.HP = p.MaxHP - 1
	log = ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if p.HP != p.MaxHP {
		t.Errorf("Rain Dish did not top the holder back up: %d/%d; log: %v", p.HP, p.MaxHP, logTexts(log))
	}
	if !logHas(log, "restored a little HP") {
		t.Errorf("Rain Dish healed without announcing it; log: %v", logTexts(log))
	}
}

// --- Trace ----------------------------------------------------------------

// Trace copies the foe's ability the moment its holder is sent out, and says
// so by the ability's display name — "flame-body" is announced as "Flame Body".
// The rendered name is what the opposing player reads, and the announcement is
// the only place the slug-to-display conversion is visible from outside the
// engine.
//
// Reached here the way a game reaches it: a switch is an ordinary action, and
// the entry hook fires as part of resolving that turn.
//
// Each case spends a quiet turn first and only then arranges the foe's
// ability, so the entry hooks of the two leads — which the engine runs at the
// top of turn 1 — are finished before the switch under test. Setting the foe's
// ability up front instead would have its own switch-in fire too, and the
// trace we are checking would be reading a board the fixture already disturbed.
func TestTraceCopiesTheFoesAbilityOnASwitchIn(t *testing.T) {
	cases := []struct {
		name     string
		foeDex   int
		foeAbil  AbilityKind
		wantName string // display form expected in the announcement
	}{
		{"single-word ability", 110, "levitate", "Levitate"},    // Weezing
		{"hyphenated ability", 146, "flame-body", "Flame Body"}, // Moltres
		{"hyphenated ability again", 130, "thick-fat", "Thick Fat"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Snorlax leads for side 0 with Porygon (Trace, slot 0) behind it.
			d := loadDex(t)
			s := speciesBattle(t, d, 5, []int{143, 137}, []int{c.foeDex})
			if got := s.Sides[0].Team[1].Ability; got != "trace" {
				t.Fatalf("Porygon's slot-0 ability should be trace, got %q", got)
			}
			teachMoves(t, d, s.Active(0), "splash")
			teachMoves(t, d, s.Active(1), "splash")
			ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
			s.Active(1).Ability = c.foeAbil

			log := ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, moveAt(0)})
			tracer := s.Active(0)
			if tracer.Ability != c.foeAbil {
				t.Errorf("tracer holds %q after switching in, want %q; log: %v",
					tracer.Ability, c.foeAbil, logTexts(log))
			}
			want := "Trace copied " + s.Active(1).Name + "'s " + c.wantName + "!"
			if !logHas(log, want) {
				t.Errorf("missing announcement %q; log: %v", want, logTexts(log))
			}
		})
	}

	// Tracing an entry ability runs that ability's own entry effect, which is
	// how Trace can act on the turn it copies rather than the turn after.
	d := loadDex(t)
	s := speciesBattle(t, d, 5, []int{143, 137}, []int{130}) // ... vs Gyarados
	teachMoves(t, d, s.Active(0), "splash")
	teachMoves(t, d, s.Active(1), "splash")
	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	s.Active(1).Ability = "intimidate"
	if got := s.Active(1).Stages.Atk; got != 0 {
		t.Fatalf("fixture: foe Atk stage is already %d before the trace", got)
	}

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, moveAt(0)})
	if got := s.Active(1).Stages.Atk; got != -1 {
		t.Errorf("traced Intimidate did not fire: foe Atk stage = %d, want -1; log: %v", got, logTexts(log))
	}
}

// A short list of abilities cannot be traced at all: Trace itself, and
// Neutralizing Gas. Against those the tracer keeps its own Trace and stays
// silent — it must not announce a copy, and it must not end up holding an
// ability the rules say it can never have.
//
// Trace-into-Trace is the case that matters most: copying it would install a
// second copier, and either an entry loop or a Pokémon permanently wearing an
// ability that does nothing.
func TestTraceRefusesTheUncopiableAbilities(t *testing.T) {
	// Same shape as the copy test: one quiet turn so the leads' own entry
	// hooks are done, then the foe's ability is arranged and the tracer comes in.
	enter := func(foeAbil AbilityKind) (*BattleState, []LogLine) {
		d := loadDex(t)
		s := speciesBattle(t, d, 5, []int{143, 137}, []int{110}) // ... vs Weezing
		teachMoves(t, d, s.Active(0), "splash")
		teachMoves(t, d, s.Active(1), "splash")
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		s.Active(1).Ability = foeAbil
		return s, ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, moveAt(0)})
	}

	for _, foeAbil := range []AbilityKind{"trace", "neutralizing-gas"} {
		t.Run(string(foeAbil), func(t *testing.T) {
			s, log := enter(foeAbil)
			if got := s.Active(0).Ability; got != "trace" {
				t.Errorf("tracer holds %q after facing %q, want to keep trace", got, foeAbil)
			}
			if logHas(log, "Trace copied") {
				t.Errorf("Trace announced a copy of an uncopiable ability; log: %v", logTexts(log))
			}
		})
	}

	// Control on the same fixture: a copiable ability on the same species is
	// taken, so the two runs above are refusals and not a dead entry hook.
	s, _ := enter("levitate")
	if got := s.Active(0).Ability; got != "levitate" {
		t.Fatalf("control: tracer holds %q, want levitate — the entry hook is not running at all", got)
	}
}

// --- Tailwind -------------------------------------------------------------

// Tailwind doubles its own side's Speed for turn ordering, and only for turn
// ordering. Snorlax (Spe 50 at L50) loses the first move to Butterfree (Spe 90)
// every time; behind a Tailwind it moves at an effective 100 and goes first.
//
// Asserted through who actually announced a move first, because that is the
// only thing the doubling is allowed to change — a Tailwind that was set on
// the wrong side looks perfectly correct to a test that just checks a side
// condition exists somewhere.
func TestTailwindFlipsTurnOrderForTheSideThatSetIt(t *testing.T) {
	setup := func(seed uint64) (*domain.Dex, *BattleState) {
		d := loadDex(t)
		s := speciesBattle(t, d, seed, []int{143}, []int{12}) // Snorlax vs Butterfree
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		teachMoves(t, d, s.Active(0), "tailwind", "tackle")
		teachMoves(t, d, s.Active(1), "splash", "tackle")
		return d, s
	}

	// Baseline: with no Tailwind the faster Butterfree moves first.
	d, s := setup(2)
	log := ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(1)})
	if got := firstMover(log); got != "Butterfree" {
		t.Fatalf("without Tailwind the faster side should move first, got %q; log: %v", got, logTexts(log))
	}

	// Set it, then race again.
	d, s = setup(2)
	log = ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHas(log, "The Tailwind blew from behind P1's team!") {
		t.Fatalf("Tailwind was not announced for the side that used it; log: %v", logTexts(log))
	}
	if s.Sides[1].Conditions.Tailwind != nil {
		t.Error("Tailwind landed on the opposing side as well")
	}

	log = ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(1)})
	if got := firstMover(log); got != "Snorlax" {
		t.Errorf("behind a Tailwind the slower Snorlax should move first, got %q; log: %v", got, logTexts(log))
	}

	// It is a timer, not a permanent buff: it expires and the order reverts.
	var expiry []LogLine
	for turn := 0; turn < 4; turn++ {
		expiry = ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(1)})
		if logHas(expiry, "Tailwind petered out.") {
			break
		}
	}
	if !logHas(expiry, "Tailwind petered out.") {
		t.Fatalf("Tailwind never expired; last log: %v", logTexts(expiry))
	}
	if s.Sides[0].Conditions.Tailwind != nil {
		t.Error("Tailwind expired in the log but the side condition is still set")
	}
	log = ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(1)})
	if got := firstMover(log); got != "Butterfree" {
		t.Errorf("after Tailwind expired the faster side should move first again, got %q; log: %v",
			got, logTexts(log))
	}
}

// Tailwind into Tailwind fails. Canon refuses to refresh an active Tailwind,
// so the timer cannot be held open indefinitely by spending a turn on it — a
// re-setter that quietly restarted the counter would make the move a permanent
// Speed doubling for the cost of one action every four turns.
func TestTailwindCannotBeRefreshedWhileItIsUp(t *testing.T) {
	d := loadDex(t)
	s := speciesBattle(t, d, 2, []int{143}, []int{12})
	s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	teachMoves(t, d, s.Active(0), "tailwind")
	teachMoves(t, d, s.Active(1), "splash")

	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if s.Sides[0].Conditions.Tailwind == nil {
		t.Fatal("the first Tailwind did not land")
	}
	after := s.Sides[0].Conditions.Tailwind.TurnsLeft

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHas(log, "But it failed!") {
		t.Errorf("a second Tailwind while one is up should fail; log: %v", logTexts(log))
	}
	if s.Sides[0].Conditions.Tailwind == nil {
		t.Fatal("the refused re-set cleared the running Tailwind")
	}
	if got := s.Sides[0].Conditions.Tailwind.TurnsLeft; got >= after {
		t.Errorf("TurnsLeft went %d → %d across the refused re-set — the timer must keep "+
			"running down, not restart", after, got)
	}
}

// --- Rest -----------------------------------------------------------------

// Rest is a full heal that costs the user its next turn: it restores every
// point of HP, replaces whatever status the user had with a two-turn Sleep,
// and clears the toxic counter along with the toxic. The sleep is the price,
// and it has to be real — a Rest that healed without reliably sleeping is
// simply an unlimited full restore.
//
// Played out over three turns so the downtime is observed rather than inferred
// from a counter: the user cannot attack on the turn after Rest, and can again
// on the one after that.
func TestRestFullHealsSleepsAndCostsATurn(t *testing.T) {
	d := loadDex(t)
	s := speciesBattle(t, d, 4, []int{143}, []int{3}) // Snorlax vs Venusaur
	s.Active(0).Ability = AbilityNone
	s.Active(1).Ability = AbilityNone
	teachMoves(t, d, s.Active(0), "rest", "tackle")
	teachMoves(t, d, s.Active(1), "splash", "splash")
	p := s.Active(0)
	p.HP = p.MaxHP / 4
	p.Status = StatusToxic
	p.ToxicCounter = 5

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if p.HP != p.MaxHP {
		t.Errorf("Rest left the user at %d/%d, want a full heal; log: %v", p.HP, p.MaxHP, logTexts(log))
	}
	if p.Status != StatusSleep {
		t.Errorf("Rest left status %q, want sleep — Rest replaces any status with its own; log: %v",
			p.Status, logTexts(log))
	}
	if p.ToxicCounter != 0 {
		t.Errorf("ToxicCounter = %d after Rest, want 0 — the escalation must not survive the cure", p.ToxicCounter)
	}
	if !logHas(log, "went to sleep and became healthy!") {
		t.Errorf("Rest did not announce itself; log: %v", logTexts(log))
	}

	// Next turn: the user tries to attack and cannot, because it is asleep.
	foeHP := s.Active(1).HP
	log = ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})
	if s.Active(1).HP != foeHP {
		t.Errorf("the turn after Rest the user attacked anyway (foe %d → %d); log: %v",
			foeHP, s.Active(1).HP, logTexts(log))
	}
	if !logHas(log, "is fast asleep.") {
		t.Errorf("no sleep refusal on the turn after Rest; log: %v", logTexts(log))
	}

	// And the turn after that it wakes up and acts: the nap is two turns, not
	// an indefinite one.
	log = ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})
	if !logHas(log, "woke up!") {
		t.Errorf("the user did not wake on the second turn of a Rest sleep; log: %v", logTexts(log))
	}
	if s.Active(1).HP >= foeHP {
		t.Errorf("the woken user did not attack (foe %d → %d); log: %v", foeHP, s.Active(1).HP, logTexts(log))
	}
}

// Rest at full HP with nothing to cure fails. Without the gate it is a free
// two-turn nap a player would never choose — but every AI that scores "heal to
// full" would, and the move would also hand out a self-inflicted Sleep that
// Sleep Clause and the status-immunity abilities never agreed to.
//
// The second half is the part a one-sided test misses: full HP alone is not a
// refusal. A poisoned Pokémon at full HP still has something to cure, so Rest
// must work.
func TestRestFailsOnlyWhenThereIsNothingToCure(t *testing.T) {
	newRester := func() (*domain.Dex, *BattleState) {
		d := loadDex(t)
		s := speciesBattle(t, d, 4, []int{143}, []int{3})
		s.Active(0).Ability = AbilityNone
		s.Active(1).Ability = AbilityNone
		teachMoves(t, d, s.Active(0), "rest")
		teachMoves(t, d, s.Active(1), "splash")
		return d, s
	}

	// Full HP, no status: refused.
	d, s := newRester()
	p := s.Active(0)
	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if p.Status != StatusNone {
		t.Errorf("Rest slept a healthy full-HP user (status %q); log: %v", p.Status, logTexts(log))
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("a pointless Rest did not report failure; log: %v", logTexts(log))
	}

	// Full HP, but poisoned: there is something to cure, so it works.
	d, s = newRester()
	p = s.Active(0)
	p.Status = StatusPoison
	log = ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if p.Status != StatusSleep {
		t.Errorf("Rest refused a full-HP but poisoned user (status %q); log: %v", p.Status, logTexts(log))
	}
	if p.HP != p.MaxHP {
		t.Errorf("HP = %d/%d after resting off a poison at full HP", p.HP, p.MaxHP)
	}
}
