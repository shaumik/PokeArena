# PokéArena

> A distributed, event-driven Pokémon battle platform — built to demonstrate real system design, not a toy.

PokéArena simulates faithful, turn-by-turn Pokémon battles. It exposes **three battle modes over one engine**:

- **Quick Sim** — fire two teams at a queue; a worker pool resolves the battle AI-vs-AI. *Throughput-optimized.*
- **Live vs AI** — you play turn-by-turn against the internal AI harness over a WebSocket, watching HP bars drain in real time. *Latency-optimized.*
- **Pv-Claude** — you play against Claude Code (or any MCP client) over a second WebSocket slot. The agent runs on the player's machine via a local MCP server, joins the battle like any other trainer, and sees only fog-of-war. *Agent-extensibility showcase.* See [`backlog/mcp-server.md`](backlog/mcp-server.md) for the design doc.

The point of this repository is the **architecture**: queues, event fan-out, externalized session state, a distributed turn state machine, scheduled timeouts, a horizontally scalable AI service, and a clean agent-side protocol that lets *any* external player (LLM, RL agent, scripted bot) drive a battle through the same boundary. The battle engine is deliberately a *solved, verifiable* problem so the focus stays on how the system is built.

| Set up — pick a mode, draft both teams | In battle — HP, type effectiveness, full turn log |
|---|---|
| ![Setup screen](docs/main-screen.png) | ![Battle screen](docs/battle-screen.png) |

![PokéArena architecture](docs/architecture.svg)

---

## Table of contents

