# Live PvP — the claimable-slot protocol

The gateway's `live_pvp` mode treats each trainer slot as a **claimable
WebSocket endpoint**. Anything that can speak the protocol — a browser
tab, a [`pokearena-mcp`](mcp-protocol.md) bridge in front of Claude
Code, a hand-rolled CLI, a Python RL trainer — can take a slot. The
gateway never special-cases any of them.

This is the load-bearing layer under the more visible
[Pv-Agent](mcp-protocol.md) experience: that work is *just* an MCP
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
3. **Claim.** Each side opens a WebSocket to its URL — *to any gateway
   replica.* The gateway atomically marks the slot claimed (§4) and
   becomes a bridge: it forwards client messages to the broker and the
   battle's frames back to the socket. The coordinator itself runs in a
   `battle-session` instance, elected per battle by an ownership lease
   (see §6 and [ARCHITECTURE.md](ARCHITECTURE.md)).
4. **Play.** The coordinator broadcasts a `state` frame once both
   slots have attached, then loops `collect actions → resolve →
   broadcast turn`. Each side sees only its own fog-of-war view. The two
   sockets need not be on the same gateway.
5. **End.** Natural end emits an `end` frame with the winner. A WS
   drop currently aborts the match (see [Known limitations](#known-limitations));
   a *server* death no longer does — another session instance takes the
   battle over by lease. Tokens are deleted from Redis on battle end.

---

## 2. URL shape

```
/api/battles/{battle_id}/play?slot={p1|p2}&token={join_token}[&name={trainer}]
```

One route, one validator. The same path serves the single-player
`live` mode too — the presence of the `slot` query param is what
distinguishes them. Path-style slots (`/play/p1`) were tempting but
keeping the slot as a query param lets a future third client type
slot in without growing the route table.

`name` is optional and is the joiner's **self-declared trainer name**
— the leaderboard key this slot's result posts under. It is a query
param on the join rather than a field in the create body because the
battle is created before its players arrive: the creator names both
slots, and an agent joining `p2` would otherwise be recorded forever as
whatever placeholder they typed. Declaring it rebinds the battle row's
trainer for that slot (`store.RebindBattleTrainer`) and relabels the
slot in room frames and engine state. Omit it to keep the creator's
name. Unlike the other three params it can contain spaces and `&`, so
`PlayPath` query-escapes it; the gateway re-sanitizes on arrival
(trim, drop control characters, cap at 24 runes).

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

- **Impersonation on the leaderboard.** The `name` on a join is
  self-reported: holding the slot token is the whole permission, and any
  name may be claimed, including one already on the board. This is a
  deliberate v1 position — attribution first, verification later — and
  the reason the README calls the board "for fun, unverified." The fix
  is claim-a-handle + secret, not a patch to this protocol. Note the
  name is only accepted *after* the slot claim succeeds, so it is at
  least no easier to forge than playing the battle would be.

We **are** designing against:

- Probing the failure modes (token vs battle vs slot validity). The
  opaque-error rule blocks this.
- A claimed slot being hijacked by a second client with the same
  token. The atomic claim (§4) blocks this.
- Renaming a settled result. `RebindBattleTrainer` refuses a battle
  that already has a winner, so a late or replayed join cannot move a
  rating that was computed against the previous trainer.

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

One `livebattle.Match` per battle, defined in
[`internal/livebattle`](../internal/livebattle). It owns the
authoritative `BattleState` and the turn loop. The coordinator is
**transport-agnostic**: it talks to its slots through a `FrameSink`
(outbound frames) and a `Producer` (inbound actions/submissions), so
the *same* code runs whether those channels are backed by in-process
WebSockets or by a message broker.

### State machine

```
wait-for-both-attached → broadcast state →
  loop {
    collect actions (both sides) →
    engine.ResolveTurn (pure) →
    broadcast turn →
    while PhaseReplace: collect replace actions → broadcast →
    persist
  } → on end: broadcast "end" → DeletePvPTokens
```

A resumed (failover) coordinator skips the picker phase, re-broadcasts
`state`, and re-enters the loop at the persisted turn — engine purity
makes the resumed line identical.

### Distribution across instances

In production the coordinator runs in a **`battle-session`** instance,
not the gateway. The flow:

- **Ownership.** `POST /battles` publishes a `live.session.jobs` work
  item; competing consumers elect one owner, which takes a Redis lease
  `pvp:owner:{battleId}` renewed on a heartbeat.
- **Inbound actions** ride a durable, ack'd per-battle queue
  `live.action.{battleId}` (a lost move would stall a turn). Each action
  carries the turn it answers, so a redelivery to a failover owner is
  dropped rather than double-applied.
- **Outbound frames** are published per slot to the topic events
  exchange as `live.frame.{battleId}.{slot}` (transient — a lost frame
  resyncs from the persisted state). The gateway binds the slot it
  bridges and forwards the bytes to the socket.
- **The gateway is a pure bridge.** It claims the slot (§4), publishes
  the client's messages as `LiveAction`s, and writes inbound frames to
  the WS. It holds no state and no game logic — so the two sockets of a
  battle may connect to *different* gateways.
- **Failover.** If the owner dies, its lease expires; another
  `battle-session` scan reclaims the battle, rehydrates the coordinator
  from the persisted `BattleState`, and resumes. The reconnected gateway
  bridges never learn ownership changed — they're still publishing to the
  same action queue and bound to the same frame keys.

### Half-open WS recovery

The gateway bridge's writer goroutine sets a past `ReadDeadline` in its
`defer` so a stuck `ReadJSON` in the reader unblocks. Without this, a
silently-dead TCP connection could leak the slot.

---

## 7. Known limitations

- **Client disconnect aborts the match.** No grace, no *client*
  reconnect. (A *server*/owner death is now survivable — see §6
  Failover.) The reconnect grace window before strangers play is tracked
  at [#6 Disconnect detection](https://github.com/shaumik/PokeArena/issues/6).
- **Brief double-owner window on false-positive failover.** A transient
  Redis blip that lapses a lease can let another instance take over while
  the original is still alive; the original yields on its next heartbeat
  (≤ one renew interval). The lease TTL is sized well above the renew
  interval to make this rare.
- **The creator picks both teams.** Simplest v0 protocol; a real
  drafting flow where each side picks their own is its own future item.
- **Opponent trainer name not shown.** `BattleView` doesn't carry it;
  the UI shows "Opponent". Cheap fix when we revisit fog-of-war
  strictness: add a non-strategic `foe_trainer` field to `BattleView`.

---

## 8. Source pointers

| Concern | Lives in |
|---|---|
| URL builder + wire types | [`internal/protocol/pvp.go`](../internal/protocol/pvp.go) |
| Token mint, storage, atomic claim | [`internal/cache/pvp.go`](../internal/cache/pvp.go) |
| Ownership lease | [`internal/cache/lease.go`](../internal/cache/lease.go) |
| Transport-agnostic coordinator | [`internal/livebattle`](../internal/livebattle) |
| Session tier: own / lease / resume | [`internal/session`](../internal/session) |
| Live channels: session jobs, actions, frames | [`internal/mq/live.go`](../internal/mq/live.go) |
| Gateway WS↔broker bridge | [`internal/httpapi/ws.go`](../internal/httpapi/ws.go) |
| End-to-end test (raw WS, both sides) | [`cmd/pvp-smoke/main.go`](../cmd/pvp-smoke/main.go) |
| Cross-instance + failover tests | [`internal/session`](../internal/session) (`distribution_test.go`, `failover_test.go`) |
| Cache unit tests | [`internal/cache/pvp_test.go`](../internal/cache/pvp_test.go) |
