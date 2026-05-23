# Gateway: claimable second trainer slot

**Status:** in progress — coordinator landed end-to-end; SPA wiring (commit 4) is the only thing between us and "two browser tabs play."

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
4. **SPA changes** — `live_pvp` mode button, two-URL display with copy button,
   slot-aware connect (default to p1; URL-bar token also accepted so the p2
   URL works in a second tab).

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
