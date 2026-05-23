# Disconnect detection

**Status:** not started

**Why:** If a trainer's WS drops mid-battle (Claude's MCP server crashes, browser
tab closes, network blip), the other player is stuck waiting for an opponent who
will never act. Some form of "are you still here?" is needed regardless of
Pv-Claude — the human-vs-local-AI mode also benefits.

**Explicitly NOT in scope:** per-turn shot clock. Pokémon has no pass action;
inventing one adds rules without earning them. A player taking 10 minutes to
think is fine; a player whose connection died is not.

**Policy:**
- Gateway watches each slot's WS for close events.
- On close: 30s grace where the same join token can reconnect to the same slot.
- After grace: battle is marked forfeited by the disconnected side; state is
  reaped from Redis; result is recorded in Postgres normally.

**Acceptance:** Killing the MCP server (or closing the browser tab) mid-battle
resolves to a clean forfeit within 30s; the remaining player sees a result
banner, not a frozen UI.

**Depends on:** [[gateway-second-slot]]. **Required by:** [[pv-claude]].
