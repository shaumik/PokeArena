# The stdio environment protocol

`cmd/pokearena-env` exposes the battle engine as a **line-oriented JSON
environment**: one JSON request object per line on stdin, one JSON response
object per line on stdout. No network listener, no database, no data directory
— the dataset and the team library are compiled into the binary.

It exists so the engine is reachable from outside Go without any infrastructure.
The reference client is the [`pokearena`](../python/README.md) Python package,
which spawns the binary as a subprocess and wraps it in Gymnasium and PettingZoo
shaped APIs. Nothing in this document is Python-specific; any language that can
spawn a process and write a line can drive it.

- **Protocol version:** `1.0` (reported by `handshake`, also `pokearena-env -protocol-version`)
- **Encoding:** UTF-8, one JSON object per line, `\n`-terminated, no embedded newlines
- **Line limit:** 8 MiB per request
- **Concurrency:** strictly one request in flight. Responses come back in order,
  one per request; interleaving two writers corrupts the stream.

```
$ pokearena-env
{"cmd":"reset","args":{"seed":42,"team":"Genesis"}}
{"cmd":"step","args":{"action":0}}
{"cmd":"close"}
```

---

## 1. Envelope

### Request

| field  | type   | required | meaning |
|--------|--------|----------|---------|
| `cmd`  | string | yes      | command name |
| `args` | object | no       | command arguments; absent means `{}` |
| `id`   | any    | no       | opaque; echoed verbatim on the response so a pipelining client can match up |

Unknown keys inside `args` are **rejected**, not ignored: a typo'd argument is a
bug, and silently defaulting it would produce a run that is not the run you
asked for.

### Response

| field    | type    | meaning |
|----------|---------|---------|
| `ok`     | bool    | success |
| `result` | object  | present when `ok` is true |
| `error`  | object  | present when `ok` is false |
| `cmd`    | string  | the command this answers |
| `id`     | any     | the request's `id`, verbatim |

### Errors

Every failure is a response object. A malformed line, an unknown command, an
illegal action, even a panic inside the engine — none of them exits the process,
writes to stdout in another format, or leaves the client to parse a stack trace
off stderr.

```json
{"cmd":"step","ok":false,"error":{"code":"illegal_action","message":"…","details":{…}}}
```

| `code` | meaning | recovery |
|--------|---------|----------|
| `bad_request` | unparseable line, or arguments that do not fit the command | fix the request |
| `unknown_command` | no such `cmd` | fix the request |
| `no_episode` | `step` / `legal_actions` / `observe` before any `reset` | call `reset` |
| `episode_over` | `step` after `terminated` or `truncated` | call `reset` |
| `illegal_action` | the action is not in the legal set | pick a legal action and step again — **the episode is untouched** |
| `internal` | a panic or engine-level failure | the episode is discarded; call `reset` |

`illegal_action` attaches `details.legal_actions` and `details.action_mask` for
the offending side, so a client can recover without a second round trip.

The one failure that is *not* a response object is a startup failure (a missing
`-data` directory, say): there is no request to answer yet, so the binary writes
`pokearena-env: <reason>` to stderr and exits 1.

---

## 2. Commands

### `handshake` (alias `info`)

No arguments. Returns the provenance record — everything needed to say which
engine, which dataset and which rules produced a trajectory. This is the same
promise the benchmark makes for its runs (see [benchmark.md §8](benchmark.md)),
carried through to environment clients.

| field | type | meaning |
|-------|------|---------|
| `protocol_version` | string | `"major.minor"`; a minor bump only adds optional fields |
| `engine_revision` | string | VCS revision the binary was built from, `-dirty` if the tree was modified, `"unknown"` if unstamped |
| `level` | int | every Pokémon's level (50) |
| `ruleset` | string | what the format permits — EV/IV bounds, natures, items, Species Clause |
| `dataset` | object | `version`, `sim_version`, `curation_sha`, `source_gen`, `synced_at`, and the counts of `species` / `moves` / `items` |
| `team_library` | object | `version`, `teams` (names), `profile` (what the teams actually use) |
| `action_space` | object | `n`, `move_slots`, `struggle_index`, `switch_base`, `team_size` |
| `agents` | string[] | the controller names `reset` accepts |
| `reward_modes` | string[] | the reward modes `reset` accepts |
| `commands` | string[] | the commands this binary implements |
| `max_turns` | int | the engine's own turn cap (300) |

### `reset`

Starts a new battle. Any previous episode is discarded.

