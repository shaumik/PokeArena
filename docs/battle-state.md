# Battle state — data model & rules

The contract for stats, status conditions, volatiles, and the move schema. The
engine is the source of truth at runtime; this doc is the source of truth for
what the engine *should* model.

Out of scope below (tracked as separate work): terrain, side
conditions / entry hazards, abilities, items, multi-hit moves, Frostbite,
the rest of the volatile catalog (LeechSeed, Substitute, Trap, Taunt,
Encore, Disable, etc.).

## Derived stats and the training spread

A Pokémon's battle stats are derived once, when the team is built, from its
species' base stats plus a **spread**: EVs, IVs, and a nature. Nothing
recomputes them mid-battle — `Pokemon.Stats` is the single read path for the
damage formula, turn ordering, and every ability that consults a stat.

Level is fixed at **50** for every Pokémon (`engine.Level`).

```
raw  = floor((2·Base + IV + floor(EV/4)) · Level / 100)
HP   = raw + Level + 10
Stat = floor((raw + 5) · N)          // N ∈ {0.9, 1.0, 1.1}
```

- The nature multiplier `N` applies **last**, after the `+5`, and only to the
  five non-HP stats. **No nature modifies HP.**
- `N` is applied as an exact integer ratio (`×11/10`, `×9/10`), not a float.
- EVs are consumed in blocks of four — `floor(EV/4)` — so at Level 50 an EV
  investment only shows up in the final number every 8 EVs.

### Legality

| Field  | Range                                   | Absent means |
|--------|-----------------------------------------|--------------|
| EVs    | 0–252 per stat, **510 total**           | 0 everywhere |
| IVs    | 0–31 per stat                           | 31 everywhere |
| Nature | one of the 25 slugs in `data/natures.json` | neutral |

`ValidateTeam` is the only gate; the build path trusts it. The per-stat EV cap
is checked before the total, so a spread that breaks both is told which stat is
illegal rather than that its budget is over.

The absent-means-default column is load-bearing: it reproduces the fixed
IV 31 / EV 0 / neutral spread every Pokémon had before spreads existed, so a
team submitted without spread fields produces byte-identical battles.

### Natures

`data/natures.json` carries all 25 natures as `{id, name, plus, minus}`, where
`plus`/`minus` are `Stats` keys (`atk`, `def`, `spatk`, `spdef`, `speed`) —
never `hp`. The five neutral natures (Hardy, Docile, Serious, Bashful, Quirky)
carry **neither** key; absence is the signal, so no consumer needs a hardcoded
list of neutral names.

> **Key-naming seam.** EV and IV spreads reuse `domain.Stats`, whose JSON keys
> are `hp/atk/def/spatk/spdef/speed`. A move's `boosts` block uses a different
> vocabulary for the first two — `attack`/`defense`. The two key sets are
> genuinely different on the wire. This is deliberate: reusing `Stats` is worth
> the seam, and inventing a third naming scheme to reconcile them would not be.

### Hidden information

EVs, IVs, and nature are **hidden information**, exactly like exact stats. Any
projection toward the opposing side must redact all four together — knowing a
foe's EVs and nature reconstructs its exact Speed and both attacking stats,
which is the same free damage calculator that redacting `stats` alone is meant
to prevent. See `ai.foeWire`.

## Stat stages

Seven stages live on a Pokémon, all integers clamped to `-6..+6`, all reset to
zero on switch-out.

| Stage | Stat affected           |
|-------|-------------------------|
| `Atk` | Physical attack         |
| `Def` | Physical defense        |
| `SpA` | Special attack          |
| `SpD` | Special defense         |
| `Spe` | Speed (used for ordering and `effectiveSpeed`) |
| `Acc` | Accuracy (attacker side) |
| `Eva` | Evasion (defender side) |

**Offensive/defensive multiplier** (Atk/Def/SpA/SpD/Spe):

```
stage s ≥ 0:   (2 + s) / 2
stage s < 0:   2 / (2 - s)
```

**Accuracy/evasion multiplier** (Acc/Eva — *different curve*):

```
stage s ≥ 0:   (3 + s) / 3
stage s < 0:   3 / (3 - s)
```

**Effective accuracy** when an attacker tries to land a move on a defender:

```
combined = clamp(atk.Stages.Acc - def.Stages.Eva, -6, +6)
chance   = move.accuracy * accMult(combined)         // percent
```

