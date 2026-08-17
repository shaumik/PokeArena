# Engine findings — open work

These came out of a six-team agent tournament run through `cmd/royale`. Each
match had a referee agent whose job was to audit the engine against its own
source while the match ran; between them the referees confirmed five defects
and cleared a good many more suspicions as correct-but-surprising mechanics.

The five confirmed defects are **already fixed** — they are listed at the
bottom so nobody redoes them. What follows first is the work that is still
open. Two of the four are genuine bugs; two are deliberate deviations that
need a product decision before anyone touches code.

Every item states what was observed, what the source actually does, and what
"done" looks like. Where a fix has a wide blast radius that is called out
explicitly — this repo's headline promise is that every battle replays
bit-for-bit from its seed, and two of these would break that.

---

## OPEN-1 — `"X protected itself!"` is logged twice per Protect

**Severity:** cosmetic. No mechanical effect.
**Found by:** r1m1 referee.

### Observed

A single successful Protect emits the line twice in the battle log. Players
see it duplicated in the client; it also doubles in any transcript built off
`LogLine`.

### Root cause

Two independent sites format the same message:

- `internal/engine/protect.go:48`
- `internal/engine/turn.go:702`

Both fire on the same successful block. Neither is dead code — they sit on
different paths through move resolution — so this is not a simple delete.

### Done looks like

One line per blocked move. Establish which of the two sites owns the
announcement (the guard-application site is the more natural owner), make the
other silent, and add a test asserting exactly one `LogLine` of type
`protect` per blocked attack. Check the interaction with Quick Guard and Wide
Guard while you are in there — they route through the same code and may share
the duplication.

### Risk

Low. Log text is not part of the replay hash.

---

## OPEN-2 — the fog-of-war projection reveals the foe's ability and item

**Severity:** medium. This is a competitive-integrity question, not a crash.
**Found by:** the final's referee.

### Observed

In the final, one contestant identified a Heat Rock on turn 0 and re-planned
its whole game around eight turns of sun instead of five — correctly, because
the item was simply printed to it. Neither an ability nor an item had been
revealed by any in-battle event at that point.

### Root cause

`redactFoeActive` in `internal/ai/agent.go:127` is careful about most hidden
state — it blanks unused moves, buckets HP to 5% granularity, zeroes
`SleepTurns` and `ToxicCounter`, and hides the confusion turn count — but it
passes `Ability` and `Item` through untouched. So `View.Foe` carries both in
full from the first turn.

### The decision to make first

This is arguably intended: the browser client renders these, and a human
player would expect the UI to show what it shows. But it is a large amount of
free information, and it is inconsistent with the same function's care over
HP and move PP.

Three defensible options, in increasing order of work:

1. **Leave it, and document it.** Say plainly in `docs/battle-state.md` that
   ability and item are public from turn 0. Cheapest, and honest.
2. **Reveal on trigger.** Hide both until an event would have revealed them —
   an ability firing, an item being consumed, Knock Off, Trick. Closest to
   canon, and the most work: it needs a per-Pokémon "revealed" set carried in
   battle state and threaded through the projection.
3. **Hide outright.** Smallest diff, worst fidelity — canon does reveal these
   through play, and hiding them permanently is its own inaccuracy.

Recommendation: option 2 if the arena is meant to be a serious competitive
surface, option 1 if not. Do not ship option 3.

### Done looks like

Whichever option is chosen, `TestMakeView_RedactsFoeFog` grows a case pinning
the decision, and `docs/battle-state.md` states the rule.

### Risk

Option 2 changes what every agent sees and will change outcomes. It does not
change the engine's own RNG stream, so existing replays stay valid — only
agent behaviour shifts.

---

## OPEN-3 — damage floors once at the end, where Showdown rounds at each step

**Severity:** low, but it is a permanent ceiling on fidelity.
**Found by:** r1m2 referee. Filed as NOT-A-BUG-by-design, raised for awareness.

### Observed

Air Slash rolled 86 on a Gengar whose cartridge maximum is 85. Rolls can
legitimately sit one or two points above the canonical maximum.

### Root cause

`internal/engine/damage.go:277` carries base damage as an unrounded float
through every modifier and applies a single `math.Floor` at the end:

```go
dmg := int(math.Floor(base * stab * eff * critMult * randMult * wmult * tmult *
    smult * abilDef * abilAtk * itemAtk * itemDef))
```

Showdown instead applies a chain of intermediate floors and `pokeRound` calls
between modifier groups. The difference is small per hit but it is systematic,
and it can cross a KO threshold.

The current behaviour is deliberate and documented in the function comment.
It is internally consistent — the engine is not wrong against itself, only
against the cartridge.