- [Why this design](#why-this-design)
- [Architecture](#architecture)
- [The three battle modes](#the-three-battle-modes) — Quick Sim, Live vs AI, Pv-Claude
- [Message topology](#message-topology)
- [Event contracts](#event-contracts)
- [Data model](#data-model)
- [The battle engine](#the-battle-engine)
- [The AI agent harness](#the-ai-agent-harness)
- [Data ingestion](#data-ingestion)
- [Scaling & failure analysis](#scaling--failure-analysis)
- [Tech stack](#tech-stack)
- [Running locally](#running-locally)
- [Connect your agent (Pv-Claude)](#connect-your-agent-pv-claude)
- [Project layout](#project-layout)
- [Provenance](#provenance)

---

## Why this design

A battle is not a request you answer inline. It is **work** — it takes time, it can be watched, and finishing it has *consequences* (ratings change, stats update). That shape is what justifies every component:

| Requirement | Consequence in the design |
|---|---|
| Battles take time; the API must stay responsive | Battles are **jobs on a queue**, not synchronous calls. `POST /battles` returns `202` immediately. |
| One finished battle triggers several unrelated updates | A `battle.completed` **event** fans out to independent consumers (leaderboard, live push). |
| A live battle is a long-lived, interactive session | Battle state is **externalized to Redis**; workers stay stateless and rehydrate per turn. |
| Throughput must scale | Workers are **competing consumers** on one queue — scale = run more worker containers. |
| The AI must not block turn resolution | The AI is a **separate service** consuming its own queue, with a bounded time budget. |
| Crashes must not corrupt or duplicate battles | Turn resolution is **idempotent** and **deterministic** (seeded RNG stored in state). |

The engine itself is a **pure function** — `(state, actionP1, actionP2) → (newState, events)` — with no I/O. That purity is what lets the same logic power both a batch worker and a real-time turn resolver, and what makes every battle perfectly replayable.

---

## Architecture

Six Go binaries built from one module — five long-running cloud services plus one user-side MCP server — over three infrastructure dependencies.

```mermaid
flowchart LR
    subgraph clients[Clients]
        B[Browser SPA]
    end

    subgraph edge[Edge]
        G[gateway<br/>REST + WebSocket + SSE]
    end

    subgraph broker[RabbitMQ]
        WX[(work exchange)]
        EX[(events exchange)]
    end

    subgraph workers[Worker fleet]
        BW[battle-worker]
        AI[ai-service]
        LB[leaderboard-worker]
    end

    subgraph state[State]
        PG[(PostgreSQL<br/>system of record)]
        RD[(Redis<br/>live state + cache)]
    end

    B <-->|HTTP / WS| G
    G -->|publish jobs| WX
    G -->|consume events| EX
    WX --> BW
    WX --> AI
    EX --> LB
    BW -->|publish events| EX
    AI -->|publish events| EX
    BW --- PG
    BW --- RD
    AI --- RD
    LB --- PG
    LB --- RD
    G --- PG
    G --- RD

    ING[ingest job] -.one-shot.-> PG
```

| Service | Type | Responsibility |
|---|---|---|
| **gateway** | long-running | REST API, WebSocket live-battle endpoint, SSE spectating, serves the SPA. Owns *no* game logic. |
| **battle-worker** | long-running | Consumes Quick Sim jobs, simulates whole battles, persists every turn, publishes events. The horizontally scaled core. |
| **ai-service** | long-running | Consumes AI-decision jobs, runs the agent harness under a time budget, returns moves. |
| **leaderboard-worker** | long-running | Consumes `battle.completed`, recomputes Elo, updates the durable + cached leaderboard. |
| **ingest** | one-shot job | Loads the curated Pokémon dataset into PostgreSQL. Re-runnable and idempotent. |
| **pokearena-mcp** | user-side | Stdio MCP server that bridges Claude Code's tool-call surface to the gateway's live-slot WebSocket protocol. Runs on the player's machine, not in the cloud. See [Connect your agent](#connect-your-agent-pv-claude). |

> The **AI harness is a library** (`internal/ai`). `ai-service` is one *deployment* of it for live battles, where decisions must not block turn resolution and must scale independently. `battle-worker` imports the same library directly for Quick Sim, where round-tripping every turn through a queue would be pointless overhead. Same code, two deployment shapes — exactly like the engine.

---

## The three battle modes

### Quick Sim — async, AI vs AI

```mermaid
sequenceDiagram
    actor U as Client
    participant G as gateway
    participant Q as RabbitMQ
    participant W as battle-worker
    participant L as leaderboard-worker
    participant P as PostgreSQL

    U->>G: POST /api/battles (mode=quicksim)
    G->>P: create battle (status=pending)
    G->>Q: publish quicksim job
    G-->>U: 202 Accepted + battleId
    Note over W: competing consumer picks up the job
    W->>P: status=running
    loop every turn
        W->>W: engine.ResolveTurn (AI vs AI, in-process harness)
        W->>P: append battle_turn
        W->>Q: publish turn.resolved event
    end
    W->>P: status=completed, winner set
    W->>Q: publish battle.completed event
    Q->>L: battle.completed
    L->>P: update Elo + W/L
    U->>G: GET /api/battles/{id}  (or SSE stream)
    G-->>U: full result / live turn feed
```

### Live vs AI — real-time, you vs internal harness

```mermaid
sequenceDiagram
    actor U as Player (browser)
    participant G as gateway
    participant R as Redis
    participant Q as RabbitMQ
    participant AI as ai-service

    U->>G: POST /api/battles (mode=live)
    G->>R: initialize battle state
    G-->>U: battleId + ws url
    U->>G: WS connect /play
    G-->>U: initial state
    loop each turn
        U->>G: submit action (WS)
        G->>Q: publish ai.job (correlated by job id)
        AI->>R: load battle state (fog-of-war view)
        AI->>AI: harness.Decide (bounded time budget)
        AI->>Q: publish ai.decided event
        Q->>G: ai.decided (matched by job id)
        G->>G: engine.ResolveTurn — pure, inline
        G->>R: save new state
        G->>Postgres: append turn
        G-->>U: turn result (WS) — HP, log, status
    end
    G->>Q: battle.completed.{battleId}
```

Two design points worth calling out:

- **The gateway resolves the turn inline.** Resolving a turn is a pure, microsecond function call — putting it on a queue would buy latency and moving parts and nothing else. What *is* worth offloading is the AI's decision: a variable-latency game-tree search, or an LLM round-trip. So the gateway offloads exactly that to the `ai-service`, and resolves the turn itself when the reply arrives, correlated by job id. The gateway holds no battle state — it all lives in Redis, so any gateway instance can serve any battle.
- **A turn timer** bounds every decision. If the player idles past it, a default action is filled in; if the `ai-service` is unreachable, the gateway falls back to a local heuristic. A battle can never freeze on a missing decision.

### Pv-Claude — real-time, you vs an external agent

Same engine, same `BattleView`, but the second trainer slot is **claimable by an external WebSocket client** instead of bound to the internal AI. The headline client is Claude Code via a local MCP server; any MCP client (or any WS client speaking the slot protocol) can take the slot.

```mermaid
sequenceDiagram
    actor U as Human (browser)
    participant G as gateway
    participant R as Redis
    participant MCP as pokearena-mcp<br/>(user's machine)
    participant CC as Claude Code

    U->>G: POST /api/battles (mode=live_pvp)
    G->>R: initialize state, mint p1+p2 join tokens
    G-->>U: {battle_id, p1_url, p2_url}
    Note over U: human shares p2_url out-of-band
    U->>G: WS connect to p1 (token)
    CC->>MCP: tool: join_battle(battle_id, p2_token, "p2")
    MCP->>G: WS connect to p2 (token)
    G-->>U: state frame (your_turn=true)
    G-->>MCP: state frame (your_turn=false)
    loop each turn
        U->>G: action (WS)
        G-->>MCP: state (your_turn=true) — unblocks wait()
        CC->>MCP: tool: wait(60) — blocks
        MCP-->>CC: BattleView + your_turn=true
        CC->>MCP: tool: act(action)
        MCP->>G: action (WS)
        G->>G: engine.ResolveTurn (pure, inline)
        G->>R: save state
        G-->>U: turn frame
        G-->>MCP: turn frame
    end
```

Three things to call out:

- **Topology: the MCP server runs on the user's machine, not in our cloud.** Each user's MCP server is single-tenant by construction. Our gateway sees authenticated WS clients with one-use join tokens — a problem we already understand — instead of forcing us to operate a second multi-tenant service with its own auth domain. This matches how every production MCP server is shaped (GitHub MCP, filesystem MCP, etc.: user-side adapter, real service on the network).
- **The MCP server is a fog-of-war proxy, not a privileged client.** Its `view()` tool returns the same `BattleView` struct that the internal `LLMAgent` consumes today — never `BattleState`. Cheating is impossible-by-construction, not a policy the agent has to honor.
- **Long-poll over MCP, by necessity.** Claude only acts via tool calls, and tool calls are unary. The only way to surface "your turn now" without busy-polling is a `wait()` tool that blocks on the server side until the turn arrives (or a timeout, currently 60s — a robustness ceiling, not a "Claude needs reminding" interval). MCP notifications exist but don't drive the agent loop, so they can't replace `wait()`.

The wire-level protocol between *any* trainer client (browser, MCP server, a future CLI, a Python RL trainer) and the gateway is the same. The MCP server is one presentation layer over that protocol; the SPA is another.

Full design doc, including tool surface (`join_battle` / `view` / `wait` / `act` / `leave_battle`), error semantics, state machine, and alternatives considered: [`backlog/mcp-server.md`](backlog/mcp-server.md). Related: [`gateway-second-slot.md`](backlog/gateway-second-slot.md), [`disconnect-detection.md`](backlog/disconnect-detection.md), [`join-token-security.md`](backlog/join-token-security.md).

**Try it:** see [Connect your agent (Pv-Claude)](#connect-your-agent-pv-claude) below for the four-step setup.

---

## Message topology

One broker, two exchanges. Routing keys are `{event}.{battleId}` so consumers can subscribe broadly *or* to a single battle.

```mermaid
flowchart TD
    G[gateway] -->|quicksim.job| WX
    G -->|ai.job| WX
    WX{{pokearena.work<br/>direct exchange}}
    WX --> QS[[quicksim.jobs]]
    WX --> AJ[[ai.jobs]]
    QS --> BW[battle-worker]
    AJ --> AI[ai-service]

    BW -->|turn.resolved.ID<br/>battle.completed.ID| EX
    AI -->|ai.decided.ID| EX
    G -->|battle.completed.ID| EX
    EX{{pokearena.events<br/>topic exchange}}
    EX -->|battle.completed.*| LBQ[[leaderboard.events]]
    LBQ --> LB[leaderboard-worker]
    EX -->|*.ID dynamic bind| GWQ[[gateway.&lt;instance&gt; exclusive]]
    GWQ --> G
```

- **`pokearena.work`** (direct) — competing-consumer work queues. Durable, manual-ack, prefetch-limited. A crashed worker's unacked job is redelivered.
- **`pokearena.events`** (topic) — domain events. `leaderboard-worker` binds the durable `leaderboard.events` queue to `battle.completed.*`. Each `gateway` instance declares an **exclusive, auto-delete** queue and **dynamically binds `*.{battleId}`** when a WebSocket opens — and unbinds on disconnect. Precise routing: an instance receives events only for battles it actually holds connections for.

---

## Event contracts

Events are JSON, published to the topic exchange with routing key
`{event}.{battleId}` — the type and the battle are in the key itself, so a
consumer can bind one battle or every battle of a type.

| Event | Published by | Consumed by | Meaning |
|---|---|---|---|
| `battle-started` | battle-worker | gateway (SSE) | A Quick Sim's turn loop began. |
| `turn-resolved` | battle-worker | gateway (SSE) | One Quick Sim turn; carries the turn log + post-turn state. |
| `ai-decided` | ai-service | gateway | The AI's chosen action for a live battle, correlated by job id. |
| `battle-completed` | battle-worker, gateway | leaderboard-worker | A battle finished; the worker reloads the authoritative record. |

Idempotency: consumers treat events as **at-least-once**. `leaderboard-worker` applies a rating change only if `battle_id` is not already in `rating_applied` (a uniqueness guard), so a redelivered `battle-completed` is a no-op.

---

## Data model

PostgreSQL is the **system of record**. Redis holds only *derived* or *ephemeral* state (live battle state, caches, the leaderboard ZSET) and can be rebuilt from Postgres.

```mermaid
erDiagram
    species ||--o{ species_moves : has
    moves   ||--o{ species_moves : in
    trainers ||--|| ratings : rated_by
    trainers ||--o{ battles : "p1 / p2"
    battles ||--o{ battle_turns : contains

    species {
        int    dex_no PK
        string name
        string type1
        string type2
        int    base_hp
        int    base_atk
        int    base_def
        int    base_spa
        int    base_spd
        int    base_spe
        string data_version
    }
    moves {
        int    id PK
        string name
        string type
        string category
        int    power
        int    accuracy
        int    pp
        int    priority
        jsonb  effect
    }
    species_moves {
        int species_dex FK
        int move_id FK
    }
    trainers {
        uuid   id PK
        string name
    }
    ratings {
        uuid   trainer_id FK
        int    rating
        int    wins
        int    losses
    }
    battles {
        uuid      id PK
        string    mode
        string    status
        bigint    seed
        uuid      p1_trainer FK
        uuid      p2_trainer FK
        jsonb     p1_team
        jsonb     p2_team
        uuid      winner
        int       turn_count
        timestamp created_at
        timestamp completed_at
    }
    battle_turns {
        uuid   battle_id FK
        int    turn_no
        jsonb  p1_action
        jsonb  p2_action
        jsonb  log
        jsonb  state_digest
    }
```

---

## The battle engine

A faithful single-battle engine. Deterministic given its seed; the RNG state is serialized *with* the battle state, so any battle replays bit-for-bit from its turn log.

**Derived stats** (level fixed for fair play, IV 31 / neutral nature):

```
HP   = floor((2·Base + IV) · L / 100) + L + 10
Stat = floor((2·Base + IV) · L / 100) + 5
```

**Damage** (Gen-3+ standard):

```
Damage = (((2·L/5 + 2) · Power · A/D) / 50 + 2) · STAB · Type · Crit · Random · Burn
```

- `A/D` — Attack/Defense for **physical** moves, Sp.Atk/Sp.Def for **special**. Status moves deal no damage.
- `STAB` ×1.5 if the move's type matches the attacker. `Type` is the product over the defender's types ∈ {0, ¼, ½, 1, 2, 4}.
- `Crit` ×1.5 (~1/24). `Random` uniform across 0.85–1.00. `Burn` ×0.5 on physical damage when burned.

**Turn order** — higher Speed first, but **priority bracket** wins (e.g. Quick Attack +1). Ties broken by the seeded RNG.

**Modeled:** full 18×18 type chart, physical/special/status, accuracy & PP, priority, crits, **status conditions** (burn, poison, paralysis, sleep, freeze), **stat stages** (−6…+6), switching on faint. **Out of scope (v1):** abilities, held items, weather — *content breadth*, not *mechanical depth*. The engine exposes pre/post-damage and turn-start modifier hooks so they slot in later without touching the core.

See `internal/engine/engine_test.go` for damage cases validated against published calculations.

---

## The AI agent harness

A **switchable strategy interface** — the engine never knows which agent is plugged in. The human player is itself just an `Agent` whose `Decide()` blocks on WebSocket input.

```go
type Agent interface {
    Decide(view BattleView) (Action, error)
}
```

`BattleView` is **strict fog of war**: own team in full, but only the opponent's *active* Pokémon and its *revealed* moves. There is no cheating mode — the AI plays on exactly the information a human has.

| Agent | Difficulty | How it works |
|---|---|---|
| `RandomAgent` | — | Uniform legal action. Test control + last-resort fallback. |
| `HeuristicAgent` | Easy | Depth-0. Scores actions by expected damage × type multiplier, KO/STAB bonuses, switch-on-bad-matchup. |
| `ExpectimaxAgent` | Hard | Depth-limited search over a **simultaneous-move, stochastic** game: builds the action payoff matrix and takes the **maximin** action; collapses damage rolls to expectation (chance nodes); handles hidden movesets by **determinization**; iterative deepening under a time budget; alpha-beta + transposition table. |
| `LLMAgent` | Nightmare *(optional)* | Claude reasons over the `BattleView` via structured output; gated behind an API key. |

**The harness wraps every agent** with a time budget and a fallback chain `LLM → Expectimax → Heuristic → Random`. `HeuristicAgent` never fails, so a battle can never hang on the AI. Every decision (and any LLM reasoning) is written to the turn log, so replays reproduce AI moves exactly.

> **Internal `LLMAgent` vs Pv-Claude mode.** These are two different things and worth keeping straight. The `LLMAgent` listed above is an *internal* difficulty level: we hold the API key, we call Anthropic from inside `ai-service`, we plug Claude's output into the same `Agent` interface as the search-based agents. **Pv-Claude mode** is architectural: the second trainer slot is a normal WebSocket client, and Claude Code runs on the *player's* machine through an MCP server. We don't hold any credentials; the player's own Claude Code instance is the player. The internal `LLMAgent` is one agent; Pv-Claude is a whole class of external agents that share a wire protocol.

---

## Data ingestion

Pokémon game data is **slowly-changing reference data** — it changes a few times per *decade*, not a feed.

- **Source of truth is a static, versioned dataset**, not a live API. A curated snapshot lives in `data/` (`pokedex.json`, `moves.json`, `typechart.json`), pinned in-repo. **The build has zero network dependency** — it runs offline and in CI.
- **`ingest` is a decoupled, re-runnable job.** It upserts (`INSERT … ON CONFLICT DO UPDATE`) keyed on stable natural keys, tagged with a `data_version`. Re-running converges.
- **Refresh is deliberate, staged and validated:** a refresh loads into a staging schema, validates (type chart still 18×18, every species has stats + ≥1 move, damage spot-checks), and only **promotes on pass**. Bad upstream data can never break a running deployment.
- **Cache invalidation is free:** Redis keys are namespaced by `data_version` (`species:v1:25`). A new version is a new namespace; stale entries age out.

---

## Scaling & failure analysis

| Concern | Mechanism |
|---|---|
| **Throughput** | `battle-worker` / `ai-service` are competing consumers with bounded prefetch. Scale = more replicas. No coordination needed. |
| **Worker crash mid-job** | Job was manual-ack; unacked on disconnect → redelivered. Turn resolution is keyed `(battle_id, turn_no)` and the seeded RNG state lives *in* the saved state → recomputation is **identical**. Safe. |
| **Duplicate events** | Consumers are idempotent (`rating_applied` guard; turn upsert on `(battle_id, turn_no)`). |
| **ai-service down** | The gateway falls back to a local heuristic for the AI's move, so live battles continue. Quick Sim is unaffected (in-process harness). |
| **leaderboard-worker down** | Battles still run; `battle.completed` events queue durably and drain on recovery. |
| **gateway instance dies** | Its exclusive queue auto-deletes; clients reconnect to another instance, which rehydrates battle state from Redis. State outlives the connection. |
| **Redis eviction of live state** | Battle state has a TTL; the durable record + turn log in Postgres can rehydrate an in-progress battle. |
| **Broker backpressure** | Queues are bounded; the gateway sheds load with `503` when depth exceeds a threshold rather than accepting unbounded work. |

---

## Tech stack

| Concern | Choice | Why |
|---|---|---|
| Language | **Go 1.26** | True concurrency for the worker fleet, tiny static binaries, fast cold starts. |
| HTTP router | `go-chi/chi` | Idiomatic, lightweight, middleware-friendly. |
| WebSocket | `gorilla/websocket` | The de-facto standard. |
| Database | **PostgreSQL** + `jackc/pgx` | Relational integrity for the system of record; `jsonb` for flexible team/log blobs. |
| Broker | **RabbitMQ** + `amqp091-go` | Work queues *and* topic fan-out in one broker; per-message ack. |
| Cache / state | **Redis** + `go-redis` | Live battle state, read-through Pokédex cache, leaderboard sorted set. |
| Frontend | Vanilla JS SPA | No build step — keeps the demo dependency-free. |

> Trade-off noted honestly: Python/FastAPI would have been faster to write; Go was chosen because the worker fleet's concurrency story and small images are exactly what this system is about. Concurrency ultimately lives in the *architecture* (scale the workers), so the language choice is about operability, not correctness.

---

## Running locally

Requires only Docker.

```bash
cp .env.example .env
docker compose up --build        # starts postgres, rabbitmq, redis + all 5 services
```

`ingest` runs automatically on first boot and seeds the Pokédex. Then open:

| URL | What |
|---|---|
| http://localhost:8080 | The SPA — browse the Pokédex, build teams, battle |
| http://localhost:8080/api/healthz | Health check |
| http://localhost:15672 | RabbitMQ management UI (`guest`/`guest`) |

```bash
make test     # run the engine + AI unit tests
make down     # stop and remove the stack
```

---

## Connect your agent (Pv-Claude)

`pokearena-mcp` is an [MCP server](https://modelcontextprotocol.io) that
bridges Claude Code's tool-call surface to the gateway's WebSocket protocol.
It runs **on your machine, not in the cloud** — the gateway just sees a
normal WS client with a one-use join token. The same setup works whether
the gateway is `localhost:8080` (you ran `docker compose up`) or a deployed
URL.

### 1. Build the binary

```bash
go build -o ./bin/pokearena-mcp ./cmd/pokearena-mcp
```

> Or `go install ./cmd/pokearena-mcp` to put it on your `$PATH`.

### 2. Register it with Claude Code

Pointed at a local gateway:

```bash
claude mcp add pokearena -- "$(pwd)/bin/pokearena-mcp"
```

Pointed at a deployed gateway (note `wss://` for TLS):

```bash
claude mcp add pokearena \
  --env POKEARENA_GATEWAY_URL=wss://pokearena.example \
  -- "$(pwd)/bin/pokearena-mcp"
```

Verify it loaded:

```bash
claude mcp list   # should include "pokearena"
```

The MCP server is added to the *current project's* config by default
(`-s user` to share it across all projects on your machine).

### 3. Create a battle in the browser

Open the gateway URL, pick **"Pv-Player — share a link to play"**, draft
both teams, hit **Start battle**. The arena view shows a share banner
with a URL like `http://…/?battle=ID&slot=p2&token=…`. Copy it — that's
Claude's seat.

### 4. Hand the URL to Claude Code

Open a **fresh** Claude Code session (one started *before* the `claude mcp add`
won't see the new MCP) and paste a prompt like:

> *Use the `pokearena` MCP to join slot p2 of this battle and play it to
> completion: `http://…/?battle=ID&slot=p2&token=…`. Extract `battle_id`,
> `slot`, and `token` from the URL, call `join_battle`, then loop:
> `wait` → `view` → pick the best legal action → `act`, until you see
> `terminal: true`.*

The browser tab is your seat (p1); make your moves there. Both sides
must submit each turn before the gateway resolves it — Claude will block
in `wait` until you act, and vice versa.

### Troubleshooting

| Symptom | Likely cause |
|---|---|
| `claude mcp list` doesn't show pokearena | Ran the add command from a different directory; either re-run from the project root or use `-s user` for machine-wide scope. |
| Claude says it has no `pokearena` tool | Session was started before `claude mcp add` ran. Open a new session. |
| `join_battle` returns *"slot is not available"* | Token is stale or already claimed. Create a fresh battle in the browser. |
| `wait` keeps timing out | Your side (the browser) hasn't acted yet. The gateway only sends a turn frame once both players have submitted. |
| You want to see the protocol in action without Claude | `go run ./cmd/mcp-smoke` walks one full turn against the running gateway with verbose checkpoints. |

The same binary works for any MCP client (Claude Code is the headline
case; the protocol is agent-agnostic). The full tool surface and design
rationale are in [`backlog/mcp-server.md`](backlog/mcp-server.md).

---

## Project layout

```
cmd/                       # one main.go per binary
  gateway/  battle-worker/  ai-service/  leaderboard-worker/  ingest/
  pokearena-mcp/           # user-side MCP server for Pv-Claude
  pvp-smoke/               # integration test driver for the gateway WS path
  mcp-smoke/               # integration test driver for pokearena-mcp
internal/
  config/      # env-driven config
  domain/      # core types: Species, Move, Pokemon, Battle
  engine/      # the pure battle engine + tests
  ai/          # the agent harness
  store/       # PostgreSQL repositories + migrations
  cache/       # Redis: live state, cache, leaderboard, PvP slot tokens
  mq/          # RabbitMQ: topology, publishers, consumers
  messages/    # versioned event/message schemas
  httpapi/     # gateway handlers, WebSocket, SSE
  mcpserver/   # pokearena-mcp internals: session + gwclient + tools
  protocol/    # shared wire types (gateway ↔ MCP / CLI / RL trainer)
data/          # curated, pinned Pokémon dataset
migrations/    # SQL schema
web/           # the static SPA
docs/          # architecture diagram
backlog/       # active design docs + action items (one .md per item)
```

---

## Provenance

This system was built incrementally — every component is its own commit. `git log` is the build journal: schema, then engine, then AI, then services. A copy-paste would be one giant dump; incremental authorship is not. Pick any file and any function — the design rationale above explains why it exists.

Pokémon data and mechanics are public reference material; the engine, the system, and every line of the implementation here are original work.
