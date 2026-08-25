---
name: play-battle
description: Play a PokéArena battle to completion over the pokearena MCP server — claim a slot from a share URL, draft a team if the picker is open, then run the wait → view → act loop until the battle is terminal. Use whenever the user hands over a PokéArena battle link, asks you to take a trainer slot, or asks you to play, finish, or spectate a battle.
argument-hint: "[battle share URL, e.g. http://localhost:8080/?battle=ID&slot=p2&token=…]"
---

# Play a PokéArena battle

You are a trainer in a 6v6 battle. You see **only** what a human in the same
seat sees: your own side in full, the opponent's active Pokémon with its HP as a
percentage, and how many foes are still standing. The hidden bench is not in the
bytes you receive — do not pretend to know it, and do not ask the user for it.

The contract below is the one in
[`docs/mcp-protocol.md`](../../../docs/mcp-protocol.md); the tool descriptions in
[`internal/mcpserver/tools.go`](../../../internal/mcpserver/tools.go) are the
authority if the two ever disagree.

## 1. Claim the seat

A share URL looks like `http://host/?battle=ID&slot=p2&token=…`. Pull the three
query parameters out of it and call:

```
join_battle(battle_id=ID, slot="p2", join_token=TOKEN)
```

- **PvP battle** — pass all three. The token is a per-slot password: never echo
  it back into the transcript or into a shell command.
- **Live vs-AI battle** — pass `battle_id` only. No slot, no token; you are
  seated as p1 against the programmatic opponent.

`join_battle` returns `battle_id`, `slot`, `your_trainer`, `opponent_trainer`,
`phase`, and — when the battle is already running — `initial_view`.

Read `phase`:

| `phase` | What to do next |
|---|---|
| `open` | The picker is up. Draft a team (§2) before anything else. |
| `starting` | Transient. Go to the loop (§3); `wait` will carry you through. |
| `active` | The battle is running. Go straight to the loop (§3). |

Failure modes: a taken slot, a bad token, and a missing battle all collapse to
one opaque message on purpose — if you get it, ask the user for a fresh share
URL rather than retrying. `errAlreadyJoined` means this session already holds a
battle; call `leave_battle` first.

## 2. Draft a team (only when `phase` is `open`)

Four discovery tools feed `submit_team`. Call them in this order and use the
IDs they return verbatim — every ID is kebab-case (`body-slam`, `flash-fire`,
`choice-band`) and an inexact ID is a rejected team, not a warning.

1. `find_pokemon(query)` — substring search over the curated dex. The dataset is
   a filtered subset, so *check* rather than assume a species exists. Returns up
   to 30 matches with `dex_no`, name, and types.
2. `get_pokemon(dex_no)` — base stats, ability slots, and the **legal move list**
   for one species. `moves[].id` is exactly what `submit_team` wants.
3. `list_natures()` — the 25 natures (each raises one stat 10% and lowers
   another; five are neutral) **plus** `rules`: the battle level and the EV/IV
   caps actually enforced. Read the caps from here instead of assuming them.
4. `list_items()` — the held-item catalog. Items are not species-restricted; any
   item is legal on any Pokémon, one per Pokémon.

Then submit exactly six picks:

```
submit_team(picks=[
  {dex_no, moves: [1–4 ids from that species' learn list],
   ability?, item?, nature?, evs?, ivs?},
  … 6 total …
])
```

`ability` defaults to `abilities[0]` if omitted. `item` omitted means no item.
`nature`/`evs`/`ivs` are optional — omitting them gives a legal neutral spread
(no EVs, 31 IVs, no nature), so a spread is an optimization, never a
requirement. `submit_team` blocks until the server accepts (`accepted: true`) or
rejects with an error; on rejection, fix the offending ID and resubmit rather
than resubmitting the same payload.

Build for the format that actually exists, not for a remembered metagame: check
`list_natures().rules` for the level and budget, confirm every move against
`get_pokemon`, and keep the six picks covering different types so one opposing
Pokémon cannot wall the whole team.

## 3. The loop

This is the whole game:

```
while True:
    r = wait(timeout_seconds=60)
    if not r.ready:        # timeout — the opponent hasn't moved yet
        continue
    if r.terminal:         # battle over
        break
    action = decide(r.view)
    act(**action)
```

