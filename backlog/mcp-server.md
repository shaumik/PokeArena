# Design doc: `pokearena-mcp` — MCP server for Pv-Claude

**Status:** in progress. Skeleton landed (commit 1); tool surface is real,
handlers are stubs returning `errNotImplemented`.
**Owner:** —
**Depends on:** ✓ [[gateway-second-slot]] · ✓ [[join-token-security]] ·
☐ [[disconnect-detection]] (not strictly blocking — a drop aborts the match for
v0, the MCP server can ship without grace and add reconnect later).
**Required by:** [[pv-claude]]

## Implementation ladder (4 commits to a Claude-playable battle)

1. **Skeleton.** ✓ landed.
   `cmd/pokearena-mcp/main.go` + `internal/mcpserver/{server,tools}.go`.
   Five tools registered (`join_battle`, `view`, `wait`, `act`, `leave_battle`)
   with typed In/Out structs and stub handlers. Binary speaks MCP over
   stdio; `tools/list` returns the surface. SDK: `github.com/modelcontextprotocol/go-sdk@v1.6.1`.
2. **Gateway WS client primitives.** Pure transport layer in
   `internal/mcpserver/gwclient.go`: dial `wss://…/api/battles/{id}/play?…`,
   read/write the matchUpdate / wsClientMsg frames, ping/pong, signal
   turn-change and battle-end on channels. Unit-tested with a fake server.
3. **Tool handlers wired through.** `join_battle` opens the WS and awaits
   the first `state` frame; `view` returns the cached BattleView; `wait`
   blocks on the turn-change channel with the timeout cap; `act` sends an
   action frame; `leave_battle` closes. Session state machine (§5)
   enforced here.
4. **End-to-end smoke test.** `cmd/mcp-smoke/main.go` drives the MCP
   server over stdio (as a CommandTransport client) and exercises one
   full battle against the running gateway — mirrors `cmd/pvp-smoke`'s
   role for the gateway-second-slot work.

---

## 1. Purpose

Let any external agent — Claude Code first, but the protocol is intentionally
agent-agnostic — play a live PokéArena battle against a human, **on exactly the
information a human has**, with no special access to the engine's internal state.

The MCP server is the bridge between the agent's tool-call surface and the
gateway's existing WebSocket protocol. It runs **on the user's machine**, not in
our cloud (see Topology below).

---

## 2. Topology

