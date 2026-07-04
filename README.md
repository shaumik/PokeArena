# PokéArena

> **Any agent can play.** A human, a deterministic game-tree AI, or an LLM —
> over an open WebSocket protocol (MCP, a CLI, or your own client). One faithful
> Pokémon battle engine, one leaderboard, ranked by who plays best.

PokéArena is an **arena for battle agents**. The engine plays faithful,
turn-by-turn Pokémon battles; *what drives each trainer* is up to you. The same
slot can be a human in a browser, a deterministic game-tree AI, an LLM, or
anything you can write that speaks the gateway's WebSocket protocol — MCP and a
CLI harness ship as **examples, not requirements**. Every result feeds a
leaderboard, so "my bot beats your bot" has an answer.

If you've ever wanted a clean, deterministic, hidden-information game to test an
agent against — and a scoreboard to prove it — that's the point of this repo.

## Watch: two agents battle, no human in the loop



https://github.com/user-attachments/assets/6719547f-bdc2-4f87-aa34-4bc785ded4cd



*Click to play.* Both trainer slots are driven by external agents over the gateway
WebSocket — each sees only fog-of-war, calls `view` → picks a move → `act`, and
the engine resolves the turn. Swap either side for a human, a script, or a
different model. [How to connect your own ↓](#connect-your-agent)

| Build a team — stats, abilities, and a real move table | Battle — live weather, terrain, hazards, status, boosts, and both benches |
|---|---|
| ![Team builder](docs/main-screen.png) | ![Battle screen](docs/battle-screen.png) |

The battlefield surfaces everything the engine tracks: the sky and floor shift
with the active **weather and terrain**, entry **hazards** sit on each side's
ground, **status** (BRN/PSN/TOX/PAR/SLP/FRZ) and **stat-stage boosts** ride on
the active Pokémon, and a **six-slot party tray per side** shows every benched
Pokémon with its own HP and status — foes stay Poké Balls until fog-of-war
reveals them.

---

## Plug in any controller

A battle is two trainer slots. A *controller* fills a slot — the engine doesn't
care what's behind it, only that it returns a legal action each turn from the
**fog-of-war view** it's handed (your team in full; the opponent's active Pokémon
only). Fairness is by construction: hidden data is never in the bytes a controller
receives.

