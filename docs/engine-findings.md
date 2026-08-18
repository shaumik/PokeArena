# Engine findings — closed

These came out of a six-team agent tournament run through `cmd/royale`. Each
match had a referee agent whose job was to audit the engine against its own
source while the match ran; between them the referees confirmed five defects
and cleared a good many more suspicions as correct-but-surprising mechanics.

**Nothing here is open.** The original five landed with the tournament branch;
the four items the referees left behind — two genuine bugs and two deliberate
deviations that needed a product decision — have since been closed too. Both
decisions were taken in favour of canon: the foe's ability and item are now
fog-of-war until an event reveals them, and damage rounds the way Showdown
rounds. This file is kept as the record of what was found and what was decided,
so a future referee does not re-file any of it.

The table below is the whole story. Each row states what was observed and what
was done about it.

## Fixed already

Listed so nobody spends a day rediscovering them. All have regression tests.

**The original five** landed on `main` in `70d13d3` (PR #142), which merged the
`claude/agent-battle-royale-o1lq9h` branch. An earlier revision of this file
warned that they were unmerged — that warning is stale, and the three commits
carrying them (`9afa363`, `d1f8a09`, `cc54bd3`) are all ancestors of `main`.

**OPEN-1 through OPEN-4** were closed afterwards, in two passes; their rows are
at the bottom of the table.

| Bug | Fix |
|---|---|
| Foe-targeted secondaries landed on a target the same hit reduced to 0 HP. `applyDamageEffects` runs inside the faint window `turn.go` documents, where a killed Pokémon still has `Fainted == false` at `HP == 0`; every guard tested the flag, and the secondaries loop had no defender guard at all. Found independently by two referees, and the fourth instance of a bug class the engine's own comment already records. | Guards route through `isDown()`, at the callers and at the sinks (`inflictStatus`, `applyItemStatusCure`). The secondary is rolled and *then* checked, so the RNG stream is unchanged. |
| Facade never doubled off the user's own status — the move appeared nowhere in the engine, so it swung a flat 70 BP with burn's Attack cut applied on top, where canon gives 140 and exempts it from the cut. | `statusDoublingMoves` widened from `func(def)` to `func(atk, def)` and Facade added, keyed on the attacker; burn exemption shared with Guts via `burnHalvesAttack` so the compensating ×2 cannot double-count. |
| Switching a sleeping Pokémon out cured its sleep. `canAct` wakes anything at `SleepTurns <= 0`, so zeroing the counter on the way out meant a pivot refunded the whole status. The test pinning this asserted the contradictory state directly and called it "Gen 5+ semantics". | Reset removed; test rewritten to assert the counter survives; `docs/battle-state.md` corrected. |
| Freeze could be inflicted in harsh sunlight, forbidden since Gen 2. `inflictStatus` consulted ability, ability-state and terrain guards but had no weather counterpart. | Weather guard added, read through `effectiveWeather` so Cloud Nine and Air Lock suppress the immunity along with the sun. |
| Weather-keyed end-of-turn abilities (Solar Power, Dry Skin, Rain Dish, Ice Body, Hydration) missed their tick on the weather's final turn, because `tickWeather` ran before them — while sandstorm's own chip landed on that same turn, because it runs before the countdown. | Countdown moved to after the ability ticks, so one residual phase gives one answer about whether the weather is up. |
| A single successful Protect logged `"X protected itself!"` twice (OPEN-1). `applyProtectMove` announced when the shield went up (`protect.go`, type `status`, side = protector) and `executeMove` announced again when it blocked (`turn.go`, type `protect`, side = `1 - side`) — different types, same rendered sentence, so nothing downstream could dedupe it. | The block site owns the announcement; the set-up site is silent. Raising the shield is already visible as `"X used Protect!"`, so a Protect nothing attacks into still reads. `TestProtectAnnouncesExactlyOncePerBlock` pins one type-`protect` line per blocked attack and none when nothing is blocked. Quick/Wide Guard were checked and never doubled — they only log on set-up (`guards.go:53,66`) and have no block-time line at all. |
| `ToxicCounter` survived a switch-out (OPEN-4), so a benched Pokémon carried an escalating clock it had no way to clear — the tournament Machamp returned on turn 37 still ticking 5/16. Canon has reset the counter on switch-out since Gen 3, and switching is the standard defensive answer to Toxic. | `doSwitchWithCarry` resets it alongside `Stages` and `Volatiles`, keyed on the status so an unpoisoned switch is untouched. Reset value is **1**, not 0 — `applyStatusResidual` reads the counter as the *next* tick's numerator, so 0 would hand the returning Pokémon a free damage-less turn. `docs/battle-state.md` updated; `toxic_switch_test.go` pins both directions. |
| The fog-of-war projection printed the foe's **ability and item in full from turn 0** (OPEN-2). `redactFoeActive` blanked unused moves, bucketed HP and zeroed the sleep, toxic and confusion clocks, but passed both of these through. In the final a contestant read a Heat Rock on turn 0 and re-planned its whole game around eight turns of sun, correctly, before any event had revealed the item. (In-process only — `foeWire` already dropped both from the wire.) | **Decision: reveal on trigger** (the doc's option 2), not "document it" and not "hide outright" — canon reveals both through play, so permanent hiding trades one inaccuracy for another. `Pokemon` carries `AbilityRevealed` / `ItemRevealed`, false at battle start, set where the engine *announces* the ability or item acting, never unset. Silent reads do not reveal. All 67 announcement sites carry a `revealAbility` / `revealItem` call, and `TestEveryAbilityAndItemAnnouncementReveals` scans the source so a new site cannot be added without one. An unrevealed item also hides `choice_lock_move_id`, which names the item on its own. |
| Damage was carried as an unrounded float through every modifier and **floored once at the end** (OPEN-3), where Showdown truncates at each modifier boundary. An Air Slash rolled 86 on a Gengar whose cartridge maximum is 85. | **Decision: match Showdown.** `computeDamage` restructured into Showdown's group order with its 4096-denominator fixed-point rounding at every boundary; terrain moved to the base-power group where canon puts it. Measured against an independent transcription, the old formula disagreed on **54% of rolls** and exceeded the canonical 100% roll on **2.4%** — systematic, signed, and able to cross a KO threshold. `ExpectedDamage` runs the same chain so the AI's model and the engine agree. Remaining gap named in `docs/battle-state.md`: ability/item hooks are a single lumped multiplier, so they all sit in the final group. |

