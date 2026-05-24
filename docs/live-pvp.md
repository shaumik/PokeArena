# Live PvP — the claimable-slot protocol

The gateway's `live_pvp` mode treats each trainer slot as a **claimable
WebSocket endpoint**. Anything that can speak the protocol — a browser
tab, a [`pokearena-mcp`](mcp-protocol.md) bridge in front of Claude
Code, a hand-rolled CLI, a Python RL trainer — can take a slot. The
gateway never special-cases any of them.

This is the load-bearing layer under the more visible
[Pv-Claude](mcp-protocol.md) experience: that work is *just* an MCP
adapter on top of the contract described here.

---

## 1. Lifecycle

```
   ┌──────────┐                              ┌──────────────┐
   │ creator  │  POST /api/battles           │   gateway    │
   │ (any     │  mode=live_pvp ────────────► │              │
   │  client) │                              │  mint tokens │
   │          │ ◄──── {battle_id,            │  init state  │
   │          │        p1_url, p2_url}       └──────────────┘
   └──────────┘
        │ share p2_url out-of-band
        ▼
   ┌──────────┐    WSS /api/battles/{id}/play?slot=p1&token=t1   ┌──────────┐
   │  slot 1  │ ──────────────────────────────────────────────►  │ gateway  │
   └──────────┘                                                  │          │
                                                                 │  pair    │
   ┌──────────┐    WSS /api/battles/{id}/play?slot=p2&token=t2   │          │
   │  slot 2  │ ──────────────────────────────────────────────►  │  state   │
   └──────────┘                                                  └──────────┘
        │                                                              │
        ▼                                                              ▼
   each side receives a fog-of-war view, plays to completion or forfeit
```

1. **Create.** `POST /api/battles` with `mode: "live_pvp"`. Difficulty
   fields are explicitly rejected — there's no AI on either side. The
   response carries `battle_id`, `p1_url`, and `p2_url`. Each URL
   embeds a freshly minted, single-use token.
2. **Distribute.** The creator shares `p2_url` (or `p1_url`) with the
   opponent over whatever out-of-band channel makes sense — chat, link,
   QR code. The URL *is* the capability; see §3.
3. **Claim.** Each side opens a WebSocket to its URL. The gateway
   atomically marks the slot claimed (§4) and runs an in-memory
   coordinator that pairs the two sides and drives the turn loop.
4. **Play.** The coordinator broadcasts a `state` frame once both
   slots have attached, then loops `collect actions → resolve →
   broadcast turn`. Each side sees only its own fog-of-war view.
