# The agent layer — what's in the box vs. what's a client

PokéArena's product is a **battle system**: engine + queues + state
externalization + a gateway WS protocol that any client can speak.
LLM play is **not** a feature of the core services. It happens on the
*client* side of the gateway WS, like any other external trainer.

This doc explains where that boundary lives, why it sits there, and
what we ship for users who want to plug an LLM into a battle.

---

## 1. The boundary

```
┌──── core services (the system) ────┐    ┌──── agent layer (clients) ────┐
│                                    │    │                               │
│  gateway · battle-worker ·         │    │  pokearena-agent              │
│  ai-service (Heuristic +           │ WS │   (reference harness,         │
│  Expectimax) · leaderboard ·       │◄──►│    BYO key, provider-pluggable)│
│  Postgres · Rabbit · Redis         │    │                               │
│                                    │    │  pokearena-mcp                │
│                                    │    │   (adapter for 3rd-party      │
│  NO LLM SDKs · NO API KEYS ·       │    │    MCP clients: Claude Code,  │
│  NO outbound LLM HTTP              │    │    OpenAI Agents, Goose, …)   │
│                                    │    │                               │
└────────────────────────────────────┘    └───────────────────────────────┘
```

Two design rules fall out of this picture:

1. **Core services have no LLM dependency.** No Anthropic / OpenAI /
   Gemini SDKs. No API keys in any service env. No outbound HTTP to a
   provider. `ai-service` runs only programmatic agents (Heuristic,
   Expectimax). The fallback chain is Expectimax → Heuristic →
   Random; there is no LLM rung.
2. **The agent layer is *optional and separately deployable*.** The
   system runs fully without any agent-layer binary present. You can
   ignore everything in this doc and PokéArena still works — you just
   play AI battles against the programmatic agents.

---

## 2. Why we don't keep an in-process LLM agent

An earlier version of the codebase shipped `internal/ai.LLMAgent` — a
direct Anthropic call from inside `ai-service`, plugged into the same
`Agent` interface as the search-based agents and exposed as a
"Nightmare" difficulty in the SPA. We deleted it. The reasons:

| Problem | Consequence |
|---|---|
| **Two LLM surfaces solving the same problem.** `LLMAgent` (single-call, internal) and the MCP tool loop (interactive, external) were two ways for an LLM to choose actions, with two prompt templates, two parsers, two failure modes. | Drift. Reviewers ask "why not just use the MCP tools for the in-process case too?" — no good answer. |
| **Provider lock-in lived in the wrong layer.** Anthropic SDK code inside `internal/ai` meant `ai-service`'s container bundled provider concerns. | Category error. `ai-service` is a *decision service*, not an LLM gateway. |
| **Maintenance tax with no system-design value.** Every Anthropic API change was code churn in a project that isn't about LLM APIs. | The 200 lines hid real cost: provider churn, key rotation, structured-output brittleness, a fallback rung that complicated the harness. |
| **The README had to apologize for it.** A "these are two different things — keep them straight" callout box explained the duplication. | When a design needs an apology, the design is wrong. |

Removing `LLMAgent` resolved all four. The system shrinks; the story
sharpens; the MCP surface stops being a parallel implementation and
becomes *the* LLM surface.

---

## 3. What we ship instead

### `internal/agentloop` — the reusable loop

A small Go package that knows how to play a battle as a trainer
client: connect, claim a slot, wait for turns, render the
`BattleView` into a prompt, parse the LLM's chosen action, submit it,
loop until terminal. It defines:

```go
type LLMClient interface {
    Complete(ctx context.Context, prompt string) (string, error)
}
```

That's the only LLM-shaped thing in the package. No provider SDKs;
just an interface. The loop is also the natural home for
`view`-rendering and action-parsing code that used to live half in
`LLMAgent` and half in the MCP server.

### `cmd/pokearena-agent` — the reference harness

A single binary that imports `internal/agentloop` and a provider
adapter, and plays a battle to completion.

- **Transport: direct WS to gateway.** Not subprocess-MCP. The
  reference harness is *our* code talking *our* wire protocol —
  putting an `stdio→MCP→WS` hop in the middle to "eat our own
  dogfood" would be ideology, not engineering. The gateway WS
  protocol is the contract everything agrees on; both
  `pokearena-agent` and `pokearena-mcp` are clients of it.
- **Provider in v1: Anthropic, behind the `LLMClient` interface.**
  Adding OpenAI-compatible / Gemini / Ollama is a small adapter file
  later. The brittle structured-output parsing lives outside the
  adapter, so providers stay thin.
- **BYO key.** Reads `ANTHROPIC_API_KEY` (or provider-specific equivalent)
  from env. No key ever touches the cloud services.

### `cmd/pokearena-mcp` — the third-party adapter

Stays as-is. Its job is to bridge agents that **speak MCP but not
our WS protocol** (Claude Code, OpenAI Agents, Goose, any future MCP
client) onto the gateway. It is not the way *our* reference harness
talks to the gateway, and it never has been the system's only LLM
surface — it's one of several optional clients.

### Optional follow-on: a hosted reference bot

`pokearena-agent` is also deployable as a long-running cloud worker
that auto-claims open Pv-Agent slots. With it running, a visitor to
the live deployment can click "Watch Claude play" and a bot joins
their battle — zero local setup. **The bot is a separately deployable
artifact; the core system runs fine without it.** That's the whole
point: LLM provider credentials live in *one optional deploy*, not in
the system.

This is the same "same library, two deployment shapes" theme already
used elsewhere in the codebase (the `Agent` harness powers Quick Sim
in-process and `ai-service` over a queue; the engine powers both
batch and real-time turn resolution). Here, the agent loop powers a
user's local CLI *and* a hosted bot.

---

## 4. Trade-offs we accepted

**LLM play now requires either Claude Code + the MCP setup, or
running `pokearena-agent` locally with an API key.** A user with
neither cannot play against an LLM on a freshly-deployed PokéArena.
The hosted-bot follow-on restores zero-friction LLM play for
visitors; until that ships, the friction is real and we're choosing
to accept it for the cleaner architecture.

**`ai-service`'s justification narrows slightly.** It used to handle
both variable-latency LLM round-trips and variable-latency game-tree
search. Now it handles only the latter. Expectimax is still
unbounded enough (different time budgets per battle, branching
factor in the action payoff matrix) that "decision service the
gateway offloads to" remains a real architectural choice — just on
weaker ground than when LLM latency was also in scope.

**We deliberately did not externalize `HeuristicAgent` /
`ExpectimaxAgent`.** They are programmatic, deterministic, and
justify `ai-service`'s existence as a horizontally-scalable,
bounded-budget decision service. Externalizing them would make
`ai-service` a load balancer in front of MCP and weaken the
system-design story. The line we drew is "externalize the *LLM*
concern, keep the *programmatic AI* concern."

---

## 5. What this means in practice

- A reviewer reading `cmd/ai-service`, `internal/ai`, or any core
  service should not find LLM-shaped code. If they do, the rule has
  been broken — flag it.
- A reviewer reading `cmd/pokearena-agent` should find: a tiny
  `main`, a config struct (gateway URL, slot URL, model, provider),
  one provider adapter, and a call into `internal/agentloop`. The
  surface area should feel proportional to "an example you'd
  copy-paste to build your own agent" — because that's what it is.
- A reviewer asking "how do I plug in my own LLM?" gets one of three
  honest answers: use Claude Code through `pokearena-mcp`; write
  your own MCP client (also through `pokearena-mcp`); or run /
  fork `pokearena-agent` with your provider of choice. The
  *product* is the gateway WS protocol and the MCP tools; the agent
  is an example.
