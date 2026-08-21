# Open work from the second battle royale

Everything the second six-team agent tournament turned up that is *not* already
fixed. The four defects it confirmed landed in `#145`, along with the report and
six new rosters; `docs/engine-findings.md` is the record of those and is closed.
This file is the opposite: the things still outstanding, with enough context to
pick one up cold.

Filed as a document rather than as issues because it is meant to be read top to
bottom once and then referenced by path — the ordering below is itself the
recommendation.

**Where this stands.** Items 1–4 and 6 are done. Items 5, 7 and 8 are open, and
5 and 7 want a decision before any code moves. A second body of work grew out of item 3 and
now matters more than anything left on the list: the test suite was rebuilt to
serve as the specification for a possible port of the engine to another
language. That is Part II, and the rules it sets apply to every new test written
from here on, including the ones items 5–8 will need.

**Three things to know before touching any of this.**

1. `bin/royale` is gitignored, so a stale binary is invisible in `git status` —
   rebuild it (`go build -o bin/royale ./cmd/royale/`) before trusting any
   observed behavior.
2. Log text is part of the golden replay fingerprint (`fingerprint` in
   `fullgame_integration_test.go` hashes `Type`, `Side` and `Text` for every
   line), so nothing cosmetic is free: a reworded line re-records fixtures. The
   first tournament learned this the expensive way — a change with no mechanical
   effect whatsoever shifted 40 fixtures.
3. New tests follow the rules in Part II. The short version: play a real battle,
   never call an unexported function, never pick a seed to make a roll land your
   way, and prove the test fails when you break the thing it covers.

---

# Part I — done

## 1. Harvest — implement it — DONE

Implemented as an end-of-turn hook in `internal/engine/abilities.go`: berry
only, empty slot only, certain in harsh sunlight and a coin flip otherwise. The
regrow reuses the `Pokemon.LastConsumedItem` memory that Recycle already reads,
so it was never blocked on new state — the "needs berry manipulation" note that
kept it in the inert group was wrong.

Covered by rate-measured tests for both weather branches, the has-item refusal,
the berries-only gate, a whole-turn test that eats a Sitrus and gets it back,
and a full-game test that plays it across the corpus seeds
(`TestFullGame_HarvestRegrowsInRealGames`).

## 2. Close the fog-of-war hole in the harness — DONE

Team files carry a `codename` and it is the only identity the other seat is ever
shown — in `team`, in `view`, in the resolved-turn recap, in the winner line,
and inside battle text, because the engine is handed the codename as the side's
trainer. A missing codename falls back to a neutral seat label rather than the
real name, and a codename equal to the team name is refused, so forgetting is
the safe outcome instead of the leaky one.

`snapshot()` records real names, so `log.jsonl` and the report pipeline are
unchanged; `royale log` prints a codename legend for the judge; `digest.py` maps
codenames back to real names for the published report. The six rosters play as
Umber, Cobalt, Saffron, Verdigris, Cinnabar and Indigo.

## 3. Test the broker — DONE

`cmd/royale/royale_test.go` covers every invariant that was listed here, each
driven through the command an agent would actually run: `-why` stripping in both
directions, judge-token refusal on `log` and `report`, no foe bench in `view`,
`act` refusing illegal actions and second submissions, and the turn cap
adjudicating on Pokémon standing then total HP then a draw.