If the move has the `bypass-acc` flag, the accuracy roll is skipped.

**Stage-change log wording** (cosmetic but load-bearing for "feel"):

| Δ stage | Going up        | Going down         |
|--------:|------------------|--------------------|
|   ±1    | rose             | fell               |
|   ±2    | rose sharply     | harshly fell       |
|   ≥ ±3  | rose drastically | severely fell      |

## Non-volatile status conditions

Each Pokémon has at most **one** non-volatile status at a time. Status persists
across switches *except* the Sleep counter, which resets (Gen 5+ semantics).

| Status        | Effect on owner                                                                                              |
|---------------|--------------------------------------------------------------------------------------------------------------|
| `Burn`        | -1/16 max HP at end of turn. Physical Atk halved in damage formula.                                          |
| `Poison`      | -1/8 max HP at end of turn.                                                                                  |
| `Toxic`       | -N/16 max HP at end of turn where `N = ToxicCounter`. Counter increments each turn, capped at 15.            |
| `Paralysis`   | 25% chance to skip the turn. Effective speed halved.                                                          |
| `Sleep`       | Cannot act for `SleepTurns` turns (initially 2–4; effective skip is 1–3 turns). Counter decrements pre-move. Counter resets on switch-out. |
| `Freeze`      | Cannot act. 20% thaw chance pre-move. **Thaws on being hit by any Fire-type damaging move**; the move still lands. |

**Type immunities** to status infliction:

- Fire-types immune to Burn.
- Ice-types immune to Freeze.
- Electric-types immune to Paralysis.
- Poison-types and Steel-types immune to Poison and Toxic.

**State alongside status** (only meaningful when the matching status is set):

- `SleepTurns int` — set on Sleep infliction (2–4 normally, 2 for Rest). The wider initial range ensures a target slept mid-turn doesn't wake up that same turn (a same-turn canAct decrement is absorbed by the +1).
- `ToxicCounter int` — set to 1 on Toxic infliction, ticks up each turn.

Both reset to zero when the status is cleared or when the Pokémon switches out
(for SleepTurns). ToxicCounter resets only when the Toxic status itself is
cleared.

## Volatile conditions

Multiple volatiles can stack on a Pokémon. All clear on switch-out via a
single `clearVolatiles(p)` call (the same place Stages clear).

```go
type Volatiles struct {
    Confusion    *ConfusionState // nil = not confused
    Flinch       bool             // transient; cleared at end of every turn
    Charging     *ChargingState   // locked into a two-turn move (Solar Beam, Fly, ...)
    MustRecharge bool             // next turn is consumed recharging (Hyper Beam)
}

type ConfusionState struct {
    Turns int  // 2-5 on inflict; decremented at the start of the owner's move attempt
}

type ChargingState struct {
    MoveIdx int // slot of the move being charged; the strike turn ignores submitted moveIdx
}
```

**Convention**: stateful volatiles are `*Pointer` (nil = absent, non-nil =
present with state). Transient volatiles (Flinch) are bool. New volatiles add
fields to the struct; the JSON shape grows by adding fields, not by mutating
existing ones.

### Confusion (Gen 7+ semantics)

- Inflicted: set `Confusion = &ConfusionState{Turns: rng.Range(2, 5)}`.
- Each turn the owner tries to act:
  1. Decrement `Turns`. If it hits zero, clear `Confusion`, log "snapped out", proceed to the move.
  2. Otherwise roll 33%. If self-hit: deal damage as a virtual typeless physical move with power 40, attacker == defender, attacker's Atk stage applied, no STAB, no crit, no type effectiveness. The intended move does **not** execute that turn.
  3. If not self-hit: proceed to the move normally.

### Flinch

