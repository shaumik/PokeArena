# Engine findings — open work

These came out of a six-team agent tournament run through `cmd/royale`. Each
match had a referee agent whose job was to audit the engine against its own
source while the match ran; between them the referees confirmed five defects
and cleared a good many more suspicions as correct-but-surprising mechanics.

Seven defects are now **fixed** — the original five from the tournament, plus
OPEN-1 and OPEN-4 below, which were closed in a follow-up pass. They are listed
at the bottom so nobody redoes them. What follows first is the work that is
still open: two items, both deliberate deviations that need a product decision
before anyone touches code.

Every item states what was observed, what the source actually does, and what
"done" looks like. Where a fix has a wide blast radius that is called out
explicitly — this repo's headline promise is that every battle replays
bit-for-bit from its seed, and one of these would break that.

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

**Scope correction:** this is an *in-process* leak only. `foeWire` /
`View.MarshalJSON` already drop both fields before anything reaches the wire,
and `internal/ai/itemfog_test.go` (`TestView_FoeItemNeverReachesWire`) pins
that, documenting the in-process asymmetry as deliberate. External MCP agents
never saw either field. The tournament finalist was an in-process agent, so the
observation stands — but the decision below is about what the reference bots
see, not about clients.

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

## Fixed already

Listed so nobody spends a day rediscovering them. All have regression tests.

**The original five** landed on `main` in `70d13d3` (PR #142), which merged the
`claude/agent-battle-royale-o1lq9h` branch. An earlier revision of this file
warned that they were unmerged — that warning is stale, and the three commits
carrying them (`9afa363`, `d1f8a09`, `cc54bd3`) are all ancestors of `main`.

**OPEN-1 and OPEN-4** were closed afterwards; their rows are at the bottom of
the table.

| Bug | Fix |
|---|---|
| Foe-targeted secondaries landed on a target the same hit reduced to 0 HP. `applyDamageEffects` runs inside the faint window `turn.go` documents, where a killed Pokémon still has `Fainted == false` at `HP == 0`; every guard tested the flag, and the secondaries loop had no defender guard at all. Found independently by two referees, and the fourth instance of a bug class the engine's own comment already records. | Guards route through `isDown()`, at the callers and at the sinks (`inflictStatus`, `applyItemStatusCure`). The secondary is rolled and *then* checked, so the RNG stream is unchanged. |
| Facade never doubled off the user's own status — the move appeared nowhere in the engine, so it swung a flat 70 BP with burn's Attack cut applied on top, where canon gives 140 and exempts it from the cut. | `statusDoublingMoves` widened from `func(def)` to `func(atk, def)` and Facade added, keyed on the attacker; burn exemption shared with Guts via `burnHalvesAttack` so the compensating ×2 cannot double-count. |
| Switching a sleeping Pokémon out cured its sleep. `canAct` wakes anything at `SleepTurns <= 0`, so zeroing the counter on the way out meant a pivot refunded the whole status. The test pinning this asserted the contradictory state directly and called it "Gen 5+ semantics". | Reset removed; test rewritten to assert the counter survives; `docs/battle-state.md` corrected. |
| Freeze could be inflicted in harsh sunlight, forbidden since Gen 2. `inflictStatus` consulted ability, ability-state and terrain guards but had no weather counterpart. | Weather guard added, read through `effectiveWeather` so Cloud Nine and Air Lock suppress the immunity along with the sun. |
| Weather-keyed end-of-turn abilities (Solar Power, Dry Skin, Rain Dish, Ice Body, Hydration) missed their tick on the weather's final turn, because `tickWeather` ran before them — while sandstorm's own chip landed on that same turn, because it runs before the countdown. | Countdown moved to after the ability ticks, so one residual phase gives one answer about whether the weather is up. |
| A single successful Protect logged `"X protected itself!"` twice (OPEN-1). `applyProtectMove` announced when the shield went up (`protect.go`, type `status`, side = protector) and `executeMove` announced again when it blocked (`turn.go`, type `protect`, side = `1 - side`) — different types, same rendered sentence, so nothing downstream could dedupe it. | The block site owns the announcement; the set-up site is silent. Raising the shield is already visible as `"X used Protect!"`, so a Protect nothing attacks into still reads. `TestProtectAnnouncesExactlyOncePerBlock` pins one type-`protect` line per blocked attack and none when nothing is blocked. Quick/Wide Guard were checked and never doubled — they only log on set-up (`guards.go:53,66`) and have no block-time line at all. |
| `ToxicCounter` survived a switch-out (OPEN-4), so a benched Pokémon carried an escalating clock it had no way to clear — the tournament Machamp returned on turn 37 still ticking 5/16. Canon has reset the counter on switch-out since Gen 3, and switching is the standard defensive answer to Toxic. | `doSwitchWithCarry` resets it alongside `Stages` and `Volatiles`, keyed on the status so an unpoisoned switch is untouched. Reset value is **1**, not 0 — `applyStatusResidual` reads the counter as the *next* tick's numerator, so 0 would hand the returning Pokémon a free damage-less turn. `docs/battle-state.md` updated; `toxic_switch_test.go` pins both directions. |

### What the OPEN-1 / OPEN-4 pass invalidated

`internal/engine/testdata/fullgame-golden.json` was re-recorded
(`go test ./internal/engine/ -run TestFullGame_MatchesGolden -update-golden`).
55 of its 147 fixtures moved.

An earlier revision of this file said log text is not part of the replay hash.
That is wrong: `fingerprint` in `fullgame_integration_test.go` hashes
`l.Type`, `l.Side` and `l.Text` for every line, so OPEN-1 shifted 40 fixtures
on its own despite having no mechanical effect. Anything cosmetic in the log
costs a golden re-record — worth knowing before calling a log change free.

The remaining drift is OPEN-4, which genuinely changes outcomes. **Still
outstanding:** the win-rate and Elo tables in `docs/benchmark.md` were produced
on the pre-reset engine and have not been re-run.

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