| arg | type | default | meaning |
|-----|------|---------|---------|
| `seed` | uint64 | `0` | the battle seed. Same seed ⇒ same battle, byte for byte |
| `team` | team spec | — | **required.** Side 0's team |
| `opponent_team` | team spec | same as `team` | Side 1's team. The default is the mirror match |
| `agents` | string[2] | `["external","heuristic"]` | who pilots each side |
| `expectimax_depth` | int | `2` (or `-depth`) | search depth for a plain `expectimax` controller |
| `reward` | string | `"win_loss"` | `win_loss` or `hp_delta` |
| `max_turns` | int | `0` | truncate after this many turns; `0` leaves only the engine's 300-turn cap |
| `max_decisions` | int | `20000` | hard cap on decision points before truncation |
| `budget_ms` | int | `0` | per-decision time budget for the built-in agents; `0` = none |
| `battle_id` | string | `"eval-<seed>"` | cosmetic battle id. The default matches `cmd/bench` |

**Team spec.** Three routes, exactly one per team:

| form | meaning |
|------|---------|
| `{"library": "Genesis"}` or just `"Genesis"` | a curated team from the embedded library |
| `{"dex": [150,149,143]}` or just `[150,149,143]` | ad-hoc: Pokédex numbers, expanded to each species' first four moves — the same expansion `bench -team` uses |
| `{"picks": [{"dex_no":150,"moves":["psystrike",…],"nature":"timid","evs":{…}}]}` | full `engine.TeamPick` control: moves, ability, item, EVs, IVs, nature, gender |

Every team goes through the engine's legality check (`ValidateTeam`: Species
Clause, 1–4 learnset-legal moves per Pokémon, the EV/IV budget) before a battle
starts.

**Controllers.** `external` hands the side to the client over stdio; anything
else is a built-in agent played in-process. The names are exactly `cmd/bench`'s
`-agents` vocabulary, so "which opponent did you train against" has the same
answer in both tools.

| name | behaviour |
|------|-----------|
| `external` | the client supplies this side's actions |
| `random` | uniform over the legal set, seeded from the battle seed |
| `heuristic` | a hand-tuned depth-0 evaluator — the strongest programmatic agent on the board, and the reference opponent |
| `expectimax` | fixed-depth expectimax at `expectimax_depth` |
| `expectimax@N` | expectimax pinned to depth *N* |

