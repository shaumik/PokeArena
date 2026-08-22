//go:build showdown

package showdown

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// harness_selftest_test.go proves the instrument before anybody trusts a
// measurement taken with it.
//
// A test harness that silently passes is worse than no harness: 2,000 ported
// cases reporting green because every assertion is a no-op would be a
// convincing and completely false claim about the engine. So the checks below
// exercise the harness's own failure paths — an assertion that should record,
// a name that should not resolve, a ledger row that should go stale — using
// throwaway *testing.T values so a real failure here is a failure of the
// harness rather than of the engine.

// --- names --------------------------------------------------------------

// TestEveryDatasetSlugNormalizesToAShowdownId is the claim names_test.go rests
// on: strip the punctuation from any slug this engine ships and you get
// Showdown's id for the same thing. If a future dataset refresh introduces a
// slug that does not — "hidden-power-fire", say, which Showdown splits
// differently — every port naming it silently fails to resolve, so this must
// break loudly instead.
func TestEveryDatasetSlugNormalizesToAShowdownId(t *testing.T) {
	d := dex(t)
	ok := regexp.MustCompile(`^[a-z0-9]+$`)
	check := func(kind, slug string) {
		t.Helper()
		if id := psID(slug); !ok.MatchString(id) || id == "" {
			t.Errorf("%s %q normalizes to %q, which is not a usable Showdown id", kind, slug, id)
		}
	}
	for id := range d.Moves {
		check("move", id)
	}
	for id := range d.Items {
		check("item", id)
	}
	for _, sp := range d.Species {
		check("species", sp.Name)
		for _, a := range sp.Abilities {
			check("ability", a)
		}
	}
}

// TestNormalizationDoesNotCollide guards the other half. psID is lossy — it
// throws punctuation away — so two distinct slugs could in principle land on
// the same id and make one of them unreachable. None do today; this fails the
// day one does.
func TestNormalizationDoesNotCollide(t *testing.T) {
	d := dex(t)
	seen := map[string]string{}
	claim := func(kind, slug string) {
		t.Helper()
		id := kind + "/" + psID(slug)
		if prev, dup := seen[id]; dup && prev != slug {
			t.Errorf("%q and %q both normalize to %q — one of them is unreachable from a port", prev, slug, id)
		}
		seen[id] = slug
	}
	for id := range d.Moves {
		claim("move", id)
	}
	for id := range d.Items {
		claim("item", id)
	}
	for _, sp := range d.Species {
		claim("species", sp.Name)
	}
}

// TestStandInsAllResolve keeps the substitution table honest: every row must
// name a species this dex actually has, and must not shadow one it already
// has under its own name.
func TestStandInsAllResolve(t *testing.T) {
	d := dex(t)
	index.build(d)
	for id, si := range standIns {
		sp, ok := d.Species[si.Dex]
		if !ok {
			t.Errorf("stand-in for %q points at Pokédex number %d, which is not in this dex", id, si.Dex)
			continue
		}
		if si.Keeps == "" {
			t.Errorf("stand-in %q → %s has no stated reason; a substitution without one cannot be reviewed", id, sp.Name)
		}
		if own, have := index.species[id]; have && own != si.Dex {
			t.Errorf("stand-in %q → %s shadows the species this dex already has under that name (%d)",
				id, sp.Name, own)
		}
	}
}

// TestResolveSpeciesRefusesTheUnknown proves the failure path: a species with
// neither a dex entry nor a stand-in must produce an error naming it, not a
// zero that quietly builds the wrong Pokémon.
func TestResolveSpeciesRefusesTheUnknown(t *testing.T) {
	d := dex(t)
	if _, _, err := resolveSpecies(d, "Iron Valiant"); err == nil {
		t.Fatal("an unmapped species resolved without error")
	}
	if n, sub, err := resolveSpecies(d, "Blissey"); err != nil || !sub || n != 113 {
		t.Fatalf("Blissey should stand in as Chansey (113): got %d, substituted=%v, err=%v", n, sub, err)
	}
	if n, sub, err := resolveSpecies(d, "Gengar"); err != nil || sub || n != 94 {
		t.Fatalf("Gengar is in the dex and should not be substituted: got %d, substituted=%v, err=%v", n, sub, err)
	}
}

