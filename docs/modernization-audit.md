# Modernization audit — Gen-1 dex, current-gen mechanics

The output of issue #30 Step 1: every move mechanic that the Gen-9 snapshot
ships but the engine doesn't yet model. Each is bucketed:

- **(a) relabel only** — vocabulary the engine can accept as informational
  today; the behavior is either already covered or a future ability/item
  hook anchor. The fix is in `transform.go` (allowlist) + `domain.go`
  (`knownFlags` / `knownVolatiles`).
- **(b) needs new state** — opens a follow-up issue. Priority is signaled
  by species coverage (how many of our 81 fully-evolved Gen-1 mons learn
  at least one move that uses the mechanic).
- **(c) out of scope** — added to a curated denylist in `transform.go`; the
  move is filtered from learnsets at sync time, not handed to the engine.

The coverage numbers are over the 81 species currently in `data/pokedex.json`
(NotPreEvolution filter on). The fewer of our 81 mons that learn a thing,
the lower the urgency.

---

## (a) Relabel only — expand vocabulary, no engine change

These are flags / effects the data wants to carry that the engine doesn't
have to act on today but should accept without warning. Most are ability /
item hook anchors that won't fire until Step 4/5 wires those systems up.

| What                | Source         | Use                                                                              | Coverage |
| ------------------- | -------------- | -------------------------------------------------------------------------------- | -------- |
| `flag:bullet`       | Showdown flag  | Hook anchor for Bulletproof ability. Inert until Step 4.                         | 71 / 81  |
| `flag:slicing`      | Showdown flag  | Hook anchor for Sharpness ability. Inert until Step 4.                           | 51 / 81  |
| `flag:wind`         | Showdown flag  | Hook anchor for Wind Rider / Wind Power. Inert until Step 4.                     | 64 / 81  |
| `flag:dance`        | Showdown flag  | Hook anchor for Dancer ability. Inert until Step 4.                              | 37 / 81  |
| `flag:pulse`        | Showdown flag  | Hook anchor for Mega Launcher item/ability. Inert until Step 5.                  | 45 / 81  |
| `flag:heal`         | Showdown flag  | Hook anchor for Heal Block (b) / Magic Bounce. Inert until then.                 | 80 / 81  |
| `flag:defrost`      | Showdown flag  | "Move thaws the user." Engine's freeze model already allows post-freeze actions. | 28 / 81  |
| `flag:nonsky`       | Showdown flag  | Sky Battle restriction — we don't model Sky Battles. Pure no-op.                 | 80 / 81  |
| `flag:bypasssub`    | Showdown flag  | Bypasses Substitute. Inert until Substitute (b) lands.                           | 80 / 81  |
| `ignoreImmunity`    | Static field   | Bypasses type immunity (e.g. Foresight enables Normal-vs-Ghost). Inert until Foresight (b) lands. | 81 / 81 |

**Action:** add these to `flagsAllowlist` in `transform.go` (mapped to
their slug names) and extend `knownFlags` / `knownVolatiles` in
`domain.go`. They appear in the data, fail no validation, do nothing at
runtime — yet.

---

## (b) Needs new state — sub-tickets

Sorted descending by species coverage. The top of the list is what makes
the modernized engine *feel* modern; the tail is "interesting but optional."

### Tier 1 — universal (≥ 70 / 81)

| Mechanic           | Coverage | Notes                                                                                                                                   |
| ------------------ | -------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| **Substitute**     | 80 / 81  | New volatile `Substitute{HP int}`; absorbs damage and most status / boost effects until depleted. `bypasssub` flag becomes meaningful.   |
| **Protect / Detect** | 80 / 81 | Volatile `Protect{}` + `stallingMove` counter on the user (consecutive successful uses halve the next success chance: 1, 1/3, 1/9, ...). `breaksProtect` flag becomes meaningful. |
| **Endure**         | 80 / 81  | Volatile `Endure{}` — incoming damage that would faint is reduced to leave 1 HP. Shares the stalling-move counter with Protect.         |
| **Weather (setters)** | 80 / 81 | Rain / Sun / Sand / Snow setter moves. Already Step 3 of issue #30. Folded.                                                          |
| **Side conditions** | 73 / 81 | Stealth Rock, Spikes, Toxic Spikes, Sticky Web, Reflect, Light Screen, Aurora Veil, Tailwind, Mist, Safeguard. Step 3 hook table fits. |
| **Curse**          | 80 / 81  | Two distinct moves keyed on user type: Ghost-Curse (HP cost, target curse volatile) vs. non-Ghost Curse (+1 Atk/+1 Def, -1 Spe boost). |
| **Attract**        | 73 / 81  | Gender-based infatuation volatile. Requires species gender ratios — not currently in `domain.Species`. Could ship without gender as "50% to do nothing each turn." |

### Tier 2 — common (30–69 / 81)

