# Pv-Agent — the MCP protocol (Claude Code path)

`pokearena-mcp` is a stdio MCP server that lets an external agent —
Claude Code first, but the protocol is intentionally agent-agnostic —
play a live PokéArena battle against a human, **on exactly the
information a human has**.

It is one presentation layer over the [live-PvP slot
protocol](live-pvp.md). The same battle could be played via the
browser, this MCP server, or a hand-rolled CLI; they all speak the
same wire shape to the gateway.

For the four-step *how to install and use it* guide, see the
[Connect your agent](../README.md#connect-your-agent-pv-agent)
section of the README. This doc is the contract.

---

## 1. Topology

```
┌──────────────── user's machine ─────────────────┐         ┌─── our cloud ───┐
│                                                 │         │                 │
│  Claude Code  ──stdio──►  pokearena-mcp  ──────WSS──────►│ gateway (slot p2)│
│  (or any        (long-           │              │         │                 │
│   MCP client)    running         │              │         │                 │
│                  Go process)     │              │         │                 │
│                                                 │         │                 │
└─────────────────────────────────────────────────┘         └─────────────────┘
                                                                     ▲
                                                                     │
                                                              ┌──────┴──────┐
                                                              │  Browser    │
                                                              │  (slot p1)  │
                                                              └─────────────┘
```

**The MCP server runs on the user's machine, not in our cloud.** Every
production MCP server is shaped this way (GitHub MCP, filesystem MCP,
etc.): user-side adapter, real service on the network. We keep our
cloud as a single-purpose WebSocket gateway — a problem we already
understand — instead of operating a second multi-tenant MCP service
with its own auth domain.

**Process model:** one long-running MCP process per user. A single
process serves **many sequential battles** (join, play, leave, join
another). Concurrent battles in one process are explicitly out of
v1 scope — adds session multiplexing for no clear v1 benefit.

---

## 2. Tool surface

The battle tools return structured JSON. Offline play starts with `start_battle`,
then `submit_team`; live play starts with `join_battle`. Reference tools
(`get_pokemon`, `find_pokemon`, `list_items`, `list_natures`) support team building. The SDK turns Go errors into
MCP error responses with `isError: true`; the agent should switch on
the message content to distinguish cases.

### `join_battle(battle_id, slot, join_token) → JoinResult`

Binds the MCP session to a battle slot. Opens the underlying WebSocket
to `/api/battles/{battle_id}/play?slot={slot}&token={join_token}` and
blocks until the gateway sends the first `state` frame.

**Returns** on successful claim:

```json
{
  "battle_id": "9f2c…",
  "slot": "p2",
  "your_trainer": "Blue",
  "initial_view": { … BattleView … }
}
```

**Errors:** slot taken / invalid token / battle not found (collapsed
to one opaque message per the slot protocol's threat model — see
[live-pvp.md §3](live-pvp.md#3-join-tokens)); `errAlreadyJoined`
(this session has an active battle, call `leave_battle` first).

**Idempotency:** calling `join_battle` twice on the same session with
the same args is fine (you'll get `errAlreadyJoined`; the existing
session is unaffected).

---

### `view() → BattleView`

Returns the current fog-of-war view of the battle. Non-blocking;
returns the most recent state/turn view plus `recent_log` (defined below). The type
is **`BattleView`, never `BattleState`** — own team in full, opponent's
active Pokémon and revealed moves only. Fog-of-war is enforced by the
*return type*, not by policy the agent has to honor.

Agents should usually call `wait()` instead — `view()` is for the
"what's the current state?" case where you don't want to block.

**Errors:** not joined.

---

### `wait(timeout_seconds: int = 60) → WaitResult`

The long-poll. Blocks until **any of**:

- It's the agent's turn → `{ready: true, view: BattleView, recent_log: [...]}`.
- The battle ends → `{ready: true, terminal: true, view: BattleView, recent_log: [...], winner, outcome}` (winner/outcome may be absent on abandonment).
- The timeout elapses → `{ready: false, recent_log: []}`.

`timeout_seconds` is clamped to `[1, 120]`, default 60. The ceiling
isn't a "Claude needs reminding" interval — it's a robustness ceiling
so the agent recovers control if the underlying WS dies silently.

**Why long-poll, not notifications:** Claude only acts via tool-call
returns. Server-initiated MCP notifications don't drive the agent
loop; they'd be additive, not a replacement. This isn't a quirk of
MCP — it's a property of how reactive agents work.

**Recommended agent loop:**

```python
r = wait(60)  # first turn, after team submission
while not r.get("terminal", False):
    if not r["ready"]:
        r = wait(60)
        continue
    observe(r["recent_log"])
    action = decide(r["view"]["legal_actions"], r["view"])
    r = act(**action)
```

**Errors:** not joined.

---

### `act(kind, index, switch_target?, wait_seconds=60) → ActResult`

Copy an entry from `view.legal_actions`. The list contains `kind` and `index`,
and may include `switch_target` for a pivot move such as U-turn:

```json
{ "kind": "move", "index": 0, "switch_target": 2 }
{ "kind": "switch", "index": 2 }
{ "kind": "move", "index": -1 }
```

Move indices are normally 0–3; -1 is the engine's sentinel for Struggle or a
forced spent turn such as recharge. Switch indices refer to the team's slots.
`switch_target` chooses which teammate a self-switch move brings in. Copy the
whole action, including that field when present.

`act` sends the action and waits for the next decision or terminal result. Its
response includes `accepted`, the submitted `turn`, and the same `ready`,
`terminal`, `view`, `recent_log`, `error`, `winner`, and `outcome` fields as
`wait`. `accepted: true` means the action was sent; a subsequent rejection is
returned as `error` with the view, allowing an immediate retry. If `ready` is
false, call `wait` to resume; do not resubmit the action.

**Errors:** not joined / not your turn / battle ended.

### Battle-view additions

- **`legal_actions`** is computed by the engine with the authoritative dex and
  battle state before redaction. It includes Choice locks, status-move
  restrictions, forced moves and pivot destinations. During a replacement,
  only the side that must replace has actions, and they are switches. An ended
  battle has `[]`. A snapshot is not a reservation: the server remains the
  authority when an action arrives.
- **Move metadata** appears on every own-team move and each revealed opponent
  move: `bp`, `accuracy`, `type`, and `category` (`physical`, `special`,
  `status`). These are public dex values, not effective damage or hit chances;
  accuracy 0 denotes bypassing accuracy checks. Own moves retain `pp` and
  `max_pp`; opponent moves omit both. Unrevealed opponent slots remain
  `{ "move_id": "" }` with no metadata.

These fields are generated by both the offline opponent and live battle
coordinator. MCP forwards the gateway's redacted view, so it does not need to
reconstruct legality or guess metadata from a separate local dataset.

### `recent_log`: events since the last submitted action

`wait` and `act` return `recent_log` beside `view`. The non-blocking `view`
tool keeps its existing flat battle-view shape and adds `recent_log` at the
same level as `turn` and `self`.

Each entry is `{ "type": "move", "side": 0, "text": "Charizard used Flamethrower!" }`.
Side is 0 or 1, or -1 for a neutral event. Entries retain engine order;
replacement frames accumulate instead of replacing the previous batch. The
final outcome includes its final events. Initial views and timeout responses
use an empty array rather than null.

Reading does not consume the log: repeated `view`/ready `wait` calls show the
same history until another action is submitted. Submitting an action clears
that history before new frames arrive, so an `act` result describes the new
attempt. A rejected action with no new events returns `[]`. Leaving or joining
a different battle resets the history. Clients should not count repeated
reads as new events.

Core event types include `turn`, `switch`, `force-switch`, `move`, `damage`,
`crit`, `effective`, `resisted`, `miss`, `immune`, `faint`, `status`, `stat`,
`heal`, `recoil`, `fail`, `cant`, and `win`. Other types describe mechanics
such as `ability`, `item`, `weather`, `terrain`, `hazard`, `screen`, `charge`,
and `protect`. Treat unknown types as displayable events; the vocabulary is
extensible. `text` is human-readable, not a stable structured damage schema.

---

### `leave_battle() → {ok: true}`

Cleanly closes the WebSocket to the gateway. The MCP session can
then `join_battle` again. If the battle is still in progress, this
is a **forfeit**.

---

## 3. Session state machine

```
   ┌──────────┐  join_battle(ok)   ┌────────────┐  (turn changes) ┌──────────┐
   │ UNBOUND  │ ─────────────────► │   JOINED   │ ◄─────────────► │ PLAYING  │
   └──────────┘                    └────────────┘                 └──────────┘
        ▲                                │ │                            │
        │      leave_battle              │ │ battle ends                │
        └────────────────────────────────┘ ▼                            │
                                       ┌──────────┐                     │
                                       │ TERMINAL │ ◄───────────────────┘
                                       └──────────┘
                                            │ leave_battle (auto-allowed)
                                            ▼
                                       (back to UNBOUND)
```

| Tool            | UNBOUND | JOINED / PLAYING            | TERMINAL                                            |
|-----------------|---------|-----------------------------|-----------------------------------------------------|
| `join_battle`   | ✓       | errAlreadyJoined            | ✓ (auto-leaves first)                               |
| `view`          | error   | ✓                           | ✓ (returns final view)                              |
| `wait`          | error   | ✓                           | returns `{ready: true, terminal: true}` immediately |
| `act`           | error   | ✓ (with not-your-turn guard)| errBattleEnded                                      |
| `leave_battle`  | no-op   | ✓ (forfeit if PLAYING)      | ✓                                                   |

There's no explicit PLAYING vs JOINED distinction visible to the agent
— the session tracks it internally to answer `wait()` correctly.

The state machine lives in [`internal/mcpserver/session.go`](../internal/mcpserver/session.go).

---

## 4. Why these tools (and not others)

| Alternative                                          | Why no |
|------------------------------------------------------|--------|
| Hosted multi-tenant MCP server                       | Two trust boundaries instead of one; we'd operate a second service for no UX gain because users already have Claude Code installed. |
| One MCP process per battle                           | Process spawn cost, no upside; the long-running model is strictly more flexible. |
| Streaming responses for `wait()`                     | MCP tools are unary; streaming isn't idiomatic. The agent loop doesn't consume streams anyway — it consumes tool-call returns. |
| Server-initiated MCP notifications for "your turn"   | Notifications don't drive the agent loop. Claude only acts on tool-call returns. |
| Polling: `view()` in a tight loop                    | Wastes tool-calls and tokens. `wait()` exists exactly to avoid this. |
| `BattleState` exposed instead of `BattleView`        | Cheating becomes a policy problem instead of impossible-by-construction. Hard no. |

---

## 5. Forward compatibility

The tool shape is deliberately presentation-agnostic. Every tool has a
CLI analog:

| MCP tool        | CLI equivalent (future)                                          |
|-----------------|------------------------------------------------------------------|
| `join_battle`   | `pokearena join <id> --token=<t> --slot=p2`                      |
| `view`          | `pokearena view` (prints JSON)                                   |
| `wait`          | `pokearena wait --timeout=60` (blocks, prints JSON when ready)   |
| `act`           | `pokearena act --move=Thunderbolt` (or `--move-index=0`)         |
| `leave_battle`  | `pokearena leave`                                                |

A Python RL trainer can speak the WebSocket protocol from
[live-pvp.md](live-pvp.md) directly without going through MCP *or*
CLI. **The protocol is the contract; MCP and CLI are presentation
layers.** That's the architectural payoff.

---

## 6. Source pointers

| Concern                            | Lives in |
|------------------------------------|----------|
| Binary entry, signal handling, env | [`cmd/pokearena-mcp/main.go`](../cmd/pokearena-mcp/main.go) |
| Server + tool registration         | [`internal/mcpserver/server.go`](../internal/mcpserver/server.go) |
| Typed In/Out structs + handlers    | [`internal/mcpserver/tools.go`](../internal/mcpserver/tools.go) |
| Session state machine + dispatcher | [`internal/mcpserver/session.go`](../internal/mcpserver/session.go) |
| Gateway WS client transport        | [`internal/mcpserver/gwclient.go`](../internal/mcpserver/gwclient.go) |
| Shared wire types (with gateway)   | [`internal/protocol/pvp.go`](../internal/protocol/pvp.go) |
| End-to-end stdio smoke test        | [`cmd/mcp-smoke/main.go`](../cmd/mcp-smoke/main.go) |
| Unit tests (race-clean)            | [`internal/mcpserver/*_test.go`](../internal/mcpserver/) |