- Set when a damaging move with a flinch secondary lands and rolls its chance.
- Checked at the start of the target's move execution this same turn: if `Flinch` is true, the target's move fails ("flinched and couldn't move"), and the flag is consumed.
- Cleared at end of every turn unconditionally (defensive — a flinch that didn't get consumed because the target never tried to act, e.g. they fainted, must not leak).

## Move schema (Showdown-inspired)

Moves are stored in `data/moves.json` and loaded into `domain.Move`. The shape
grows by adding optional fields, never by mutating existing ones.

```json
{
  "id": "body-slam",
  "name": "Body Slam",
  "type": "normal",
  "category": "physical",
  "power": 85,
  "accuracy": 100,
  "pp": 15,
  "priority": 0,
  "target": "foe",
  "flags": ["contact"],
  "secondaries": [
    { "chance": 30, "status": "paralysis" }
  ]
}
```

### Fields

- `target` — `"foe"` (default for damage moves) or `"self"` (status moves that act on the user).
- `flags` — string set drawn from a known vocabulary; unknown flags fail validation. Current vocabulary:
  - `contact`, `punch`, `bite`, `sound`, `powder` (informational; future ability/item hooks)
  - `bypass-acc` (skip accuracy roll — Aerial Ace, Swift, Aura Sphere)
  - `high-crit` (1/8 crit rate instead of 1/24 — Slash, Karate Chop, Cross Chop)
  - `two-turn` (charge turn 1, strike turn 2 — Solar Beam, Sky Attack, Dig, Fly, Razor Wind, Skull Bash)
  - `recharge` (user must skip the turn after the hit lands — Hyper Beam)
  - `selfdestruct` (user faints on use whether or not the move connects — Explosion, Self-Destruct)
  - `fixed-damage-level` (deal exactly user level damage, ignoring stats/STAB/effectiveness; type immunity still blocks — Seismic Toss, Night Shade)
  - `multi-hit` (reserved; mechanics not yet implemented)
- `override_offensive_stat` / `override_defensive_stat` — name the stat the damage formula reads in place of the category default (`attack`/`defense` physical, `spatk`/`spdef` special). Body Press is physical but swings the user's `defense`; Psystrike and Psyshock are special but land against the target's `defense`. Only the stat read moves — the move's *category* still decides burn's halving, which screen applies, and everything else. Stages and stat-boosting items follow the stat, not the category, the same way Wonder Room's swap does.
- `primary` — guaranteed effect of a *status* move (Swords Dance's +2 Atk, Recover's heal, Thunder Wave's paralyze). Implicit 100% chance, no roll.
- `self` — guaranteed effect on the *user* of a damaging move (Close Combat's -1 Def/-1 SpD, Overheat's -2 SpA). Implicit 100% chance, no roll. A user-side effect that is *rolled* rather than guaranteed belongs in `secondaries` with `"self": true`, not here.
- `secondaries` — array of rolled riders on a damaging move. Each has its own `chance`. Multiple secondaries roll independently (Tri Attack: three secondaries, each 20%). A secondary with `"self": true` applies to the **user** instead of the target — Rapid Spin's +1 Speed, Power-Up Punch's +1 Atk, Ancient Power's 10% omniboost. It still rolls its `chance`, and it is still the attacker's own effect: the defender's Shield Dust or Covert Cloak can't refuse it, though the attacker's Sheer Force suppresses it along with every other secondary.

### Effect blocks

`primary`, `self`, and each entry in `secondaries` share one shape:

```jsonc
{
  "chance": 30,                              // only on secondaries; primary/self imply 100
  "status": "paralysis",                     // burn | poison | toxic | paralysis | sleep | freeze
  "volatile": "confusion",                   // confusion | flinch (current vocabulary)
  "boosts": { "attack": 2, "speed": -1 },    // stage deltas, by stat name
  "heal": 0.5,                               // fraction of max HP healed
  "drain": 0.5,                              // fraction of damage dealt healed to attacker
  "recoil": 0.33,                            // fraction of damage dealt as self-damage
  "cure": true,                              // self-cure status (Refresh)
  "rest": true                               // cure + full heal + force 2-turn sleep
}
```

A single effect block may set multiple fields (e.g. a secondary that both
inflicts a status *and* drops a stage). The engine applies each present field
in order: boosts → status → volatile → heal → drain → recoil → cure → rest.

### Validation

Load-time invariants enforced by `Dex.validate()`:

- All flags in `flags` are from the known vocabulary above.
- `target` is `"foe"` or `"self"`.
- Each `secondaries[i].chance` is in `1..100`.
- `status` values are from the status vocabulary.
- `volatile` values are from the volatile vocabulary.
- `boosts` keys are valid stat names (`attack`, `defense`, `spatk`, `spdef`, `speed`, `accuracy`, `evasion`); values are integers.
- `category == "status"` moves have no `power` > 0 and no `secondaries`.

Unknown / typo'd fields fail loading. We have no users to break; strictness is
free insurance.

## Weather

Battle-level field condition. Four kinds (`rain`, `sun`, `sandstorm`,
`snow`) plus the implicit absent / clear state.

```go
type WeatherState struct {
    Kind      WeatherKind // "rain" | "sun" | "sandstorm" | "snow"
    TurnsLeft int          // counts down at end of turn; cleared at 0
}
```

On `BattleState` as `*WeatherState` (nil = clear).

**Setter moves** carry their target kind on `Move.Weather`. Default
duration is 5 turns. A setter that names the *currently active* weather
fails (matches Showdown). Hail (legacy) and Snowscape (Gen 9) both set
`snow` — modernization-plan unification (issue #30).

**Damage modifiers** in `computeDamage`:

| Active weather | Move type → multiplier        | Defender boost              |
| -------------- | ------------------------------ | --------------------------- |
| Rain           | water ×1.5, fire ×0.5          | —                            |
| Sun            | fire ×1.5, water ×0.5          | —                            |
| Sandstorm      | —                              | Rock-type SpD ×1.5           |
| Snow           | —                              | Ice-type Def ×1.5            |

**End-of-turn residual** (after burn/poison/toxic):

- **Sandstorm:** any active Pokémon that is not Rock / Ground / Steel
  takes `MaxHP/16` chip damage.
- **Snow, Rain, Sun:** no chip damage.

After residuals, the engine ticks `TurnsLeft--`. When it hits zero the
weather clears with a "stopped" log line; otherwise a "continues" line
fires for the turn.

**Deferred:** Solar Beam's "skip charge in sun" / "halved BP in rain"
interactions, Thunder / Hurricane / Blizzard weather-accuracy tweaks,
weather-rock items, ability auto-setters (Drizzle / Drought / Sand
Stream / Snow Warning). Land with the matching system (items #?, abilities
#9).

## Abilities

Passive per-Pokémon effect that fires from a fixed table of hooks. The
shipped set is ~60 abilities, picked to cover every Gen-1 slot-0 ability
the data pipeline emits plus selected slot-1/H entries that are
strategically meaningful. Slugs not in the registry are treated as
no-ops, so the dataset can carry every Showdown ability ahead of engine
support.

```go
type AbilityKind string                  // slug, e.g. "intimidate"
type Pokemon struct { /* ... */ Ability AbilityKind }

type Ability struct { /* hook fields, all optional */ }
var abilityRegistry map[AbilityKind]*Ability  // populated in init()
```

Slot-0 default. `domain.Species.Abilities` is ordered `[slot0, slot1?,
slotH?]`; `buildPokemon` picks slot 0. A picker UI for slots 1 / H is
deferred (future PR).

**Hook table.** Each integration site goes through a dispatcher
(`apply*` for void hooks, `ability*` for query hooks) that nil-checks
the registry entry, so adding an ability is one map entry with no
integration-site change.

| Hook                                 | Where                                                  | Examples                                |
| ------------------------------------ | ------------------------------------------------------ | --------------------------------------- |
| `OnSwitchIn`                         | `doSwitch` + start-of-turn-1 leads                     | Intimidate, Drought / Drizzle / Sand Stream / Snow Warning |
| `OnSwitchOut`                        | `doSwitch` before stages/volatiles reset               | Natural Cure, Regenerator               |
| `TypeMultOverride` + `OnImmunityBonus` | `computeDamage` / `ExpectedDamage` (eff lookup)      | Levitate, Volt Absorb, Lightning Rod, Flash Fire, Sap Sipper |
| `IncomingDamageMult`                 | defender side of damage multiplier chain               | Thick Fat, Filter, Multiscale, Dry Skin (×1.25 vs fire) |
| `OutgoingDamageMult`                 | attacker side of damage multiplier chain               | Technician, Tinted Lens, Hustle, Reckless, Iron Fist, Solar Power, Sheer Force, Analytic, Flash Fire's armed boost, Guts |
| `SurviveOHKO`                        | post-formula damage cap                                | Sturdy                                  |
| `AccuracyMult`                       | `resolveAccuracy` user-side                            | Compound Eyes, Hustle                   |
| `BlockCrit`                          | `computeDamage` crit roll                              | Battle Armor, Shell Armor               |
| `BlockSecondaries`                   | `applyDamageEffects` foe-secondaries loop (defender)   | Shield Dust                             |
| `BlockOwnSecondaries`                | `applyDamageEffects` foe-secondaries loop (attacker)   | Sheer Force                             |
| `BlocksStatus`                       | `inflictStatus`                                        | Immunity, Limber, Water Veil, Magma Armor, Insomnia, Vital Spirit, Sweet Veil |
| `BlocksFlinch` / `OnFlinched`        | `applyVolatile` flinch case                            | Inner Focus, Steadfast                  |
| `OnHit`                              | `dealDamage` after damage applies                      | Static, Flame Body, Poison Point, Effect Spore |
| `BlocksStatLowerByFoe` / `OnStatLoweredByFoe` | `applyStagesFromFoe`                          | Clear Body, Hyper Cutter, Big Pecks, Keen Eye / Defiant, Competitive |
| `SpeedMult`                          | `effectiveSpeed`                                       | Swift Swim, Chlorophyll, Sand Rush, Slush Rush, Quick Feet |
| `SuppressWeather`                    | consumers of weather (damage, residual, speed)         | Cloud Nine                              |
| `BlocksIndirectDamage`               | `applyResidual`, `applyWeatherResidual`, recoil        | Magic Guard                             |
| `EndOfTurn`                          | `ResolveTurn` after weather residual + tick            | Speed Boost, Rain Dish, Ice Body, Dry Skin (rain heal / sun chip), Solar Power chip |

A few abilities are wired inline rather than through their own hook because
the registry shape doesn't see the state they need: **Sniper** lives in
`computeDamage` (the multiplier hook can't see whether a hit crit-ed) and
**Soundproof** lives in `resolveAccuracy` (the sound-flag immunity hooks
the accuracy roll itself). Empty registry entries pin the dispatch path
so future readers can find them.

**Turn-order signal for Analytic.** Analytic fires when the holder is the
last scheduled mover this turn. `ResolveTurn` sets
`Volatiles.MovedLast = true` on the last entry of the ordered-movers
slice before its `executeMove` runs, and clears it in the end-of-turn
sweep alongside `Flinch`. The flag is also true when the foe switched —
the lone mover is by definition last to act. The Analytic hook reads
this flag from inside `OutgoingDamageMult`.

**Sheer Force suppression.** The `Ability` struct carries a
`BlockOwnSecondaries` bool; `applyDamageEffects` consults
`abilityBlocksOwnSecondaries(atk)` and skips the `m.Secondaries` loop
when set. `m.Self` is untouched so recoil and self-debuffs still apply
(matches canonical mechanics — Sheer Force only suppresses the
foe-targeted secondary that paid for the +30% boost).

**Special signal: `DamageResult.Sturdy`** — Sturdy is the only batch-1
ability whose trigger has to be visible from outside `computeDamage`, so
the formula reports it back via a flag on the result; `dealDamage` emits
the log line.

**Lead trigger.** On-switch-in hooks for the starting leads fire at the
top of the first `ResolveTurn` rather than burdening `NewBattle` with a
log channel. Side 0 fires first, then side 1 (relevant for stacked
Intimidate or competing weather setters).

**Cloud Nine.** Implemented as `SuppressWeather: true` on the registry
entry. `effectiveWeather(s)` returns `nil` whenever either active has it,
and `computeDamage` / `ExpectedDamage` defensively nil-out the weather
parameter even if the raw value was passed — so any external caller (AI
search, tests) automatically sees the suppressed state without needing
to know about Cloud Nine.

**Deferred:** ability picker in the team picker room (currently slot 0
only); per-ability hidden-until-first-trigger fog of war (today an
opponent's ability is visible on the View as a side-effect of cloning
`Pokemon` by value); abilities that depend on systems not yet built —
**Trace** (ability swap), **Mummy** (mutates attacker's ability),
**Cursed Body** (disable), **Frisk / Pickup / Sticky Hold / Unburden /
Harvest / Gluttony** (need item system), **Magic Bounce** (reflect
status moves — needs move-reflection plumbing).

## Held items

One optional item per Pokémon, chosen in the team picker and carried on
`Pokemon.Item`. Items mirror the ability system: a slug, a registry of
optional hooks, and dispatchers at fixed integration sites. A slug the
registry doesn't know is an **inert hold** — legal to carry, does nothing —
so the catalog can list an item ahead of engine support the same way an
unimplemented ability slug is a no-op.

```go
type ItemKind string                    // slug, e.g. "choice-band"
type Pokemon struct { /* ... */ Item ItemKind }

type Item struct { /* hook fields, all optional */ }
var itemRegistry map[ItemKind]*Item     // populated in init()
```

**Catalog vs registry.** `data/items.json` is the identity layer (id +
display name) and defines what `ValidateTeam` accepts; the registry is the
behavior layer. Two tests hold them together: `TestItemCoverage` fails on any
catalog item the engine doesn't model (the committed
`testdata/item_coverage.json` is the reviewed snapshot of that gap list), and
`TestItemRegistrySubsetOfCatalog` fails on any modeled item the catalog
doesn't ship. `engine.ItemCatalog` joins the two and is what `GET /api/items`
serves to the team builder, so the picker can never offer something the
validator rejects.

The registry is split by family — `items_core.go` (always-on stat and damage
modifiers), `items_berries.go` (consumables) — each registering from its own
`init()`, the same pattern the volatile handlers use.

**Hook table.**

| Hook | Fires | Examples |
|---|---|---|
| `OutgoingDamageMult` | attacker side of the `computeDamage` chain | Choice Band/Specs, Life Orb |
| `SpeedMult` | `effectiveSpeed` | Choice Scarf |
| `SurviveOHKO` | post-formula damage cap, defender side | Focus Sash |
| `EndOfTurn` | after the weather residual and tick | Leftovers |
| `ChoiceLock` | flag; set on first move use, enforced in `LegalActions` | the three Choice items |
| `Recoil` | fraction of max HP after a damaging move connects | Life Orb |
| `ResistType` | declarative ×0.5 on the defender | the eighteen resist berries |
| `OnHPThreshold` | the holder's HP fell to or below `HPThreshold` × max | pinch berries |
| `OnStatus` | the holder just gained a status or confusion | cure berries |
| `OnHitTaken` | a damaging move connected on the holder | Enigma, Jaboca, Kee |
| `OnHitTakenPassive` | same trigger, permanent item | Rocky Helmet, Sticky Barb |
| `OnDealtDamage` | the holder's strike connected, attacker side | King's Rock |
| `DrainFraction` | share of a move's *total* damage recovered | Shell Bell |
| `OnMoveUsed` / `OnMoveMissed` | the holder's move resolved / whiffed | Throat Spray, Blunder Policy |
| `OnStatCheck` | the holder has a drop or restriction to undo | White Herb, Mental Herb |
| `StatMult` / `CritStage` / `DrainMult` | read where the formula reads that value | Assault Vest, Scope Lens, Big Root |
| `AccuracyMult(If/Vs)` | the accuracy roll, attacker or defender side | Wide Lens, Zoom Lens, Bright Powder |
| `EndOfTurnLate` | the very end of the residual block | Flame Orb, Sticky Barb |
| immunity flags | a specific decision gate the engine already had | Heavy-Duty Boots, Shed Shell, Iron Ball |

**One-shot contract.** A hook that returns `bool` reports *"I fired, consume
me."* `fireItemTrigger` buffers the hook's own log lines and emits the consume
line ahead of them, so the log reads `X ate its Sitrus Berry!` then
`X restored 62 HP.` while the hook still gets to decline. Returning false must
leave no trace: the item stays held and is re-checked at the next trigger
point. `consumeItem` is the only path that clears `Pokemon.Item`, which is
also what arms Unburden.

**Two residual slots.** Canon splits the item residuals in two and the gap is
load-bearing: Leftovers and Black Sludge heal at order 5, *ahead of* the poison
and burn chip they exist to out-pace, while Flame Orb, Toxic Orb and Sticky Barb
sit at the very end so the turn an orb fires costs the holder nothing. `EndOfTurn`
is the early slot, `EndOfTurnLate` the late one.

**Trigger points for HP-threshold items.** Canon activates a pinch berry the
moment the effect that lowered HP finishes resolving, not at a fixed point in
the turn, so `applyItemHPTrigger` is called at every point HP can fall:

```
dealDamage tail       — inside the multi-hit loop, so a berry fires between strikes
executeMove tail      — after recoil, Life Orb, and Struggle self-damage
ResolveTurn residuals — after the item end-of-turn tick, so a Leftovers heal
                        that lifts the holder back over the line means no berry
doSwitch              — entry-hazard chip can land the incoming in range
```

**Determinism.** An item that draws from the RNG (Starf Berry's random stat)
must draw from the *battle's* stream, or replays stop reproducing. That is why
`doSwitch` / `applySelfSwitch` carry the `*RNG`, and why `ResolveReplace`
opens the carried `RNGState` with the same `NewRNG` + deferred-writeback
pattern `ResolveTurn` uses. Nothing draws unless an item actually fires, so the
common case leaves `RNGState` untouched.

**Fog of war.** A held item is hidden information, like an ability: it rides on
the `View` struct for in-process agents (their damage model needs it) but the
`foeWire` projection drops it, along with the `choice_lock_move_id` volatile —
a non-empty lock names the item on turn one. Your own side is unredacted.

**Degradations, and what is deliberately absent.** A few items ship with a
documented gap rather than a guess, and a few don't ship at all — the same
"don't ship what we can't honor" line the move denylist draws:

| Item | Why |
|---|---|
| Figy / Wiki / Mago / Aguav / Iapapa | Natures aren't modeled, so there is no disliked flavor to confuse on. Pure heals here. |
| Leppa Berry | Checked where PP is paid, not on every possible drain (Spite isn't in the move set). |
| Ability Shield | Nothing can suppress an ability yet — Gastro Acid sets a volatile no lookup reads. |
| Eviolite | The dataset carries no evolution data. |
| Float Stone | Weight isn't modeled. |
| Eject Button / Eject Pack / Red Card | Forcing a switch mid-move reorders faint resolution, self-switch and the pinch checks at once — a turn-resolution change, not an item. |
| Mirror Herb / Room Service | Each needs an event the engine doesn't emit (a stat change; a pseudo-weather starting). |

**The item-manipulation move family.** Nine of the sixteen are modeled, in
`items_moves.go`: `knock-off` (×1.5 into a held item, then removes it),
`thief` / `covet` (steal when empty-handed), `trick` / `switcheroo` (swap),
`bestow` (hand over), `corrosive-gas` (destroy), `poltergeist` (fails against
an empty-handed target), and `recycle` (restore what was consumed).
`acrobatics` reads only the holder's own slot and is modeled with them.

Two shared rules: a Substitute stops item theft, and Sticky Hold refuses every
removal. **Documented divergence:** Showdown's `sticky-hold` `onTakeItem`
exempts Knock Off, so Knock Off removes an item through Sticky Hold there.
Every other reference — and the reason the ability exists — says Sticky Hold
stops the removal while Knock Off still collects its damage boost. We follow the
latter, so `knockOffBoosts` and `itemIsRemovable` deliberately disagree.

The recycle memory is why `consumeItem` and `loseItem` are separate: only an
item you *used up* comes back, never one that was knocked off, stolen or traded.

Fourteen of the sixteen are now modeled. `fling` takes its base power from the
thrown item and the target eats a thrown berry; `natural-gift` takes its type
and power from the held berry; `pluck` / `bug-bite` eat the target's berry and
gain its effect, and `incinerate` burns it. Both data tables live in
`items_fling.go`, keyed by `ItemKind` rather than synced into
`data/items.json` — they are behavior, not catalog identity, the same line the
registry draws. `TestFlingAndNaturalGiftCoverTheCatalog` fails if a new item
arrives without a Fling power, or a new berry without a Natural Gift entry.

**Documented departure on Fling and berries.** Showdown declares no `fling`
block on any berry — not in the current data file, and not in the gen5, gen7 or
gen8 mods — and its Fling implementation does `if (!item.fling) return false`,
which taken literally would fail the move on all 46 of ours. Every other
reference says berries Fling at 10 base power and the target eats the thrown
berry, and Showdown's own Fling code carries an `if (item.isBerry)` branch that
would be unreachable otherwise. Berries are entered at 10.

A thrown or plucked berry fires its effect for whoever ends up eating it,
*regardless of that Pokémon's own condition* — a full-HP target still eats a
thrown Sitrus, and a thrown Liechi genuinely hands the opponent +1 Attack. That
last part is canon, not a bug: it is the well-known trap that makes Fling a poor
idea with a stat berry. The damage-reaction berries (Jaboca, Rowap, Kee,
Maranga) hang off a different hook and have no meaning for a berry thrown at
someone, so they do not fire.

Fling and Natural Gift are the only members of the family that consult item
suppression, matching canon's `ignoringItem`: an Embargoed, Magic-Roomed or
Klutzed holder cannot throw what it cannot use. The theft moves read the raw
slot on purpose — a suppressed item is still there to be taken. Fling spends
the item before the throw resolves, which is where canon's `onPrepareHit` puts
it; deferring it to the end of the move let a Life Orb boost and recoil for the
orb being thrown, and let the user eat the very berry it was throwing.

**All sixteen are now modeled.** Embargo and Magic Room complete the family.
Both suppress held items rather than removing them: the slot still counts for
Acrobatics and Unburden, and the item can still be knocked off, stolen or
traded — it just does nothing. Klutz is registered on the same predicate
(`itemSuppressed`), which is consulted from `itemOf` so all 51 call sites are
covered at one point.

Magic Room is field state but `itemOf` has no `BattleState` in hand, so it is
mirrored onto each active as `Volatiles.MagicRoomHere`. `syncMagicRoomFlags` is
the only writer — the setter, the expiry tick, and every switch-in — and
`ValidateStateInvariants` checks the mirror against the field, which is the
failure mode a mirror invites.

**Invariant checking is opt-in.** `engine.OnInvariantViolation` is nil by
default, and the check is skipped entirely when it is — production pays nothing
and gains no new failure mode. `TestMain` in `engine`, `eval`, `ai` and
`livebattle` sets it, so every test in those packages that resolves a turn is
also an invariant test. Measured honestly: neither corruption found in this
engine so far would have been caught this way, because no test outside the
dedicated ones produces those states. What it caught on its first run was a
fixture setting a Snorlax to 999 HP against a MaxHP of 235. Cheap insurance and
a fixture-quality gate, not a substitute for a targeted test.

**The faint window is deliberate.** From the damage loop to the faint block in
`executeMove`, a killed Pokémon has `HP == 0` and `Fainted == false`. Anything
that runs in that stretch and asks "is this Pokémon out of the fight?" must test
the HP, not the flag — `isDown()` is that predicate. Three bugs came from sites
that checked `Fainted` alone.

Do not close the window by fainting inline. Showdown has the same window for the
same reason (it batches faints in `faintMessages()` and guards each site with
`if (!target.hp)` — its own Knock Off `onAfterHit` opens with `if (source.hp)`),
so fainting inline would be a divergence *from* canon, and would reorder Destiny
Bond, Life Orb recoil, `applyOnFaint`/`applyOnKO`, Shell Bell and the
self-switch suppression at once. Guard at the site.

**Residual order** follows canon's `onResidualOrder`: weather chip (1), held
item heals (5), Aqua Ring (6), Ingrain (7), Leech Seed (8), status chip (9).
Weather first is the part this engine had backwards — a 1-HP Leftovers holder in
sand used to survive and now dies, which is canon.

**Contact abilities do not fire through a Substitute.** Static, Flame Body,
Poison Point and Effect Spore all ignored the `hitSub` flag their hook is handed;
only Cursed Body checked it. The guard now lives in `applyOnHit`, which also
resolves the inconsistency the items feature introduced: Rocky Helmet follows
canon and refuses to fire through a doll, so the item and the abilities had
disagreed.

## Engine phases

`executeMove` is factored into named phases so future ability/item hooks can
slot between them without rewriting the function:

```
canAct                  — status gating: freeze/sleep/para skip, confusion self-hit, flinch consumption
choosePP                — decrement PP, fall back to Struggle
announceMove            — log "X used Y!"
resolveAccuracy         — skip if bypass-acc, else roll acc * (Acc/Eva combined)
dealDamage              — only for damaging categories; computeDamage + apply
applySelf               — guaranteed self-effects (boosts, heal) on damage moves
applySecondaries        — for each secondary, roll its chance and apply
```

`applyResidual` (burn / poison / toxic end-of-turn damage) remains separate,
called once per side after both moves have resolved.

## Deferred (tracked as GitHub issues post-merge)

- Terrain (Electric, Grassy, Misty, Psychic) — modifies damage, status immunities, priority.
- Side conditions / entry hazards (Spikes, Toxic Spikes, Stealth Rock, Sticky Web, Reflect, Light Screen, Aurora Veil, Tailwind, Mist, Safeguard, Wish).
- More volatiles (LeechSeed, Substitute, Trap, Taunt, Encore, Disable, Charging, Locked-into-move).
- Multi-hit moves (Bullet Seed, Rock Blast, Triple Kick).
- Frostbite (Gen 8+; mirrors Burn for the special side).