| Mechanic            | Coverage | Notes                                                                                                                                            |
| ------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| **multihit**         | 60 / 81  | Roll N times (2–5 weighted, or exact for moves like Triple Kick). Each hit checks crit + STAB independently. Engine wiring: loop in `dealDamage`. |
| **selfSwitch**       | 50 / 81  | U-turn / Volt Switch / Flip Turn / Baton Pass / Teleport. Strike then force-switch the attacker. Baton Pass also copies boosts; defer the boost-pass to a sub-sub-ticket. |
| **flag:pulse**       | 45 / 81  | Already (a) for vocab, but if we want Mega Launcher item we'll come back here.                                                                  |
| **flag:dance**       | 37 / 81  | Same — (a) until Dancer ability ships.                                                                                                          |
| **forceSwitch**      | 36 / 81  | Whirlwind / Roar / Dragon Tail / Circle Throw. Random switch from the opponent's bench. Needs bench API on BattleState (we have it).            |
| **lockedmove**       | 23 / 81  | Outrage / Thrash / Petal Dance — locked into the move for 2-3 turns, confused at end. Volatile `LockedMove{Turns int, MoveIdx int}`.            |
| **Taunt**            | 27 / 81  | Volatile `Taunt{Turns int}` — target can't pick status moves while active.                                                                       |
| **laserfocus**       | 27 / 81  | Volatile `LaserFocus{Turns int}` (Laser Focus, Lock-On, Mind Reader). Guaranteed crit / guaranteed hit on next move.                            |
| **focusenergy**      | 21 / 81  | Volatile `FocusEnergy{}` — +2 crit stages while active. Stacks with `high-crit` flag.                                                          |
| **terrain (setters)** | 21 / 81 | Electric / Grassy / Misty / Psychic Terrain. Same hook table as Weather. Fold into Step 3 or its own step.                                     |
| **defensecurl**      | 20 / 81  | Volatile `DefenseCurl{}` boost mark for Rollout-style moves; +1 Def boost is the actual primary effect. Just (a) if we don't ship Rollout.    |
| **torment**          | 20 / 81  | Volatile `Torment{}` — target can't pick the same move twice in a row.                                                                          |
| **pseudoWeather**    | 20 / 81  | Trick Room (speed reversal), Gravity, Magic Room, Wonder Room, Mud Sport, Water Sport. Same hook table.                                       |
| **Encore**           | 19 / 81  | Volatile `Encore{Turns int, MoveIdx int}` — target locked to last-used move for 3 turns.                                                       |

### Tier 3 — niche (5–29 / 81)

These are real mechanics but reach < a third of the roster. Defer until
Tier 1/2 ships, or scope-cut if they're complex.

| Mechanic              | Coverage | Notes                                                                                                                                 |
| --------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| **thawsTarget**        | 28 / 81  | Scald / Scorching Sands / Steam Eruption thaw the frozen target *despite* not being Fire-type. Small `engine/turn.go` patch.        |
| **ohko**               | 28 / 81  | Fissure / Horn Drill / Guillotine / Sheer Cold. Either implement (rare hit, instant KO) or denylist. **Recommend denylist (c)** — they're a coinflip mechanic that doesn't feel "modern fight." |
| **Nightmare**          | 17 / 81  | Volatile applied to a sleeping target: -1/4 HP each turn until they wake.                                                            |
| **Roost**              | 16 / 81  | Self-volatile: heal 50% HP and lose Flying type for the turn. Type-stripping is the engine-change cost.                              |
| **Telekinesis**        | 16 / 81  | 3-turn volatile: target ignores Ground immunity / Gravity, accuracy auto-100 against them.                                            |
| **Magic Coat**         | 14 / 81  | Volatile: reflects status moves used at the user this turn. Cousin of Magic Bounce ability.                                          |
| **Disable**            | 13 / 81  | Volatile `Disable{Turns int, MoveIdx int}` — target can't use last-used move for 4 turns.                                            |
| **Snatch**             | 13 / 81  | Volatile: steals a self-targeting status move the opponent uses this turn. Niche, defer.                                              |
| **futuremove**         | 11 / 81  | Future Sight / Doom Desire. Delayed-impact damage 2 turns later. Needs a battle-state queue. **Recommend defer.**                    |
| **Imprison**           | 10 / 81  | Volatile: targets can't use moves the user knows. Niche but mechanically clean — defer.                                              |
| **breaksProtect**       | 10 / 81  | Feint / Hyperspace Hole / Phantom Force. Becomes meaningful when Protect ships.                                                      |
| **Yawn**               | 8 / 81   | Two-turn delayed Sleep: target gets `Yawn{Turns: 1}` volatile; next turn, Yawn applies Sleep if still active.                       |
| **Charge**             | 8 / 81   | Volatile: next Electric move BP is doubled. +1 SpD boost is the immediate effect.                                                    |
| **Foresight / Odor Sleuth** | 8 / 81 | Volatile: target's Ghost / Dark immunities to Normal / Fighting are bypassed, evasion ignored. Couples to `ignoreImmunity`.    |
| **Magnet Rise**        | 7 / 81   | 5-turn volatile: user ignores Ground immunity.                                                                                       |
| **willCrit**           | 7 / 81   | Frost Breath / Storm Throw — guaranteed crit. Simple flag (`always-crit`).                                                           |
| **Minimize**           | 5 / 81   | +2 Eva and marks user as "minimized" for damage-doubling from Stomp / Body Slam. Just `volatile` accept + the Stomp interaction is messy. Defer or denylist Stomp interaction. |
| **Embargo**            | 5 / 81   | 5-turn volatile: target can't use held item. Inert until items ship.                                                                  |
| **Gastro Acid**        | 5 / 81   | Volatile: nullifies target's ability for the rest of the battle. Inert until abilities ship.                                          |
| **slotCondition** (Wish / Healing Wish) | 5 / 81 | Wish: heal 50% max HP to whoever's in this slot 2 turns from now. Needs side-state queue. Niche; defer. |
| **Leech Seed**         | 4 / 81   | Volatile: -1/8 HP from target, +1/8 HP to user each turn. Classic, but only 4 of our 81 learn it (mostly Grass-types). Easy win.    |
| **Stockpile / Spit Up / Swallow** | 4 / 81 | Stockpile counter (1-3) volatile, consumed by Spit Up (damage) or Swallow (heal). Niche.                                  |
| **Aqua Ring**          | 3 / 81   | Persistent volatile: +1/16 HP each turn.                                                                                              |
| **Destiny Bond**       | 2 / 81   | Volatile: if user is fainted this turn after using it, attacker faints too. Two of our 81 (Gengar, Misdreav-not-in-scope).         |
| **Miracle Eye**        | 2 / 81   | Like Foresight but Psychic-vs-Dark and evasion ignore.                                                                                |
| **Ingrain**            | 1 / 81   | Persistent self-volatile: heal each turn, can't switch.                                                                              |
| **Grudge**             | 1 / 81   | Volatile: if user is fainted by a move, that move's PP is set to 0.                                                                  |

