# AGENTS.md — PokéArena for coding agents

Read this first if you are an agent (Claude Code, Cursor, Codex, …) deciding
whether this repo is usable. Short answer: **yes, with zero infrastructure**, as
long as you stay on the benchmark/engine path. The browser arena is the only
part that needs services.

Module path: `github.com/shaumik/PokeArena` (Go 1.26). License: MIT.

---

## What this is, in one paragraph

PokéArena is a **deterministic Pokémon battle engine** plus an **LLM-agent
benchmark** and an **arena** built on top of it. The engine is a pure function —
`(state, actionP1, actionP2) → (newState, events)`, no I/O — so a battle is
replayable bit-for-bit from its turn log. It is **not** a wrapper around Pokémon
Showdown, which is the whole point: because the RNG stream is ours and seeded,
the benchmark can run **variance-controlled mirror matches** (same seed, same
team, both sides, both seat orientations), so the only free variable in a result
is the policy. It is a two-player, turn-based, **hidden-information (fog-of-war)
multi-agent environment**: each side sees its own team in full and only a
redacted view of the opponent's active Pokémon. Agents can plug in as an
in-process Go `ai.Agent`, over a WebSocket, or through an **MCP server**.

---

## Fastest verified path to a result (no services, no API key)

```bash
git clone https://github.com/shaumik/PokeArena && cd PokeArena
go run ./cmd/bench -agents heuristic,random -games 2 -out run.jsonl -runs ""
```

**Time:** a few seconds after the first compile. **Needs:** a Go toolchain. No
Postgres, no Redis, no RabbitMQ, no Docker, no network, no model key.

**Expected output** (verbatim — this is a real run):

```
[bench] round-robin: 2 contestants, 1 pairings x 6 teams, 2 seeds x2 orientations = 4 games/team, 24 total

per-team Elo:
  Genesis    heuristic 1676  random 1324
  Spectrum   heuristic 1676  random 1324
  Keystone   heuristic 1676  random 1324
  Bruiser    heuristic 1676  random 1324
  Bastion    heuristic 1676  random 1324
  Blitz      heuristic 1676  random 1324

overall standings (Elo, win rate with Wilson 95% CI):
  agent           elo  winrate  95% CI             W-L-D
  heuristic      1804   100.0%  [ 86.2%, 100.0%]  24-0-0 (n=24)
  random         1196     0.0%  [  0.0%,  13.8%]  0-24-0 (n=24)
```