// --- assertions ---------------------------------------------------------

// TestAssertionsActuallyRecord is the one that matters most. Each entry
// arranges a state the assertion must reject, and the harness must come back
// with exactly one recorded failure. An assertion that forgets to record is
// invisible in a green run.
func TestAssertionsActuallyRecord(t *testing.T) {
	d := dex(t)
	mon := &engine.Pokemon{Name: "Test", HP: 50, MaxHP: 100, Item: engine.ItemNone, Ability: "guts"}
	full := &engine.Pokemon{Name: "Full", HP: 100, MaxHP: 100, Item: "leftovers", Status: engine.StatusBurn}

	cases := []struct {
		what string
		fn   func(p *ps)
	}{
		{"equal on different values", func(p *ps) { p.equal("a", "b", "") }},
		{"notEqual on the same value", func(p *ps) { p.notEqual("a", "a", "") }},
		{"ok on false", func(p *ps) { p.ok(false, "x") }},
		{"isFalse on true", func(p *ps) { p.isFalse(true, "x") }},
		{"fullHP on a damaged Pokémon", func(p *ps) { p.fullHP(mon, "") }},
		{"damaged on an undamaged Pokémon", func(p *ps) { p.damaged(full, "") }},
		{"fainted on a living Pokémon", func(p *ps) { p.fainted(mon, "") }},
		{"notFainted on a fainted Pokémon", func(p *ps) {
			p.notFainted(&engine.Pokemon{Name: "Dead", Fainted: true}, "")
		}},
		{"holdsItem on an itemless Pokémon", func(p *ps) { p.holdsItem(mon, "") }},
		{"noItem on a Pokémon holding one", func(p *ps) { p.noItem(full, "") }},
		{"hasAbility on the wrong ability", func(p *ps) { p.hasAbility(mon, "levitate", "") }},
		{"hasStatus on the wrong status", func(p *ps) { p.hasStatus(full, "par", "") }},
		{"noStatus on a statused Pokémon", func(p *ps) { p.noStatus(full, "") }},
		{"statStage on the wrong stage", func(p *ps) { p.statStage(mon, "atk", -1, "") }},
		{"bounded outside the range", func(p *ps) { p.bounded(5, 10, 20, "") }},
		{"atLeast below the threshold", func(p *ps) { p.atLeast(5, 10, "") }},
		{"atMost above the threshold", func(p *ps) { p.atMost(25, 20, "") }},
		{"hurts when nothing happened", func(p *ps) { p.hurts(mon, func() {}, "") }},
		{"hurtsBy with the wrong figure", func(p *ps) { p.hurtsBy(mon, 10, func() { mon.HP -= 3 }, "") }},
		{"constant over a change", func(p *ps) {
			p.constant(func() any { return mon.HP }, func() { mon.HP-- }, "")
		}},
		{"sets to the wrong value", func(p *ps) {
			p.sets(func() any { return mon.HP }, 999, func() {}, "")
		}},
		{"logHas on an empty log", func(p *ps) { p.logHas("nothing here", "") }},
		{"logLacks on a line that is present", func(p *ps) {
			p.all = []engine.LogLine{{Text: "Snorlax fainted!"}}
			p.logLacks("fainted", "")
		}},
		{"a panic inside the body", func(p *ps) { panic("boom") }},
	}

	for _, c := range cases {
		p := &ps{t: t, dex: d, seed: 1}
		p.exec(c.fn)
		if len(p.fails) != 1 {
			t.Errorf("%s recorded %d failures, want exactly 1 (%v)", c.what, len(p.fails), p.fails)
		}
	}
}

