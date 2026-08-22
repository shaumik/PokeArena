# Porting Pokémon Showdown's test suite

This engine's own tests were written against this engine. They pin what it
does, which is exactly the wrong instrument for the question "where does what
it does differ from competitive Pokémon?" Answering that needs tests written
against somebody *else's* implementation, and the community has one:
[smogon/pokemon-showdown](https://github.com/smogon/pokemon-showdown)'s
`test/sim/**`, 1,977 `it(...)` cases across 357 files, which is the closest
thing to an executable specification of the modern game.

`internal/engine/showdown/` is the port of that corpus.

## The pattern, and why the suite is allowed to be red

This follows the route the PostgreSQL-in-Rust rewrites took: bring the upstream
regression corpus over **first, in full**, and let it fail. Do not port only the
cases you expect to pass — that selects for the bugs you already know about,
which is the one category worth nothing.

A red case here is a **question**, not a defect report. It has four possible
answers, and the ledger records which:

| Kind | Meaning | What happens next |
|---|---|---|
| `gapBug` | the engine is wrong | fix the engine — these are the findings |
| `gapMissing` | the mechanic is not implemented at all | a feature request |
| `gapScope` | deliberately not modeled (singles-only, 80 species) | stays as a record of the decision |
| `gapPort` | the translation is suspect | re-translate; should not survive triage |

## Running it

```sh
make test-showdown                       # the whole port, with a tally
make test-showdown-report                # same, plus showdown-report.json
go test -tags showdown ./internal/engine/showdown/ -run TestMovesKnockOff -v
PS_SEEDS=50 make test-showdown           # deeper per-case seed sweep
```

The suite is behind the `showdown` build tag, so `go test ./...` and CI never
compile it. That is not a way of hiding failures: `make test-showdown` is
itself pass/fail, and a case fails the build whenever it disagrees with the
ledger **in either direction**.

## The ledger

`gaps_test.go` holds one row per non-passing case, keyed
`"<upstream describe>: <upstream it>"`. The four-way reconciliation is the
whole point:

```
ledger says pass, case passes    →  quiet
ledger says pass, case fails     →  FAIL — a regression, or an untriaged port
ledger has a gap, case fails     →  quiet, counted, reason reported
ledger has a gap, case passes    →  FAIL — the gap closed; delete the row
```

That last row is what keeps this maintainable. A quarantine list nobody prunes
rots into a list of tests nobody runs. This one prunes itself, because leaving
a stale entry in means the *next* thing to break that case gets reported as
expected.

## What this engine is, and what that costs the port

| | |
|---|---|
| Format | **Singles only.** `Side.Active` is one index. No ally, no spread targeting, no Follow Me, no Ally Switch. |
| Generation | **Gen 9 data** (`data/_provenance.json`: `source_gen: 9`, sim 0.10.9) with no gen-mod layer. Upstream's `common.gen(4).createBattle(...)` blocks have no counterpart. |
| Level | **Fixed at 50** (`damage.go: const Level = 50`). Upstream defaults to 100, so absolute damage figures never transfer — only fractions of max HP, and comparisons. |
| Roster | **80 fully-evolved Kanto species.** Upstream draws on the National Dex. |
| Moves / items / abilities | 538 / 128 / 118. |
| Not modeled at all | Mega Evolution, Z-moves, Dynamax/G-Max, Terastallization, formes and forme changes, Transform/Imposter, weight, multi-battles, team preview as a phase. |

The roster is the constraint that shapes the port most. 77% of the species
mentions upstream name something this dex does not have — and the single
most-used species in the corpus is **Wynaut, 513 mentions**, used purely as a
body for something to happen to. Refusing to substitute would throw away most
of the corpus for a reason that usually has nothing to do with the mechanic
under test.

So ports substitute, through the table in `names_test.go`. Every row states
what the substitution *preserves*, because a stand-in is only safe with respect
to a particular question:

```go
"blissey": {113, "the same species one stage down: normal, Natural Cure and
                  Serene Grace both present, huge HP"},
"skarmory": {82, "Magneton is the only steel body in the dex; flying is lost,
                  so ports turning on Ground immunity must not use this"},
```

**A port whose case turns on something the row does not promise must not use
the stand-in.** Name an in-dex species that does preserve it, or skip. Getting
this wrong produces the worst possible artefact: a green case that measured
something else.

There is deliberately no stand-in for a species whose *identity* is the
mechanic — Ditto in a Transform test, Arceus under Multitype, Shedinja under
Wonder Guard. Those skip.

## Writing a port

One Go file per upstream file, named for where it came from:
`test/sim/moves/knockoff.js` → `moves_knockoff_test.go`, holding exactly one
exported test function named `Test<Category><File>` —
`TestMovesKnockOff`, `TestAbilitiesIntimidate`, `TestItemsLeftovers`,
`TestMiscAccuracy`. Every upstream `describe` in the file becomes a `describe`
block inside that one function, so the name cannot collide with another port's.

Keep the upstream `describe` and `it` strings **verbatim** — they are half the
ledger key and the only way a reader finds the original.

A port file declares nothing at package level except its one test function. No
shared helpers, no constants, no types: three hundred files in one package have
no room for a second `func expectBurn`. Anything a case needs, it builds
inline. And a port never edits `harness_test.go`, `names_test.go` or
`gaps_test.go` — the first two are the shared instrument and the third is
written by triage.

```go
//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/knockoff.js.

func TestMovesKnockOff(t *testing.T) {
	describe(t, "Knock Off", func(g *psg) {
		g.it("should remove most items", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Ability: "synchronize", Moves: mv("knockoff")}},
				team{{Species: "Blissey", Ability: "naturalcure", Item: "shedshell",
					Moves: mv("softboiled")}},
			)
			p.makeChoices("move knockoff", "move softboiled")
			p.equal(p.foe().Item, "", "Shed Shell should have been knocked off")
		})

		g.skip("should not remove plates from Arceus",
			"Arceus is not in this 80-species dex and Multitype is not modeled")
	})
}
```

Next to its original:

```js
it('should remove most items', () => {
    battle = common.createBattle([[
        {species: "Mew", ability: 'synchronize', moves: ['knockoff']},
    ], [
        {species: "Blissey", ability: 'naturalcure', item: 'shedshell', moves: ['softboiled']},
    ]]);
    battle.makeChoices('move knockoff', 'move softboiled');
    assert.equal(battle.p2.active[0].item, '');
});
```

### Vocabulary

| Showdown | Port |
|---|---|
| `describe(name, fn)` | `describe(t, name, func(g *psg){})` |
| `it(name, fn)` | `g.it(name, func(p *ps){})` |
| — | `g.skip(name, reason)` — out of scope, reason is reported |
| — | `g.itRate(name, lo, hi, seeds, fn) bool` — for probabilities |
| — | `g.itSeed(name, seed, reason, fn)` — escape hatch, needs a reason |
| `common.createBattle([[...],[...]])` | `p.battle(team{...}, team{...})` |
| `battle.makeChoices('move x', 'move y')` | `p.makeChoices("move x", "move y")` |
| `battle.makeChoices()` | `p.turn()` |
| `battle.p1.active[0]` / `battle.p2.active[0]` | `p.mine()` / `p.foe()` |
| `battle.p1.pokemon[2]` | `p.slot(0, 3)` — 1-based, like `switch 3` |
| `battle.field.weather` / `.terrain` | `p.weather()` / `p.terrain()` |

Choice strings are Showdown's own grammar: `"move knockoff"`, `"move 1"`,
`"switch gyarados"`, `"switch 3"`, `""`/`"default"` for the first legal action.
`switch <species>` accepts the name the port was written under, so
`"switch blissey"` finds the Chansey standing in for it.

Assertions mirror `test/assert.js`, each taking a trailing message string
(`""` for none): `equal`, `notEqual`, `ok`, `isFalse`, `statStage`, `fullHP`,
`damaged`, `fainted`, `notFainted`, `hasAbility`, `holdsItem`, `noItem`,
`hasStatus`, `noStatus`, `bounded`, `atLeast`, `atMost`, `hurts`, `hurtsBy`,
`constant`, `sets`, `species`, `cantMove`, `canMove`, `trapped`, `notTrapped`,
`logHas`, `logLacks`, `logCount`.

String comparison is normalized, so the right-hand side can be a Showdown id, a
display name, or a slug — `p.equal(mon.Item, "leftovers", "")` and
`p.equal(mon.Item, "Leftovers", "")` are the same assertion.

### Setup shortcuts the original does not have

`set` carries two fields Showdown expresses through play, because doing it
through play puts a damage roll in the setup where it is noise:

```go
team{{Species: "Snorlax", Moves: mv("splash"), HP: 42, Status: "brn"}}
```

`Ability: "noability"` strips the ability, matching upstream's own idiom for a
body that must not interfere. An omitted `Ability` gets the species default,
also matching upstream.

### Three rules that are not obvious

**1. Every case runs over five seeds, and must hold on all of them.** This
engine has no rigged-RNG hook, and `internal/engine/probability_test.go` argues
at length against tests that pick a lucky seed — a seed-picked test pins
splitmix64's output rather than the game's rule, and cannot be ported anywhere.
So the default is stronger than upstream's: the assertion must hold under every
seed. A case that only holds under some is either about a probability (use
`g.itRate`) or is measuring something it did not mean to.

**2. A missing move, item or ability is a finding, not a skip.** "This engine
has no Transform" is precisely what the port exists to enumerate, so
`p.battle` records it as a failure naming the thing. A *species* with no
stand-in is a porting decision and should be `g.skip`ped with the reason, or
substituted.

**3. Log assertions match a fragment, not a sentence.** Upstream matches
protocol lines (`|-ability|p2a: Gyarados|Intimidate|boost`); this engine emits
prose. Match the short mechanical part (`"Intimidate cuts"`), never a whole
sentence — a port that pins the wording is a spelling test that will fail on
the next copy edit.

### Skip categories

Use these words, so the tally groups them:

- `"doubles"` / `"triples"` — no second active slot
- `"gen N mechanics"` — no gen-mod layer
- `"Z-moves"`, `"Dynamax"`, `"mega evolution"`, `"Terastallization"`
- `"formes"` — no forme layer
- `"<Species> is not in this 80-species dex and <X> is not modeled"`
- `"random battles"` / `"team validator"` / `"server"` — different subsystem

## Triage

1. `make test-showdown-report` → `showdown-report.json`, one row per case.
2. Everything with `"status": "regress"` is untriaged. For each, decide which
   of the four kinds it is and add the row to `gaps` in `gaps_test.go`.
3. `gapBug` rows are the output of the whole exercise. They belong in the
   engine's issue list with the upstream case named.
4. A `gapPort` row is a bug in the translation. Fix the port and delete the row
   — none should survive a triage pass.

Re-run. A clean run means every case either passes or is accounted for.
