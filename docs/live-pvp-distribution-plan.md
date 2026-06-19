# Plan: Make live battles instance-independent (dedicated battle-session tier)

**Status:** design handoff — to be implemented by another agent.
**Author of plan:** architecture review session, 2026-06-18.

---

## 0. The problem, stated precisely

Live battles (`live` = human vs AI, and `live_pvp` = human/agent vs human/agent)
are coordinated by an **in-process goroutine** on whichever gateway instance
handled the `POST /api/battles`. The live session lives in:

- `internal/httpapi/server.go` — `matches map[string]*pvpMatch` (per-process map)
- `internal/httpapi/pvp.go` — `pvpMatch`, whose `actions/submits/updates` are
  **in-process Go channels**, and `go m.run(s)` (a process-pinned goroutine)

Consequences:

1. **Both WebSockets + the creating POST must land on the same gateway instance.**
   With >1 replica behind a non-affinity LB, a non-owner instance's
   `attachPvPSlot` finds no entry in its local `matches` map and returns
   `"room not found"`. The battle breaks.
2. **No failover.** If the owning instance dies mid-battle, the goroutine +
   channels die; the Redis `BattleState` is orphaned (nothing rehydrates a
   coordinator). The live-pvp doc's own "Known limitations" admits "disconnect
   aborts the match."
3. **The docs overclaim.** `docs/ARCHITECTURE.md` says "any gateway instance can
   serve any battle … reconnects survive instance churn." True for spectating
   (SSE) and the persisted state, **false for live coordination.** Must be fixed.

The single-owner-per-battle model is **sound** (it's why the hot path is
lock-free). The flaw is that ownership is *co-tenant with the edge tier*,
*assumed-not-enforced*, and *non-recoverable*.

---

## 1. Target architecture (the chosen design)

Move live battle coordination **out of the gateway into a dedicated
`battle-session` service**, mirroring how `battle-worker` already owns Quick Sim
execution. The gateway becomes a **pure WebSocket↔broker bridge** for live modes:
it terminates the socket, claims the slot, forwards player actions inbound, and
forwards per-slot frames outbound. It holds **no battle logic and no battle
state** — finally making the "gateway owns no game logic" claim literally true.

```
              POST /battles (live*)                  ai.job / ai.decided
  Browser/Agent ───────────────► gateway ──start job──► battle-session ◄────────► ai-service
        ▲   │  WS (slot frames)      │ (WS bridge)        (owns coordinator,        (unchanged)
        │   │                        │                     runs ResolveTurn)
        │   └── action ──► live.action.{id} (durable) ──────► │
        │                                                     │ publishes:
        └──────── live.frame.{id}.{slot} (topic) ◄────────────┤  turn-resolved.{id} (SSE/spectators)
                  (gateway dynamically binds, forwards)        └  battle-completed.{id} (leaderboard)

  State: BattleState in Redis (unchanged) + Redis ownership lease (new)
```

### Ownership model

- **Election:** `POST /battles` (live modes) publishes a durable
  `live.session.start` job to the **work exchange**. The competing-consumer
  semantics mean **exactly one** `battle-session` instance picks it up and
  becomes the owner. (Quick Sim already uses this pattern for `quicksim.jobs`.)
- **Lease:** on claim, the owner writes a Redis lease key
  `pvp:owner:{battleID} = {instanceID}` with a TTL, renewed on a heartbeat.
  The lease is the source of truth for "who owns this battle" and the hook for
  failover (Phase 3). Belt-and-suspenders against double-claim.
- **Inbound actions (must not be lost):** owner declares a durable queue bound
  to `live.action.{battleID}` on the work (direct) exchange and consumes with
  **manual ack**. Actions carry a `turn` number; the owner **ignores actions for
  an already-resolved turn** (idempotent under redelivery).
- **Outbound frames (loss-tolerant):** owner publishes per-slot `MatchUpdate`
  frames to the **topic events exchange** with key `live.frame.{battleID}.{slot}`.
  The gateway holding that slot's socket **dynamically binds** that key using the
  existing `Hub`/`EventQueue` mechanism (the same pattern already used for
  `*.{battleID}` spectator events) and forwards bytes to the WS. Frame loss is
  recoverable: the full `BattleState` is in Redis, so a reconnecting client
  resyncs.

### Why RabbitMQ for both directions (not Redis pub/sub)

