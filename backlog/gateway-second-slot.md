# Gateway: claimable second trainer slot

**Status:** done — server-side coordinator + SPA wiring landed. Two browser tabs play end-to-end.

**Why:** Today's `live` mode auto-binds slot 2 to a local AI. For Pv-Claude (and any
future Pv-anything), slot 2 must be claimable by an external WS client instead. The
gateway should not grow a Claude-specific code path — it should grow a generic
"second-slot open" path that *any* WS client can take, including a future second
human player.

## Layered plan (4 commits to "two browser tabs play each other")

1. **Token + slot primitives + createBattle accepts `live_pvp`.** ✓ landed.
   - `internal/cache/pvp.go`: `GenerateToken`, `SavePvPTokens`, `ClaimSlot` (atomic
     via Lua to avoid the read-decide-write race two clients could exploit),
     `DeletePvPTokens`. Token = 32 bytes from CSPRNG, base64url.
   - `internal/httpapi/server.go`: `mode=live_pvp` accepted; difficulty fields
     rejected (no AI on either side); response is
     `{battle_id, mode, p1_url, p2_url}` with embedded tokens. `playURL` helper
     centralizes the WS path shape so the gateway and the future MCP server
     can't drift on it.
   - Schema comment updated; no migration needed (mode is `TEXT NOT NULL`, no
     CHECK constraint).
2. **WS handler dispatches on `?slot=`.** ✓ landed.
   - `handleWS` is now a tiny dispatcher; the legacy single-player body is
     renamed `handleLiveWS`; new sibling `handlePvPWS` claims the slot,
     sends the fog-of-war view (via the same `ai.MakeView` the internal
     `LLMAgent` uses), and reads actions in a loop. Actions are validated
     against `engine.LegalActions` for the right side and acked — pairing
     and resolution come with the coordinator in commit 3.
   - `cache.ReleaseSlot` added; called on every WS exit path so a flaky
     client can reconnect (identity-bound grace is a separate item).
   - Error opacity enforced: all four `ClaimSlot` failure modes collapse to
     one client message; the operator gets the precise reason via log.
   - `miniredis` brought in as a test dep; `cache/pvp_test.go` covers
     token generation, first-claim-wins, wrong-token rejection (and that
     a wrong attempt doesn't lock the slot), unknown-battle/slot, slot
     independence, release-allows-reclaim, TTL alignment.
3. **The `pvpMatch` coordinator goroutine.** ✓ landed.
   - `internal/httpapi/pvp.go`: `pvpMatch` owns the authoritative state and
     the turn loop. WS handlers attach via `Server.attachPvPSlot`; the
     handler becomes a dumb shuttle (raw actions in, frames out) and the
     coordinator owns validation, resolution, and broadcast.
   - Per-slot channels: `actions` (cap 1, handler → coordinator),
     `updates` (cap 8, coordinator → handler writer goroutine). Closed
     `actions` is the canonical disconnect signal — coordinator aborts.
   - State machine: wait-for-both-attached (with 5min cap + disconnect
     detection during wait) → broadcast state → loop {collectActions →
     ResolveTurn → broadcast turn → if PhaseReplace, collectReplaceActions
     → ResolveReplace → broadcast → persist}. On natural end: broadcast
     "end" with winner, call finishLiveBattle, DeletePvPTokens.
   - Half-open WS fix in the handler: writer goroutine sets a past
     ReadDeadline on exit so a stuck `ReadJSON` unblocks. Otherwise a
     half-open conn could leave actions never closed and the coordinator
     deadlocked on a full updates buffer.
   - Disconnect grace + reconnect = [[disconnect-detection]] (separate
     item). For v0, disconnect = match aborts; reconnect spawns a fresh
     match from the latest persisted state, losing only the in-progress
     turn.
4. **SPA changes.** ✓ landed.
   - `web/index.html`: new `live_pvp` mode option; difficulty label
     wrapped so it can be hidden when the mode doesn't need it.
   - `web/app.js`: mode-change listener hides difficulty for pvp.
     `tryAutoJoin()` on init reads `?battle=…&slot=…&token=…` and goes
     straight to the arena. `startBattle` drops difficulty fields for
     `live_pvp`. `enterArena` handles the third mode (share banner +
     `connectPvPWS`). `handlePvPWSMessage` understands the new wire
     shape (BattleView, not BattleState; matchUpdate's `state` / `turn`
     / `end` / `info` / `error`). `viewToRenderableState` adapts a
     fog-of-war View into a state-shaped object the existing renderer
     consumes — opponent gets `[Foe, ...?, ...?]` placeholders for the
     bench-alive count; we never invent fainted info we don't have.
     `showShareBanner` builds the page join URL from the gateway's WS
     URL and offers clipboard copy.
   - `web/style.css`: share-banner styling.
   - `internal/ai/agent.go`: `View` got JSON tags. It's wire-protocol
     now (pvp WS + future MCP server), and lowercase snake_case
     matches the engine types.

## Known v0 limitations (each tracked separately)

- **Disconnect = match aborts.** Reconnect spawns a fresh match from
  the latest persisted state (in-progress turn's actions are lost).
  Proper grace lives in [[disconnect-detection]].
- **Creator picks both teams.** The joiner has no agency over their
  team composition for now — the simplest v0 protocol. A real lobby
  flow where each side drafts their own team is its own backlog item
  (not yet filed; add when needed).
- **Opponent trainer name not shown.** BattleView doesn't carry it;
  UI shows "Opponent". Cheap fix: add a non-strategic
  `foe_trainer` field to View when we revisit fog-of-war strictness.
- **No WS-level integration tests.** Cache primitives are tested via
  miniredis; the coordinator and the handler are exercised only by
  manual two-tab play. Worth a small test-infra commit before MCP
  work lands.

## Decisions locked in

- **URL shape:** `/api/battles/{id}/play?slot=p1|p2&token=…`. One handler, one
  validator. Path-style slot was tempting but query params keep the route
  table flat and let the same handler serve `live` (no `slot`) and `live_pvp`.
- **Token storage:** Redis hash `battle:{id}:slots` with TTL aligned to the
  battle state. A gateway restart can't strand an in-progress battle's tokens.
- **Mode name:** `live_pvp`. Existing `live` mode untouched.
- **Difficulty fields rejected for `live_pvp`:** defaulting fields that have
  no effect is a footgun; the API rejects the request so the contract is
  taught immediately.
- **Error opacity:** `ClaimSlot` returns distinct error types so the gateway
  can log them, but the gateway must collapse them to a single message for
  the client — otherwise we leak "battle exists / token valid / slot taken".

## Acceptance (end of all 4 commits)

Two browser tabs (or one browser + one MCP server) can claim slot 1 and slot 2
of the same battle and play it to completion. A second WS trying to claim a
claimed slot is rejected.

**Depends on:** none. **Required by:** [[pv-claude]].
