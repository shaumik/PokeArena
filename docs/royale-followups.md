# Open work from the second battle royale

Everything the second six-team agent tournament turned up that is *not* already
fixed. The four defects it confirmed landed in `#145`, along with the report and
six new rosters; `docs/engine-findings.md` is the record of those and is closed.
This file is the opposite: the things still outstanding, with enough context to
pick one up cold.

Filed as a document rather than as issues because it is meant to be read top to
bottom once and then referenced by path — the ordering below is itself the
recommendation.

**Where this stands.** Items 1–4 and 6 are done. Item 5 is in progress: its
audit has been re-derived against upstream and confirmed at forty misplaced
modifiers, one of its two side-findings has been withdrawn as wrong, and the
base-power half has landed. Items 7 and 8 are open, and 9–11 were filed while
doing 5. Item 7 still wants a decision.

A second body of work grew out of item 3 and now matters more than anything
left on the list: the test suite was rebuilt to
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

With item 6 done, item 5 is the only substantial engine work left — and it is
substantially larger than this document previously said. It was written up as
two misplaced mechanics; auditing against Showdown's source found **forty**.
The rewrite below is a handoff: the full list, how to re-derive it, and the
order to do it in. Item 7 is recommended against except for one third of it;
item 8 is blocked on plumbing only worth laying for some other reason.

## 5. Damage-model grouping — 40 modifiers in the wrong group

**This item was under-scoped.** It was filed as two mechanics (Sheer Force and
the type-boost items) because those are the two the referees happened to name.
Auditing every damage modifier the engine models against Showdown's own source
found that **40 of the 48 are in the wrong group**. It is one bug with forty
instances, not two bugs.

### What is actually wrong

Showdown applies damage modifiers in **three** groups, and the group decides
where the number gets truncated:

1. **Base-power group** — `runEvent('BasePower')` chains every `onBasePower`
   handler and applies it to base power *before* the damage formula.
2. **Attack/defense stat group** — `onModifyAtk` / `onModifySpA` /
   `onModifyDef` / `onModifySpD`, applied to the stat the formula reads.
3. **Final group** — `runEvent('ModifyDamage')`, after weather, crit, the
   random roll, STAB and type effectiveness.

This engine has all three groups already (`damage.go`): terrain sits in the
base-power group at `bp := applyMod(power, toMod(tmult))`, and items reach the
stat group through `StatMult` via `itemStatMult` in `offensiveDefensiveStats`.
The problem is that abilities and items expose their damage influence as a
single lumped multiplier — `OutgoingDamageMult` / `IncomingDamageMult` — and
every one of those lands in the final group regardless of where canon puts it.

Two things follow from being in the wrong group, and they are why this changes
real numbers rather than just reading wrong:

- **The `+2` gets boosted.** The final group multiplies the finished figure,
  which includes the `+2` constant from `base + 2`. Canon adds it after the
  base-power boost, so it is never scaled. Everything misplaced this way is
  systematically a little high.
- **Base power never truncates to an integer.** Canon rounds base power to a
  whole number before it enters the formula. Here the boost rides all the way
  through STAB and type effectiveness before a single rounding, so the error
  compounds with effectiveness rather than staying flat.

The arithmetic itself is already correct — `toMod` / `chainMod` / `applyMod`
(`damage.go:602-639`) reproduce Showdown's 4096 fixed point and its rounding.
Nothing about this item is about the maths. It is entirely about which group.

### The full list

Audited 48 modeled modifiers. Correct: 8. Misplaced: 40.

**Belongs in the base-power group, currently final — 29.**
None of these appears on any corpus team, so this half moves **zero** golden
fixtures.

| | |
|---|---|
| abilities (8) | `rivalry`, `technician`, `reckless`, `iron-fist`, `analytic`, `sheer-force`, `sand-force`, `dry-skin` |
| items (3) | `muscle-band`, `wise-glasses`, `punching-glove` |
| type boosters (18) | `silk-scarf`, `charcoal`, `mystic-water`, `magnet`, `miracle-seed`, `never-melt-ice`, `black-belt`, `poison-barb`, `soft-sand`, `sharp-beak`, `twisted-spoon`, `silver-powder`, `hard-stone`, `spell-tag`, `dragon-fang`, `black-glasses`, `metal-coat`, `fairy-feather` |