5. **End.** Natural end emits an `end` frame with the winner. A WS
   drop currently aborts the match (see [Known limitations](#known-limitations)).
   Tokens are deleted from Redis on battle end.

---

## 2. URL shape

```
/api/battles/{battle_id}/play?slot={p1|p2}&token={join_token}
```

One route, one validator. The same path serves the single-player
`live` mode too — the presence of the `slot` query param is what
distinguishes them. Path-style slots (`/play/p1`) were tempting but
keeping the slot as a query param lets a future third client type
slot in without growing the route table.

The URL builder lives in [`internal/protocol/pvp.go`](../internal/protocol/pvp.go)
as `PlayPath`. Both the gateway (when issuing URLs to the creator)
and any client constructing its own connect URL must use it — so
the shape can't drift.

---

## 3. Join tokens

Battle IDs end up in URLs, screenshots, logs, and chat apps. They
*cannot* be the capability that grants slot access — they're for
naming, not authorization. The token is.

### Rules

- **32 bytes from CSPRNG**, base64url-encoded. Returned over TLS
  *only* to the battle creator in the create-battle response.
- **First-claim-wins.** The first WebSocket to present a valid token
  claims the slot. Subsequent attempts — valid token, wrong token,
  no token — all return the same opaque error to the client. The
  gateway logs the precise reason; the wire response leaks nothing
  about whether the token, battle, or slot was the failing factor.
- **Treated as passwords.** Never logged. The opaque-error rule
  exists precisely so failure modes can't be probed by an attacker.
- **Per-battle expiry.** Tokens expire when the battle ends. Redis
  TTL is aligned to the battle state's TTL so a gateway restart
  can't strand an in-progress battle's tokens.

### Threat model boundaries

We're **not** designing against:

- A creator pasting `p2_url` into a compromised chat or screenshare.
  That's a "don't share your password in public" problem.
- An attacker who already has the token racing the legitimate
  opponent. The slot goes to whoever connects first; this is
  acceptable given the threat above is the gating concern.

We **are** designing against:

- Probing the failure modes (token vs battle vs slot validity). The
  opaque-error rule blocks this.
- A claimed slot being hijacked by a second client with the same
  token. The atomic claim (§4) blocks this.

### Explicitly out of scope for v1

- Token rotation, separate resume tokens, time-bound expiry of
  unclaimed tokens, rate-limiting failed claims. Revisit only if
  abuse appears.

---

## 4. Slot claim semantics

The claim is atomic via a Lua script in
[`internal/cache/pvp.go`](../internal/cache/pvp.go) (`ClaimSlot`).
Two clients racing the same valid token would otherwise interleave
read-decide-write on the "claimed" flag and both win. The script
collapses the check-and-set into one Redis round trip:

```lua
local stored = redis.call('HGET', KEYS[1], ARGV[1] .. '_token')
if not stored then return 'unknown' end
if stored ~= ARGV[2] then return 'invalid' end
local already = redis.call('HGET', KEYS[1], ARGV[1] .. '_claimed')
if already then return 'taken' end
redis.call('HSET', KEYS[1], ARGV[1] .. '_claimed', '1')
return 'ok'
```

Four failure modes (`unknown`, `invalid`, `taken`, plus any infra
error) — all map to one client-facing message. The gateway logs the
precise reason for operators.

A side effect that's load-bearing: a wrong-token attempt on a slot
must *not* mark the slot as taken. The test
[`TestClaimSlotRejectsWrongTokenWithoutLocking`](../internal/cache/pvp_test.go)
locks that in.

---

## 5. Wire protocol

The shared shapes live in
[`internal/protocol/pvp.go`](../internal/protocol/pvp.go) so the
gateway and every client import the same definitions.

### Server → client

JSON, one frame per WebSocket message:

```json
{ "type": "state",   "view": { … BattleView … }, "turn": 0 }
{ "type": "turn",    "view": { … },  "log": [ … ], "turn": 7 }
{ "type": "end",     "view": { … },  "winner": 0,  "turn": 12 }
{ "type": "info",    "message": "Waiting for opponent to join…" }
{ "type": "error",   "message": "that action is not legal right now" }
```

- `view` carries the fog-of-war `BattleView` (own team in full,
  opponent's active Pokémon and revealed moves only).
- `info` is human-readable status; the agent loop should ignore it
  for control flow.
- `error` is non-fatal — typically an illegal action; the slot
  stays open.

### Client → server

```json
{ "type": "action", "kind": "move",   "index": 0 }
{ "type": "action", "kind": "switch", "index": 2 }
```

`index` is the move slot (0..3) for `move`, or the team slot (0..5)
for `switch`. Validated server-side against the legal action set for
that side at that phase.

---

## 6. The coordinator

One `pvpMatch` goroutine per battle, defined in
[`internal/httpapi/pvp.go`](../internal/httpapi/pvp.go). It owns the
authoritative `BattleState` and the turn loop; the two WS handlers
attach via `Server.attachPvPSlot` and become dumb shuttles (raw
actions in, frames out).

### Per-slot channels

- **`actions`** — handler → coordinator, cap 1. Closing this channel
  is the disconnect signal; the coordinator aborts the match.
- **`updates`** — coordinator → handler writer goroutine, cap 8. A
  burst at start-of-turn (state → info → turn frame) fits the buffer;
  a slow client stalls only itself.

### State machine

```
wait-for-both-attached → broadcast state →
  loop {
    collect actions (both sides) →
    engine.ResolveTurn (pure, inline) →
    broadcast turn →
    if PhaseReplace: collect replace actions → broadcast →
    persist
  } → on end: broadcast "end" → DeletePvPTokens
```

### Half-open WS recovery

The handler's writer goroutine sets a past `ReadDeadline` in its
`defer` so a stuck `ReadJSON` in the reader unblocks. Without this, a
silently-dead TCP connection could leave `actions` never closed and
the coordinator deadlocked on a full `updates` buffer.

### Pairing-phase nil-channel pattern

Waiting for both slots to attach uses the canonical Go
nil-channel-in-select idiom: once a side attaches, the corresponding
local channel alias is set to `nil` so its case never re-fires (a
closed channel is always ready in `select`, which would otherwise
spin-loop on the info-frame send). Caught by `cmd/pvp-smoke` —
exactly the kind of bug unit tests don't catch.

---

## 7. Known limitations

- **Disconnect aborts the match.** No grace, no reconnect. Fine for
  friendly demos; the must-have before strangers play is tracked at
  [#6 Disconnect detection](https://github.com/shaumik/PokeArena/issues/6).
- **The creator picks both teams.** Simplest v0 protocol; a real
  drafting flow where each side picks their own is its own future
  item.
- **Opponent trainer name not shown.** `BattleView` doesn't carry it;
  the UI shows "Opponent". Cheap fix when we revisit fog-of-war
  strictness: add a non-strategic `foe_trainer` field to `BattleView`.

---

## 8. Source pointers

| Concern | Lives in |
|---|---|
| URL builder + wire types | [`internal/protocol/pvp.go`](../internal/protocol/pvp.go) |
| Token mint, storage, atomic claim | [`internal/cache/pvp.go`](../internal/cache/pvp.go) |
| Coordinator + match goroutine | [`internal/httpapi/pvp.go`](../internal/httpapi/pvp.go) |
| WS handler dispatch | [`internal/httpapi/ws.go`](../internal/httpapi/ws.go) |
| End-to-end test (raw WS, both sides) | [`cmd/pvp-smoke/main.go`](../cmd/pvp-smoke/main.go) |
| Cache unit tests | [`internal/cache/pvp_test.go`](../internal/cache/pvp_test.go) |