// TestAssertionsStaySilentWhenSatisfied is the mirror. An assertion that fires
// on a correct state produces false findings, which is the other way to make
// this suite worthless.
func TestAssertionsStaySilentWhenSatisfied(t *testing.T) {
	d := dex(t)
	full := &engine.Pokemon{Name: "Full", HP: 100, MaxHP: 100, Item: "leftovers", Ability: "guts", Status: engine.StatusBurn}
	hurt := &engine.Pokemon{Name: "Hurt", HP: 40, MaxHP: 100}

	p := &ps{t: t, dex: d, seed: 1}
	p.exec(func(p *ps) {
		p.equal(full.Item, "leftovers", "")
		p.equal(full.Item, "Leftovers", "") // display name normalizes to the same id
		p.notEqual(full.Item, "lumberry", "")
		p.ok(true, "")
		p.isFalse(false, "")
		p.fullHP(full, "")
		p.damaged(hurt, "")
		p.notFainted(full, "")
		p.holdsItem(full, "")
		p.noItem(hurt, "")
		p.hasAbility(full, "guts", "")
		p.hasStatus(full, "brn", "")
		p.hasStatus(full, "burn", "")
		p.noStatus(hurt, "")
		p.statStage(full, "atk", 0, "")
		p.bounded(15, 10, 20, "")
		p.atLeast(15, 10, "")
		p.atMost(15, 20, "")
		p.hurts(hurt, func() { hurt.HP -= 5 }, "")
		p.hurtsBy(hurt, 10, func() { hurt.HP -= 10 }, "")
		p.constant(func() any { return full.HP }, func() {}, "")
		p.sets(func() any { return hurt.HP }, 20, func() { hurt.HP = 20 }, "")
	})
	if len(p.fails) != 0 {
		t.Errorf("satisfied assertions recorded failures: %v", p.fails)
	}
}

// --- driving ------------------------------------------------------------

// TestChoiceGrammar covers the strings a port is allowed to write, including
// the two that are easy to get subtly wrong: a 1-based slot number, and a
// switch naming a species that was substituted on the way in.
func TestChoiceGrammar(t *testing.T) {
	p := &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		p.battle(
			team{
				{Species: "Snorlax", Moves: mv("tackle", "splash", "rest")},
				{Species: "Blissey", Moves: mv("softboiled")},
			},
			team{{Species: "Gengar", Moves: mv("shadowball")}},
		)
	})
	if len(p.fails) > 0 {
		t.Fatalf("setup failed: %v", p.fails)
	}

	cases := []struct {
		in   string
		want engine.Action
	}{
		{"move tackle", engine.Action{Kind: engine.ActionMove, Index: 0}},
		{"move splash", engine.Action{Kind: engine.ActionMove, Index: 1}},
		{"move 3", engine.Action{Kind: engine.ActionMove, Index: 2}},
		{"move Rest", engine.Action{Kind: engine.ActionMove, Index: 2}},
		{"switch 2", engine.Action{Kind: engine.ActionSwitch, Index: 1}},
		{"switch chansey", engine.Action{Kind: engine.ActionSwitch, Index: 1}},
		{"switch blissey", engine.Action{Kind: engine.ActionSwitch, Index: 1}}, // the name it was ported under
		{"", engine.Action{Kind: engine.ActionMove, Index: 0}},
		{"default", engine.Action{Kind: engine.ActionMove, Index: 0}},
	}
	for _, c := range cases {
		p.fails = nil
		got, ok := p.parse(0, c.in)
		if !ok {
			t.Errorf("%q did not parse: %v", c.in, p.fails)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%q parsed to %+v, want %+v", c.in, got, c.want)
		}
	}

	// And the refusals: a move the active does not know, a slot past the end,
	// and a species that is not on the team must all be reported, not guessed.
	for _, bad := range []string{"move earthquake", "move 9", "switch mewtwo", "switch 7", "wobble 1"} {
		p.fails = nil
		if _, ok := p.parse(0, bad); ok {
			t.Errorf("%q parsed, and should not have", bad)
		}
		if len(p.fails) == 0 {
			t.Errorf("%q was refused without recording why", bad)
		}
	}
}

