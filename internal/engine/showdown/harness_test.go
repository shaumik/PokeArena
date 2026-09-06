//go:build showdown

package showdown

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
)

// harness_test.go is the vocabulary the ports are written in. It exists so a
// translated case can sit next to its Showdown original and be checked
// line-for-line by eye:
//
//	it('should remove most items', () => {                 g.it("should remove most items", func(p *ps) {
//	  battle = common.createBattle([[                        p.battle(
//	    {species: "Mew", moves: ['knockoff']},                 team{{Species: "Mew", Moves: mv("knockoff")}},
//	  ], [                                                     team{{Species: "Blissey", Item: "shedshell",
//	    {species: "Blissey", item: 'shedshell',                        Moves: mv("softboiled")}},
//	     moves: ['softboiled']},                             )
//	  ]]);                                                   p.makeChoices("move knockoff", "move softboiled")
//	  battle.makeChoices('move knockoff', 'move softboiled') p.equal(p.foe().Item, "", "")
//	  assert.equal(battle.p2.active[0].item, '');           })
//	});
//
// Three things it does that the JS original does not, and each is a
// deliberate departure rather than an accident of the language:
//
//  1. Every case runs over several battle seeds, not one. Showdown pins its
//     RNG with an explicit seed array; this engine has no such hook and
//     internal/engine/probability_test.go argues at length against tests that
//     pick a lucky seed. So the default is to demand the assertion hold on
//     every seed in psSeeds, which is strictly stronger for the deterministic
//     majority of the corpus and correctly refuses to state a fixed answer for
//     the rest. Genuinely probabilistic cases use g.itRate.
//
//  2. Assertions record instead of failing. A port is allowed to be red — that
//     is the point of bringing the corpus over before the engine can pass it —
//     so the runner compares the outcome against the ledger in gaps_test.go
//     and only calls t.Error when the outcome and the ledger disagree. A case
//     that starts passing is as much a failure as one that starts failing,
//     because a stale ledger entry hides the next regression.
//
//  3. Species get substituted. See names_test.go.