### Done looks like

Only attempt this as a deliberate, scoped project. It means restructuring
`computeDamage` into Showdown's modifier-group order with the correct rounding
at each boundary, then regenerating every fixture.

### Risk — read before starting

**High blast radius.** This changes damage numbers, which changes battle
outcomes, which invalidates:

- every stored replay and turn log,
- the engine's regression fixtures,
- the published benchmark numbers in `runs/` and `docs/benchmark.md`.

The repo's core promise is bit-for-bit replay from a seed. Breaking it is
acceptable for a fidelity fix of this kind, but it has to be a version bump
with the benchmark re-run and the docs updated in the same change, not a
quiet patch.

---

## OPEN-4 — `ToxicCounter` is not reset when a Pokémon switches out

**Severity:** medium. A real divergence from canon, currently by design.
**Found by:** r1m3 referee. Filed as NOT-A-BUG (documented), escalated anyway.

### Observed

A Machamp was badly poisoned on turn 9, ticked up to 5, switched out on turn
13, sat on the bench for 24 turns, and returned on turn 37 still on a 5/16
clock — a 61-point tick against 31 HP. It got exactly one action for the rest
of the match. A Snorlax died the same way, on a bench clock it could not
reset.

### Root cause

`doSwitchWithCarry` (`internal/engine/switching.go:35`) resets `Stages` and
`Volatiles` on the outgoing Pokémon but deliberately leaves `ToxicCounter`
alone. Every other writer to that field is accounted for — `faint`,
`clearStatus`, Healing Wish, and the initial set to 1 — and none is a switch.

`docs/battle-state.md` states the rule outright, so the engine is behaving as
designed and documented.

### The decision to make

From Gen 3 onward, canon resets the Toxic counter when the badly-poisoned
Pokémon leaves the field; switching out is the standard way to reset the
clock to 1/16. The engine's deviation makes Toxic strictly stronger and
removes a real defensive option.

It was load-bearing in at least one tournament match. Both pilots played
around it correctly, so it created no unfairness there — but it is a genuine
strategic difference from the game being modelled, and it should be a choice
rather than an accident.

### Done looks like

Either reset `ToxicCounter` in `doSwitchWithCarry` alongside `Stages` and
`Volatiles`, with a test pinning it, and update `docs/battle-state.md` — or
keep it and record in the docs *why* the deviation is wanted, so the next
referee does not re-file it.

### Risk

Changing it alters outcomes and invalidates stored replays, same as OPEN-3
but much smaller in scope. It does not disturb the RNG stream.

---

## Fixed already — do not redo

Listed so nobody spends a day rediscovering them. All five have regression
tests.

| Bug | Fix |
|---|---|
| Foe-targeted secondaries landed on a target the same hit reduced to 0 HP. `applyDamageEffects` runs inside the faint window `turn.go` documents, where a killed Pokémon still has `Fainted == false` at `HP == 0`; every guard tested the flag, and the secondaries loop had no defender guard at all. Found independently by two referees, and the fourth instance of a bug class the engine's own comment already records. | Guards route through `isDown()`, at the callers and at the sinks (`inflictStatus`, `applyItemStatusCure`). The secondary is rolled and *then* checked, so the RNG stream is unchanged. |
| Facade never doubled off the user's own status — the move appeared nowhere in the engine, so it swung a flat 70 BP with burn's Attack cut applied on top, where canon gives 140 and exempts it from the cut. | `statusDoublingMoves` widened from `func(def)` to `func(atk, def)` and Facade added, keyed on the attacker; burn exemption shared with Guts via `burnHalvesAttack` so the compensating ×2 cannot double-count. |
| Switching a sleeping Pokémon out cured its sleep. `canAct` wakes anything at `SleepTurns <= 0`, so zeroing the counter on the way out meant a pivot refunded the whole status. The test pinning this asserted the contradictory state directly and called it "Gen 5+ semantics". | Reset removed; test rewritten to assert the counter survives; `docs/battle-state.md` corrected. |
| Freeze could be inflicted in harsh sunlight, forbidden since Gen 2. `inflictStatus` consulted ability, ability-state and terrain guards but had no weather counterpart. | Weather guard added, read through `effectiveWeather` so Cloud Nine and Air Lock suppress the immunity along with the sun. |
| Weather-keyed end-of-turn abilities (Solar Power, Dry Skin, Rain Dish, Ice Body, Hydration) missed their tick on the weather's final turn, because `tickWeather` ran before them — while sandstorm's own chip landed on that same turn, because it runs before the countdown. | Countdown moved to after the ability ticks, so one residual phase gives one answer about whether the weather is up. |

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