// TestBattleSetupAppliesPostBuildState covers the three fields the pick struct
// cannot carry, because each is silent when it does not work: a stripped
// ability just behaves like the species default, and a starting HP or status
// that fails to apply leaves a test measuring the wrong thing.
func TestBattleSetupAppliesPostBuildState(t *testing.T) {
	p := &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		p.battle(
			team{{Species: "Snorlax", Ability: "noability", Moves: mv("splash"), HP: 42, Status: "brn"}},
			team{{Species: "Gengar", Moves: mv("splash")}},
		)
	})
	if len(p.fails) > 0 {
		t.Fatalf("setup failed: %v", p.fails)
	}
	mine := p.mine()
	if mine.Ability != engine.AbilityNone {
		t.Errorf(`Ability: "noability" left %s with %q`, mine.Name, mine.Ability)
	}
	if mine.HP != 42 {
		t.Errorf("HP: 42 left %s at %d", mine.Name, mine.HP)
	}
	if mine.Status != engine.StatusBurn {
		t.Errorf(`Status: "brn" left %s with %q`, mine.Name, mine.Status)
	}
	// The other side keeps its species default, so "noability" is doing
	// something rather than the field being ignored in both directions.
	if p.foe().Ability == engine.AbilityNone {
		t.Error("the foe lost its species ability without being asked to")
	}
	// An HP over the maximum is a broken fixture and must be refused rather
	// than clamped — a clamp would let a test claim a Pokémon survived a hit
	// it was never at risk from.
	p2 := &ps{t: t, dex: dex(t), seed: 1}
	p2.exec(func(p *ps) {
		p.battle(
			team{{Species: "Snorlax", Moves: mv("splash"), HP: 9999}},
			team{{Species: "Gengar", Moves: mv("splash")}},
		)
	})
	if len(p2.fails) == 0 {
		t.Error("a starting HP above the maximum was accepted")
	}
}

// TestDrivingAcrossAFaint covers the phase boundary that hundreds of ported
// cases cross without mentioning it. When an active faints, this engine leaves
// the battle in PhaseReplace and wants ResolveReplace rather than ResolveTurn;
// upstream expresses the same moment as an ordinary makeChoices with a forced
// switch. If the harness got that wiring wrong, every multi-Pokémon port would
// either stall on the dead lead or silently skip the replacement, and the
// assertions after it would be measuring the wrong Pokémon.
func TestDrivingAcrossAFaint(t *testing.T) {
	// Magikarp stands in as Seaking; what the fixture needs is only that the
	// lead is at 1 HP and the foe out-damages that.
	build := func(p *ps) {
		p.battle(
			team{
				{Species: "Magikarp", Ability: "noability", Moves: mv("splash"), HP: 1},
				{Species: "Snorlax", Ability: "noability", Moves: mv("tackle")},
			},
			team{{Species: "Machamp", Ability: "noability", Moves: mv("closecombat")}},
		)
	}

	// The explicit form: upstream's makeChoices('switch 2', '').
	p := &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		build(p)
		p.turn()
		p.fainted(p.mine(), "the lead at 1 HP should be dead")
		if p.state().Phase != engine.PhaseReplace {
			p.fail("a faint left the battle in phase %q, not replace", p.state().Phase)
		}
		p.makeChoices("switch 2", "")
		p.species(p.mine(), "Snorlax", "the replacement should be out")
		if p.state().Phase != engine.PhaseChoosing {
			p.fail("after the replacement the battle is in phase %q", p.state().Phase)
		}
	})
	if len(p.fails) > 0 {
		t.Errorf("explicit replacement: %v", p.fails)
	}

	// The default form: an empty choice in the replace phase has to resolve to
	// the switch, not to a move the dead Pokemon cannot make.
	p = &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		build(p)
		p.turn()
		p.turn()
		p.species(p.mine(), "Snorlax", "an empty choice in a replace phase must pick the switch")
	})
	if len(p.fails) > 0 {
		t.Errorf("default replacement: %v", p.fails)
	}

	// And a move choice in a replace phase is a broken port, not something to
	// quietly reinterpret.
	p = &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		build(p)
		p.turn()
		p.makeChoices("move splash", "")
	})
	if len(p.fails) == 0 {
		t.Error("a move submitted during a forced switch was accepted")
	}
}

