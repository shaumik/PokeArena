# Join-token security model

**Status:** landed (the parts that don't depend on disconnect-grace).
- ✓ 32-byte CSPRNG token, base64url, generated server-side at battle creation.
- ✓ Returned only to the creator over the create-battle response.
- ✓ First-claim-wins via atomic Lua `ClaimSlot` in `internal/cache/pvp.go`.
- ✓ Opaque error to client (all four failure modes collapse to one message;
  operator gets the precise reason via log).
- ✓ Tokens deleted on battle end via `DeletePvPTokens`.
- ☐ Disconnect-grace token reuse — pending [[disconnect-detection]]. Today the
  slot is released on WS exit so a reconnect *can* re-claim it, but there's no
  identity binding or grace window.

**Why:** Battle IDs end up in URLs, screenshots, logs, and chat apps. They cannot
be the capability that grants slot access. The join token is.

**Rules:**
- **32 bytes from CSPRNG**, base64url-encoded. One token per slot, generated
  server-side at battle creation.
- Returned **only** to the battle creator, over TLS. The creator distributes the
  P2 join URL out-of-band to their opponent.
- **First-claim-wins:** the first WebSocket to present a valid token claims the
  slot. Subsequent claim attempts on a claimed slot are rejected with a clean
  error (no information leak — same error whether the token is valid or not).
- **Disconnect grace:** during the 30s grace window after a WS drop, the same
  token can reconnect to its own slot. After grace, the token is dead and the
  battle is forfeited per [[disconnect-detection]].
- Tokens are never logged. Treated as passwords.
- Tokens expire when the battle ends (cleanup task).

**Explicitly out of scope:** rotating tokens, separate resume tokens, time-bound
expiry of unclaimed tokens, rate-limiting failed claim attempts. Revisit only if
abuse appears.

**Trade-off accepted:** if the battle creator pastes the P2 URL into a
compromised chat app or screenshares it, an attacker could race to claim the
slot before the intended opponent. We're not designing against this — it's a
"don't share your password in public" problem.

**Required by:** [[pv-claude]], [[gateway-second-slot]].