`dry-skin` is the defender-side one: canon hooks it as `onSourceBasePower`
(the ×1.25 it takes from Fire), not as incoming damage.

**Belongs in the attack/defense stat group, currently final — 11.**
Five of these *are* on corpus teams, so this half **will** move fixtures and
the published tables.

| | |
|---|---|
| abilities (9) | `flash-fire`, `hustle`, `solar-power`\*, `blaze`, `torrent`, `overgrow`, `swarm`, `guts`\*, `thick-fat`\* |
| items (2) | `choice-band`\*, `choice-specs`\* |

\* on a corpus team. `thick-fat` is defender-side: canon lowers the
*attacker's* Atk/SpA (`onSourceModifyAtk` / `onSourceModifySpA`) rather than
reducing damage, which is not the same number once truncation is involved.

**Correctly placed — 8.** Leave these alone: `tinted-lens`, `filter`,
`multiscale`, `expert-belt`, `life-orb`, `metronome` are genuinely
`onModifyDamage`; `thick-club` and `assault-vest` already use `StatMult` and
are already in the stat group.

### Two findings that are not about grouping

- **~~Technician's threshold is wrong independently of its group.~~ Withdrawn —
  it was right all along, and this entry had the priority ordering backwards.**
  The original claim was that canon reads the base power *after* earlier
  modifiers, from `const basePowerAfterMultiplier = this.modify(basePower,
  this.event.modifier); if (basePowerAfterMultiplier <= 60)`, so this engine's
  raw `m.Power <= 60` was half a bug.

  It is not. `Battle.comparePriority` sorts handlers **priority high to low**,
  and Technician's `onBasePowerPriority: 30` is the highest `onBasePower`
  priority in the entire gen-9 dataset. Nothing runs before it, so
  `this.event.modifier` is still 1 and `modify(bp, 1)` is `bp` — the line reads
  the raw base power. Upstream's own `test/sim/abilities/technician.js` pins
  both sides of this: it refuses the boost after a **gen-7** Battery (22) and
  grants it after a **gen-9** Steely Spirit (22), because `data/mods/gen7/
  abilities.ts` overrides Technician's priority down to 19. The
  `basePowerAfterMultiplier` line exists to make the shared implementation
  correct under that mod, not to describe gen 9.

  Raw does still mean *post-`basePowerCallback`* — Rage Fist, Trump Card and the
  rest — which is what `m.Power` on the working copy already is, and *pre-Charge*,
  since Charge is a priority-9 handler. Both were already right. **No change was
  needed and none was made**; the reasoning is now recorded at technician's
  registry entry so it does not get "fixed" later.
- **Chain order inside a group is observable.** `chainMod` rounds at each
  pairing, so composing three modifiers in a different order can differ by a
  point. Showdown orders handlers by `onBasePowerPriority`, highest first. If
  more than one modifier can apply to the same hit, the order needs to match —
  `basePowerMod` in `damage.go` now carries canon's priorities as named
  constants and sorts on them.

### Re-derived, 2026-08 — the table above is confirmed

The procedure below was run again before any code moved. The list is
**unchanged**: 48 modeled modifiers, 29 base-power, 11 stat, 8 already correct.
Two things worth recording from the re-run:

- The engine models 66 damage-influencing registry entries, not 48. The other
  18 are the resist berries, which are `onSourceModifyDamage` upstream and
  therefore correctly final; the audit counted them as one line of prose rather
  than eighteen rows. If a later pass reads "58 of 66", that is this same
  finding with the berries counted in and nothing new.
- **Several canon modifiers are not the decimal they look like.** Muscle Band
  and Wise Glasses are `[4505, 4096]`, and `toMod(1.1)` gives 4506. Reckless and
  Iron Fist are `[4915, 4096]`, the type boosters' 1.19995 rather than 1.2.
  Writing the decimal costs a point of damage often enough to matter, so the
  moved handlers now spell the numerator (`mod4096`). Two modifiers **left in
  the final group have the same defect** and are filed as item 9 below.

