# Battle state — data model & rules

The contract for stats, status conditions, volatiles, and the move schema. The
engine is the source of truth at runtime; this doc is the source of truth for
what the engine *should* model.

Out of scope below (tracked as separate work): terrain, side
conditions / entry hazards, abilities, items, multi-hit moves, Frostbite,
the rest of the volatile catalog (LeechSeed, Substitute, Trap, Taunt,
Encore, Disable, etc.).

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
- `primary` — guaranteed effect of a *status* move (Swords Dance's +2 Atk, Recover's heal, Thunder Wave's paralyze). Implicit 100% chance, no roll.
- `self` — guaranteed effect on the *user* of a damaging move (Power-Up Punch's +1 Atk on hit). Implicit 100% chance, no roll.
- `secondaries` — array of rolled riders on a damaging move. Each has its own `chance`. Multiple secondaries roll independently (Tri Attack: three secondaries, each 20%).

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

**One-shot contract.** A hook that returns `bool` reports *"I fired, consume
me."* `fireItemTrigger` buffers the hook's own log lines and emits the consume
line ahead of them, so the log reads `X ate its Sitrus Berry!` then
`X restored 62 HP.` while the hook still gets to decline. Returning false must
leave no trace: the item stays held and is re-checked at the next trigger
point. `consumeItem` is the only path that clears `Pokemon.Item`, which is
also what arms Unburden.

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

**Degradations.** Two families are shipped with a documented gap rather than a
guess, both from data the engine doesn't carry: the flavor berries (Figy /
Wiki / Mago / Aguav / Iapapa) skip the Nature-disliked confusion because
Natures aren't modeled, and Leppa Berry is checked where PP is paid rather
than on every possible PP drain.

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