// TestUnknownNamesAreRecordedAsFailures pins the decision that a move or item
// this dataset lacks is a *finding*, not a skip: "the engine has no Belch" is
// precisely what the port is here to enumerate, and a skip would file it under
// "did not try".
func TestUnknownNamesAreRecordedAsFailures(t *testing.T) {
	for _, c := range []struct {
		what string
		tm   team
	}{
		{"an unknown move", team{{Species: "Snorlax", Moves: mv("transform")}}},
		{"an unknown item", team{{Species: "Snorlax", Item: "eviolite", Moves: mv("splash")}}},
		{"an unmapped species", team{{Species: "Iron Valiant", Moves: mv("splash")}}},
	} {
		p := &ps{t: t, dex: dex(t), seed: 1}
		p.exec(func(p *ps) {
			p.battle(c.tm, team{{Species: "Gengar", Moves: mv("splash")}})
		})
		if len(p.fails) == 0 {
			t.Errorf("%s was accepted silently", c.what)
		}
		if !p.dead {
			t.Errorf("%s did not mark the case dead, so later calls would run against no battle", c.what)
		}
	}
}

// TestUnmodeledMechanicsReportThemselves covers the diagnosis that turns a
// confusing failure into an obvious one. An ability the engine has no record of
// is *silent*: the Pokemon simply plays without it, and the case then fails on
// whatever it was measuring. "Normalize did not change the move's type" is a
// bad way to learn that Normalize is not implemented, and it is the difference
// between a triage row filed as gapBug and one filed as gapMissing.
func TestUnmodeledMechanicsReportThemselves(t *testing.T) {
	// Sanity first: an ability the engine does model must not be flagged, or
	// every port carrying a real ability would report a false finding.
	p := &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		p.battle(
			team{{Species: "Snorlax", Ability: "thickfat", Item: "leftovers", Moves: mv("bodyslam")}},
			team{{Species: "Gengar", Ability: "cursedbody", Moves: mv("shadowball")}},
		)
	})
	if len(p.fails) > 0 {
		t.Errorf("abilities and items the engine models were flagged: %v", p.fails)
	}

	// And an ability with no registry entry must say exactly that.
	p = &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		p.battle(
			team{{Species: "Snorlax", Ability: "normalize", Moves: mv("bodyslam")}},
			team{{Species: "Gengar", Moves: mv("shadowball")}},
		)
	})
	if len(p.fails) != 1 || !strings.Contains(p.fails[0], "no record of this ability") {
		t.Errorf("an unmodeled ability produced %v, want one finding naming it", p.fails)
	}
	// The case still runs — the finding is a diagnosis, not a reason to stop —
	// so whatever the port went on to assert is also reported.
	if p.dead {
		t.Error("an unmodeled ability killed the scenario; it should only annotate it")
	}
}

// TestDeadScenarioStopsDriving: once setup has failed there is no battle, and
// every later call must be a no-op rather than a nil dereference that turns
// one legible failure into a panic.
func TestDeadScenarioStopsDriving(t *testing.T) {
	p := &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		p.battle(team{{Species: "Snorlax", Moves: mv("transform")}}, team{{Species: "Gengar", Moves: mv("splash")}})
		p.makeChoices("move transform", "move splash")
		p.turn()
	})
	if len(p.fails) != 1 {
		t.Errorf("a dead scenario recorded %d failures, want the one that killed it: %v", len(p.fails), p.fails)
	}
}