Reuse one transport and the existing dynamic-binding pattern. Redis pub/sub is
**at-most-once** — an action published while the owner is momentarily
unsubscribed is silently lost and the turn stalls to its timeout. Actions need
the durable, ack'd work-queue path. Frames can tolerate loss but riding the
existing topic-exchange + Hub binding is less new surface than adding a second
transport. (If the team later prefers Redis Streams for the action channel,
that's a drop-in swap — the contract in §3 is transport-shaped, not RabbitMQ-specific.)

### What does NOT change

- The engine (`internal/engine`) — still a pure function. **This is what makes
  action idempotency and failover-rehydrate safe.**
- Quick Sim (`battle-worker`), `ai-service`, `leaderboard-worker`.
- Spectator SSE — it consumes `turn-resolved`/`battle-completed` domain events
  via the Hub; the `battle-session` service publishes the same events the
  gateway used to. Unchanged.
- `cache.ClaimSlot` (the per-slot first-claim-wins token guard) — still used,
  now by the gateway bridge before it starts relaying.

---

## 2. Guiding constraints for the implementer

- **Small, independently-green commits**, one logical unit each (repo convention;
  every commit must build + pass `make test`).
- **No co-author trailer** in commits (repo convention).
- **Keep the old in-gateway path working until the cutover commit (C6).** Land
  the new tier behind the scenes, then flip, so no commit leaves live battles
  broken.
- Engine purity is sacrosanct — do not add I/O to `internal/engine`.

---

## 3. New contracts (define these first, in one place)

### `internal/messages/messages.go`
```go
// Job: published by gateway on POST of a live/live_pvp battle. One
// battle-session instance consumes it and becomes the owner.
const QueueLiveSession = "live.session.jobs"
type LiveSessionStart struct {
    BattleID string
    Mode     string            // "live" | "live_pvp"
    Seed     uint64
    P1Name   string
    P2Name   string
    Kinds    [2]string         // "ws" | "ai"
    AITeam   []engine.TeamPick // populated for the AI slot in "live" mode
}
```

### Action channel (work exchange, durable)
Routing key `live.action.{battleID}`. Owner declares+binds a durable queue on claim.
```go
type LiveAction struct {
    BattleID string
    Slot     string         // "p1" | "p2"
    Turn     int            // for idempotent dedup
    Phase    string         // "submit" | "action" | "replace"
    Picks    []engine.TeamPick // when Phase == "submit"
    Action   engine.Action     // when Phase == "action" | "replace"
}
```

### Frame channel (topic events exchange, loss-tolerant)
Routing key `live.frame.{battleID}.{slot}`. Body is the existing
`protocol.MatchUpdate` (already the per-slot frame type — reuse as-is).

### `internal/cache` — ownership lease
```go
func (c *Cache) ClaimBattleOwner(ctx, battleID, instanceID string, ttl time.Duration) (bool, error)
func (c *Cache) RenewBattleOwner(ctx, battleID, instanceID string, ttl time.Duration) (bool, error)
func (c *Cache) GetBattleOwner(ctx, battleID string) (string, error)
func (c *Cache) ReleaseBattleOwner(ctx, battleID, instanceID string) error
```
Implement with `SET key val NX PX ttl` for claim; a Lua compare-and-set for
renew/release so only the holder can renew/release. (miniredis supports these in
tests; verify `SET NX PX` + `EVAL` coverage.)

---

## 4. Commit-by-commit plan

### Phase 0 — Decouple coordination logic from the gateway (no behavior change)

**C1 — Extract the coordinator into a transport-agnostic package.**
- New package `internal/livebattle` (or keep in `httpapi` but behind an interface
  — prefer a new package for a clean tier boundary).
- Move `pvpMatch` + `run` + `runOpenPhase` + `collectActions` /
  `collectReplaceActions` + turn loop + `broadcast`/`send` here.
- Introduce a `SlotTransport` interface the coordinator talks to instead of raw
  channels:
  ```go
  type SlotTransport interface {
      Actions(slot int) <-chan engine.Action      // inbound moves/switches
      Submits(slot int) <-chan []engine.TeamPick   // inbound team picks
      SendFrame(slot int, u protocol.MatchUpdate)   // outbound per-slot frame
      Closed(slot int) <-chan struct{}              // slot disconnected
  }
  ```
- Provide an **in-process implementation** (the current channels) wired in the
  gateway exactly as today.
- **Acceptance:** all existing tests pass unchanged; live battles behave
  identically. This commit is pure refactor.

### Phase 1 — Contracts + Redis lease (no behavior change)

**C2 — Add the message/contract types** from §3 to `internal/messages` and (if
needed) `internal/protocol`. Add the `QueueLiveSession` work-queue declaration to
`mq.declareTopology`. Compile-only; nothing publishes/consumes yet.
- **Acceptance:** builds; `make test` green.

**C3 — Add ownership-lease primitives** to `internal/cache` + unit tests
(miniredis): claim is exclusive, renew only by holder, release only by holder,
TTL expiry frees it.
- **Acceptance:** new cache tests pass.

**C4 — Add broker helpers** for the new channels in `internal/mq`:
- `PublishLiveSession(ctx, LiveSessionStart)` → work exchange.
- `ConsumeLiveSession(ctx, handler)` → competing consumer on `live.session.jobs`.
- `PublishLiveAction` / a per-battle action consumer (`ConsumeLiveActions(ctx, battleID, handler)`)
  that declares+binds a durable queue to `live.action.{battleID}`.
- `PublishFrame(ctx, battleID, slot, MatchUpdate)` → topic events exchange.
- Extend the gateway `Hub` to also bind/forward `live.frame.{battleID}.{slot}`
  keys (generalize the existing `*.{battleID}` binding so a slot-scoped frame
  routes to the right local socket).
- **Acceptance:** mq unit/integration tests (if present) pass; helpers covered.

### Phase 2 — The new service + the bridge (still behind the old path)

**C5 — New `cmd/battle-session` binary.**
- Mirror `cmd/battle-worker/main.go`: load dex, connect PG/Redis/Rabbit, consume
  `live.session.jobs`.
- On a job: `ClaimBattleOwner` (skip if not won — another instance owns it),
  start a heartbeat goroutine renewing the lease, build a **broker-backed
  `SlotTransport`**:
  - `Actions/Submits` channels fed by `ConsumeLiveActions(battleID)` (route by
    `Slot`/`Phase`, dedup by `Turn`).
  - `SendFrame` → `PublishFrame(battleID, slot, …)`.
  - `Closed` → driven by a disconnect signal (see §5).
- Run the extracted `livebattle` coordinator over that transport.
- Move AI driving (`driveAITurn`/`driveAIReplace`/`localAIDecision`) into this
  service: it publishes `ai.job` and consumes `ai.decided` (correlate by job id,
  same as the gateway does today), with the local-heuristic fallback on timeout.
- Persist turns + publish `turn-resolved`/`battle-started`/`battle-completed`
  domain events (move `persistLiveTurn`/`finishLiveBattle` logic here).
- Add to `docker-compose.yml`, `Dockerfile`, `Makefile`, `railway.json`,
  `internal/config` (it needs the same env as battle-worker).
- **Acceptance:** service boots, consumes a hand-published `live.session.start`,
  runs a full AI-vs-AI or scripted battle end-to-end via the broker transport
  (add an integration test, mirroring any existing battle-worker test). The
  gateway still uses the OLD in-process path at this point.

**C6 — Gateway live handlers become bridges (the cutover).**
- `POST /battles` for `live`/`live_pvp`: instead of `startPvPRoom`/`startLiveRoom`
  (in-process), publish `LiveSessionStart`. Keep eager-at-POST semantics so the
  picker deadline starts at create time (the deadline now lives in the session
  service).
- `handlePvPWS` / `handleLiveWS`: after `ClaimSlot`, become a bridge —
  - reader loop: WS frame → `PublishLiveAction{slot, turn, phase, …}`.
  - writer: subscribe (via Hub) to `live.frame.{battleID}.{slot}` → `WriteJSON`.
  - keep the half-open-socket guard (writer sets a past read deadline on exit).
- Delete `matches`/`matchesMu`/`startPvPRoom`/`startLiveRoom`/`attachPvPSlot`/
  the in-process coordinator hosting from the gateway.
- **Acceptance:** with **two gateway replicas** in compose, a battle whose two
  sockets connect to *different* gateways plays to completion. This is the
  headline test — add it. All existing live-battle tests updated to the bridge
  model and green.

### Phase 3 — Failover + docs (the HA payoff)

**C7 — Failover via lease takeover (optional but the real prize).**
- A `battle-session` instance periodically scans for live battles whose lease has
  expired (owner died) and, for those, attempts `ClaimBattleOwner` and
  **rehydrates the coordinator from the Redis `BattleState` + the Postgres turn
  log** (the turn count + state digest let it resume deterministically — engine
  purity guarantees the resumed line is identical).
- Define resume semantics: re-enter the turn loop at the persisted phase;
  in-flight (unacked) actions for the current turn are redelivered by RabbitMQ to
  the new owner's action queue.
- **Acceptance:** kill the owning `battle-session` mid-battle; another instance
  takes over within the lease TTL and the battle completes. Add a chaos-style
  integration test.

**C8 — Fix the docs.**
- `docs/ARCHITECTURE.md`: scope the "any gateway instance can serve any battle"
  claim to spectating + persisted state; document the new `battle-session` tier,
  the ownership lease, and the action/frame channels. Add it to the service
  table and the mermaid topology.
- `docs/live-pvp.md`: replace the single-instance assumptions; update "Known
  limitations" (disconnect/reconnect now possible via Phase 3).
- `README.md`: update the service list (now seven binaries) and the "Under the
  hood" paragraph.
- **Acceptance:** no doc sentence claims a property the code doesn't have.

---

## 5. Edge cases & failure modes the implementer MUST handle

1. **Action loss vs. duplication.** Actions ride the durable ack'd queue; dedup by
   `(battleID, turn, slot)` so a redelivered action after owner restart is a
   no-op. Never default-action a turn just because an action was redelivered.
2. **Disconnect detection across the bridge.** Today a closed in-process channel
   signals "slot gone." Across the broker there's no channel close. Options:
   gateway publishes an explicit `LiveAction{Phase:"disconnect"}` on socket close;
   AND the session service relies on the per-turn timeout as backstop. Decide and
   document. (Current product behavior = disconnect aborts; preserving that is
   acceptable for v1 of this refactor — don't expand scope into reconnect except
   in C7.)
3. **Double-attach race** across instances — `cache.ClaimSlot` already guards
   this; keep it in the bridge before relaying.
4. **Picker-room phase** must relay too: `submit_team` frames become
   `LiveAction{Phase:"submit"}`; the `RoomDeadline` timer now lives in the session
   service. AI sides auto-submit there (as today in `runOpenPhase`).
5. **Frame ordering.** Per-slot frames must arrive in order. RabbitMQ preserves
   order per routing key on a single queue; ensure one consumer/queue per
   slot-binding on the gateway. Client already tolerates a missed frame (resync
   from state), but must not apply frames out of order — include `turn` in the
   frame and have the client ignore stale turns.
6. **Spectators + leaderboard unchanged** — verify SSE still receives
   `turn-resolved` and the leaderboard still gets `battle-completed`, now from the
   session service. Add an assertion to an existing spectator test.
7. **`ai.decided` correlation** moves to the session service; ensure the
   gateway's old AI event-pump is removed and not double-handling.

---

## 6. Testing strategy

- **Unit:** lease primitives (C3), action dedup, frame routing.
- **Integration (single process, miniredis + a real/rabbit test double):** a full
  live_pvp battle driven entirely through the broker transport (C5).
- **The headline test (C6):** two gateway instances, sockets on different
  instances, battle completes. If full multi-process is heavy in CI, simulate
  with two `Server` instances sharing one broker+redis in-test.
- **Chaos (C7):** kill owner mid-battle, assert takeover + completion.
- Keep `make test` green at **every** commit.

---

## 7. Scope notes / descope path

- Phases 0–1 (C1–C4: extracted coordinator + lease + contracts) are **shared with
  the cheaper "lease + in-gateway relay" design.** If the team later decides the
  separate tier isn't worth operating, C1–C4 are not wasted — you can host the
  extracted coordinator inside the gateway behind the same broker transport and
  skip `cmd/battle-session`. The decision point is C5.
- C7 (failover) is the only part that's strictly "HA, not correctness." Phases
  0–2 already deliver the core goal: **any instance serves any battle.** Ship
  C1–C6 + C8 first; C7 can follow.

---

## 8. Definition of done

- [ ] Two+ gateway replicas, no affinity routing, live battles correct when
      sockets land on different instances.
- [ ] Gateway holds no live battle state or coordinator (`matches` map gone).
- [ ] `battle-session` owns live coordination; engine still pure; Quick Sim,
      ai-service, leaderboard, SSE spectating unchanged.
- [ ] (C7) Owner death is survivable via lease takeover + replay.
- [ ] Docs make no false statelessness/any-instance claims.
- [ ] `make test` green; new headline + chaos tests added.