### How to re-derive this list

Do not trust the table above; it will drift as the registry grows. Regenerate
it. This is how it was produced:

```sh
git clone --depth 1 --filter=blob:none --sparse \
  https://github.com/smogon/pokemon-showdown.git /tmp/sd
cd /tmp/sd && git sparse-checkout set data sim
```

Then, for each slug this engine models, find its entry in `data/abilities.ts`
or `data/items.ts` and read which hook it registers. Split the files on
`^\t([a-z0-9]+): \{$` and take each entry up to the *next* match — slicing a
fixed number of characters instead bleeds into the following entry and
misreports the hooks, which it did on the first attempt here. Then:
`on*BasePower` → base-power group, `on*Modify{Atk,SpA,Def,SpD}` → stat group,
`on*ModifyDamage` → final group.

### Do this test-first

Every one of these changes a damage number, and there is currently **no test
that would fail if the grouping were wrong** — which is exactly why forty of
them sat here unnoticed. So the tests come first, and they have to be seen
failing before any production code moves. In order:

1. **Write the failing tests first.** Pick a concrete matchup per group,
   compute the canonical damage by hand from Showdown's formula, and assert the
   exact number. `damage_rounding_test.go` is the precedent and the shape to
   copy — it pins exact figures for exactly this reason.
2. **Watch them fail, and check the failure says what you expect.** A test that
   fails by 1 HP when you predicted 3 is telling you the hand-computed figure is
   wrong, not that the engine is worse than you thought. Resolve that before
   moving on: the whole value of this pass is that the expected numbers are
   independently derived rather than recorded from the engine.
3. **Never record the expected value from current output.** That is the one
   failure mode that would make this entire item pointless — it would pin the
   bug as the specification.
4. Only then move the mechanic to its group, and watch the test go green.
5. Re-run the perturbation audit from Part II. These changes do not touch draw
   order, so nothing new should fail; anything that does is a real finding.
6. A whole-battle test as well as the damage-number test, per Part II — the
   number proves the formula, a played turn proves the wiring reaches it.

### Order to do it in

**Do the base-power half first, as its own commit.** 29 of the 40, no corpus
team touches any of them, so it re-records **zero** golden fixtures and needs
no benchmark re-derivation. That makes it a large but low-risk diff whose
correctness rests entirely on the new tests. Fold the Technician threshold fix
into it.

**Then the stat half, as a second commit.** Only 11, but `choice-band`,
`choice-specs`, `guts`, `solar-power` and `thick-fat` are all on corpus teams,
so this one *does* re-record the 147 fixtures
(`go test ./internal/engine/ -run TestFullGame_MatchesGolden -update-golden`)
and *does* require re-deriving the published tables in `docs/benchmark.md` in
the same change — there is precedent in the rounding work, and a note in
`engine-findings.md` about why measurements that cannot be re-derived are not
allowed to sit in that document. Say in the commit message which games moved
and why.

Splitting it this way means the risky commit is 11 changes with the grouping
mechanism already proven by the first one, rather than 40 changes and a fixture
re-record landing together.

### Plumbing that already exists

- Base-power group: `damage.go:243`, `bp := applyMod(power, toMod(tmult))`.
  Terrain is already there. Needs ability and item base-power hooks chained in
  beside `tmult`.
- Stat group: `damage.go:423-424`, `itemStatMult(atk, offSlug)`. Items already
  reach it. **Abilities have no equivalent** — that hook is the one genuinely
  new piece of interface this item needs.
- Rounding: `toMod` / `chainMod` / `applyMod` are correct and should not be
  touched.

### A note on measuring this

Do not reach for "how much does the corpus change" as evidence that this matters.
It was proposed here and it is the wrong instrument: the corpus does not contain
a single base-power-group mechanic, so that half measures 0.00 and the number
says nothing about whether the fix is needed. The engine is wrong against canon;
that is the whole argument. Measure the corpus only to know what the fixture
re-record will cost, which is a scheduling question and not a correctness one.