// TestLegalityAssertionsAgainstARealBattle exercises the four helpers that
// read LegalActions rather than the battle state — the ones Disable, Taunt,
// Torment, Imprison, choice-lock and every trapping port depends on. They are
// the assertions most likely to be silently vacuous: cantMove passes when the
// move is absent for *any* reason, including a port that misspelled it, so it
// needs a positive control (canMove) proving the move was choosable a moment
// earlier.
func TestLegalityAssertionsAgainstARealBattle(t *testing.T) {
	p := &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		p.battle(
			team{
				{Species: "Alakazam", Ability: "noability", Moves: mv("psychic", "disable")},
				{Species: "Snorlax", Ability: "noability", Moves: mv("tackle")},
			},
			team{
				{Species: "Snorlax", Ability: "noability", Moves: mv("tackle", "splash")},
				{Species: "Chansey", Ability: "noability", Moves: mv("splash")},
			},
		)
		// Nothing is restricting anybody yet.
		p.canMove(1, "tackle", "")
		p.notTrapped(1, "")
		p.notTrapped(0, "")

		// Disable needs a last move to name, and Alakazam outspeeds — so the
		// first turn is what gives the foe one. Disabling on turn 1 would fail
		// for a reason that has nothing to do with the assertion.
		p.makeChoices("move psychic", "move tackle")
		p.makeChoices("move disable", "move tackle")
		p.cantMove(1, "tackle", "Disable should have taken Tackle away")
		p.canMove(1, "splash", "Disable takes one move, not the whole set")
	})
	if len(p.fails) > 0 {
		t.Errorf("Disable control: %v", p.fails)
	}

	// Trapping: Mean Look on a foe with a bench must remove every switch.
	p = &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		p.battle(
			team{{Species: "Gengar", Ability: "noability", Moves: mv("meanlook", "splash")}},
			team{
				{Species: "Snorlax", Ability: "noability", Moves: mv("splash")},
				{Species: "Chansey", Ability: "noability", Moves: mv("splash")},
			},
		)
		p.notTrapped(1, "a foe with a live bench starts free to switch")
		p.makeChoices("move meanlook", "move splash")
		p.trapped(1, "Mean Look should have stopped the switch")
	})
	if len(p.fails) > 0 {
		t.Errorf("Mean Look control: %v", p.fails)
	}

	// And the refusal: naming a move the active does not know must be reported,
	// not read as "it is blocked".
	p = &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		p.battle(
			team{{Species: "Snorlax", Ability: "noability", Moves: mv("splash")}},
			team{{Species: "Snorlax", Ability: "noability", Moves: mv("splash")}},
		)
		p.cantMove(0, "earthquake", "")
	})
	if len(p.fails) == 0 {
		t.Error("cantMove passed for a move the Pokemon never knew, which makes every use of it vacuous")
	}
}

// TestTeamValidationHelpers proves the two directions and the padding, because
// a legalTeam that always passes and an illegalTeam that always fails look
// identical in a green run — and both are easy to write, since the padding
// alone could make every roster legal or every roster illegal.
func TestTeamValidationHelpers(t *testing.T) {
	p := &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		// A one-Pokemon roster the validator should accept once padded.
		p.legalTeam(team{{Species: "Snorlax", Ability: "immunity", Moves: mv("bodyslam")}},
			"a Snorlax with a move it learns")
	})
	if len(p.fails) > 0 {
		t.Errorf("a legal roster was rejected: %v", p.fails)
	}

	for _, c := range []struct {
		what string
		tm   team
	}{
		{"a species that does not exist", team{{Species: "Nonexistent Pokemon", Moves: mv("thunderbolt")}}},
		{"an item that does not exist", team{{Species: "Raichu", Ability: "static", Item: "nonexistentItem", Moves: mv("thunderbolt")}}},
		{"an ability the species cannot have", team{{Species: "Raichu", Ability: "levitate", Moves: mv("thunderbolt")}}},
		{"a move that does not exist", team{{Species: "Raichu", Ability: "static", Moves: mv("nonexistentMove")}}},
		{"a move the species cannot learn", team{{Species: "Snorlax", Moves: mv("leafstorm")}}},
		{"an EV spread over the cap", team{{Species: "Snorlax", Moves: mv("bodyslam"), EVs: evs(map[string]int{"hp": 253})}}},
	} {
		p := &ps{t: t, dex: dex(t), seed: 1}
		p.exec(func(p *ps) { p.illegalTeam(c.tm, c.what) })
		if len(p.fails) > 0 {
			t.Errorf("%s was accepted as legal: %v", c.what, p.fails)
		}
	}

	// The padding must not be the thing making rosters legal or illegal: a
	// full six-Pokemon legal roster needs no pad and must still validate, and
	// the pad must never collide with a species the case named (which would
	// trip Species Clause and fail every case for the wrong reason).
	p = &ps{t: t, dex: dex(t), seed: 1}
	p.exec(func(p *ps) {
		p.legalTeam(team{{Species: "Snorlax", Moves: mv("bodyslam")}}, "the pad must avoid the named species")
	})
	if len(p.fails) > 0 {
		t.Errorf("padding collided with the roster: %v", p.fails)
	}
}