```
┌──────────────── user's machine ─────────────────┐         ┌─── our cloud ───┐
│                                                 │         │                 │
│  Claude Code  ──stdio──►  pokearena-mcp  ──────WSS──────► gateway (slot p2)│
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

**Why on the user's machine:** every production MCP server is a user-side proxy
into a real service. We keep our cloud as a single-purpose WebSocket gateway —
a problem we already understand — instead of operating a multi-tenant MCP
service with its own auth domain. See parent doc on [[pv-claude]] for the full
argument.

**Process model:** one long-running MCP process per user. A single process can
serve **many sequential battles** (join, play, leave, join another). Concurrent
battles in one MCP session are explicitly **not** supported in v1 — adds session
multiplexing for no clear v1 benefit.

---

## 3. Tool surface

All tools return JSON. Errors are returned as MCP errors with a `code` field
(see §6) — the agent should distinguish transient from permanent.

### `join_battle(battle_id: string, join_token: string, slot: "p1"|"p2") → JoinResult`

Binds the MCP session to a battle slot. Opens the underlying WebSocket to
`/api/battles/{battle_id}/play?slot={slot}&token={join_token}`.

**Returns** on successful claim:
```json
{
  "battle_id": "9f2c…",
  "slot": "p2",
  "your_trainer": "Trainer Blue",
  "your_team_preview": [
    { "dex": 25, "species": "Pikachu", "level": 50, "moves": ["Thunderbolt", "Quick Attack", "Iron Tail", "Substitute"] },
    …
  ],
  "opponent_trainer": "Trainer Red",
  "rules": { "max_team_size": 6, "format": "gen1-singles-50" }
}
```

**Errors:** `BATTLE_NOT_FOUND`, `SLOT_TAKEN`, `INVALID_TOKEN`, `BATTLE_ENDED`,
`ALREADY_JOINED` (this session has an active battle — call `leave_battle` first).

**Idempotency:** calling `join_battle` twice on the same session with the same
args is fine (returns the same `JoinResult`). With *different* args, it's
`ALREADY_JOINED`.

---

### `view() → BattleView`

Returns the current fog-of-war view of the battle. Same shape as the
`BattleView` consumed by `internal/ai.LLMAgent` today — own team in full,
opponent's active Pokémon and revealed moves only.

```json
{
  "turn": 7,
  "your_active": {
    "species": "Pikachu", "hp": 84, "max_hp": 120, "status": null,
    "stages": { "atk": 0, "def": 0, "spa": 0, "spd": 0, "spe": 1 },
    "moves": [
      { "name": "Thunderbolt", "type": "electric", "power": 90, "pp": 12, "category": "special" },
      …
    ]
  },
  "opponent_active": {
    "species": "Charizard", "hp_pct": 62, "status": "burn",
    "stages": { "atk": 0, "def": -1, "spa": 0, "spd": 0, "spe": 0 },
    "revealed_moves": ["Flamethrower", "Earthquake"]
  },
  "your_bench": [ …full info… ],
  "opponent_bench_count": 4,
  "last_turn_log": ["Charizard used Flamethrower!", "Pikachu lost 36 HP", …],
  "your_turn": true,
  "legal_actions": [
    { "kind": "move", "index": 0, "name": "Thunderbolt" },
    { "kind": "move", "index": 1, "name": "Quick Attack" },
    { "kind": "switch", "slot": 2, "to": "Blastoise" },
    …
  ]
}
```

**Critical:** the type is **`BattleView`, never `BattleState`**. Fog-of-war is
enforced by the return type, not by policy the agent has to honor.

**Errors:** `NOT_JOINED`, `BATTLE_ENDED`.

**Blocking:** non-blocking. Returns whatever the current view is, including when
it's the opponent's turn (`your_turn: false`). Agents should usually call
`wait()` instead.

---

### `wait(timeout_seconds: int = 60) → WaitResult`

The long-poll. Blocks until **any of**:
- It's the agent's turn → returns `{ready: true, view: BattleView}`.
- The battle ends → returns `{ready: true, terminal: true, view: BattleView, result: {…}}`.
- The timeout elapses → returns `{ready: false}`.

`timeout_seconds` is clamped to `[1, 120]`. Default 60.

**Why 60s default:** see [[pv-claude]] discussion. The MCP↔Claude boundary is
stdio so this could be arbitrarily long, but a ceiling lets the agent recover
control if the underlying WS dies silently or if the human disconnects.

**Errors:** `NOT_JOINED`, `WS_DISCONNECTED` (gateway dropped us mid-wait;
reconnection logic handles transient cases internally, so this means we gave
up).

**Recommended agent loop:**
```python
while True:
    r = wait(60)
    if not r.ready: continue
    if r.terminal: break
    action = decide(r.view)
    act(action)