`reset` returns a [step result](#step-result), positioned at the first decision
point where an external side has to act. With **no** external sides, `reset`
plays the whole battle and returns the terminal result — the baseline-vs-baseline
reproduction mode.

### `step`

Submits actions for the current decision point and advances.

| arg | type | meaning |
|-----|------|---------|
| `action` | action | shorthand, valid only when exactly one external side must move |
| `actions` | array[2] of action-or-null | the general form, indexed by side; `null` for a side that is not acting |

Set one or the other, never both. An action supplied for a side that is not an
external side to move is a `bad_request` — a client cannot reach across and play
the baseline's side.

After resolving, `step` **auto-advances** through any following decision points
that need no external input (a lone baseline replacing a fainted Pokémon, for
instance), so a client is only ever asked when it actually has a choice.

Every supplied action is validated *before* anything is resolved. A rejection
therefore leaves the episode exactly where it was.

### `legal_actions`

| arg | type | default | meaning |
|-----|------|---------|---------|
| `side` | int | the single external side to move | 0 or 1 |

| result field | type | meaning |
|--------------|------|---------|
| `side` | int | the side described |
| `turn`, `phase` | int, string | where the battle is |
| `to_move` | int[] | every side that owes an action right now |
| `legal_actions` | LegalAction[] | see [§3](#3-actions) |
| `action_mask` | int[11] | 0/1 over the discrete space |

### `observe`

| arg | type | default | meaning |
|-----|------|---------|---------|
| `side` | int | the single external side to move | 0 or 1 |

| result field | type | meaning |
|--------------|------|---------|
| `side` | int | the side described |
| `turn`, `phase` | int, string | where the battle is |
| `observation` | object | that side's fog-of-war view — see [§5](#5-the-fog-of-war-guarantee) |
| `state_hash` | string | FNV-1a fingerprint of the observation bytes |
| `terminated`, `truncated`, `winner` | bool, bool, int | outcome so far |

### `close`

No arguments. Returns `{"closed": true}` and then the process exits cleanly.
Closing stdin has the same effect, so a client that just wants the process gone
can close the pipe.

---

## 3. Actions

Two encodings are accepted everywhere, and `legal_actions` emits both.

**Flat integer** — a fixed `Discrete(11)` for RL clients:

| index | meaning |
|-------|---------|
| `0`–`3` | use move slot 0–3 |
| `4` | Struggle, or the forced move on a charge / recharge / Sky Drop turn |
| `5`–`10` | switch to team slot 0–5 |

The space is deliberately **fixed-size**, not "however many actions are legal
right now". Renumbering per turn would make the same integer mean different
things at different times, which destroys both learned policies and saved
trajectories. Legality is expressed as a mask instead.

**Object** — the engine's own form, and the only one that can aim a pivot:

```json
{"kind": "move",   "index": 2}
{"kind": "switch", "index": 3}
{"kind": "move",   "index": 1, "switch_target": 4}
```

`switch_target` names the bench slot a self-switch move (U-turn, Volt Switch,
Flip Turn, Teleport, Baton Pass) should bring in. Omitted — and always omitted
by the flat encoding — the engine picks deterministically: the lowest-indexed
live teammate.

**LegalAction** entries carry every encoding at once, so no client has to
convert:

```json
{"index": 0, "action": {"kind": "move", "index": 0}, "label": "use Psystrike (10/10 PP)"}
{"index": 6, "action": {"kind": "switch", "index": 1}, "label": "switch to Dragonite"}
```

`label` is rendered from the viewer's own observation only, so it is safe to
show to an LLM agent verbatim.

On the wire, only the two encodings above are accepted — a whole LegalAction
record is not an action. The Python client unwraps the record for you (so
`env.step(env.legal_actions()[0])` works), but a client written directly against
this protocol must send `record["index"]` or `record["action"]`.

---

## 4. Step result

`reset` and `step` return the same object. Every per-side field is a 2-element
array indexed by board side, with `null` for a side this response says nothing
about.

| field | type | meaning |
|-------|------|---------|
| `turn` | int | the battle's turn counter |
| `phase` | string | `choosing`, `replace`, or `ended` |
| `to_move` | int[] | sides that owe an action; empty once the episode is over |
| `observations` | [obs\|null, obs\|null] | one per external side that must act; at the end, one per external side |
| `legal_actions` | [LegalAction[]\|null, …] | for the same sides, while the episode is live |
| `action_mask` | [int[11]\|null, …] | the same set as a 0/1 mask |
| `rewards` | [float, float] | this step's reward per side; always zero-sum |
| `terminated` | bool | the battle reached a decided end |
| `truncated` | bool | a turn or decision cap stopped it first |
| `winner` | int | `-1` ongoing, `0`/`1` the winning side, `2` draw |
| `events` | LogLine[] | every engine log line produced by this step, including any auto-advanced decision points |
| `info` | object | see below |

`info`:

| field | type | meaning |
|-------|------|---------|
| `decision_index` | int | decision points resolved so far |
| `seed` | uint64 | the battle seed |
| `battle_id` | string | the battle id |
| `teams` | string[2] | resolved team labels |
| `agents` | string[2] | resolved controller labels |
| `state_hash` | string[2] | FNV-1a of each returned observation's bytes; `""` for a side with no observation |
| `fallback` | bool[2] | a built-in baseline proposed something illegal and was replaced by the first legal action |
| `turn_limit` | int | the cap that will truncate this episode |

**`terminated` vs `truncated`** follows the Gymnasium distinction. A battle that
ends because a side is wiped out, or because the engine's own 300-turn cap
decided it on remaining HP, is *terminated* — it reached a real outcome. A
battle stopped by a client-supplied `max_turns` or `max_decisions` is
*truncated*: the outcome is unknown and bootstrapping from the final value is
the correct thing to do.

**Rewards.** `win_loss` (the default) is `0` every step and `+1` / `−1` / `0` at
the terminal step. `hp_delta` adds the per-step change in (own team HP fraction
− foe team HP fraction). That reads privileged state — the opponent's exact team
HP is deliberately absent from every observation — which is normal for a
training signal and would be dishonest in an observation. That asymmetry is why
it is opt-in and why it lives in `rewards` and nowhere else.

---

## 5. The fog-of-war guarantee

> **A side's observation contains its own team in full, and of the opponent only
> the active Pokémon, redacted. The opponent's hidden information is not present
> in the bytes.**

This is enforced by construction rather than by filtering. An observation is
`ai.View` marshaled — the single projection path that the MCP server and the
live PvP WebSocket also serialize through. There is no second implementation
that could drift out of agreement with it.

What the projection does:

| aspect | what the viewer gets |
|--------|----------------------|
| own side | everything: full team, exact HP, stats, spreads, abilities, items, PP |
| opponent's bench | **nothing** — only `foe_bench_alive`, a count of unfainted benched Pokémon |
| opponent's active HP | `hp_pct`, a floored 0–100 percentage. No `hp`, no `max_hp` |
| opponent's ability | absent |
| opponent's item | absent, and the Choice-lock / Metronome / Micle / Unburden volatiles that would name one are cleared with it |
| opponent's stats, EVs, IVs, nature | absent — they are a damage calculator, and EVs+nature reconstruct exact Speed |
| opponent's moves | slot count preserved so you can see "revealed 1 of 4"; each slot carries `move_id` once revealed and nothing else. No PP |
| opponent's status, boosts, volatiles | present — these are announced publicly in the games |
| field state | weather, terrain, pseudo-weather, and the opponent's side conditions are public |
| pending foe Wish | the caster and the countdown, never the snapshotted heal amount (which would leak the caster's max HP) |

In the single-agent shape the guarantee is even simpler: the opponent's
observation is not merely redacted, it is not in the response at all
(`observations[1]` is `null`).

Two clients on the same battle each get their own projection. Both pass through
one process, so a script holding both can of course read both — that is the
script's own information, not a leak between the agents.

The audit is `TestFogOfWar_NoHiddenFieldsLeak` in
`cmd/pokearena-env/env_test.go`, which walks a whole battle with deliberately
asymmetric teams (a mirror match would make a leaked bench Pokémon
indistinguishable from one of your own) and checks every observation on both
sides.

---

## 6. The determinism guarantee

> **Same seed, same teams, same controllers ⇒ byte-identical battle.**

Every source of randomness in the engine is seeded from the battle seed, which
travels inside the battle state:

- The engine RNG is `RNGState`, initialised from `seed`.
- Gender rolls draw from a separate stream derived from the same seed, so
  introducing them could not shift any existing replay.
- The `random` baseline is seeded from the battle seed; side 1's agent is salted
  by `seed ^ 0xA5A5A5A5A5A5A5A5` so two stochastic agents in a mirror do not
  move in lockstep while the game stays a pure function of the seed.
- `heuristic` and fixed-depth `expectimax` are deterministic functions of the
  view. `expectimax@N` pins the depth so a choice never depends on machine
  speed.

The seeding path is deliberately identical to `internal/eval`'s, which is what
`cmd/bench` runs. `TestMatchesEvalRunGame` plays the same pairing both ways —
through `eval.RunGame` and through this protocol — and requires the same winner,
the same turn count, and the same per-decision state hashes. So a number
measured through this environment is comparable to a number on the published
board, and that comparability is tested rather than asserted.

**How to check it yourself.** `info.state_hash[side]` is the FNV-1a fingerprint
of that side's observation bytes at that decision point — the same fingerprint
`eval.Decision.StateHash` records. Collect the sequence across an episode and
compare two runs:

```bash
$ printf '%s\n' \
    '{"cmd":"reset","args":{"seed":4242,"team":"Genesis","agents":["heuristic","random"]}}' \
  | pokearena-env > a.jsonl
$ printf '%s\n' \
    '{"cmd":"reset","args":{"seed":4242,"team":"Genesis","agents":["heuristic","random"]}}' \
  | pokearena-env > b.jsonl
$ diff a.jsonl b.jsonl && echo identical
```

What is *not* covered: the timestamp-free parts of the protocol are all that is
promised. The engine revision is a build stamp, not a runtime value, and two
different engine revisions may legitimately produce different battles from the
same seed — which is exactly why `handshake` names the revision.

---

## 7. Running the binary

```
pokearena-env [flags]

  -data string              dataset directory (default: the dataset embedded in the binary)
  -teams string             team library JSON (default: the library embedded in the binary)
  -data-version string      label recorded as the dataset version (default "embedded")
  -depth int                default expectimax search depth (default 2)
  -protocol-version         print the protocol version and exit
```

The defaults need no files on disk: the dex, the team library and the dataset
provenance record are all compiled in via the module-root `go:embed`
(`dataset.go`). `-data` and `-teams` exist for running against a modified
dataset or a custom team library.

Install it with:

```bash
go install github.com/shaumik/PokeArena/cmd/pokearena-env@latest
```

One process is one environment instance — the binary holds at most one episode
at a time, which is what keeps each episode's RNG stream trivially isolated.
Run *N* processes for *N* parallel environments.

---

## 8. See also

- [`python/README.md`](../python/README.md) — the Python client, Gymnasium and
  PettingZoo APIs
- [`docs/benchmark.md`](benchmark.md) — the benchmark this environment shares
  its seeding and ruleset with
- [`docs/battle-state.md`](battle-state.md) — the battle-state and move schema,
  including the hidden-information contract the projection implements
- [`docs/agent-harness.md`](agent-harness.md) — the in-process agent interface
  the built-in baselines implement