// --- the ledger ---------------------------------------------------------

// recorder stands in for *testing.T so reconcile's verdict can be read off
// directly. A real sub-test would have to actually fail to prove the "this
// case passes now, delete its ledger row" path, which is not something a
// green suite can contain.
type recorder struct {
	errored bool
	skipped bool
	msg     string
}

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.errored = true
	r.msg = fmt.Sprintf(format, args...)
}

func (r *recorder) Skipf(format string, args ...any) {
	r.skipped = true
	r.msg = fmt.Sprintf(format, args...)
}

// TestReconcileDecidesCorrectly walks the four-way table in gaps_test.go's
// doc comment, plus the skip row. Getting any cell wrong makes the whole
// suite lie: a "listed and passing" cell that stays quiet leaves dead
// quarantine entries hiding real regressions, and an "unlisted and failing"
// cell that stays quiet means the port reports green while it is red.
func TestReconcileDecidesCorrectly(t *testing.T) {
	const listed = "Selftest: a listed case"
	const unlisted = "Selftest: an unlisted case"
	gaps[listed] = gap{Kind: gapBug, Why: "selftest fixture"}
	defer delete(gaps, listed)

	// The tally is package state that the summary reads; the rows this test
	// pushes into it are fixtures, not results, so put it back afterwards.
	tallyMu.Lock()
	saved := tally
	tallyMu.Unlock()
	defer func() {
		tallyMu.Lock()
		tally = saved
		tallyMu.Unlock()
	}()

	cases := []struct {
		what              string
		key               string
		fails             []string
		skip              string
		wantErr, wantSkip bool
		wantStatus, inMsg string
	}{
		{"unlisted and passing", unlisted, nil, "", false, false, "pass", ""},
		{"unlisted and failing", unlisted, []string{"boom"}, "", true, false, "regress", "boom"},
		{"listed and failing", listed, []string{"boom"}, "", false, true, "gap", "selftest fixture"},
		{"listed and passing", listed, nil, "", true, false, "stale", "passes now"},
		{"skipped", unlisted, nil, "doubles", false, true, "skip", "doubles"},
	}
	for _, c := range cases {
		tallyMu.Lock()
		tally = nil
		tallyMu.Unlock()

		r := &recorder{}
		reconcile(r, c.key, c.fails, c.skip)

		if r.errored != c.wantErr || r.skipped != c.wantSkip {
			t.Errorf("%s: errored=%v skipped=%v, want errored=%v skipped=%v (message: %s)",
				c.what, r.errored, r.skipped, c.wantErr, c.wantSkip, r.msg)
		}
		if c.inMsg != "" && !strings.Contains(r.msg, c.inMsg) {
			t.Errorf("%s: message %q does not mention %q", c.what, r.msg, c.inMsg)
		}
		tallyMu.Lock()
		got := tally
		tallyMu.Unlock()
		if len(got) != 1 || got[0].Status != c.wantStatus {
			t.Errorf("%s: tallied %+v, want one row with status %q", c.what, got, c.wantStatus)
		}
	}
}

// TestReportIsWritable covers the JSON side-channel the triage tooling reads.
func TestReportIsWritable(t *testing.T) {
	path := t.TempDir() + "/report.json"
	tallyMu.Lock()
	saved := tally
	tally = []outcome{{Key: "A: b", Status: "gap", Kind: string(gapBug), Why: "selftest"}}
	tallyMu.Unlock()
	writeReport(path)
	tallyMu.Lock()
	tally = saved
	tallyMu.Unlock()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("report was not written: %v", err)
	}
	var got []outcome
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	if len(got) != 1 || got[0].Key != "A: b" || got[0].Kind != string(gapBug) {
		t.Errorf("report round-tripped as %+v", got)
	}
}