- **`wait(timeout_seconds=60)`** is the primitive. It blocks until it is your
  turn, the battle ends, or the timeout elapses; the value is clamped to
  `[1, 120]`. A `{ready: false}` return is **normal** — the engine resolves a
  turn only after *both* sides submit, so you are simply waiting on the other
  trainer. Call `wait` again. Do not treat a timeout as an error, and do not
  poll `view` in a tight loop instead: that burns tool calls and tokens for
  nothing.
- **`view()`** is the non-blocking snapshot, for when you want the current state
  right now (e.g. to re-read the board before explaining a decision). It returns
  the same object `wait` hands you.
- **`act(kind, index)`** submits your action: `kind="move"` with `index` 0–3, or
  `kind="switch"` with `index` 0–5 (a team slot). It returns
  `{accepted: true, turn: N}` the moment the write hits the wire — that is *not*
  confirmation the gateway liked it. An illegal action is rejected
  asynchronously and surfaces as an error on your next `wait`, so **validate
  against the view before calling**: the move slot must exist and have PP, the
  switch target must be a different, unfainted team member.
- **`leave_battle()`** closes the session. Mid-battle this is a **forfeit** —
  only call it when the battle is terminal or the user asks you to quit.

Act promptly each turn. The gateway substitutes a default action for a slot that
takes too long, and a defaulted turn is a turn you did not choose.

## 4. Reading the view

Keys you get every turn:

| Key | Meaning |
|---|---|
| `me` | Which side index you control. |
| `self` | Your side **in full**: `trainer`, `team` (six Pokémon with exact HP, moves, PP, stats, status), `active` (index into `team`), `conditions`, `slot_conditions`. |
| `foe` | The opponent's active Pokémon — `hp_pct` (0–100, *not* exact HP), types, status, boosts, and `moves` as revealed `move_id`s with no PP. No ability, no item, no stats, no spread. |
| `foe_bench_alive` | How many unfainted Pokémon the opponent still has benched. |
| `turn`, `phase` | Turn counter and battle phase. |
| `replace` | `true` when your active fainted and you **must** switch — the only legal action is `kind="switch"`. |
| `weather`, `terrain`, `pseudo_weather` | Field state; absent keys mean none active. |
| `foe_conditions`, `foe_slot_conditions` | The foe's public side effects — screens, hazards, a pending Wish (healer + turns left, never the heal amount). |

What is **absent** is information, too. The foe's ability and item are hidden
until they visibly activate; infer them from events (a Choice lock, a Sash save,
a Leftovers tick) rather than guessing at the start.

## 5. Choosing well

Rank the legal actions each turn on the state you can actually see:

- **Speed decides who acts.** Compare your active's Speed against what the foe
  has shown, adjusted for boosts, paralysis, and terrain. Winning the speed tie
  changes a trade into a free KO; losing it changes a "safe" attack into a
  faint.
- **Type matchups both ways.** Score your best move's effectiveness against the
  foe *and* the foe's likely best move against you. A neutral hit you survive
  usually beats a super-effective hit you don't get to use.
- **Count the KO.** If a move likely knocks the foe out this turn, take it — but
  note that `foe_bench_alive > 0` means the game is *not* over. A KO buys you a
  free switch-in for the opponent, not a win.
- **Switch on purpose.** A switch costs a turn and eats one hit; it is worth it
  to break a bad matchup, to preserve a Pokémon that wins a later matchup, or to
  absorb status. Do not switch merely because the current mon is damaged.
- **Status and hazards compound.** Burn, paralysis, poison, and entry hazards
  pay out over the rest of the game; they are usually the strongest play on a
  turn where no attack does meaningful damage.
- **`replace: true` restricts you.** Bring in something that resists what just
  KO'd you, not simply the healthiest body on the bench.
- **Read the PP.** Your `self` view has exact PP. A move at 0 PP is not a legal
  action.

When the user is watching, say what you chose and why in one short line per
turn — the interesting part of this benchmark is the reasoning, not the win.

## 6. Ending

The loop exits when `wait` returns `terminal: true`. The accompanying view is
the final board: report the result (who won, on what turn, what was left
standing) and then call `leave_battle()` to release the session. One MCP process
can play many battles in sequence, so after leaving you are free to
`join_battle` again on a new share URL.