Standings go to **stderr**; the per-decision JSONL trace goes to `-out` (or
stdout if you omit it — that's hundreds of lines, so pass `-out`). `-runs ""`
suppresses the persisted run record; drop it to save `runs/<run_id>.json` and
append `runs/index.jsonl`.

Bigger, still infrastructure-free (~1 minute for 240 games):

```bash
go run ./cmd/bench -agents heuristic,expectimax -games 20 -out run.jsonl
```

Add LLM contestants (needs the relevant `<VENDOR>_API_KEY`; `ollama` needs none):

```bash
go run ./cmd/bench -agents heuristic \
  -llm 'haiku=claude-haiku-4-5-20251001,openai:gpt-5/cot,ollama:llama3.1:8b' \
  -games 10 -out run.jsonl
```

### `cmd/bench` flags (from `cmd/bench/main.go`)

| Flag | Default | Meaning |
|---|---|---|
| `-agents` | `random,heuristic,expectimax` | programmatic contestants; `expectimax@N` pins a depth as a distinct contestant |
| `-llm` | — | LLM contestants, `[label=][vendor:]model[/condition]`; vendor ∈ `anthropic` (default), `openai`, `gemini`, `ollama`; condition `raw` (default) or `cot` |
| `-games` | `20` | seeds per pairing per team, each played in both orientations |
| `-teams` | `data/benchmark-teams.json` | curated team library; every team is mirror-matched |
| `-team` | — | ad-hoc override: comma-separated dex numbers, mirrored to both sides |
| `-depth` | `2` | fixed search depth for expectimax |
| `-budget-ms` | `0` | per-decision time budget (recommended for LLM agents) |
| `-out` | stdout | JSONL trace path |
| `-runs` | `runs` | run-record dir (`""` disables) |
| `-pricing` | `data/model-pricing.json` | price table for costing measured token usage |
| `-cot-budget` | `2048` | thinking-token budget for `/cot` contestants |
| `-data` | `data` | dataset directory, **read from disk** |

> `-data` is why `go run github.com/shaumik/PokeArena/cmd/bench@latest` does not
> work from an arbitrary directory: the dataset is loaded from `data/` on disk,
> not embedded in this binary. Clone the repo (or point `-data` at a checkout).

Reproducibility: deterministic contestants on the same agents/teams/seeds give
byte-identical games — same winners, same turn counts, same per-decision
`state_hash`. Every trace opens with a run header pinning engine revision,
dataset sim-version + curation SHA, ruleset, `team_library`, `team_profile`,
contestants, depth and seeds.

---

## What needs the full stack, and what does not

| Thing | Command | Postgres / Redis / RabbitMQ? |
|---|---|---|
| Engine + AI unit tests | `make test` | **No** |
| Build everything | `go build ./...` | **No** |
| Benchmark round-robin | `go run ./cmd/bench …` | **No** |
| LLM contestants in the benchmark | `go run ./cmd/bench -llm …` | **No** (needs a vendor API key, or local Ollama) |
| Benchmark report / history | `go run ./cmd/bench-report`, `./cmd/bench-history` | **No** (reads persisted run JSON) |
| Team legality + cross-team balance | `go run ./cmd/team-validate` | **No** |
| Spread-impact measurement | `go run ./cmd/spread-impact` | **No** |
| File-backed 2-agent match broker | `go run ./cmd/royale …` | **No** |
| Dataset validation | `make validate-data` | **No** |
| Python environment (`python/`) | `pip install pokearena` | **No** |
| Browser arena / team builder UI | `docker compose up --build` | **Yes** |
| MCP server vs the built-in AI (`start_battle`) | `make mcp` | **No** |
| Live PvP, spectating, MCP against a *live* battle | `docker compose up --build` + `make mcp` | **Yes** |
| Elo leaderboard (arena side) | `docker compose up --build` | **Yes** |
| Integration tests | `make test-integration` | **Yes** (brings its own stack up) |

The stack is Postgres (system of record), Redis (live state + cache), RabbitMQ
(work + events), and five Go services: `gateway`, `battle-worker`,
`battle-session`, `ai-service`, `leaderboard-worker`.

### Two more zero-infrastructure paths

- **`cmd/royale`** — a file-backed, two-seat match director. Two independent
  agent processes play a full battle against the real engine with no server, no
  WebSocket and no shared memory; `state.json` is the only source of truth and
  each seat reaches it through `royale view --id M --slot p1 --wait` and
  `royale act --id M --slot p1 --action move:0`. `view` renders the engine's own
  fog-of-war projection, so a player agent cannot see the opponent's bench even
  by accident. Referee commands (`log`, `report`, `state`) are gated behind a
  judge token.
- **`internal/eval`** — the harness `cmd/bench` is built from, if you want to
  drive matches from Go directly (`RunMatch`, `SeedRange`, `Contestant`).

---

## The MCP tool surface

`cmd/pokearena-mcp` is a **stdio MCP server that runs on your machine**. It
plays a battle two ways:

- **`start_battle` — no infrastructure at all.** The battle runs inside the MCP
  process against the built-in opponent, on the dataset embedded in the binary.
  No gateway, no Docker, no second player, and no `data/` directory — so it
  works from any working directory, including a `go install`ed binary. This is
  the path to use unless you specifically want the arena.
- **`join_battle` — attach to a live arena.** Needs a running gateway (see the
  stack table above), and a `battle_id` that gateway issued.

The reference tools (`find_pokemon`, `get_pokemon`, `list_items`,
`list_natures`) answer from the embedded dataset too, so team-building works on
either path.

```bash
go build -o bin/pokearena-mcp ./cmd/pokearena-mcp    # or: make mcp
claude mcp add pokearena -- "$(pwd)/bin/pokearena-mcp"
claude mcp list                                       # should list "pokearena"
```

`POKEARENA_GATEWAY_URL` (default `ws://localhost:8080`; `wss://` for TLS) is
read only by `join_battle`. `start_battle` never opens a socket, so an
unreachable gateway is not an error until you actually ask to join one.

Eleven tools (`internal/mcpserver/tools.go`):

| Tool | Purpose |
|---|---|
| `start_battle(opponent?, seed?)` | Create and join a battle against the built-in AI, in-process. No gateway needed. `opponent` is `heuristic` (default) or `expectimax`; `seed` pins the RNG *and* the opponent's roster, and is echoed back so an unseeded battle is still replayable. Returns `phase: "open"` plus a **`briefing`** — every legal species, item and nature, the caps and the clauses — so a team can be written with no lookups at all. |
| `join_battle(battle_id, slot, join_token)` | Bind the session to a battle and get the initial view. Call first — everything else requires it. For a live vs-AI battle pass only `battle_id` (you are seated p1); for PvP pass `slot` + `join_token`. |
| `submit_team(team)` | Required while `phase: "open"`. `team` is a **Showdown paste** — display names, one block per Pokémon, blank line between; only the species line and one move are required each. `picks` (the old structured form) still works. On rejection you get `accepted: false` and a `report` listing **every** problem at once, each with the legal alternatives; `report.warnings` flags legal-but-weak choices. |
| `wait(timeout_seconds=60)` | The loop primitive. Blocks until it's your turn / the battle ends / timeout. Clamped to `[1,120]`. Returns `{ready, terminal?, view?}`. |
| `view()` | Non-blocking current fog-of-war view. Prefer `wait` between turns. |
| `act(kind, index)` | Submit the turn's action. `kind` is `"move"` (index 0–3) or `"switch"` (team slot 0–5). |
| `leave_battle()` | Close the session. A forfeit if the battle is live. |
| `find_pokemon(query)` | Substring search of the curated dex. Returns `{dex_no, name, type1, type2}`, capped at 30. |
| `get_pokemon(dex_no)` | Full species detail: base stats, ability slots, and the authoritative legal move list for `submit_team`. |
| `list_items()` | The held-item catalog. Any item is legal on any Pokémon, one per Pokémon. |
| `list_natures()` | The 25 natures plus the battle level and the EV/IV caps `submit_team` enforces. |

Standard loop: `start_battle` (or `join_battle`) → `submit_team` while
`phase == "open"` → repeat `wait` → `act` until `terminal: true` →
`leave_battle`. Three calls reach the first move.

`find_pokemon` / `get_pokemon` / `list_items` / `list_natures` are still there
for detail work — a species' full movepool is the one thing the briefing does
not carry, because movepools are two orders of magnitude larger than the rest of
the dataset put together. You rarely need them: a move written from memory for a
species you were told about is right about 39 times in 40, and the rejection
names the near misses when it is not.

**This format is not standard competitive play**, and the briefing lists the
differences. The one a team written from memory breaks most often is the **Item
Clause**: no two Pokémon may hold the same item.

If `act` is refused — a move chosen while a fainted Pokémon needs replacing is
the common case — the next `wait` returns **immediately** with `error` naming
the legal actions, the view attached, and the turn still yours. It is reported
once and cleared, so a stale message never reappears on a later turn.
Contract details and error semantics: [docs/mcp-protocol.md](docs/mcp-protocol.md).

`go run ./cmd/mcp-smoke` walks one full turn through the real binary with verbose
checkpoints (needs a running gateway).

---

## Observation and action shape

**Action** — one of two things per turn:

```json
{"kind": "move",   "index": 0}   // move slot 0..3 of the active Pokémon
{"kind": "switch", "index": 3}   // team slot 0..5
```

Illegal actions are rejected by the gateway. In the benchmark harness an
unparseable or illegal LLM reply falls back to the first legal move and is
flagged in the trace as `fallback: true` — that legality-fallback rate is itself
a reported signal.

**Observation** — `ai.View` (`internal/ai/agent.go`), the engine's fog-of-war
projection. Keys:

| Key | Contents |
|---|---|
| `me` | side index you control |
| `self` | **your whole side, unredacted** — all six Pokémon, exact HP, stats, EVs/IVs/nature, moves with PP, items, abilities |
| `foe` | the opponent's **active Pokémon only**, redacted (below) |
| `foe_bench_alive` | count of unfainted benched opponents — a number, never their identities |
| `turn`, `phase`, `replace` | turn counter; engine phase; `replace: true` when you must replace a fainted active |
| `weather`, `terrain`, `pseudo_weather` | field state (public) |
| `foe_conditions`, `foe_slot_conditions` | the foe's side conditions (screens, hazards) and pending slot effects, with the Wish heal *amount* stripped |

What the `foe` object **never** contains (`foeWire`, mirroring what Showdown
sends a player): exact `hp`/`max_hp` — you get `hp_pct` 0–100 instead; `stats`;
`evs`/`ivs`/`nature`; `ability` and `item` until each visibly activates (tracked
by `AbilityRevealed` / `ItemRevealed`, which never un-set); `last_consumed_item`;
and PP on revealed moves (revealed slots carry `move_id` only). A live Choice
lock is cleared too, since its presence would name the item.

Fog of war is enforced by the **return type**, not by policy the agent is asked
to respect — the hidden bytes are never sent. Full contract:
[docs/battle-state.md](docs/battle-state.md) (§ *Hidden information*, § *Ability
and item fog of war*).

---

## Repo layout

```
cmd/          one main package per binary
  bench             the benchmark round-robin  ← start here
  bench-report      a saved run → self-contained HTML report
  bench-history     Elo/cost timeline across runs
  pokearena-mcp     stdio MCP server (user-side adapter)
  pokearena-agent   reference headless LLM harness (dials the gateway WS)
  royale            file-backed two-seat match broker, no server
  gateway           edge service: REST + WebSocket + SSE + static SPA
  battle-worker     quick-sim consumer   battle-session  live-battle owner
  ai-service        AI-decision consumer  leaderboard-worker  Elo consumer
  team-validate / spread-impact / data-sync / data-validate / db-replay
  mcp-smoke / pvp-smoke / showdown-triage   (smoke + triage tools)

internal/
  engine        the battle engine — pure, deterministic, the bulk of the code
  ai            agent harness: Agent interface, random/heuristic/expectimax,
                and MakeView — the fog-of-war projection
  eval          benchmark harness: RunMatch, seeds, Elo, Wilson CIs, run records
  llm           provider adapters (anthropic/openai/gemini/ollama) behind one iface
  agentloop     the one-shot LLM decision loop used by bench and pokearena-agent
  mcpserver     the MCP tool surface + session
  domain        static reference data loading (dex, moves, items, natures, types)
  specs         the engine's slug vocabulary
  protocol      gateway↔client wire shapes
  gwclient      thin WS client to the gateway's live_pvp path
  httpapi, livebattle, session, store, cache, mq, messages, config, usage

data/         curated dataset: pokedex.json (80 species), moves.json,
              typechart.json, items.json, natures.json (25),
              benchmark-teams.json (6 mirror-match teams), ai-teams.json,
              model-pricing.json, _provenance.json
docs/         architecture, benchmark scope/limits, MCP protocol, battle-state
              contract, live-PvP protocol, engine findings
web/          the SPA served by the gateway
royale/       tournament runbook, team files, report generator (Python)
scripts/bench live agentic-harness comparison scripts
tools/        data-sync helpers (Node)
```

---

## Build, test, lint

```bash
go build ./...        # everything
make test             # go test ./... -count=1  + royale/test_report.py (python3)
make vet              # go vet, including the showdown build tag
make lint             # golangci-lint, config in .golangci.yml
make lint-fix         # apply auto-fixable findings
make fmt              # gofmt -w .
make hooks            # opt in to .githooks/pre-commit (build + lint)
```

`make lint` uses **golangci-lint v2.12.2**, pinned in the `Makefile` and
auto-installed with the same Go toolchain that builds the module. `.golangci.yml`
starts from the `standard` set and adds correctness linters (`staticcheck`,
`bodyclose`, `errorlint`, `noctx`, `rowserrcheck`, `sqlclosecheck`,
`ineffassign`, `unused`, …). Test files are linted too.

`make test-showdown` runs the ~2,000-case port of Showdown's sim suite behind the
`showdown` build tag. It is **expected to be partly red** — it documents where
this engine and competitive Pokémon disagree, and only fails when a case
disagrees with the ledger in `gaps_test.go`. Plain `go test ./...` never compiles
it.

---

## Known limitations — read before you cite anything

- **The arena leaderboard has no identity.** Trainers are keyed on a free-text
  name with no ownership and clients barely prompt for one, so live-arena games
  collapse onto `"Trainer Red"` vs `"AI"`. That board is *for fun, unverified*.
  The `cmd/bench` benchmark, with named contestants and fixed seeds, is the part
  that is measurement-grade.
- **Expectimax is not an optimality oracle.** Fixed-depth expectimax on this
  format is non-monotonic in depth — deeper search plays *worse* (d1 48.1%, d2
  36.7%, d3 42.1% vs the heuristic, per `docs/benchmark.md` §6). The known cause
  is an opponent model that cannot switch. Per-move regret against it was cut
  from the benchmark for exactly this reason. Use it as a strong baseline
  opponent, never as ground truth.
- **The format is custom, not a downloadable competitive tier.** 80 Gen-1
  species with full modern movepools, L50, EV/IV/nature spreads, curated item
  catalog, Species/Item/Evasion/OHKO/Sleep clauses, mirror-matched. Teams in
  `data/benchmark-teams.json` were authored *for* it; standard competitive
  intuitions do not transfer cleanly.
- **Elo here is relative within one round-robin.** Bradley-Terry MM, so it is
  order-independent and reproducible — but it is not calibrated against Showdown
  or any external ladder, and not comparable across runs.
- **LLM contestants are not deterministic.** They are not seeded; that is handled
  by Wilson intervals and a reported legality-fallback rate, not pretended away.
- **The team library is deliberately not internally balanced.** That is correct
  for a mirror benchmark (a team only plays itself) but means `cmd/team-validate`
  will report imbalance — it is advisory, and exits 0.

Scope, metrics and the full limitation list live in
[docs/benchmark.md](docs/benchmark.md); it is worth reading before publishing a
number from this repo.