### What these fixes invalidated, and what was redone

**Replay fixtures.** `internal/engine/testdata/fullgame-golden.json` was
re-recorded on each pass (`go test ./internal/engine/ -run
TestFullGame_MatchesGolden -update-golden`): 55 of 147 fixtures on OPEN-1 /
OPEN-4, then all 147 on OPEN-3, since a damage change touches every game, then
twice more for corrections to the rounding chain itself. The repo's bit-for-bit
replay promise holds from here; it does not reach back across these commits.

Both corrections are worth knowing about, because both were off-by-ones in the
fixed-point arithmetic that no behavioural test could have caught:

- the float→4096 conversion truncated where Showdown's published constants are
  rounded (1.3 is 5325, not 5324);
- `applyMod` used a bias of 4095 where Showdown uses 2047 — and the reference
  transcription in `damage_rounding_test.go` had the same slip, so the two
  agreed with each other and the suite stayed green. That is the failure mode a
  transcription test exists to prevent, so the test now spells the arithmetic
  out inline instead of sharing the helper.

**A correction worth keeping.** An earlier revision of this file said log text
is not part of the replay hash. That is wrong: `fingerprint` in
`fullgame_integration_test.go` hashes `l.Type`, `l.Side` and `l.Text` for every
line, so OPEN-1 shifted 40 fixtures on its own despite having no mechanical
effect whatsoever. Nothing cosmetic in the log is free against the golden.

**Benchmark numbers.** Both published tables in `docs/benchmark.md` were
re-derived on the fixed engine and the document updated in the same change.

The expectimax depth sweep moved — every point estimate dropped 5–6 points —
but the result that section actually rests on did not. The d1→d2 drop is 11.4
points on the fixed engine against 11.5 before it: the two runs agree to a
tenth of a point across a change that moved every damage roll in the format.
Deeper search still costs real win rate.

One earlier claim did not survive and was withdrawn rather than quietly
restated: d2 and d3 were described as statistically tied with a stable d1→d3
slope. On the corrected chain they separate (36.7% vs 42.1%) and the slope
halves, so the doc now claims only what the data supports — the sign and the
first step.

The spread-impact table's measurement had never been committed — it was an ad
hoc replay that could not be re-derived after an engine change moved it, which
is precisely the situation Section 8 of that document says must not arise. It
is now `cmd/spread-impact`, and the table is that command's output.

**Agent behaviour.** OPEN-2 changes what every agent sees from turn 0. It does
not touch the engine's RNG stream, so replays stay valid across it — but any
conclusion drawn about *agent skill* from a pre-OPEN-2 run was drawn on agents
that could read the foe's ability and item for free.

One process defect is also worth recording, because it is the kind of mistake
that repeats: the tournament ran on a **stale `bin/royale`**, built before the
first two engine patches, so no match ever exercised them. `bin/` is
gitignored, which makes a stale binary invisible in `git status`. If the
harness is used for anything that matters again, rebuild it as part of
starting a run rather than trusting that it is current.

---

## What the referees cleared

Recorded so these do not get re-filed. All were investigated against source
and found correct: damage figures larger than the target's remaining HP (the
log reports HP actually lost, clamped before printing); Skill Link's Icicle
Spear stopping at three hits (the strike loop breaks on a KO); Focus Sash not
firing (its holder was already off full HP); Cursed Body firing on a fatal
hit; Giga Drain printing no heal line at full HP (zero-amount elision); Hex
correctly *not* doubling on an unstatused target; Static correctly *not*
firing on non-contact moves; two consecutive successful Protects (a legitimate
roll on the 100/33/11/4/1 chain); every Trick Room lasting exactly five turns
with priority −7 still losing its bracket; and the entire weather chain —
Drought on switch-in, Heat Rock's eight turns not retroactively shortened when
Knock Off removed the rock, Solar Beam's charge-skip only in sun, Chlorophyll's
Speed halving the moment the sun expired.
