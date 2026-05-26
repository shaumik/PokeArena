# Battle state — data model & rules

The contract for stats, status conditions, volatiles, and the move schema. The
engine is the source of truth at runtime; this doc is the source of truth for
what the engine *should* model.

Out of scope below (tracked as separate work): weather, terrain, side
conditions / entry hazards, abilities, items, multi-hit moves, two-turn
moves, Frostbite, the rest of the volatile catalog (LeechSeed, Substitute,
Trap, Taunt, Encore, Disable, etc.).

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
| `Sleep`       | Cannot act for `SleepTurns` turns (initially 1–3). Counter decrements pre-move. Counter resets on switch-out. |
| `Freeze`      | Cannot act. 20% thaw chance pre-move. **Thaws on being hit by any Fire-type damaging move**; the move still lands. |

**Type immunities** to status infliction:

- Fire-types immune to Burn.
- Ice-types immune to Freeze.
- Electric-types immune to Paralysis.
- Poison-types and Steel-types immune to Poison and Toxic.

**State alongside status** (only meaningful when the matching status is set):

- `SleepTurns int` — set on Sleep infliction (1–3 normally, 2 for Rest).
- `ToxicCounter int` — set to 1 on Toxic infliction, ticks up each turn.

Both reset to zero when the status is cleared or when the Pokémon switches out
(for SleepTurns). ToxicCounter resets only when the Toxic status itself is
cleared.

## Volatile conditions

Multiple volatiles can stack on a Pokémon. All clear on switch-out via a
single `clearVolatiles(p)` call (the same place Stages clear).

```go
type Volatiles struct {
    Confusion *ConfusionState  // nil = not confused
    Flinch    bool              // transient; cleared at end of every turn
}

type ConfusionState struct {
    Turns int  // 2-5 on inflict; decremented at the start of the owner's move attempt
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
  - `two-turn`, `multi-hit` (reserved; mechanics not yet implemented)
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

- Weather (Rain, Sun, Sand, Hail / Snow) — modifies damage, residuals.
- Terrain (Electric, Grassy, Misty, Psychic) — modifies damage, status immunities, priority.
- Side conditions / entry hazards (Spikes, Toxic Spikes, Stealth Rock, Sticky Web, Reflect, Light Screen, Aurora Veil, Tailwind, Mist, Safeguard, Wish).
- Abilities (`onBeforeMove`, `onSourceModifyDamage`, etc.).
- Items (Choice, Life Orb, Leftovers, Toxic Orb, Flame Orb, berries, plates).
- More volatiles (LeechSeed, Substitute, Trap, Taunt, Encore, Disable, Charging, Locked-into-move).
- Multi-hit moves (Bullet Seed, Rock Blast, Triple Kick).
- Two-turn moves (Solar Beam, Fly, Dig, Sky Attack).
- Frostbite (Gen 8+; mirrors Burn for the special side).