On top of that there is a full end-to-end match — `new` to knockout, including
the replace phase — that keeps one transcript per seat and asserts neither ever
saw the other's name, theme or reasoning. `TestFullMatchDigestsIntoTheReport-
Pipeline` runs the real `royale/digest.py` over a match it just played, which is
the only check in the repo that crosses the language boundary.

## 4. Make `royale validate` refuse a roster built on nothing — DONE

`royale validate` warns on every pick whose ability or item the engine models as
nothing, naming the slug and the reason; `-strict` turns the warnings into a
non-zero exit for an organizer gating a bracket. The answer comes from the
registry itself (`engine.AbilityInertReason` / `engine.ItemInertReason`), and a
test pins the inert list against both the registry group and its documentation,
so it cannot drift the way a hand-kept list would.

It found a defect on its first run. **Own Tempo** was registered with a comment
saying its confusion guard lived "elsewhere" — it did not, nothing in the
package read the slug, and the ability was inert while describing itself as
working. Fixed (`BlocksConfusion`, checked in `applyConfusionVolatile`, silent
like the other status-immunity guards); no golden fixture moved. The registry
audit now also fails the build on any hookless registration that nothing reads,
which is the shape that hid it.

The one warning left on the tournament rosters was The Caltrops' Weezing and its
Neutralizing Gas — the pick that cost that team a Pokémon. Item 6 below
implemented it, so all six rosters now pass `-strict` clean.

That emptied the warning the two `validate` tests were built on, and both went
red. They had quietly been fixtured as "whichever real roster happens to still
be built on nothing", which is a fixture that expires the moment the work
succeeds; they now point at `cmd/royale/testdata/inert-bench.json`, which exists
to be unsound. `TestCaltropsNoLongerWarns` keeps the real roster in the suite
from the other direction: if Neutralizing Gas ever regresses to inert, the team
it harmed is the one that says so.

## 6. Neutralizing Gas — DONE

The one referee-confirmed defect from the second tournament that #145 did not
fix. While the holder is on the field every *other* ability is suppressed; on
switch-out, faint, or a Gastro Acid landed on the holder, they resume.

**How the read path was threaded.** Not by giving `abilityOf` a `*BattleState`
— it has 62 call sites and several of them are hook signatures that never carry
one. Instead suppression is a mirror on the Pokémon, `Volatiles.AbilitySuppressed`,
and `abilityOf` returns nil when it is set. That is one gate on the single
lookup every mechanic already goes through, so all 62 sites went quiet without
any of them changing. It is the shape `Volatiles.MagicRoomHere` /
`syncMagicRoomFlags` already uses for field-wide item suppression, including
the part that makes a mirror defensible: `ValidateStateInvariants` checks it
against the field, so a missing sync is a loud failure rather than a Pokémon
silently playing without its ability.

`syncAbilitySuppression` is the sole writer. It is seeded at battle
construction — a controller asks `LegalActions` before turn 1 resolves, and
"may I switch out of this Arena Trap" is answered from state alone — then
re-derived at the top of each turn, after every switch, after each move
resolves, before the end-of-turn ability ticks, at the turn boundary, and in
the replace phase. Every one of those six is load-bearing; the mutation pass
below removed each in turn and a test went red each time. The narrowest is the
gas holder killed by weather chip at the *top* of the residual block, whose foe
is owed its Magic Guard by the time that same chip reaches it.

**What "resume" turned out to mean.** More than clearing a flag: canon re-runs
the switch-in ability of everything still on the field (Showdown's
`neutralizinggas.onEnd` calls `singleEvent('Start', ...)`), which is why a
Drought holder that entered *into* the gas gets its sun at the moment the gas
clears rather than never. What does not come back is anything already spent —
weather Drought set before the gas arrived stays up, an Intimidate that already
fired does not un-fire. Suppression stops abilities; it does not rewind their
effects. Multiscale and Regenerator genuinely stop and restart, which needs no
special handling once the read is gated.

**Gastro Acid came with it, and is called out here because it is a rider rather
than part of item 6.** Its volatile was already set by the move and read by
nothing: `applyGastroAcidVolatile` printed "its ability was suppressed!" and
then nothing was, with a comment saying suppression "isn't threaded into the
ability hook layer". It is the same question as the gas asked twice, so it now
shares the same gate. Canon's ordering is preserved — Gastro Acid suppresses
anything, Neutralizing Gas included, which is what makes it the answer to a gas
(Showdown's `Pokemon#ignoringAbility` tests the volatile before the gas
exemption).

**The golden fixtures did not move.** The doc predicted they would; that
prediction assumed the corpus plays the ability. It does not — neither
`neutralizing-gas` nor `gastro-acid` appears in `archetype-teams.json`, so with
the suppression flag false everywhere the corpus behaves identically, and all
147 fingerprints verified unchanged. Worth keeping in mind for the next item
that "expects to move fixtures": check the corpus first, it is one query.

**Tested at the battle level**, per Part II: 17 tests in
`abilitysuppression_behavior_test.go`, none of which calls an unexported
function or steers a seed. Every rate-free claim is a paired or swept
comparison against a control on the same fixture, because a Multiscale that
never fires also "does not halve" and a test that cannot tell those apart is
not testing suppression. Verified by mutation: 16 one-line breaks applied to
the suppression code and every sync site, 16 caught. The first pass caught 13
of 16, and the three survivors were each a real gap rather than a scoring
problem — they are what added the switch-into-standing-gas test, the
Magic-Guard-in-time-for-the-sand-chip test, and the two boundary-contract
tests, and they are why a redundant second writer of the mirror was deleted
instead of kept.

Two of the 17 assert `ValidateStateInvariants` rather than a play. That is
deliberate and flagged in the file: the late-residual KO and the
hazard-killed replacement leave a window that every later reader re-derives
past, so what is actually at stake there is the state contract, and no sequence
of actions makes it visible any other way.

