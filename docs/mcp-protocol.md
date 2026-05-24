# Pv-Claude — the MCP protocol

`pokearena-mcp` is a stdio MCP server that lets an external agent —
Claude Code first, but the protocol is intentionally agent-agnostic —
play a live PokéArena battle against a human, **on exactly the
information a human has**.

It is one presentation layer over the [live-PvP slot
protocol](live-pvp.md). The same battle could be played via the
browser, this MCP server, or a hand-rolled CLI; they all speak the
same wire shape to the gateway.

For the four-step *how to install and use it* guide, see the
[Connect your agent](../README.md#connect-your-agent-pv-claude)
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

All five tools return structured JSON. The SDK turns Go errors into
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
returns whatever the most recent state/turn frame contained. The type
is **`BattleView`, never `BattleState`** — own team in full, opponent's
active Pokémon and revealed moves only. Fog-of-war is enforced by the
*return type*, not by policy the agent has to honor.

Agents should usually call `wait()` instead — `view()` is for the
"what's the current state?" case where you don't want to block.

**Errors:** not joined.

---

### `wait(timeout_seconds: int = 60) → WaitResult`

The long-poll. Blocks until **any of**:

- It's the agent's turn → `{ready: true, view: BattleView}`.
- The battle ends → `{ready: true, terminal: true, view: BattleView}`.
- The timeout elapses → `{ready: false}`.

`timeout_seconds` is clamped to `[1, 120]`, default 60. The ceiling
isn't a "Claude needs reminding" interval — it's a robustness ceiling
so the agent recovers control if the underlying WS dies silently.

**Why long-poll, not notifications:** Claude only acts via tool-call
returns. Server-initiated MCP notifications don't drive the agent
loop; they'd be additive, not a replacement. This isn't a quirk of
MCP — it's a property of how reactive agents work.

**Recommended agent loop:**

```
while True:
    r = wait(60)
    if not r.ready: continue
    if r.terminal: break
    action = decide(r.view)
    act(action)
```

**Errors:** not joined.

---

### `act(action) → ActResult`

Submit the agent's chosen action for the current turn.

```json
{ "kind": "move",   "index": 0 }
{ "kind": "switch", "index": 2 }
```

`index` is the move slot (0..3) for `move`, or the team slot (0..5)
for `switch`. **Validate against the legal action set implied by the
latest view before calling** — the gateway will reject illegal
actions and the rejection arrives as an `error` frame on the next
`wait()` (`act()` returns optimistically the moment the wire write
succeeds).

**Returns:**

```json
{ "accepted": true, "turn": 7 }
```

`accepted: true` means "we wrote it to the wire", not "the gateway
processed it". This matches the gateway's async-ack model — the *result*
of the turn is observed via the next `wait()` returning a turn frame.

**Errors:** not joined / not your turn / battle ended.

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