## 7. Cosmetic log fidelity — probably not worth it

Recorded so nobody re-files them, with a recommendation against acting:

- `"It's super effective!"` prints *after* the damage line; Showdown emits it
  before (sf1 referee).
- Drought's line is generic — `"X's ability set the weather!"`
  (`abilities.go:1369`) — and does not name the ability (r1m3 referee).
- Effect Spore splits its three outcomes uniformly inside the 30% trigger;
  canon is 9/10/11% (r1m3 referee, deliberate and documented at the call site).

The first two are pure presentation and each re-records 147 fixtures. If they
are ever done, bundle them with item 5's *second* commit — the stat-group half
— which is the one that re-records fixtures anyway. Not the first commit: that
one moves zero fixtures, and folding a log change into it would throw away its
best property. The third is trivially correctable and genuinely canon-inexact, so
it is the only one of the three with a real argument for doing — and note it is
a *rate* change, so it wants a rate-measured test, not a seeded one.

## 8. Forewarn

Inert, and blocked on threading the dex into `OnSwitchIn` so it can rank the
foe's moves by power. Low value on its own; worth doing only if that plumbing is
wanted for something else. `royale validate` warns on any roster that brings it,
so it can no longer be built on by accident.

## 9. Two final-group modifiers carry the wrong numerator

Found while re-deriving item 5, and *not* a grouping bug — both of these are in
the right group. They are written as decimals where upstream writes a fraction
over 4096, and `toMod` rounds the decimal to a different numerator:

| | canon | `toMod(decimal)` |
|---|---|---|
| Life Orb | `[5324, 4096]` | `toMod(1.3)` = 5325 |
| Metronome, 3rd repeat | `[6553, 4096]` | `toMod(1.6)` = 6554 |
| Metronome, 4th repeat | `[7372, 4096]` | `toMod(1.8)` = 7373 |

Metronome's other three steps and every other final-group modifier already
agree. The fix is to spell the numerators (`mod4096`, `damage.go`) and to carry
Metronome's `dmgMod` table verbatim instead of computing `1 + 0.2n`.

Small, but it re-records the 147 golden fixtures — Life Orb is on corpus teams —
so it wants its own commit rather than riding along with item 5's, which needs
its own fixture movement attributable to grouping alone.

## 10. Reckless does not boost crash-damage moves

Canon is `if (move.recoil || move.hasCrashDamage)`. This engine tests recoil
only (`m.Self != nil && m.Self.Recoil > 0`), and High Jump Kick and Jump Kick
are both in the dataset carrying `hasCrashDamage` rather than recoil. So a
Reckless user gets nothing on either of them where canon gives ×1.19995.

Needs a `crash` flag or equivalent through `cmd/data-sync` — the field is not in
the transform's list today, the same omission that cost Sonic Boom its `damage`
field. Not a grouping bug; noticed while moving Reckless into the base-power
group and deliberately left alone there.

## 11. Four tests drifted back into pinning the RNG

Part II's perturbation audit (`state: seed ^ 0x9E3779B9`) leaves two tests
failing on purpose. It now leaves six:

- `TestBelchNeedsABerryFirst`
- `TestNaturePowerBreaksAFocusPunch`
- `TestSimpleBeamBattleDoublesTheTargetsLaterBoosts`
- `TestSuperFangHalvesCurrentHP`

All four arrived with the move-coverage pass (#154), after the audit that
cleared the other sixteen, and all four are steering a roll with a seed rather
than measuring a rate. Confirmed as pre-existing by running the perturbation
against `main`; item 5 introduced none of them. Rewrite them the way
`probability_test.go` documents.

---

## Not in scope, deliberately

`illuminate`, `run-away` and `healer` are inert **by design** in a
trainer-versus-trainer singles battle — wild-encounter rates, fleeing, and
healing an ally that does not exist. They are correctly registered and
documented as such. No action.