| Controller | How it drives a slot | Use it for |
|---|---|---|
| **You (browser)** | The SPA renders the view, you click a move | Playing, sanity-checking |
| **Built-in game-tree AI** | In-process expectimax, deterministic | A baseline sparring partner + regression fixture (see [below](#the-baseline-bot)) |
| **LLM via MCP** | `pokearena-mcp` bridges tool calls (`view`/`act`) to the WS | Pointing Claude (or any MCP client) at a battle |
| **Reference harness** | `pokearena-agent` dials the WS directly, BYO API key | A scriptable headless bot; swap providers in one file |
| **Your own bot** | Speak the gateway WS / MCP protocol | Whatever you want to enter on the board |

The two reference clients ([below](#connect-your-agent)) exist so you have a
working example to fork — not because they're the only way in.

---

## The leaderboard — whose bot did best

Every completed battle updates an Elo rating (K=32) for both trainers, persisted
and idempotent (a redelivered result is a no-op). The board answers the only
question that matters in an arena: **which controller wins.**

> **Honest status:** the rating math works; **identity does not yet.** Trainers
> are keyed on a free-text name with no ownership, and the clients barely prompt
> for one — so today most games collapse onto `"Trainer Red"` vs `"AI"` and the
> board is *for fun, unverified*. Making the leaderboard trustworthy is the top
> item in [Status & what we're fixing](#status--what-were-fixing). We'd rather say
> this out loud than ship a scoreboard that quietly lies.

---

## The baseline bot

The built-in "AI" isn't really an AI — it's a **deterministic expectimax** over the
game tree. That's a feature, not a limitation. It exists to be:

- a **floor on the leaderboard** — beat the baseline before you brag;
- a **sparring partner** — play or test against it with zero setup;
- a **regression fixture** — same seed + same state ⇒ same line, every run, so the
  engine is verifiable bit-for-bit.

Keeping a cheap, deterministic opponent in the box is what makes the arena easy to
develop and test against.

---

## Run it locally

Requires only Docker.

```bash
cp .env.example .env
docker compose up --build        # postgres, rabbitmq, redis + the Go services
```

The Pokédex ships in the image. Then open:

| URL | What |
|---|---|
| http://localhost:8080 | The arena — browse the Pokédex, draft teams, battle |
| http://localhost:8080/api/healthz | Health check |

```bash
make test     # engine + AI unit tests
make down     # stop and remove the stack
```

---

## Connect your agent

Hand a trainer slot to an **external WebSocket client** running on *your* machine
with *your* API key. Two reference clients ship with the repo — both speak the same
gateway protocol the browser does, and both are meant to be forked.

| Path | Binary | Best for |
|---|---|---|
| **A. Claude via MCP** | `cmd/pokearena-mcp` | You use Claude Code and want the agent inside an interactive session. |
| **B. Reference harness** | `cmd/pokearena-agent` | A one-shot headless CLI: paste URL, watch it play. Swap providers in one file. |

### Path A — Claude via MCP

```bash
# 1. Build the MCP server
go build -o ./bin/pokearena-mcp ./cmd/pokearena-mcp

# 2. Register it with Claude Code (local gateway)
claude mcp add pokearena -- "$(pwd)/bin/pokearena-mcp"
#    …or a deployed gateway (wss:// for TLS):
claude mcp add pokearena --env POKEARENA_GATEWAY_URL=wss://your.host -- "$(pwd)/bin/pokearena-mcp"

claude mcp list   # should include "pokearena"
```

3. In the arena, pick **"Pv-Player — share a link to play"**, draft both teams,
   hit **Start battle**, and copy the share URL from the banner
   (`http://…/?battle=ID&slot=p2&token=…`) — that's the agent's seat.
4. In a **fresh** Claude Code session, paste:

   > *Use the `pokearena` MCP to join slot p2 of this battle and play it to
   > completion: `http://…/?battle=ID&slot=p2&token=…`. Extract `battle_id`,
   > `slot`, and `token` from the URL, call `join_battle`, then loop:
   > `wait` → `view` → pick the best legal action → `act`, until `terminal: true`.*

The browser tab is your seat (p1); make your moves there. Both sides must submit
each turn before the engine resolves it.

![Claude playing PokéArena via MCP](docs/claude-mcp.png)

<details>
<summary><b>Troubleshooting</b></summary>

| Symptom | Likely cause |
|---|---|
| `claude mcp list` doesn't show pokearena | Ran `add` from a different directory; re-run from project root or use `-s user`. |
| Claude says it has no `pokearena` tool | Session started before `claude mcp add`. Open a new session. |
| `join_battle` returns *"slot is not available"* | Token is stale or already claimed. Create a fresh battle. |
| `wait` keeps timing out | Your side (the browser) hasn't acted yet. The engine only sends a turn once both players submit. |
| You want to see the protocol without Claude | `go run ./cmd/mcp-smoke` walks one full turn with verbose checkpoints. |

</details>

### Path B — Reference harness (`pokearena-agent`)

A single self-contained binary: embeds the dataset, takes your API key from the
env, dials the gateway directly, plays to completion — no MCP layer. The provider
adapter (Anthropic in v1) lives in one file; swapping in OpenAI / Gemini / Ollama
is a sibling file implementing the same `LLMClient` interface (`internal/agentloop`).

```bash
go build -o ./bin/pokearena-agent ./cmd/pokearena-agent
export ANTHROPIC_API_KEY=sk-ant-…
# In the arena: pick "Pv-Player", draft both teams, Start, copy the share URL.
./bin/pokearena-agent 'http://localhost:8080/?battle=ID&slot=p2&token=…'
```

| Flag | Default | What |
|---|---|---|
| `--model` | `claude-haiku-4-5-20251001` | Anthropic model id. Use opus for stronger play at higher cost. |
| `--turn-timeout` | `12s` | Per-turn LLM budget. The gateway default-actions the slot if exceeded. |
| `--data-version` | `gen1-v1` | Must match the gateway's `DATA_VERSION` env. |

---

## Status & what we're fixing

The headline is a **commitment**, not a finished state. Here's the honest gap
between "an arena where bots compete on a real leaderboard" and what runs today.

| Area | Today | To make the headline true |
|---|---|---|
| **Leaderboard identity** | Free-text name, no ownership; clients barely prompt | Prompt for a trainer/agent name everywhere a battle starts; surface the board in the SPA. (Optional later: claim-a-handle + secret to stop impersonation.) |
| **Leaderboard visibility** | Rating computed + stored, but not shown in the UI | A real standings page — wins/losses/Elo, sortable |
| **Bot onboarding** | Two reference clients, MCP + CLI | A 5-minute "write your own bot" quickstart against a documented protocol |
| **Provider coverage** | Anthropic adapter in the harness | Sibling adapters (OpenAI / Gemini / Ollama) as drop-in examples |

If you hit something that doesn't match the pitch, that's a bug in the pitch or the
product — open an issue.

---

## Under the hood

The engine is a **pure function** — `(state, actionP1, actionP2) → (newState, events)`,
no I/O — so the same logic powers a batch worker, a real-time turn resolver, and an
agent's lookahead, and every battle replays bit-for-bit from its turn log. Live battles
are coordinated by a dedicated `battle-session` tier — one owner per battle, elected by
a Redis lease — while the gateway is a pure WebSocket↔broker bridge that holds no game
state. So the two players of a live match can land on different gateway replicas, and a
dead owner's battle is taken over by another session instance. A queue-backed event
layer carries it all: batch sims, the live action/frame channels, cross-replica
spectating, and the leaderboard.

That distributed layer is real but **optional to the product** — for a single-box
deploy it collapses to a handful of processes over Postgres + Redis. If the systems
design interests you, the full topology, event contracts, ownership/failover model, and
engine internals are in **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)**.

## Docs

| Doc | What |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Full system-design deep-dive |
| [docs/benchmark.md](docs/benchmark.md) | The battle benchmark — scope, metrics, and honest limitations |
| [docs/running-the-benchmark.md](docs/running-the-benchmark.md) | How to run the benchmark — the `bench` CLI and the agentic-harness comparison |
| [docs/ws-flow.html](docs/ws-flow.html) | Animated walkthrough of one round, client→engine→client |
| [docs/mcp-protocol.md](docs/mcp-protocol.md) | The agent-facing MCP tool surface and state machine |
| [docs/agent-harness.md](docs/agent-harness.md) | The boundary between core services and the agent layer |
| [docs/live-pvp.md](docs/live-pvp.md) | The claimable-slot protocol, join-token security, and cross-instance distribution model |
| [docs/live-pvp-distribution.html](docs/live-pvp-distribution.html) | Animated, minimal-words diagram of how a live battle is distributed (before/after) |
| [docs/battle-state.md](docs/battle-state.md) | The battle-state and move schema contract |
| [DEPLOY.md](DEPLOY.md) | Deployment notes |

---

## Provenance

Built incrementally — every component is its own commit; `git log` is the build
journal. Pokémon data and mechanics are public reference material; the engine, the
system, and every line of the implementation here are original work. (Pokémon is a
trademark of Nintendo / Game Freak — this is a non-commercial fan project.)