// psSeeds is how many battle seeds each case is replayed under. Five is
// enough to catch a port that accidentally depends on a damage roll or a
// secondary landing, and cheap enough that the whole corpus stays runnable.
// Override with PS_SEEDS for a deeper sweep.
var psSeeds = func() int {
	if v := os.Getenv("PS_SEEDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 5
}()

var (
	dexOnce sync.Once
	dexVal  *domain.Dex
	dexErr  error
)

// dex loads the shipped dataset once for the whole package.
func dex(t *testing.T) *domain.Dex {
	t.Helper()
	dexOnce.Do(func() { dexVal, dexErr = domain.LoadDex("../../../data", "showdown-port") })
	if dexErr != nil {
		t.Fatalf("load dex: %v", dexErr)
	}
	return dexVal
}

// --- the team spec ------------------------------------------------------

// set is one Pokémon in a ported team, named for Showdown's word for it. The
// fields mirror the object literal upstream tests write, minus the ones this
// engine has no concept of (level is fixed at 50, happiness and shininess do
// not exist, and there is no second active slot for `ally`).
type set struct {
	// Species is the Showdown species name, written exactly as upstream wrote
	// it. Anything outside this dex is routed through standIns; see
	// names_test.go.
	Species string
	// As names the in-dex species to build instead, for a substitution this
	// port is making on its own rather than through the shared table.
	//
	// It exists so the upstream name can stay in the fixture. A port that
	// silently rewrites "Tapu Koko" to "Raichu" is no longer diffable against
	// its original, and the next reader cannot tell a considered substitution
	// from a species the porter happened to like. With As, the line still says
	// which case this is a translation of:
	//
	//	{Species: "Tapu Koko", As: "Raichu", Moves: mv("electricterrain")},
	//
	// Set it whenever Species is not in this dex and standIns has no row, and
	// say in a comment what the substitution preserves. A stand-in row is
	// still preferable when one exists — one reviewed table beats three
	// hundred local decisions — but a port cannot add rows to that table
	// without colliding with every other port being written at the same time.
	As string
	// Ability is a Showdown ability id. Empty means "the species' first
	// ability", which is what Showdown does. The literal "noability" strips
	// it, which is what upstream tests write when they want a body that does
	// not interfere.
	Ability string
	Item    string
	Moves   []string
	EVs     *domain.Stats
	IVs     *domain.Stats
	Nature  string
	Gender  string
	// HP sets the Pokémon's starting HP absolutely. Zero leaves it full.
	// Upstream expresses the same thing by attacking first; setting it
	// directly keeps a port short and takes the damage roll out of the
	// setup, where it is noise.
	HP int
	// Status is a non-volatile status to start with ("brn", "par", "slp",
	// "psn", "tox", "frz"), for the same reason as HP.
	Status string
}

// team is one side's roster. The first entry leads.
type team []set

// mv is shorthand for a move list, so a ported set reads at the same width as
// the JS it came from.
func mv(ids ...string) []string { return ids }

// evs / ivs build a stat spread from Showdown's object form. Showdown omits
// the stats it does not set and defaults them to 0 (EVs) or 31 (IVs); these
// do the same.
func evs(kv map[string]int) *domain.Stats { return spreadFrom(kv, 0) }
func ivs(kv map[string]int) *domain.Stats { return spreadFrom(kv, 31) }

func spreadFrom(kv map[string]int, dflt int) *domain.Stats {
	s := domain.Stats{HP: dflt, Atk: dflt, Def: dflt, SpA: dflt, SpD: dflt, Spe: dflt}
	for k, v := range kv {
		switch k {
		case "hp":
			s.HP = v
		case "atk":
			s.Atk = v
		case "def":
			s.Def = v
		case "spa", "spatk":
			s.SpA = v
		case "spd", "spdef":
			s.SpD = v
		case "spe", "speed":
			s.Spe = v
		}
	}
	return &s
}

// --- the runner ---------------------------------------------------------

// psg is a `describe` block: a group of ported cases from one upstream
// describe, carrying the name so the ledger key reads like the original.
type psg struct {
	t    *testing.T
	name string
	// seen counts how many cases in this group have already claimed each
	// name, so a group carrying the same `it` string twice still produces two
	// distinct ledger keys. Upstream does this — brickbreak.js has two
	// different tests both called "should break Reflect against a Ghost type
	// whose type immunity is being ignored" — and mocha simply runs both. Here
	// the string is a primary key, so the second occurrence is suffixed " #2".
	// The source string stays byte-for-byte upstream's, which is what makes
	// the original findable; only the key is decorated.
	seen map[string]int
}

// describe opens a ported describe block. The name must match the upstream
// string exactly — it is half of the ledger key, and matching it is what lets
// a reader find the original.
func describe(t *testing.T, name string, body func(g *psg)) {
	t.Helper()
	body(&psg{t: t, name: name, seen: map[string]int{}})
}

// it ports one upstream `it`. The body is replayed under every seed in
// psSeeds; the case passes only if it holds under all of them.
func (g *psg) it(name string, body func(p *ps)) {
	g.run(name, func(t *testing.T) (fails []string, skip string) {
		for seed := 1; seed <= psSeeds; seed++ {
			p := &ps{t: t, dex: dex(t), seed: uint64(seed)}
			p.exec(body)
			if p.skipped != "" {
				return nil, p.skipped
			}
			if len(p.fails) > 0 {
				for _, f := range p.fails {
					fails = append(fails, fmt.Sprintf("seed %d: %s", seed, f))
				}
				// One seed's worth of detail is enough to diagnose; replaying
				// the rest just multiplies the same message.
				return fails, ""
			}
		}
		return nil, ""
	})
}

// itSeed is it for the rare case whose setup genuinely cannot be stated
// seed-independently. The reason is required and shows up in the report, so
// the escape hatch stays visible.
func (g *psg) itSeed(name string, seed uint64, reason string, body func(p *ps)) {
	g.run(name, func(t *testing.T) ([]string, string) {
		p := &ps{t: t, dex: dex(t), seed: seed, seedReason: reason}
		p.exec(body)
		if p.skipped != "" {
			return nil, p.skipped
		}
		return p.fails, ""
	})
}

// itRate ports a case whose subject is a probability. body reports whether
// the event fired on its seed; the measured rate must land inside [lo, hi].
// Upstream pins these with a rigged RNG, which is not available here — see
// internal/engine/probability_test.go for why measuring is preferred anyway.
func (g *psg) itRate(name string, lo, hi float64, seeds int, body func(p *ps) bool) {
	g.run(name, func(t *testing.T) ([]string, string) {
		fired, counted := 0, 0
		for seed := 1; seed <= seeds; seed++ {
			p := &ps{t: t, dex: dex(t), seed: uint64(seed)}
			var got bool
			p.exec(func(p *ps) { got = body(p) })
			if p.skipped != "" {
				return nil, p.skipped
			}
			if len(p.fails) > 0 {
				return p.fails, ""
			}
			counted++
			if got {
				fired++
			}
		}
		rate := float64(fired) / float64(counted)
		if rate < lo || rate > hi {
			return []string{fmt.Sprintf("fired on %d of %d seeds (%.1f%%), want within [%.0f%%, %.0f%%]",
				fired, counted, 100*rate, 100*lo, 100*hi)}, ""
		}
		return nil, ""
	})
}

// skip records an upstream case the port deliberately does not attempt, with
// the category that explains why. Use it at group level for whole files that
// are out of scope (doubles, Z-moves, older generations); use ps.skip inside a
// body when only that case is.
func (g *psg) skip(name, reason string) {
	g.run(name, func(*testing.T) ([]string, string) { return nil, reason })
}

// run is the shared shell: it names the subtest, executes fn, and reconciles
// the outcome against the ledger.
func (g *psg) run(name string, fn func(*testing.T) (fails []string, skip string)) {
	g.seen[name]++
	key := g.name + ": " + name
	if n := g.seen[name]; n > 1 {
		key = fmt.Sprintf("%s #%d", key, n)
	}
	g.t.Run(strings.ReplaceAll(name, "/", "|"), func(t *testing.T) {
		fails, skip := fn(t)
		reconcile(t, key, fails, skip)
	})
}

// --- the scenario -------------------------------------------------------

// ps is one ported case in flight under one seed: the battle, the log so far,
// and the assertions that have failed.
type ps struct {
	t          *testing.T
	dex        *domain.Dex
	seed       uint64
	seedReason string

	st  *engine.BattleState
	log []engine.LogLine // the most recent turn
	all []engine.LogLine // every line since the battle started

	fails   []string
	skipped string
	dead    bool // a fatal setup problem; later calls no-op
}

// exec runs a case body, turning a panic into a recorded failure. An
// unimplemented mechanic can nil-deref deep inside the engine, and a port
// crashing the whole run would hide every case after it.
func (p *ps) exec(body func(p *ps)) {
	defer func() {
		if r := recover(); r != nil {
			p.fails = append(p.fails, fmt.Sprintf("panic: %v", r))
		}
	}()
	body(p)
}

// battle builds the two teams and starts the battle. Every name is resolved
// through names_test.go; a name this dataset does not have is recorded as a
// failure naming it, because "the engine has no Belch" is exactly the kind of
// gap this suite is here to enumerate.
//
// # One divergence from createBattle that every port has to know
//
// Showdown runs the leads' switch-in events as part of starting the battle, so
// upstream can assert on them with no turn played:
//
//	battle = common.createBattle([[...Smeargle...], [...Gyarados/Intimidate...]]);
//	assert.statStage(battle.p1.active[0], 'atk', -1);   // already -1
//
// This engine fires them at the top of turn 1 instead, inside ResolveTurn
// (turn.go:60), and says why: "We piggyback on turn 1 rather than burdening
// NewBattle/NewBattleFromPicks with a log channel." Nothing can act between
// the two moments, so no game state observable to a player differs — but the
// state a *test* reads straight after building does.
//
// So a port that translates the two lines above literally sees +0 and reports
// a false finding about Intimidate. Play a turn first, with both sides on an
// inert move:
//
//	p.battle(... Moves: mv("splash") ..., ... Moves: mv("splash") ...)
//	p.leadsEnter()
//	p.statStage(p.mine(), "atk", -1, "")
//
// leadsEnter is p.turn() under a name that says why it is there, so the next
// reader does not delete it as a stray turn.
func (p *ps) battle(t1, t2 team) {
	if p.dead {
		return
	}
	picks1, ok1 := p.picks(t1, "p1")
	picks2, ok2 := p.picks(t2, "p2")
	if !ok1 || !ok2 {
		p.dead = true
		return
	}
	st, err := engine.NewBattleFromPicks(p.dex, "showdown-port", "P1", picks1, "P2", picks2, p.seed)
	if err != nil {
		p.fail("could not start the battle: %v", err)
		p.dead = true
		return
	}
	p.st = st
	// Post-build state the pick struct cannot carry: an explicitly stripped
	// ability, a starting HP, a starting status. Applied before the first turn
	// so switch-in hooks that have already fired are not re-run.
	for side, ts := range [2]team{t1, t2} {
		for i, s := range ts {
			mon := &st.Sides[side].Team[i]
			if psID(s.Ability) == "noability" {
				mon.Ability = engine.AbilityNone
			}
			if s.HP > 0 {
				if s.HP > mon.MaxHP {
					p.fail("%s: starting HP %d exceeds its max of %d", mon.Name, s.HP, mon.MaxHP)
					continue
				}
				mon.HP = s.HP
			}
			if s.Status != "" {
				p.setStatus(mon, s.Status)
			}
		}
	}
}

// picks translates a ported team into engine picks.
func (p *ps) picks(ts team, label string) ([]engine.TeamPick, bool) {
	if len(ts) == 0 {
		p.fail("%s: a side needs at least one Pokémon", label)
		return nil, false
	}
	out := make([]engine.TeamPick, 0, len(ts))
	for i, s := range ts {
		build := s.Species
		if s.As != "" {
			build = s.As
			// A stand-in that is itself not in the dex is a typo, and one that
			// resolves through standIns is a second hop nobody asked for —
			// both would build something the port did not intend.
			index.build(p.dex)
			if _, ok := index.species[psID(build)]; !ok {
				p.fail("%s slot %d: As: %q is not a species in this dex", label, i+1, s.As)
				return nil, false
			}
		}
		num, _, err := resolveSpecies(p.dex, build)
		if err != nil {
			p.fail("%s slot %d: %v", label, i+1, err)
			return nil, false
		}
		pick := engine.TeamPick{DexNo: num, EVs: s.EVs, IVs: s.IVs, Nature: s.Nature, Gender: s.Gender}
		for _, m := range s.Moves {
			slug, err := resolveMove(p.dex, m)
			if err != nil {
				p.fail("%s slot %d: %v", label, i+1, err)
				return nil, false
			}
			pick.MoveIDs = append(pick.MoveIDs, slug)
		}
		if s.Item != "" {
			slug, err := resolveItem(p.dex, s.Item)
			if err != nil {
				p.fail("%s slot %d: %v", label, i+1, err)
				return nil, false
			}
			// The item exists in the dataset but the engine may model nothing
			// for it, in which case the holder plays bare and the case fails
			// somewhere downstream with no hint why. Say so here instead.
			if why := engine.ItemInertReason(slug); why != "" {
				p.fail("%s slot %d: item %q — %s", label, i+1, s.Item, why)
			}
			pick.Item = slug
		}
		if a := psID(s.Ability); a != "" && a != "noability" {
			slug := deSlug(a, p.dex)
			// Same for the ability, and this one matters more: an ability the
			// engine does not model is silent, so the Pokemon simply plays
			// without it and every assertion downstream reports the wrong
			// thing. "Normalize did not change the type" is a confusing way to
			// learn that Normalize is not implemented.
			//
			// Note that a slug no in-dex species carries is *not* a problem —
			// the registry is keyed on the slug, not on the species — so the
			// question asked here is only whether the engine models it.
			if why := engine.AbilityInertReason(slug); why != "" {
				p.fail("%s slot %d: ability %q — %s", label, i+1, s.Ability, why)
			}
			pick.Ability = slug
		}
		out = append(out, pick)
	}
	return out, true
}

// deSlug turns a Showdown ability id back into this engine's kebab slug.
//
// The obvious source is the species list, and for most abilities it is enough.
// It is not enough for the nine the engine models that no Kanto species
// carries — Speed Boost, Sand Stream, Snow Warning, Sap Sipper, Storm Drain,
// Motor Drive, Slush Rush, Sweet Veil, Magma Armor. Resolving only through
// species leaves "stormdrain" unhyphenated, `AbilityInertReason` reports "no
// record of this ability at all", and the port emits a false finding saying an
// implemented ability is missing. That is the worst output this suite can
// produce, and it would have hit every case built on any of those nine.
//
// So the fallback consults the engine's own source, with the engine as the
// oracle: every kebab-shaped string literal in internal/engine is offered to
// AbilityInertReason, and the ones it recognizes are real ability slugs. No
// hand-maintained list, and it cannot drift from the registry.
func deSlug(id string, d *domain.Dex) string {
	index.build(d)
	for _, sp := range d.Species {
		for _, a := range sp.Abilities {
			if psID(a) == id {
				return a
			}
		}
	}
	if slug, ok := engineAbilitySlugs()[id]; ok {
		return slug
	}
	return id
}

// engineAbilitySlugs maps a Showdown ability id to this engine's slug for
// every ability the registry knows, including those no in-dex species carries.
var engineAbilitySlugs = sync.OnceValue(func() map[string]string {
	const noRecord = "no record of this ability"
	out := map[string]string{}
	for _, lit := range engineStringLiterals() {
		if !kebabSlug.MatchString(lit) {
			continue
		}
		if why := engine.AbilityInertReason(lit); !strings.Contains(why, noRecord) {
			out[psID(lit)] = lit
		}
	}
	return out
})

// kebabSlug matches the shape this engine uses for ability and item slugs.
// Requiring at least one hyphen keeps the scan off the thousands of ordinary
// words in the source: a single-word ability id needs no translation anyway.
var kebabSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)+$`)

func (p *ps) setStatus(mon *engine.Pokemon, s string) {
	switch psID(s) {
	case "brn", "burn":
		mon.Status = engine.StatusBurn
	case "par", "paralysis":
		mon.Status = engine.StatusParalysis
	case "psn", "poison":
		mon.Status = engine.StatusPoison
	case "tox", "toxic":
		mon.Status, mon.ToxicCounter = engine.StatusToxic, 1
	case "slp", "sleep":
		mon.Status, mon.SleepTurns = engine.StatusSleep, 3
	case "frz", "freeze":
		mon.Status = engine.StatusFreeze
	default:
		p.fail("unknown starting status %q", s)
	}
}

// --- driving the battle -------------------------------------------------

// makeChoices submits one action per side and resolves. The strings are
// Showdown's own choice grammar, so a ported line is the upstream line:
//
//	"move knockoff"   by name        "move 1"      by 1-based slot
//	"switch gyarados" by name        "switch 3"    by 1-based team index
//	"default" / ""    the first legal action for that side
//
// In the replace phase (a fainted active waiting for its replacement) only
// switch choices are read, matching Showdown's forced-switch request.
func (p *ps) makeChoices(c1, c2 string) {
	if p.dead || p.st == nil {
		return
	}
	if p.st.Ended() {
		p.fail("makeChoices(%q, %q) after the battle already ended", c1, c2)
		return
	}
	switch p.st.Phase {
	case engine.PhaseReplace:
		var sw [2]*engine.Action
		for side, c := range [2]string{c1, c2} {
			if !p.st.Replace[side] {
				continue
			}
			act, ok := p.parse(side, c)
			if !ok {
				return
			}
			if act.Kind != engine.ActionSwitch {
				p.fail("side %d must replace a fainted Pokémon, but the port chose %q", side+1, c)
				return
			}
			a := act
			sw[side] = &a
		}
		p.record(engine.ResolveReplace(p.st, sw))
	default:
		var acts [2]engine.Action
		for side, c := range [2]string{c1, c2} {
			act, ok := p.parse(side, c)
			if !ok {
				return
			}
			acts[side] = act
		}
		p.record(engine.ResolveTurn(p.dex, p.st, acts))
	}
}

// turn is makeChoices with both sides defaulting, matching upstream's
// argument-less battle.makeChoices().
func (p *ps) turn() { p.makeChoices("", "") }

// leadsEnter plays the turn this engine needs before the leads' switch-in
// abilities have fired — see the divergence noted on battle. Both sides should
// be holding an inert move (Splash) so the turn contributes nothing but the
// entry hooks.
//
// A separate name rather than a bare p.turn() because the turn is not part of
// the case being translated: it is scaffolding for a representational
// difference, and an unexplained extra turn at the top of a port is exactly
// the kind of thing a later reader deletes.
func (p *ps) leadsEnter() { p.makeChoices("", "") }

// parse turns one Showdown choice string into an action for that side.
func (p *ps) parse(side int, c string) (engine.Action, bool) {
	legal := engine.LegalActionsDex(p.dex, p.st, side)
	c = strings.TrimSpace(strings.ToLower(c))
	if c == "" || c == "default" || c == "auto" || c == "pass" {
		if len(legal) == 0 {
			p.fail("side %d has no legal action", side+1)
			return engine.Action{}, false
		}
		return legal[0], true
	}
	kind, arg, _ := strings.Cut(c, " ")
	arg = strings.TrimSpace(arg)
	switch kind {
	case "move":
		if psID(arg) == "struggle" {
			return engine.Action{Kind: engine.ActionMove, Index: -1}, true
		}
		idx, err := p.moveSlot(side, arg)
		if err != nil {
			p.fail("side %d: %v", side+1, err)
			return engine.Action{}, false
		}
		return engine.Action{Kind: engine.ActionMove, Index: idx}, true
	case "switch":
		idx, err := p.teamSlot(side, arg)
		if err != nil {
			p.fail("side %d: %v", side+1, err)
			return engine.Action{}, false
		}
		return engine.Action{Kind: engine.ActionSwitch, Index: idx}, true
	}
	p.fail("side %d: %q is not a choice this harness understands", side+1, c)
	return engine.Action{}, false
}

// moveSlot resolves "knockoff" or "2" into a move index on the active.
func (p *ps) moveSlot(side int, arg string) (int, error) {
	mon := p.st.Active(side)
	if n, err := strconv.Atoi(arg); err == nil {
		if n < 1 || n > len(mon.Moves) {
			return 0, fmt.Errorf("move slot %d is out of range for %s (%d moves)", n, mon.Name, len(mon.Moves))
		}
		return n - 1, nil
	}
	want := psID(arg)
	for i, ms := range mon.Moves {
		if psID(ms.MoveID) == want {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%s does not know %q (it knows %s)", mon.Name, arg, moveList(mon))
}

// teamSlot resolves "gyarados" or "3" into a team index.
func (p *ps) teamSlot(side int, arg string) (int, error) {
	sd := &p.st.Sides[side]
	if n, err := strconv.Atoi(arg); err == nil {
		if n < 1 || n > len(sd.Team) {
			return 0, fmt.Errorf("team slot %d is out of range (%d Pokémon)", n, len(sd.Team))
		}
		return n - 1, nil
	}
	want := psID(arg)
	for i := range sd.Team {
		if psID(sd.Team[i].Name) == want {
			return i, nil
		}
	}
	// The port may have named a species that was substituted; resolve the
	// stand-in and try again so "switch blissey" finds the Chansey standing
	// in for it.
	if num, _, err := resolveSpecies(p.dex, arg); err == nil {
		for i := range sd.Team {
			if sd.Team[i].DexNo == num {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("no Pokémon named %q on side %d", arg, side+1)
}

func moveList(mon *engine.Pokemon) string {
	ids := make([]string, 0, len(mon.Moves))
	for _, m := range mon.Moves {
		ids = append(ids, m.MoveID)
	}
	return strings.Join(ids, ", ")
}

func (p *ps) record(log []engine.LogLine) {
	p.log = log
	p.all = append(p.all, log...)
}

// --- reading the battle -------------------------------------------------

// active returns the Pokémon currently out on a side (0 or 1).
func (p *ps) active(side int) *engine.Pokemon {
	if p.st == nil {
		p.fail("no battle has been built")
		return &engine.Pokemon{}
	}
	return p.st.Active(side)
}

// mine / foe name the two actives the way a ported test thinks about them:
// p1's active is the one whose behavior is usually under test.
func (p *ps) mine() *engine.Pokemon { return p.active(0) }
func (p *ps) foe() *engine.Pokemon  { return p.active(1) }

// slot reaches a benched Pokémon by 1-based team position, matching
// Showdown's battle.p1.pokemon[n].
func (p *ps) slot(side, n int) *engine.Pokemon {
	if p.st == nil || n < 1 || n > len(p.st.Sides[side].Team) {
		p.fail("side %d has no slot %d", side+1, n)
		return &engine.Pokemon{}
	}
	return &p.st.Sides[side].Team[n-1]
}

// state exposes the battle for the handful of assertions that read field
// conditions directly. Ports should prefer the named helpers below.
func (p *ps) state() *engine.BattleState { return p.st }

// weather / terrain report the active field conditions as Showdown ids, or ""
// for none, so a port can compare against the string the original used.
func (p *ps) weather() string {
	if p.st == nil || p.st.Weather == nil {
		return ""
	}
	return string(p.st.Weather.Kind)
}

func (p *ps) terrain() string {
	if p.st == nil || p.st.Terrain == nil {
		return ""
	}
	return string(p.st.Terrain.Kind)
}

// stage reads one stat stage off a Pokémon by Showdown's stat name.
func (p *ps) stage(mon *engine.Pokemon, stat string) int {
	switch psID(stat) {
	case "atk", "attack":
		return mon.Stages.Atk
	case "def", "defense":
		return mon.Stages.Def
	case "spa", "spatk", "specialattack":
		return mon.Stages.SpA
	case "spd", "spdef", "specialdefense":
		return mon.Stages.SpD
	case "spe", "speed":
		return mon.Stages.Spe
	case "accuracy", "acc":
		return mon.Stages.Acc
	case "evasion", "eva":
		return mon.Stages.Eva
	}
	p.fail("unknown stat %q", stat)
	return 0
}

// logText is every line produced so far, joined, for substring assertions.
func (p *ps) logText() string {
	var b strings.Builder
	for _, l := range p.all {
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// lastTurnText is the same for only the most recent turn.
func (p *ps) lastTurnText() string {
	var b strings.Builder
	for _, l := range p.log {
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// --- team validation ----------------------------------------------------
//
// test/sim/team-validator/** asks a different question from the rest of the
// corpus: not "what does this battle do" but "is this roster legal". Upstream
// spells it `assert.legalTeam(team, 'gen7customgame')`; here it is
// engine.ValidateTeam, and two things have to be arranged around it.
//
// **Team size.** Upstream's custom-game formats accept a team of one, and
// nearly every case there is a one-Pokemon team probing a single rule. This
// engine's ValidateTeam requires exactly six, so a literal translation would
// report every case illegal for a reason the case is not about. legalTeam pads
// to six with filler the validator is known to accept, and says so — the pad
// is visible in the failure message, so a case that is actually failing on the
// pad cannot be mistaken for one failing on its subject.
//
// **Lenient name resolution.** Half these cases hand the validator a
// deliberately non-existent species, move, item or ability and expect it to
// object. The battle path resolves names first and records "not in this
// dataset" as a finding, which is right for a battle and wrong here — it would
// answer the question before the validator saw it. So these two helpers pass
// unknown names straight through, and let the validator be the thing that
// rejects them.

// fillerTeam is the roster legalTeam pads with: six species that are in the
// dex, each with a move it genuinely learns, distinct so Species Clause is not
// tripped by the padding itself.
func fillerPicks(d *domain.Dex, used map[int]bool, n int) []engine.TeamPick {
	candidates := []struct {
		dex  int
		move string
	}{
		{143, "body-slam"},
		{94, "shadow-ball"},
		{65, "psychic"},
		{130, "waterfall"},
		{9, "surf"},
		{6, "flamethrower"},
		{26, "thunderbolt"},
		{112, "earthquake"},
		{3, "giga-drain"},
		{131, "ice-beam"},
		{68, "cross-chop"},
		{123, "x-scissor"},
	}
	var out []engine.TeamPick
	for _, c := range candidates {
		if len(out) == n {
			break
		}
		if used[c.dex] {
			continue
		}
		used[c.dex] = true
		out = append(out, engine.TeamPick{DexNo: c.dex, MoveIDs: []string{c.move}})
	}
	_ = d
	return out
}

// validatePicks builds picks leniently — unknown names survive as themselves —
// and returns the validator's verdict along with how many filler slots were
// added.
func (p *ps) validatePicks(ts team) (padded int, err error) {
	index.build(p.dex)
	used := map[int]bool{}
	picks := make([]engine.TeamPick, 0, len(ts))
	for _, s := range ts {
		name := s.Species
		if s.As != "" {
			name = s.As
		}
		pick := engine.TeamPick{EVs: s.EVs, IVs: s.IVs, Nature: s.Nature, Gender: s.Gender}
		if num, _, e := resolveSpecies(p.dex, name); e == nil {
			pick.DexNo = num
			used[num] = true
		}
		// A species the resolver cannot place keeps DexNo 0, which
		// ValidateTeam reports as an unknown Pokedex number — the rejection
		// the case is asking for.
		for _, m := range s.Moves {
			if slug, e := resolveMove(p.dex, m); e == nil {
				pick.MoveIDs = append(pick.MoveIDs, slug)
			} else {
				pick.MoveIDs = append(pick.MoveIDs, psID(m))
			}
		}
		if s.Item != "" {
			if slug, e := resolveItem(p.dex, s.Item); e == nil {
				pick.Item = slug
			} else {
				pick.Item = psID(s.Item)
			}
		}
		if a := psID(s.Ability); a != "" && a != "noability" {
			pick.Ability = deSlug(a, p.dex)
		}
		picks = append(picks, pick)
	}
	if n := engine.TeamSize - len(picks); n > 0 {
		picks = append(picks, fillerPicks(p.dex, used, n)...)
		padded = n
	}
	return padded, engine.ValidateTeam(picks, p.dex)
}

// legalTeam asserts the roster validates. Mirrors assert.legalTeam.
func (p *ps) legalTeam(ts team, msg string) {
	padded, err := p.validatePicks(ts)
	if err != nil {
		p.fail("%s: the team was rejected — %v%s", orDefault(msg, "team should be legal"), err, padNote(padded))
	}
}

// illegalTeam asserts the roster is refused. Mirrors assert.false.legalTeam.
func (p *ps) illegalTeam(ts team, msg string) {
	padded, err := p.validatePicks(ts)
	if err == nil {
		p.fail("%s: the team validated%s", orDefault(msg, "team should be rejected"), padNote(padded))
	}
}

func padNote(padded int) string {
	if padded == 0 {
		return ""
	}
	return fmt.Sprintf(" (the roster was padded with %d filler Pokemon to reach this engine's fixed team size of %d)",
		padded, engine.TeamSize)
}

// --- assertions ---------------------------------------------------------
//
// These mirror test/assert.js. Each records rather than aborting, so one
// ported case reports everything wrong with it in a single run rather than
// one thing per iteration.

func (p *ps) fail(format string, args ...any) {
	// A case that has declared itself out of scope records nothing further.
	// The runner already prefers the skip over any failures, so this changes
	// no verdict — but it keeps the invariant next to the thing that has to
	// hold it, instead of depending on the order of two checks in `it`. An
	// assertion evaluated after a skip is measuring a battle the case stopped
	// driving, and it should not appear in the report at all.
	if p.skipped != "" {
		return
	}
	p.fails = append(p.fails, fmt.Sprintf(format, args...))
}

// skip abandons this case as out of scope. The reason is shown in the report
// and should name the category — "doubles", "gen 4 mechanics", "Z-moves" —
// not just say "unsupported".
func (p *ps) skip(reason string) {
	p.skipped = reason
	p.dead = true
}

func (p *ps) ok(cond bool, msg string) {
	if !cond {
		p.fail("%s", msg)
	}
}

func (p *ps) isFalse(cond bool, msg string) {
	if cond {
		p.fail("%s", msg)
	}
}

func (p *ps) equal(got, want any, msg string) {
	if !sameValue(got, want) {
		p.fail("%s: got %v, want %v", orDefault(msg, "values differ"), render(got), render(want))
	}
}

func (p *ps) notEqual(got, unwanted any, msg string) {
	if sameValue(got, unwanted) {
		p.fail("%s: got %v, which it must not be", orDefault(msg, "value must differ"), render(got))
	}
}

// sameValue compares across the string-ish types the engine uses for slugs
// (ItemKind, AbilityKind, StatusCond, WeatherKind) so a port can write the
// Showdown id on the right-hand side and not think about Go types.
func sameValue(a, b any) bool {
	as, aok := asString(a)
	bs, bok := asString(b)
	if aok && bok {
		return psID(as) == psID(bs)
	}
	return a == b
}

func asString(v any) (string, bool) {
	switch s := v.(type) {
	case string:
		return s, true
	case engine.ItemKind:
		return string(s), true
	case engine.AbilityKind:
		return string(s), true
	case engine.StatusCond:
		return string(s), true
	case domain.Type:
		return string(s), true
	}
	return "", false
}

func render(v any) any {
	if s, ok := asString(v); ok {
		if s == "" {
			return `""`
		}
		return s
	}
	return v
}

func orDefault(s, dflt string) string {
	if s == "" {
		return dflt
	}
	return s
}

func (p *ps) statStage(mon *engine.Pokemon, stat string, want int, msg string) {
	if got := p.stage(mon, stat); got != want {
		p.fail("%s: %s's %s stage is %+d, want %+d",
			orDefault(msg, "wrong stat stage"), mon.Name, stat, got, want)
	}
}

func (p *ps) fullHP(mon *engine.Pokemon, msg string) {
	if mon.HP != mon.MaxHP {
		p.fail("%s: %s is at %d/%d HP, want full", orDefault(msg, "should be undamaged"), mon.Name, mon.HP, mon.MaxHP)
	}
}

func (p *ps) damaged(mon *engine.Pokemon, msg string) {
	if mon.HP == mon.MaxHP {
		p.fail("%s: %s is still at full HP", orDefault(msg, "should have taken damage"), mon.Name)
	}
}

func (p *ps) fainted(mon *engine.Pokemon, msg string) {
	if !mon.Fainted {
		p.fail("%s: %s is at %d/%d HP and has not fainted", orDefault(msg, "should have fainted"), mon.Name, mon.HP, mon.MaxHP)
	}
}

func (p *ps) notFainted(mon *engine.Pokemon, msg string) {
	if mon.Fainted {
		p.fail("%s: %s fainted", orDefault(msg, "should have survived"), mon.Name)
	}
}

func (p *ps) hasAbility(mon *engine.Pokemon, ability, msg string) {
	if psID(string(mon.Ability)) != psID(ability) {
		p.fail("%s: %s has %q, want %q", orDefault(msg, "wrong ability"), mon.Name, mon.Ability, ability)
	}
}

func (p *ps) holdsItem(mon *engine.Pokemon, msg string) {
	if mon.Item == engine.ItemNone {
		p.fail("%s: %s is holding nothing", orDefault(msg, "should still hold its item"), mon.Name)
	}
}

func (p *ps) noItem(mon *engine.Pokemon, msg string) {
	if mon.Item != engine.ItemNone {
		p.fail("%s: %s is still holding %s", orDefault(msg, "should have lost its item"), mon.Name, mon.Item)
	}
}

func (p *ps) hasStatus(mon *engine.Pokemon, status, msg string) {
	want := psID(status)
	switch want {
	case "brn":
		want = "burn"
	case "par":
		want = "paralysis"
	case "psn":
		want = "poison"
	case "tox":
		want = "toxic"
	case "slp":
		want = "sleep"
	case "frz":
		want = "freeze"
	}
	if psID(string(mon.Status)) != want {
		p.fail("%s: %s has status %q, want %q", orDefault(msg, "wrong status"), mon.Name, orDefault(string(mon.Status), "none"), status)
	}
}

func (p *ps) noStatus(mon *engine.Pokemon, msg string) {
	if mon.Status != engine.StatusNone {
		p.fail("%s: %s is %s", orDefault(msg, "should be status-free"), mon.Name, mon.Status)
	}
}

func (p *ps) bounded(v, lo, hi int, msg string) {
	if v < lo || v > hi {
		p.fail("%s: %d is outside [%d, %d]", orDefault(msg, "out of range"), v, lo, hi)
	}
}

func (p *ps) atLeast(v, threshold int, msg string) {
	if v < threshold {
		p.fail("%s: %d is below %d", orDefault(msg, "too low"), v, threshold)
	}
}

func (p *ps) atMost(v, threshold int, msg string) {
	if v > threshold {
		p.fail("%s: %d is above %d", orDefault(msg, "too high"), v, threshold)
	}
}

// hurts runs fn and requires the Pokémon to have lost HP over it.
func (p *ps) hurts(mon *engine.Pokemon, fn func(), msg string) {
	before := mon.HP
	fn()
	if mon.HP >= before {
		p.fail("%s: %s went from %d to %d HP", orDefault(msg, "should have been hurt"), mon.Name, before, mon.HP)
	}
}

// hurtsBy is hurts with the exact figure, for the fractions canon states
// precisely (1/16 of max HP, 1/8, and so on).
func (p *ps) hurtsBy(mon *engine.Pokemon, damage int, fn func(), msg string) {
	before := mon.HP
	fn()
	if got := before - mon.HP; got != damage {
		p.fail("%s: %s lost %d HP, want exactly %d", orDefault(msg, "wrong damage"), mon.Name, got, damage)
	}
}

// constant runs fn and requires the getter to read the same before and after.
func (p *ps) constant(getter func() any, fn func(), msg string) {
	before := getter()
	fn()
	if after := getter(); !sameValue(before, after) {
		p.fail("%s: changed from %v to %v", orDefault(msg, "should not have changed"), render(before), render(after))
	}
}

// sets runs fn and requires the getter to read want afterwards.
func (p *ps) sets(getter func() any, want any, fn func(), msg string) {
	fn()
	if got := getter(); !sameValue(got, want) {
		p.fail("%s: got %v, want %v", orDefault(msg, "wrong result"), render(got), render(want))
	}
}

// species asserts which Pokémon is out, by Showdown name (resolved through
// the stand-in table, so a port can name the original).
func (p *ps) species(mon *engine.Pokemon, name, msg string) {
	num, _, err := resolveSpecies(p.dex, name)
	if err != nil {
		p.fail("%v", err)
		return
	}
	if mon.DexNo != num {
		p.fail("%s: active is %s, want %s", orDefault(msg, "wrong Pokémon out"), mon.Name, name)
	}
}

// cantMove asserts a move is not a legal choice for a side this turn — the
// Disable / Taunt / Torment / Imprison / Choice-lock shape.
func (p *ps) cantMove(side int, move, msg string) {
	idx, err := p.moveSlot(side, move)
	if err != nil {
		p.fail("%v", err)
		return
	}
	for _, a := range engine.LegalActionsDex(p.dex, p.st, side) {
		if a.Kind == engine.ActionMove && a.Index == idx {
			p.fail("%s: %s is still choosable", orDefault(msg, "move should be blocked"), move)
			return
		}
	}
}

// canMove is its mirror, for the "and the others still work" half that a
// blocking test needs to be worth anything.
func (p *ps) canMove(side int, move, msg string) {
	idx, err := p.moveSlot(side, move)
	if err != nil {
		p.fail("%v", err)
		return
	}
	for _, a := range engine.LegalActionsDex(p.dex, p.st, side) {
		if a.Kind == engine.ActionMove && a.Index == idx {
			return
		}
	}
	p.fail("%s: %s is not choosable", orDefault(msg, "move should be available"), move)
}

// trapped asserts a side has no switch available — Mean Look, Arena Trap,
// Magnet Pull, Ingrain, a partial trap.
func (p *ps) trapped(side int, msg string) {
	for _, a := range engine.LegalActionsDex(p.dex, p.st, side) {
		if a.Kind == engine.ActionSwitch {
			p.fail("%s: side %d can still switch", orDefault(msg, "should be trapped"), side+1)
			return
		}
	}
}

func (p *ps) notTrapped(side int, msg string) {
	for _, a := range engine.LegalActionsDex(p.dex, p.st, side) {
		if a.Kind == engine.ActionSwitch {
			return
		}
	}
	p.fail("%s: side %d cannot switch", orDefault(msg, "should be free to switch"), side+1)
}

// logHas / logLacks assert on the turn narration. Upstream matches on
// protocol lines (`|-ability|p2a: Gyarados|Intimidate`); this engine emits
// prose, so ports match a distinctive fragment of the sentence instead. Keep
// the fragment short and mechanical — matching a whole sentence makes the
// port a spelling test.
//
// Both check the fragment against the engine's own vocabulary first, and that
// check is the more important half.
//
// A logLacks whose fragment is misspelled passes every time and proves
// nothing: "the log does not say Intimidate cut" is true whether or not
// Intimidate fired, because the engine says "cuts". Across two thousand ported
// cases that failure mode is invisible — a green assertion measuring nothing —
// and no amount of running the suite surfaces it. logHas has the same problem
// in the other direction: it fails, but it fails with "no log line contains
// X", which reads like an engine bug rather than a typo and costs somebody an
// afternoon.
//
// So a fragment that no engine string could ever contain is reported as what
// it is. See engineLogFragments.
func (p *ps) logHas(substr, msg string) {
	if !p.knownFragment(substr) {
		return
	}
	if !strings.Contains(p.logText(), substr) {
		p.fail("%s: no log line contains %q", orDefault(msg, "missing log line"), substr)
	}
}

func (p *ps) logLacks(substr, msg string) {
	if !p.knownFragment(substr) {
		return
	}
	if strings.Contains(p.logText(), substr) {
		p.fail("%s: a log line contains %q and should not", orDefault(msg, "unexpected log line"), substr)
	}
}

// knownFragment reports whether substr could appear in some line this engine
// emits, and records a failure naming the problem when it could not.
func (p *ps) knownFragment(substr string) bool {
	if substr == "" {
		p.fail("an empty log fragment matches everything; name the line you mean")
		return false
	}
	for _, chunk := range engineLogFragments() {
		if strings.Contains(chunk, substr) {
			return true
		}
	}
	p.fail("no string in internal/engine can contain %q, so this assertion could never "+
		"mean anything. Either it is a typo, or the fragment spans a %%s/%%d substitution, "+
		"or the engine emits no line for this event — in the last case assert on the state "+
		"change instead", substr)
	return false
}

// engineLogFragments is every verb-free run of text that appears inside a
// string literal in internal/engine. It is deliberately an over-approximation:
// it takes *all* literals rather than only the ones reaching a LogLine, so it
// can never reject a fragment that is really emitted. What it catches is the
// fragment that appears nowhere in the engine at all.
//
// Splitting on the format verbs is what enforces the documented rule that a
// fragment must not span a substitution — "restored 12 HP" is not a fragment
// of "%s restored %d HP.", and pinning it would be pinning one damage roll.
var engineLogFragments = sync.OnceValue(func() []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range engineStringLiterals() {
		for _, chunk := range verbSplit.Split(s, -1) {
			if chunk != "" && !seen[chunk] {
				seen[chunk] = true
				out = append(out, chunk)
			}
		}
	}
	if len(out) == 0 {
		// Without the engine source the check cannot run. A single catch-all
		// keeps every port working rather than failing all of them for a
		// reason that has nothing to do with the port.
		return []string{""}
	}
	return out
})

// engineStringLiterals is every string literal in internal/engine's
// non-test sources, read once. Two things need it — the log-fragment check and
// the ability-slug resolver — and both want the same "ask the engine rather
// than keep a list" property.
var engineStringLiterals = sync.OnceValue(func() []string {
	fset := token.NewFileSet()
	//nolint:staticcheck // go/packages would pull a heavy dependency into a test helper that only needs string literals.
	pkgs, err := parser.ParseDir(fset, "..", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		ast.Inspect(pkg, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || seen[s] {
				return true
			}
			seen[s] = true
			out = append(out, s)
			return true
		})
	}
	return out
})

// verbSplit matches a printf verb, so a template splits into the fixed text
// around its substitutions.
var verbSplit = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z%]`)

// logCount is for the "exactly once" assertions — a doubled announcement is a
// bug this engine has actually shipped.
func (p *ps) logCount(substr string) int {
	return strings.Count(p.logText(), substr)
}
