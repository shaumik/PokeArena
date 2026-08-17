# sf1 — Solaris vs The Apothecary (Semifinal)

*(match in flight — verdict, story, scorecard, MVP and notable turns pending; the
engine audit below is complete as far as the turns resolved so far.)*

## Verdict

## The story

## Scorecard

## MVP

## Notable turns

## Fix verification

**The faint-window patch (`9afa363`) held. Nothing downstream of it regressed.**

I probed each branch the patch touches and each neighbour it could plausibly have
broken, over 120–400 seeds apiece, on top of watching live play:

| Path | Result | Verdict |
|---|---|---|
| Foe secondary on a target the same hit killed | regression test `TestSecondaryDoesNotLandOnDyingTarget` passes; 0/400 | fixed |
| Poison Touch on a lethal contact hit | corpse poisoned **0/200** | fixed, and the ability-side twin of the same bug is clean |
| Drain move (Giga Drain) landing the KO | attacker healed **120/120**, victim left alive **0/120** | correct — canon drains off the damage dealt even on a KO; the guard did not suppress it |
| Recoil (Flare Blitz) on a lethal hit | attacker took recoil **120/120** | unaffected |
| Rocky Helmet vs a lethal contact hit | attacker chipped **120/120** | unaffected |
| Self-targeted secondary (Close Combat's own −1/−1) on a lethal hit | applied **120/120** | unaffected — the deleted `sec.Self && atk.Fainted` guard did not over-fire |
| Contact rider on a holder the hit killed (Static) | fired **93/300 ≈ 31%** | correct — canon runs `DamagingHit` before `faintMessages`; the patch did not over-suppress |

Live corroboration, turn 8: Ninetales' +2 Fire Blast took Golbat from 182/182 to 0
and Golbat's **Sitrus Berry did not fire**. `applyItemHPTrigger`
(`internal/engine/items.go:626`) guards on `p.Fainted || p.HP <= 0`, so the
"heal berry resurrects a corpse" family the patch belongs to is closed at that
site too.

**One hardening note, not a live defect.** The patch guarded the *callers* in
`applyDamageEffects`, not the sink. `inflictStatus` (`effects.go:511`) still
opens `if p.Status != StatusNone || p.Fainted` and `applyItemStatusCure`
(`items.go:677`) still opens `if p == nil || p.Fainted` — both read the flag, not
the HP, and both sit inside the faint window whenever a contact rider or a
Synchronize bounce reaches them. Nothing in this match got there (the new guards
upstream stop it), but the next effect wired into that window will reintroduce
the bug for free. Routing those two through `isDown()` as well would make the
invariant hold at the sink rather than at each caller.

## BUGS

### 1. Switching a sleeping Pokémon out cures its sleep — CONFIRMED (high severity, decided this match)

**What I saw.** Turn 1: `Jynx used Lovely Kiss! / Ninetales was put to sleep!`
Turn 2, Solaris switches Ninetales to Arcanine. Ninetales sits on the bench
through turns 2–5, returns on turn 6, and on turn 7 the very first line is
`Ninetales woke up!` — it then used Nasty Plot the same turn. Sleep cost it
exactly zero turns of action. The Apothecary did the same thing back on turn 8
with a Gengar slept on turn 5, and both pilots named the mechanic in their
private reasoning: Solaris — *"wakes on entry because the earlier pivot zeroed
its sleep counter"*; The Apothecary — *"Gengar switched out asleep, which zeroes
its sleep counter — it is guaranteed to wake on its first move attempt and still
act, exactly as their Ninetales did on turn 7."*

**What I expected.** Sleep is inflicted for `rng.Range(2, 4)` turns. Ninetales'
counter was therefore 2 at minimum. Under the counter being preserved across the
bench, its first action back decrements 2 → 1 and it stays asleep; it cannot
possibly wake on turn 7. In canon, switching out never cures sleep in any
generation — Gen 5+ preserves the counter outright, and the older Gen 1–4
"counter reset" made the Pokémon sleep a *fresh* full duration on return, i.e.
strictly worse for the sleeper, never better.

**What the source says.** `doSwitchWithCarry` (`internal/engine/switching.go:47`)
does `if out.Status == StatusSleep { out.SleepTurns = 0 }` — bare, with no
explanatory comment, which is unusual in a file where every deliberate decision
carries a paragraph. `canAct` (`internal/engine/turn.go:1441`) then reads:

```go
case StatusSleep:
    if p.SleepTurns > 0 { p.SleepTurns--; ... }
    if p.SleepTurns <= 0 {
        p.Status = StatusNone
        *log = append(*log, LogLine{... p.Name + " woke up!"})
        return true
    }
```

Zero is the wake sentinel, so zeroing the counter *is* curing the status one
action later. I reproduced it in isolation (`TestJudgeProbeSleepPivot`): status
`sleep`, turns `3` → switch out → turns `0`, status still `sleep` → switch back
in → first action logs `Ninetales woke up!` and the move resolves normally.

**On the "it's documented" defence.** `docs/battle-state.md:163` says "Counter
resets on switch-out" and `docs/ARCHITECTURE.md:294` calls it "sleep with
**Gen-5+ switch reset**". That label is backwards: Gen 5 is precisely the
generation that *removed* the switch reset. And even taken as an approximation of
the Gen 1–4 rule it is implemented inside-out — "reset" there means re-roll a new
duration, not set the counter to the value that means *awake*. The docs describe
an intent; the code implements the opposite of that intent's canonical effect.

Note also the asymmetry with the sibling field, which the r1m3 referee raised:
the same function does **not** reset `ToxicCounter` on switch-out, where canon
says it should. The engine resets exactly the counter canon preserves and
preserves exactly the counter canon resets.

**Why it matters here.** This is a semifinal against a team whose declared
archetype opens with the word "Sleep". The Apothecary led Jynx and spent its
first action — the one action a Focus Sash guaranteed it — on Lovely Kiss into
the opposing weather setter. Under canon that buys 1–3 turns of a free-swinging
Ninetales; here it bought one switch. Solaris then reused the same trick to
neutralise its own liability and, on turn 8, benefited a second time from the
*status* persisting on the bench, since a Pokémon that still reads `sleep` keeps
Sleep Clause locked against the foe while paying nothing for it. Both sides
exploited it, so the match is not unfair — but an entire status axis is
effectively deleted from the game, and it is deleted asymmetrically against the
archetype built on it. **Verdict: CONFIRMED.**

### 2. Facade does not double off the user's own status — CONFIRMED (previously reported in r1m3, still unpatched)

**What I saw.** Nothing yet in live play — Raticate has not been sent out at the
time of writing — but the defect is present in the binary this match is running
and The Apothecary's roster is built on it.

**What I expected.** Facade's defining property is base power 70 → 140 when the
user is burned, poisoned or paralysed (and, from Gen 6, it also ignores burn's
Attack cut). The Apothecary's team file pairs Raticate's Facade with a Flame Orb
and Guts specifically to cash this; the trainer theme says so outright — *"Facade
doubles off the team's own burn."*

**What the source says.** `grep -rn "facade" --include=*.go internal/` returns
nothing. The only dynamic base-power rewriter is `applyCallbackPower`
(`internal/engine/callbackmoves.go:282`), and its `statusDoublingMoves` map
(line 257) has exactly two entries, `hex` and `venoshock`, both keyed on the
**defender**. `data/moves.json` carries Facade as a flat 70-BP Normal physical
move with no marker. Nothing anywhere reads the attacker's status for base power.

**Measured.** `TestJudgeProbeFacade`, Raticate → Snorlax, ability and item
stripped so only base power varies: clean **51**, poisoned **51**, burned **27**.
Poison should roughly double the clean figure and returns it unchanged; burn
returns roughly *half*, because the engine applies the burn Attack halve
(`damage.go:359`) to a move that in canon both doubles and ignores it. Guts
itself is fine — verified separately at ×3.0 — so with Raticate's real kit the
observable effect is a Facade firing at 70 BP where both agents' plans assume
140, i.e. Raticate is about half as threatening as its archetype advertises.

**Status.** This was raised as CONFIRMED by the r1m3 referee, with the same
arithmetic and the same one-line fix sketch (add `facade` to
`statusDoublingMoves`, widening the map's `func(def *Pokemon) bool` signature to
take the attacker). The Round-1-to-semifinal patch shipped the faint-window fix
but not this one, so it is live for a second tournament match in a row — this
time in a knockout, on the team that reported it. **Verdict: CONFIRMED
(regression of a known, accepted report).**

### 3. Stale comment on `setWeatherFromAbility` — NOT-A-BUG (code correct, comment wrong)

**What I saw.** Turn 5, Solaris switched Ninetales back in explicitly to reset
the weather clock — *"Ninetales re-triggers Drought for 8 turns off Heat Rock"*
— and the turn-6 log shows no `Ninetales's ability set the weather!` line at all.
The sun kept the clock it was set with on turn 1 and expired on schedule at the
end of turn 8.

**What the source says.** `setWeatherFromAbility` (`abilities.go:1303`) opens
with `if s.Weather != nil && s.Weather.Kind == kind { return }` — a hard no-op
when the same weather is already up. That is canon: Showdown's `setWeather`
returns false for a same-weather set, so a Drought holder re-entering its own sun
extends nothing. Confirmed with `TestJudgeProbeDroughtReentry`: sun at
`TurnsLeft: 2`, Drought holder re-enters, still `TurnsLeft: 2`.

**But** the doc comment three lines above says the opposite: *"an ability
auto-setter never 'fails' when the same weather is already up; it just refreshes
to the default duration silently when it would be a no-op."* It does not refresh;
it returns. The behaviour is right and the comment is wrong. **Verdict:
NOT-A-BUG**, but worth a one-line correction — a pilot reading this engine could
reasonably build a line on that sentence, and one arguably did.

### Everything else checked clean

Verified against `internal/engine/*.go`, `data/moves.json`, `data/pokedex.json`,
`data/typechart.json` and `data/items.json`:

- **Drought / Heat Rock duration.** Fired on Ninetales' turn-0 entry;
  `weatherTurnsFor` → `extendedFieldTurns = 8` (`items_field.go:70`). Sun active
  turns 1–8 inclusive, `The sunlight faded.` at end of turn 8. Exactly 8.
- **Sun damage multipliers.** `damageMultByType` gives Fire ×1.5 / Water ×0.5
  under sun. Turn 1 Fire Blast into Jynx and turn 8 Fire Blast into Golbat both
  land in the sun-boosted band; turn 8's raw roll (263–309 into a 182-HP Golbat)
  is consistent only with the ×1.5 applied.
- **Chlorophyll.** `abilities.go:1194`, ×2 in sun and ×1 otherwise, read through
  `weatherFor` so an umbrella would suppress it. Measured Venusaur 132 → 264,
  matching the pilot's own figure; turn 5's Sleep Powder resolving before
  Gengar's 178 Speed is the live proof.
- **Solar Beam charge skip.** `skipChargeTurn` (`turn.go:1096`) short-circuits
  for `solar-beam`/`solar-blade` in sun only, before the Power Herb branch so a
  herb is not spent for free. No Solar Beam has resolved yet this match.
- **Solar Power.** ×1.5 on special moves in sun and 1/8 max-HP end-of-turn chip,
  both gated on sun. Measured: 51 → 76 damage (×1.49) and 19 chip off
  Charizard's 153 max HP (153/8 = 19). Not yet exercised live.
- **Status residual arithmetic.** `residuals.go:26–33` — burn `MaxHP/16`, poison
  `MaxHP/8`, toxic `MaxHP × counter / 16` with the counter capped at 15. Matches
  canon.
- **Burn's Attack cut.** `damage.go:359` halves the physical side of the ratio,
  keyed on category not stat. **Guts** (`abilities.go:1226`) returns ×3.0 when
  burned (×1.5 boost × ×2.0 to cancel the halve) and ×1.5 for any other status —
  correct, and independently confirmed by the r1m3 damage numbers.
- **Hex / Venoshock.** `statusDoublingMoves` — Hex doubles against any
  non-volatile status, Venoshock only against `StatusPoison`/`StatusToxic`.
  Measured Hex 57 → 111 against a paralysed target and Venoshock ×2 for poison
  and toxic but *not* for burn. Both refuse to double against a clean target.
- **Sleep Clause.** `sleepClauseBlocks` (`clauses.go:142`) walks the whole team
  including the bench and is reachable only from `inflictStatusFrom`, so Rest is
  exempt by call graph. Probed live-shaped: `Gengar stayed awake! (Sleep Clause)
  / But it failed!` when a bench Pokémon on that side is already asleep.
- **Status immunities.** `inflictStatus` (`effects.go:520–535`) refuses burn on
  Fire, freeze on Ice, paralysis on Electric, and poison/toxic on Poison **and**
  Steel, after the ability, ability-state and terrain guards. Relevant here:
  Venusaur and Victreebel are Poison-types and cannot be Toxic'd.
- **Confusion.** 33% self-hit (`turn.go:1426`), 40-BP typeless physical with no
  STAB/crit/type and no burn halve — the modern Gen-7+ rate.
- **Speed order.** Level 50 throughout. Turn 1 Ninetales 167 vs Jynx 161 —
  Ninetales moved first, which is right, and The Apothecary's opening note
  ("Jynx outspeeds Ninetales at 161") was simply a miscalculation, not an engine
  fault. Turn 3 Extreme Speed at priority +2 beat a faster Jynx. Turn 5 a
  Chlorophyll Venusaur at 264 beat Gengar at 178. `effectiveSpeed`
  (`damage.go:85`) applies stages, then the paralysis halve, then ability and
  item multipliers, all through `weatherFor`.
- **Type effectiveness.** Turn 1 Fire→Ice/Psychic 2× (super-effective line
  printed). Turn 3 Normal→Ghost 0 — which is why Gengar could enter for free
  against a Choice-locked Extreme Speed. Turn 4 Poison→Grass/Poison **1×**: 2 ×
  0.5, no effectiveness line printed, and the 123 damage matches the neutral
  band (a 2× read would have been 216–255). Both pilots wrote "2× on Venusaur";
  the engine and `data/typechart.json` are right and they were wrong.
- **Items.** Focus Sash clamped a lethal turn-1 Fire Blast to leave Jynx on 1 HP
  and announced. Life Orb ×1.3 with 13 recoil off Gengar's 136 max HP (10%,
  floored). Choice Band's lock is a volatile (`ChoiceLockMoveID`) and therefore
  cleared correctly by Arcanine's turn-4 switch. Rocky Helmet
  (`items_modifiers.go:279`) guards on `atk.Fainted || atk.HP <= 0` before
  chipping.
- **Intimidate** fired exactly once, on Arcanine's turn-2 entry, with the −1 on
  Jynx logged.
- **Damage clamping.** Turn 3 `Jynx took 1 damage` and turn 8 `Golbat took 182
  damage` are both the target's exact remaining HP, not a roll — the engine logs
  damage *applied*. Same convention the r1m2 referee documented; not an anomaly.
- **Fidelity note carried forward** (r1m2, still true): `computeDamage` floors
  once at the end rather than at each Showdown intermediate, so a roll can sit
  1–2 points above the cartridge maximum. Deliberate, documented, and it has not
  crossed a KO threshold in this match.