---

## (c) Out of scope — denylist

These moves get added to a `denylistMoves` set in `transform.go` and
filtered from learnsets at sync time. They never reach validation; the
engine doesn't need to know about them.

| Move(s)                                                                                | Why                                                                                              |
| -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| Helping Hand, Follow Me, Rage Powder, Spotlight, Ally Switch, After You, Quash, Decorate, Dragon Cheer | Doubles / triples mechanics. Issue #30 scope-out.                                       |
| Fire Pledge, Water Pledge, Grass Pledge                                                | Pledge combos (`pledgecombo` flag) — doubles mechanic.                                          |
| Future Sight, Doom Desire                                                              | `futuremove` queue is non-trivial state and < 15% coverage; **defer to a sub-ticket later** if missed. |
| Counter, Mirror Coat, Metal Burst, Bide                                                | Reactive-damage (return 2x physical / special damage taken). Needs a "damage taken this turn" register on Pokemon; not impossible, just not high-impact. |
| Mimic, Mirror Move, Copycat, Sketch, Assist, Me First, Metronome, Sleep Talk, Snore   | Calls-another-move mechanics. Each is its own mini-engine. Strong candidates for permanent (c). |
| Fissure, Horn Drill, Guillotine, Sheer Cold                                            | OHKO moves — coinflip mechanic, not the "modern feel" we want.                                  |
| Transform, Conversion, Conversion 2, Soak, Camouflage, Reflect Type                    | Type / identity changes. Complex, niche; not on roadmap.                                        |
| Sky Drop                                                                                | Two-turn move where the user grabs the target. Doubles-only really.                              |
| Belly Drum, Pain Split, Endeavor, Super Fang, Final Gambit, Memento                    | Custom HP arithmetic / sacrifice. Defer; can flip to (b) per move if a roster mon depends on it. |
| Mind Reader / Lock-On (without Laser Focus volatile)                                   | Same volatile as Laser Focus once that ships. Until then, deny.                                  |
| Mud Sport, Water Sport                                                                  | Old-gen pseudoweather; superseded by Terrain. Low value.                                         |

Implementation: a `denylistMoves` set in `transform.go`, checked inside
`translateLearnset` — entries are silently skipped from each species's
movepool. Audit-style log: print "dropped N denylisted moves from learnset"
per species at debug volume.

---

## What we ship after Step 2

1. **Vocabulary expansion** in `transform.go` and `domain.go` for the (a)
   bucket — no engine behavior change, but the warnings stop and
   subsequent syncs are quieter.
2. **`denylistMoves`** in `transform.go` for the (c) bucket — those moves
   never reach `data/moves.json`.
3. **Sub-tickets** for the (b) tiers, with the coverage number as priority
   signal. Tier 1 fans out first; Substitute / Protect / Endure / Curse
   are the universal-coverage anchors that unlock the rest.

After Step 2, the next step (#30 Step 3 — Weather) starts unblocked, with
the side-condition / pseudoweather hook table mapped out by the audit.