```

---

### `act(action: Action) → ActResult`

Submit the agent's chosen action for the current turn.

```json
// move:
{ "kind": "move", "index": 0 }
// or by name (server validates against legal_actions):
{ "kind": "move", "name": "Thunderbolt" }
// switch:
{ "kind": "switch", "slot": 2 }
```

**Returns:**
```json
{ "accepted": true, "turn": 7 }
```

**Errors:** `NOT_JOINED`, `NOT_YOUR_TURN`, `ILLEGAL_ACTION` (with details: e.g.
"PP exhausted", "target slot is fainted", "move name not in legal_actions").
`BATTLE_ENDED`.

**Blocking:** non-blocking. Returns as soon as the gateway accepts; the *resolution*
of the turn is observed via the next `wait()`/`view()`.

---

### `leave_battle() → {ok: true}`

Cleanly closes the WS to the gateway. The MCP session can then `join_battle`
again. If the battle is still in progress, this is a **forfeit**.

---

### `propose_team(slots: int, constraints?: object) → Team`  *(post-v1)*

See [[claude-team-drafting]]. Returns a legal team for blind drafting. Out of
v1 scope; listed here so the tool surface is forward-compatible.

---

## 4. Wire protocol to the gateway

The MCP server is a normal WS client of the gateway. The new endpoint shape
(see [[gateway-second-slot]]):

```
WSS /api/battles/{battle_id}/play?slot={p1|p2}&token={join_token}
```

### Frames (JSON, one frame per message)

**Server → client (gateway → MCP):**
```json
{ "type": "state",   "view": { … BattleView … },           "your_turn": true,  "turn": 7 }
{ "type": "turn",    "log": [ … ],  "view": { … },         "your_turn": true,  "turn": 8 }
{ "type": "ended",   "view": { … },  "result": { … } }
{ "type": "ping" }    // keepalive (every 30s)
{ "type": "error",   "code": "…", "message": "…" }
```

**Client → server (MCP → gateway):**
```json
{ "type": "action",  "action": { "kind": "move", "index": 0 } }
{ "type": "pong" }
```

### Keepalive

Both sides send `ping`/`pong` every **30s** of idle. Either side missing 3
consecutive expected pongs closes the connection. Gorilla WebSocket's standard
`SetPingHandler`/`SetPongHandler` covers this — already a generic WS hygiene
need, not MCP-specific.

### Reconnection

If the WS drops while the MCP server still holds a battle:
1. MCP server retries connecting with the same `slot` + `token` immediately, then
   with exponential backoff (250ms, 500ms, 1s, 2s, 4s — max 5 attempts) within
   the 30s grace window.
2. On successful reconnect, the gateway replays the latest `state` frame so the
   MCP server can resume serving `view()`/`wait()`.
3. If all retries fail, the MCP session enters `WS_DISCONNECTED` state. Next
   tool call returns the error; the agent decides whether to `leave_battle` and
   retry `join_battle`, or give up.

---

## 5. Session state machine

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

| Tool            | UNBOUND | JOINED / PLAYING | TERMINAL |
|-----------------|---------|------------------|----------|
| `join_battle`   | ✓       | ALREADY_JOINED   | ✓ (auto-leaves first) |
| `view`          | NOT_JOINED | ✓             | ✓ (returns final view) |
| `wait`          | NOT_JOINED | ✓             | returns `{ready: true, terminal: true}` immediately |
| `act`           | NOT_JOINED | ✓ (with NOT_YOUR_TURN guard) | BATTLE_ENDED |
| `leave_battle`  | no-op   | ✓ (counts as forfeit if PLAYING) | ✓ |

There's no explicit "PLAYING vs JOINED" distinction visible to the agent — the
MCP server tracks it internally so it can answer `wait()` correctly.

---

## 6. Error semantics

Every error has a `code` (machine-readable) and `message` (human-readable).
Agents should switch on the code; the message is for logs.

| Code                | Class      | Retry? | Meaning |
|---------------------|------------|--------|---------|
| `BATTLE_NOT_FOUND`  | permanent  | no     | The battle ID isn't real. |
| `SLOT_TAKEN`        | permanent  | no     | Someone else already claimed this slot. |
| `INVALID_TOKEN`     | permanent  | no     | Token doesn't match. |
| `BATTLE_ENDED`      | permanent  | no     | Battle is over; nothing to do here. |
| `ALREADY_JOINED`    | permanent  | no     | Call `leave_battle` first. |
| `NOT_JOINED`        | permanent  | no     | Agent bug — `join_battle` first. |
| `NOT_YOUR_TURN`     | permanent  | no     | Agent bug — call `wait` first. |
| `ILLEGAL_ACTION`    | permanent  | no     | Agent bug — action wasn't in `legal_actions`. |
| `WS_DISCONNECTED`   | transient  | maybe  | Gave up reconnecting; agent decides what to do. |
| `INTERNAL`          | transient  | yes    | Server hiccup; retry once. |

"Permanent" means retrying the same call with the same args will produce the
same error. The agent's behavior on permanent errors should typically be: log
+ give up + tell the user. Transient errors are worth retrying once.

---

## 7. Lifecycle / process model

- **One process, many battles.** The MCP server starts when Claude Code spawns
  it (via stdio MCP transport) and lives until Claude Code's session ends.
  Between battles, the session is in `UNBOUND` state.
- **No persistent state on disk.** All state is in-memory. If the process dies,
  the WS dies with it; the gateway detects this via WS close and triggers
  disconnect-grace on the slot.
- **Configuration via env or MCP init args:** `POKEARENA_GATEWAY_URL`
  (default `wss://pokearena.example`). No credentials baked in; tokens come
  from `join_battle` args.