// TestLedgerKeysAreUnique reads the ports themselves and checks that no two
// cases claim the same "<describe>: <it>" key.
//
// The key is the ledger's primary key, and a collision is silent in every
// direction that matters: one row would cover two cases, so triaging one of
// them marks the other expected, and the stale check would clear the row while
// the second case is still red. Nothing in a run surfaces that.
//
// It is a real risk rather than a theoretical one. Upstream nests describes,
// and the byte-for-byte rule means a nested block is keyed on its own literal
// string — download.js contributes a block named just "[Gen 4]", which is not
// a name anybody would expect to be unique across three hundred files.
//
// Checked by reading the source rather than by watching a run, so it holds for
// -count=2 and for a filtered -run as well.
func TestLedgerKeysAreUnique(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse the port package: %v", err)
	}

	// key -> where it was claimed
	seen := map[string][]string{}
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 3 {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok || id.Name != "describe" {
					return true
				}
				group, ok := stringArg(call.Args[1])
				if !ok {
					return true
				}
				// The third argument is func(g *psg) { ... }; every case name
				// inside it belongs to this group.
				body, ok := call.Args[2].(*ast.FuncLit)
				if !ok {
					return true
				}
				ast.Inspect(body, func(n ast.Node) bool {
					inner, ok := n.(*ast.CallExpr)
					if !ok || len(inner.Args) == 0 {
						return true
					}
					sel, ok := inner.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					switch sel.Sel.Name {
					case "it", "skip", "itRate", "itSeed":
					default:
						return true
					}
					name, ok := stringArg(inner.Args[0])
					if !ok {
						return true
					}
					key := group + ": " + name
					seen[key] = append(seen[key],
						fmt.Sprintf("%s:%d", filepath.Base(path), fset.Position(inner.Pos()).Line))
					return true
				})
				return true
			})
		}
	}

	if len(seen) == 0 {
		t.Fatal("found no ported cases at all; the AST walk is not matching describe/it")
	}
	dupes := 0
	for key, where := range seen {
		if len(where) > 1 {
			dupes++
			t.Errorf("ledger key %q is claimed by %d cases (%s).\n"+
				"    One ledger row would cover both: triaging one silently marks the other\n"+
				"    expected, and the stale check clears the row while the other is still red.\n"+
				"    Disambiguate by giving the describe block the name mocha would show —\n"+
				"    the outer block's name plus the inner one.",
				key, len(where), strings.Join(where, ", "))
		}
	}
	t.Logf("%d ported cases, %d duplicate keys", len(seen), dupes)
}

// stringArg unwraps a string literal argument, or reports that it was not one.
func stringArg(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// --- spreads ------------------------------------------------------------

// TestSpreadHelpersDefaultLikeShowdown: an omitted EV is 0 and an omitted IV
// is 31, and getting that backwards would shift every damage figure in the
// corpus by a few points — enough to move a KO threshold, not enough to be
// obvious.
func TestSpreadHelpersDefaultLikeShowdown(t *testing.T) {
	e := evs(map[string]int{"hp": 4, "spa": 252})
	if *e != (domain.Stats{HP: 4, SpA: 252}) {
		t.Errorf("evs defaulted wrong: %+v", *e)
	}
	i := ivs(map[string]int{"atk": 0})
	if *i != (domain.Stats{HP: 31, Atk: 0, Def: 31, SpA: 31, SpD: 31, Spe: 31}) {
		t.Errorf("ivs defaulted wrong: %+v", *i)
	}
	// Showdown writes both "spa" and "spatk" depending on the file's age.
	if evs(map[string]int{"spatk": 8}).SpA != 8 || evs(map[string]int{"spd": 8}).SpD != 8 {
		t.Error("the stat aliases Showdown uses do not all land")
	}
}

// TestStandInReportRenders keeps the documentation generator working, since
// docs/showdown-port.md is generated from it and a silent break there means a
// doc that stops matching the table.
func TestStandInReportRenders(t *testing.T) {
	out := standInReport(dex(t))
	if !strings.Contains(out, "| blissey | Chansey |") {
		t.Errorf("the stand-in report does not contain the Blissey row:\n%s", out)
	}
	if n := strings.Count(out, "\n"); n != len(standIns) {
		t.Errorf("the report has %d rows for %d stand-ins", n, len(standIns))
	}
}