**Still short of canon, said rather than pinned.** Ability Shield and the
`cantsuppress` family (As One, Comatose, Disguise, Multitype and the rest) are
not modeled here at all, so nothing is exempt from the gas except Neutralizing
Gas itself. None of those abilities or that item is in the dataset, so this is a
gap that cannot currently be reached, not a wrong answer being given.

---

---

# Part II — the test suite is now the port specification

This started as item 3 and turned into the larger piece of work. It is written
down here because it changes how every future test in this repo should be
written, and because the reasoning is not obvious from reading the tests
themselves.

## Why

The engine may be ported to another language. The plan for that is: translate
the tests first, then write the engine until they pass. Under that plan a test
is only useful if it can be translated — which rules out two kinds that were
common in this suite:

- **Tests that call unexported functions.** `applyOnHit`, `applyVolatile`,
  `executeMove` are *this* engine's decomposition of a turn. A port that
  organizes differently has nothing to call. These do not lie, but they cannot
  be written first, so they cannot drive a port.
- **Tests that pin the random number generator.** "Seed 2 makes the 30% roll
  fire" is true only of splitmix64 seeded this way and drawn from in this order.
  A port has to reproduce the RNG bit-for-bit before such a test can tell it
  whether the mechanic works — and worse, a correct port fails them.

## What was done

**The RNG-coupled tests were found by experiment, not by reading.** Perturb the
generator and run everything; whatever fails is describing the generator rather
than the game:

```go
// in internal/engine/rng.go
func NewRNG(seed uint64) *RNG { return &RNG{state: seed ^ 0x9E3779B9} }
```

```
go test ./internal/engine/ -count=1
```

Sixteen of 662 tests failed. Two are meant to (below); the rest were rewritten
as measured rates over many seeds, with their guards asserted as absolutes over
the same sweep — strictly stronger than the one lucky seed each had. Writing the
evasion case as a rate immediately caught the author assuming Tangled Feet was
another 20% shave when it halves accuracy outright.

Three of the sixteen were over-specified rather than probabilistic, and one was
a genuine trap: the full-game audit flagged `"X is frozen solid!"` — the thaw
check on an already-frozen Pokémon — as a freeze inflicted in harsh sunlight.
Sun forbids freezing; it does not thaw. **A port satisfying that assertion would
have had to implement sun-thaws-freeze**, which is wrong. It never fired under
this generator and fired immediately under another.

**A whole-battle layer was added for everything that only unit tests reached.**
Seven `*_behavior_test.go` files, 110 tests, written in parallel and each
verified by breaking the production code it covers — 132 mutations, 131 caught,
the one miss documented where it sits. Two of the agents found their own tests
too weak this way (`cost := p.HP/4` is indistinguishable from `MaxHP/4` at full
HP; a Spite test passed while draining the wrong slot) and strengthened them.

| | before | after |
|---|---|---|
| engine statements covered by tests that play real battles | 68.0% | **82.6%** |
| mechanics reachable *only* through internals-calling tests | 72 | **0** |
| whole suite coverage | 87.6% | 89.5% |

`internal/engine/behavior_helpers_test.go` holds the shared vocabulary —
`neutralBattle`, `speciesBattle`, `teachMoves`, `moveAt`, `switchTo`,
`playTurn`. `probability_test.go` holds the rate helpers and repeats the
perturbation recipe above.

## Rules for new tests

1. **Play a real battle.** `NewBattle` / `NewBattleFromPicks`, then
   `ResolveTurn` / `ResolveReplace`. Arranging a position by writing exported
   fields is fixture setup and fine; calling an unexported function is not.
2. **Never steer a roll with a seed.** Measure the rate with `assertRate` over
   many seeds, and assert every guard with `assertNever` / `assertAlways`.
3. **State the rule in the comment**, in plain language, and say why it matters
   — ideally naming the bug it guards. These comments are what a porter reads
   before writing any code.
4. **Prove the test fails.** Break the code it covers, watch it go red, restore.
   A test that cannot fail is worse than no test, because it reads as coverage.
5. **Say so when the engine is short of canon** rather than pinning the gap as
   if it were the rule. The gimmicks tests do this for Grudge's PP drain and
   Gastro Acid's suppression; `resolveOHKOImmunity` has no level term at all and
   the test says so instead of inventing one.

Unit tests that call internals are still welcome beside these — they are faster
and land closer to a bug. What must not happen again is a mechanic that *only*
they cover.

## Two tests pin the RNG on purpose

`TestFullGame_MatchesGolden` (147 fingerprints over 21 pairings × 7 seeds) and
`TestEffectSporeImmunityCheckSitsAfterBothRolls` (which counts splitmix64 draws
by stepping the state) both fail under the perturbation above, deliberately.
They are the replay-parity contract, not game rules. A port should satisfy them
*last*, after the behavior tests pass — do not "fix" them and do not chase them
early.