---

## 8. Why not these alternatives

| Alternative                                  | Why we said no |
|----------------------------------------------|----------------|
| Hosted multi-tenant MCP server               | Two trust boundaries instead of one; we operate a second service for no UX gain because users already have Claude Code installed (see [[pv-claude]]). |
| One MCP process per battle                   | Process spawn cost, no upside; the long-running model is strictly more flexible. |
| Streaming responses for `wait()`             | MCP tools are unary; streaming isn't idiomatic. The agent loop doesn't consume streams anyway — it consumes tool-call returns. |
| Server-initiated MCP notifications for "your turn" | Notifications don't drive the agent loop. Claude only acts on tool-call returns. Notifications would be additive at best, not a replacement. |
| Polling: `view()` in a tight loop            | Wastes tool-calls and tokens. `wait()` exists exactly to avoid this. |
| `BattleState` exposed instead of `BattleView`| Cheating becomes a policy problem instead of impossible-by-construction. Hard no. |

---

## 9. Forward-compatibility

The tool shape is deliberately presentation-agnostic — every tool has a CLI
analog:

| MCP tool        | CLI equivalent (future) |
|-----------------|--------------------------|
| `join_battle`   | `pokearena join <id> --token=<t> --slot=p2` |
| `view`          | `pokearena view` (prints JSON) |
| `wait`          | `pokearena wait --timeout=60` (blocks, prints JSON when ready) |
| `act`           | `pokearena act --move=Thunderbolt` |
| `leave_battle`  | `pokearena leave` |

A Python RL trainer can speak the same WebSocket protocol directly without
going through MCP or CLI. The protocol is the contract; MCP/CLI are
presentation layers. This is the actual architectural payoff.

---

## 10. Acceptance criteria

- A user installs `pokearena-mcp` (e.g. `claude mcp add pokearena -- npx -y @pokearena/mcp`),
  receives a P2 join URL from a friend's battle, and Claude Code plays the
  battle end-to-end while the user watches in the browser.
- The MCP server never receives the opponent's hidden state.
- Killing `pokearena-mcp` mid-battle resolves to a forfeit within 30s.
- Restarting it mid-battle (within 30s) resumes the same slot cleanly.
- A second MCP client trying to join a claimed slot is rejected with
  `SLOT_TAKEN`, with no leak about whether the token was valid.

---

## 11. Open questions

- **Action format**: index-based (`{kind: "move", index: 0}`) vs name-based
  (`{kind: "move", name: "Thunderbolt"}`). Server should accept both;
  recommendation in `legal_actions` is index for stability across turns. Worth
  thinking about whether the response normalizes the action.
- **`view()` chattiness**: should `view()` cache and return the last received
  `state` frame instantly, or always round-trip to the gateway? Caching is fine
  because state only changes between turns; just invalidate on `turn` frame.
- **Tool descriptions** (the MCP `description` field): worth carefully writing
  so Claude makes good decisions — e.g. `wait`'s description should explicitly
  say "call this between turns, not `view`".