## What to do going forward

**For the engine, whichever item you pick up next.** Any new mechanic needs a
whole-battle test, not only a unit test — that is what keeps the 82.6% from
sliding back. Re-run the perturbation audit after any change that touches rolls
or draw order; anything newly failing is either a real regression or a test that
learned to describe the generator.

**One function is still specified only by internals-calling tests**: `Clone`,
a state deep-copy rather than a game rule, which is why it was left. If that
count grows, it is a signal. The split is measurable — run the suite once whole
and once restricted to the behavior-level tests, then diff `go tool cover
-func` per function. As of this commit that reads 82.6% against 89.5% over 254
behavior-level tests.

**For a port specifically, in this order.**

1. Translate the behavior layer and TDD the engine against it. That is 82.6% of
   the statements, and it needs no RNG parity — a port can use any fair
   generator and still pass.
2. Adopt the RNG contract: splitmix64, single `uint64` of state, seeded plainly
   (`internal/engine/rng.go` is 60 lines). Draw *order* matters as much as the
   generator.
3. Then chase transcript parity. The 147 golden fingerprints are already a
   differential oracle, but they are hashes — they say *that* you diverged, not
   where. Exporting the same pre-image as a transcript instead of hashing it
   makes it a line-by-line diff; it measures 1.4 MB for the whole corpus
   (147 games, 42,641 lines). Worth doing at the point someone actually starts a
   port, not before.
4. Keep a differential harness beyond the fixtures. A battle is a pure function
   of (teams, seed, actions), so both implementations can be run over the same
   random inputs and compared — that finds what a fixed corpus cannot.

---

# Part III — still open

With item 6 done, nothing left here is both unblocked and clearly worth doing.
Item 5 is the largest and wants a decision because it moves published numbers;
item 7 is recommended against except for one third of it; item 8 is blocked on
plumbing that is only worth laying for some other reason. If item 5 is approved,
do it with the item 7 presentation fixes folded in — one fixture re-record
covers all of them, and that is the only reason to touch item 7 at all.

## 5. Damage-model grouping — a decision, then one commit

Two referees independently noted the same approximation, and `damage.go:329-337`
already documents it openly — the comment even names it a "fidelity gap worth
naming": ability and item hooks are applied as a single
lumped multiplier in the final group, so **Sheer Force** behaves as a final
damage multiplier rather than a base-power modifier, and type-boost items
(**Charcoal**, **Poison Barb**) sit in the final group where canon puts them in
the base-power group.

Both were cleared as NOT-A-BUG because the engine states the gap rather than
hiding it. But it is the last named gap in the Showdown-rounding work, and
unlike everything else in this file it changes real damage numbers.

**If you do it:** do both in one commit, because both re-record all 147 golden
fixtures (`go test ./internal/engine/ -run TestFullGame_MatchesGolden
-update-golden`), and re-derive the published tables in `docs/benchmark.md` in
the same change — there is precedent for exactly this in the rounding work, and
a note in `engine-findings.md` about why measurements that cannot be re-derived
are not allowed to sit in that document.

**Wants a decision first.** It is the only open item that changes published
numbers.

## 7. Cosmetic log fidelity — probably not worth it

Recorded so nobody re-files them, with a recommendation against acting:

- `"It's super effective!"` prints *after* the damage line; Showdown emits it
  before (sf1 referee).
- Drought's line is generic — `"X's ability set the weather!"`
  (`abilities.go:1369`) — and does not name the ability (r1m3 referee).
- Effect Spore splits its three outcomes uniformly inside the 30% trigger;
  canon is 9/10/11% (r1m3 referee, deliberate and documented at the call site).

The first two are pure presentation and each re-records 147 fixtures. If they
are ever done, bundle them with item 5 so one fixture re-record covers
everything. The third is trivially correctable and genuinely canon-inexact, so
it is the only one of the three with a real argument for doing — and note it is
a *rate* change, so it wants a rate-measured test, not a seeded one.

## 8. Forewarn

Inert, and blocked on threading the dex into `OnSwitchIn` so it can rank the
foe's moves by power. Low value on its own; worth doing only if that plumbing is
wanted for something else. `royale validate` warns on any roster that brings it,
so it can no longer be built on by accident.

---

## Not in scope, deliberately

`illuminate`, `run-away` and `healer` are inert **by design** in a
trainer-versus-trainer singles battle — wild-encounter rates, fleeing, and
healing an ally that does not exist. They are correctly registered and
documented as such. No action.
